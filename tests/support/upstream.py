"""A local stand-in for a remote host, so the HTTP cases need no network.

``host.http.get`` and friends take a URL and enforce an allow-list of domains.
Proving that a *permitted* request really goes out -- and that the body, status
and headers come back intact -- needs something to answer it. Reaching for
example.com would make the suite depend on the internet, on DNS and on a third
party's uptime, so this serves the same purpose on 127.0.0.1.

The allow-list is compared against the URL's hostname, so a config listing
``127.0.0.1`` is exactly as restricted as one listing ``example.com``; the
domain rules themselves are unchanged.
"""

import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HTML_PAGE = (
    "<html><head><title>Stub Page</title></head>"
    "<body><h1>stub page</h1><p>served by the test suite</p></body></html>"
)


class Upstream:
    """A threaded HTTP server that answers every request and records it."""

    def __init__(self):
        self.requests = []
        self._server = None
        self._thread = None
        self._lock = threading.Lock()

    # -- lifecycle ---------------------------------------------------------

    def start(self):
        upstream = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args):
                pass

            def _handle(self, method):
                length = int(self.headers.get("Content-Length") or 0)
                body = self.rfile.read(length).decode() if length else ""
                with upstream._lock:
                    upstream.requests.append(
                        {
                            "method": method,
                            "path": self.path,
                            "headers": dict(self.headers),
                            "body": body,
                        }
                    )
                status, content_type, payload = upstream._route(method, self.path, body)
                data = payload.encode()
                self.send_response(status)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                if data:
                    self.wfile.write(data)

            def do_GET(self):
                self._handle("GET")

            def do_POST(self):
                self._handle("POST")

            def do_PUT(self):
                self._handle("PUT")

            def do_PATCH(self):
                self._handle("PATCH")

            def do_DELETE(self):
                self._handle("DELETE")

            def do_HEAD(self):
                self._handle("HEAD")

        self._server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self._server.daemon_threads = True
        self._thread = threading.Thread(
            target=self._server.serve_forever,
            kwargs={"poll_interval": 0.02},
            daemon=True,
        )
        self._thread.start()
        return self

    def stop(self):
        if self._server is not None:
            self._server.shutdown()
            self._server.server_close()
            self._server = None
        if self._thread is not None:
            self._thread.join(timeout=5)
            self._thread = None

    def __enter__(self):
        return self.start()

    def __exit__(self, *exc):
        self.stop()
        return False

    # -- accessors ---------------------------------------------------------

    @property
    def port(self):
        return self._server.server_address[1]

    @property
    def base_url(self):
        return f"http://127.0.0.1:{self.port}"

    @property
    def host(self):
        return "127.0.0.1"

    def url(self, path="/"):
        return f"{self.base_url}{path}"

    def last_request(self, method=None):
        for request in reversed(self.requests):
            if method is None or request["method"] == method.upper():
                return request
        return None

    # -- routing -----------------------------------------------------------

    def _route(self, method, path, body):
        if path.startswith("/status/"):
            try:
                code = int(path.rsplit("/", 1)[1])
            except ValueError:
                code = 400
            return code, "text/plain", f"status {code}\n"
        if path.startswith("/json"):
            return 200, "application/json", json.dumps({"ok": True, "path": path})
        if path.startswith("/echo"):
            return 200, "application/json", json.dumps(
                {"method": method, "path": path, "body": body}
            )
        if path.startswith("/delay/"):
            try:
                seconds = float(path.rsplit("/", 1)[1])
            except ValueError:
                seconds = 0.0
            time.sleep(min(seconds, 10))
            return 200, "text/plain", "delayed\n"
        if path.startswith("/slow"):
            return 200, "text/plain", "slow\n"
        return 200, "text/html", HTML_PAGE
