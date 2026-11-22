"""A minimal MCP client, in the two transports mcphe serves.

There is no MCP SDK here on purpose. The suite is testing the wire, so the
client is deliberately dumb: it builds the JSON-RPC envelope, posts it, and
parses whatever comes back. That makes the awkward cases expressible -- a
missing ``Accept`` header, an unknown session id, a body that is not JSON --
which a SDK would either hide or refuse to send.

Two things about the real server that shape this code:

* responses come back as Server-Sent Events (``text/event-stream``) even when
  the request accepts JSON, so every response body is parsed as SSE first;
* HTTP-level failures (401, 404, 415, ...) carry a plain-text message rather
  than a JSON-RPC error, so those statuses are returned to the caller instead
  of being raised.
"""

import json
import queue
import subprocess
import threading
import urllib.error
import urllib.request

PROTOCOL_VERSION = "2025-03-26"
DEFAULT_ACCEPT = "application/json, text/event-stream"
CLIENT_INFO = {"name": "mcphe-tests", "version": "1.0"}


# ---------------------------------------------------------------------------
# Responses
# ---------------------------------------------------------------------------


def parse_body(content_type, body):
    """Return the JSON-RPC message in body, whether it is SSE or plain JSON."""
    text = body.decode() if isinstance(body, bytes) else (body or "")
    if "text/event-stream" in (content_type or ""):
        payloads = []
        for line in text.splitlines():
            if line.startswith("data:"):
                payloads.append(line[len("data:"):].lstrip())
        # A single response may arrive as one event; several would be a stream.
        for payload in reversed(payloads):
            try:
                return json.loads(payload)
            except ValueError:
                continue
        return None
    if not text.strip():
        return None
    try:
        return json.loads(text)
    except ValueError:
        return None


class Response:
    """One HTTP response: status, headers, body, and the JSON-RPC message in it."""

    def __init__(self, status, headers, body):
        self.status = status
        self.headers = dict(headers or {})
        self.body = body.decode() if isinstance(body, bytes) else (body or "")
        self.rpc = parse_body(self.header("Content-Type"), self.body)

    def header(self, name):
        for key, value in self.headers.items():
            if key.lower() == name.lower():
                return value
        return None

    @property
    def result(self):
        return (self.rpc or {}).get("result")

    @property
    def error(self):
        return (self.rpc or {}).get("error")

    def __repr__(self):
        return f"<Response {self.status} {self.body[:120]!r}>"


class ToolResult:
    """The result of one tools/call, in the shape a client cares about."""

    def __init__(self, rpc, response=None):
        self.rpc = rpc or {}
        self.response = response

    @property
    def result(self):
        return self.rpc.get("result") or {}

    @property
    def error(self):
        return self.rpc.get("error")

    @property
    def is_error(self):
        return bool(self.result.get("isError"))

    @property
    def texts(self):
        return [
            item.get("text", "")
            for item in self.result.get("content") or []
            if item.get("type", "text") == "text"
        ]

    @property
    def text(self):
        texts = self.texts
        return texts[0] if texts else ""

    def json(self):
        """Parse the text content as JSON, which is how object results arrive."""
        return json.loads(self.text)

    def __repr__(self):
        return f"<ToolResult error={self.is_error} text={self.text[:120]!r}>"


# ---------------------------------------------------------------------------
# HTTP transport
# ---------------------------------------------------------------------------


class HttpSession:
    """A JSON-RPC session against a running server's ``/rpc`` endpoint."""

    def __init__(self, base_url, token=None, timeout=30):
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout
        self.session_id = None
        self.server_info = {}
        self._id = 0

    @property
    def rpc_url(self):
        return f"{self.base_url}/rpc"

    def next_id(self):
        self._id += 1
        return self._id

    # -- plumbing ----------------------------------------------------------

    def post(self, payload, *, headers=None, accept=DEFAULT_ACCEPT,
             content_type="application/json", session=True, path=None, query=""):
        """POST a payload to /rpc and return the Response, whatever its status."""
        if isinstance(payload, (dict, list)):
            body = json.dumps(payload).encode()
        elif isinstance(payload, str):
            body = payload.encode()
        else:
            body = payload

        request_headers = {"Content-Type": content_type}
        if accept is not None:
            request_headers["Accept"] = accept
        if self.token is not None:
            request_headers["Authorization"] = f"Bearer {self.token}"
        if session and self.session_id:
            request_headers["Mcp-Session-Id"] = self.session_id
        request_headers.update(headers or {})

        url = (self.rpc_url if path is None else f"{self.base_url}{path}") + query
        request = urllib.request.Request(
            url, data=body, headers=request_headers, method="POST"
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return Response(response.status, response.headers, response.read())
        except urllib.error.HTTPError as error:
            return Response(error.code, error.headers, error.read())

    def get(self, path, *, accept=None):
        headers = {}
        if self.token is not None:
            headers["Authorization"] = f"Bearer {self.token}"
        if accept:
            headers["Accept"] = accept
        request = urllib.request.Request(
            f"{self.base_url}{path}", headers=headers, method="GET"
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return Response(response.status, response.headers, response.read())
        except urllib.error.HTTPError as error:
            return Response(error.code, error.headers, error.read())

    def options(self, path="/rpc", *, headers=None):
        request = urllib.request.Request(
            f"{self.base_url}{path}", headers=headers or {}, method="OPTIONS"
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return Response(response.status, response.headers, response.read())
        except urllib.error.HTTPError as error:
            return Response(error.code, error.headers, error.read())

    def request(self, method, params=None, request_id=None, *, headers=None, query=""):
        payload = {
            "jsonrpc": "2.0",
            "id": self.next_id() if request_id is None else request_id,
            "method": method,
        }
        if params is not None:
            payload["params"] = params
        return self.post(payload, headers=headers, query=query)

    def request_method(self, method, path, *, headers=None):
        """Send an arbitrary HTTP method to path and return the Response."""
        request_headers = dict(headers or {})
        if self.token is not None:
            request_headers["Authorization"] = f"Bearer {self.token}"
        request = urllib.request.Request(
            f"{self.base_url}{path}", headers=request_headers, method=method
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return Response(response.status, response.headers, response.read())
        except urllib.error.HTTPError as error:
            return Response(error.code, error.headers, error.read())

    def notify(self, method, params=None):
        payload = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            payload["params"] = params
        return self.post(payload)

    # -- MCP ---------------------------------------------------------------

    def initialize(self, protocol_version=PROTOCOL_VERSION, *, headers=None, query=""):
        response = self.request(
            "initialize",
            {
                "protocolVersion": protocol_version,
                "capabilities": {},
                "clientInfo": CLIENT_INFO,
            },
            headers=headers,
            query=query,
        )
        session_id = response.header("Mcp-Session-Id")
        if session_id:
            self.session_id = session_id
        self.server_info = response.result or {}
        if response.status == 200 and response.rpc is not None:
            self.notify("notifications/initialized")
        return response

    def list_tools(self):
        response = self.request("tools/list", {})
        if response.result is None:
            raise AssertionError(
                f"tools/list failed: status {response.status} {response.body[:400]}"
            )
        return response.result.get("tools") or []

    def tool_names(self):
        return [tool["name"] for tool in self.list_tools()]

    def call_tool(self, name, arguments=None):
        params = {"name": name}
        if arguments is not None:
            params["arguments"] = arguments
        response = self.request("tools/call", params)
        return ToolResult(response.rpc, response)

    def openapi(self):
        return self.get("/rpc/openapi.json")


def http_session(server, token=None, *, initialize=True):
    """Return an initialised session against ``server``."""
    session = HttpSession(server.base_url, token=token)
    if initialize:
        session.initialize()
    return session


# ---------------------------------------------------------------------------
# stdio transport
# ---------------------------------------------------------------------------

_EOF = object()


class StdioSession:
    """A JSON-RPC session over the process's stdin/stdout, one message per line."""

    def __init__(self, config_path, *, cwd=None, env=None, timeout=30):
        self.config_path = str(config_path)
        self.cwd = None if cwd is None else str(cwd)
        self.env = env or {}
        self.timeout = timeout
        self._process = None
        self._messages = queue.Queue()
        self._reader = None
        self._id = 0
        self._stderr_lines = []

    def start(self):
        from tests.support.harness import binary, clean_env

        self._process = subprocess.Popen(
            [str(binary()), self.config_path],
            cwd=self.cwd,
            env=clean_env(**self.env),
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
        )
        self._reader = threading.Thread(target=self._read_loop, daemon=True)
        self._reader.start()
        self._stderr_reader = threading.Thread(target=self._stderr_loop, daemon=True)
        self._stderr_reader.start()
        return self

    def _read_loop(self):
        for line in self._process.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                self._messages.put(json.loads(line))
            except ValueError:
                self._messages.put(line)
        self._messages.put(_EOF)

    def _stderr_loop(self):
        for line in self._process.stderr:
            self._stderr_lines.append(line)

    # -- plumbing ----------------------------------------------------------

    def send(self, message):
        self._process.stdin.write(json.dumps(message) + "\n")
        self._process.stdin.flush()

    def send_raw(self, text):
        self._process.stdin.write(text if text.endswith("\n") else text + "\n")
        self._process.stdin.flush()

    def read(self, timeout=None):
        try:
            message = self._messages.get(timeout=timeout or self.timeout)
        except queue.Empty:
            raise AssertionError(
                f"no stdio response within {timeout or self.timeout}s\n{self.stderr()}"
            )
        if message is _EOF:
            raise AssertionError(
                "the server closed stdout before answering\n" + self.stderr()
            )
        return message

    def request(self, method, params=None):
        self._id += 1
        payload = {"jsonrpc": "2.0", "id": self._id, "method": method}
        if params is not None:
            payload["params"] = params
        self.send(payload)
        while True:
            message = self.read()
            if isinstance(message, dict) and message.get("id") == self._id:
                return message
            # Anything else is a notification or server-initiated request; this
            # suite never sends any, so skip and keep waiting.

    def notify(self, method, params=None):
        payload = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            payload["params"] = params
        self.send(payload)

    # -- MCP ---------------------------------------------------------------

    def initialize(self):
        envelope = self.request(
            "initialize",
            {
                "protocolVersion": PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": CLIENT_INFO,
            },
        )
        self.notify("notifications/initialized")
        return envelope

    def list_tools(self):
        envelope = self.request("tools/list", {})
        return ((envelope.get("result") or {}).get("tools")) or []

    def call_tool(self, name, arguments=None):
        params = {"name": name}
        if arguments is not None:
            params["arguments"] = arguments
        return ToolResult(self.request("tools/call", params))

    # -- lifecycle ---------------------------------------------------------

    @property
    def returncode(self):
        return None if self._process is None else self._process.poll()

    def stderr(self):
        return "".join(self._stderr_lines)

    def close_stdin(self):
        try:
            self._process.stdin.close()
        except (BrokenPipeError, ValueError):
            pass

    def wait(self, timeout=15):
        return self._process.wait(timeout=timeout)

    def stop(self):
        if self._process is None:
            return
        if self._process.poll() is None:
            self.close_stdin()
            try:
                self._process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self._process.kill()
                self._process.wait(timeout=10)
        # The reader threads have hit EOF by now, so the pipes can be released
        # without a ResourceWarning.
        for stream in (self._process.stdin, self._process.stdout, self._process.stderr):
            try:
                stream.close()
            except (BrokenPipeError, ValueError, OSError):
                pass
        self._process = None
