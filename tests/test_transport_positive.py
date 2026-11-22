"""Positive cases for the two transports and for authentication.

The HTTP transport is the MCP streamable-HTTP one: JSON-RPC posted to /rpc, and
answered as Server-Sent Events. The stdio transport is the same protocol over a
pipe. Both are exercised through the real binary, so what is asserted here is
what a client actually sees on the wire.
"""

import json
import time

from tests.support import harness, mcp_client
from tests.support.cases import ServerCase, StdioCase

SECRET = "suite-token-secret-0123456789"


def wait_for(predicate, timeout=5.0, interval=0.05):
    """Poll predicate until it is true, or the timeout expires."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if predicate():
            return True
        time.sleep(interval)
    return False


class SseFramingTests(ServerCase):
    plugins = ("ping",)

    def test_response_is_an_sse_message_frame(self):
        """Responses arrive as SSE, not as a bare JSON body."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.request(
            "initialize",
            {
                "protocolVersion": mcp_client.PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": mcp_client.CLIENT_INFO,
            },
        )
        self.assertEqual(response.status, 200)
        self.assertIn("text/event-stream", response.header("Content-Type"))
        self.assertTrue(response.body.startswith("event: message"))
        data_line = next(
            line for line in response.body.splitlines() if line.startswith("data:")
        )
        self.assertEqual(json.loads(data_line[len("data:"):])["id"], 1)


class SessionTests(ServerCase):
    plugins = ("ping",)

    def test_session_id_is_issued_and_required_for_follow_up(self):
        """initialize hands out a session id, and later calls carry it."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.initialize()
        self.assertEqual(response.status, 200)
        self.assertTrue(session.session_id)

        follow_up = session.request("tools/list", {})
        self.assertEqual(follow_up.status, 200)
        self.assertEqual(
            [tool["name"] for tool in follow_up.result["tools"]], ["ping"]
        )


class TokenSecretAuthTests(ServerCase):
    plugins = ("ping", "echo")

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def mint(self, *args):
        completed = harness.run_cli(
            "token", "create", "-config", self.config.path, *args
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        return completed.stdout.strip()

    def test_minted_token_is_accepted(self):
        """A token signed with token_secret authenticates."""
        session = self.new_session(token=self.mint("alice", "1h"))
        self.assertEqual(session.tool_names(), ["echo", "ping"])

    def test_identity_is_the_token_username(self):
        """host.identity is the username the token was minted for."""
        session = self.new_session(token=self.mint("alice", "1h"))
        payload = json.loads(session.call_tool("echo").text)
        self.assertEqual(payload["identity"], "alice")


class LegacyBearerTokenTests(ServerCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["bearer_token"] = "legacy-shared-token"
        return fields

    def test_legacy_bearer_token_is_accepted(self):
        """The deprecated bearer_token still authenticates a client."""
        session = self.new_session(token="legacy-shared-token")
        self.assertEqual(session.tool_names(), ["ping"])


class OpenAuthTests(ServerCase):
    plugins = ("ping",)

    def test_no_token_configured_means_no_token_required(self):
        """With neither secret nor bearer token, /rpc is open (loopback default)."""
        session = mcp_client.HttpSession(self.server.base_url)
        self.assertEqual(session.initialize().status, 200)
        self.assertEqual(session.tool_names(), ["ping"])


class CorsOriginTests(ServerCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["cors_origin"] = "https://app.example.com"
        return fields

    def test_configured_origin_is_returned(self):
        """The configured origin is sent whatever Origin the request claims."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post(
            {"jsonrpc": "2.0", "id": 1, "method": "ping"},
            headers={"Origin": "https://somewhere-else.example"},
        )
        self.assertEqual(
            response.header("Access-Control-Allow-Origin"),
            "https://app.example.com",
        )


class PreflightTests(ServerCase):
    """A browser's preflight is answered, because CORS wraps the auth middleware."""

    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = "suite-token-secret-0123456789"
        fields["cors_origin"] = "https://app.example.com"
        return fields

    def test_preflight_needs_no_token(self):
        """OPTIONS without an Authorization header is 200 with the CORS headers.

        A preflight never carries credentials -- the browser does not send them
        cross-origin -- so when auth wrapped CORS every browser client was
        stopped before it could ask permission.
        """
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.options(
            headers={
                "Origin": "https://app.example.com",
                "Access-Control-Request-Method": "POST",
            }
        )
        self.assertEqual(response.status, 200)
        self.assertEqual(
            response.header("Access-Control-Allow-Origin"), "https://app.example.com"
        )
        self.assertIn("POST", response.header("Access-Control-Allow-Methods") or "")


class CorsDisabledTests(ServerCase):
    plugins = ("ping",)

    def test_empty_cors_origin_omits_the_header(self):
        """An empty cors_origin means no Access-Control-Allow-Origin at all."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post(
            {"jsonrpc": "2.0", "id": 1, "method": "ping"},
            headers={"Origin": "https://app.example.com"},
        )
        self.assertIsNone(response.header("Access-Control-Allow-Origin"))


class StdioCallTests(StdioCase):
    plugins = ("ping", "echo")

    def test_tools_over_stdio(self):
        """initialize, tools/list and tools/call all work over a pipe."""
        session = self.start()
        server_info = session.initialize()["result"]["serverInfo"]
        self.assertEqual(server_info["name"], "mcphe")
        self.assertEqual(
            [tool["name"] for tool in session.list_tools()], ["echo", "ping"]
        )
        self.assertEqual(session.call_tool("ping").text, "pong")


class StdioNoAuthTests(StdioCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_stdio_does_not_authenticate(self):
        """token_secret is an HTTP concern; stdio needs no token."""
        session = self.start()
        session.initialize()
        self.assertEqual(session.call_tool("ping").text, "pong")


class StdioStreamTests(StdioCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["verbosity_level"] = 1
        return fields

    def test_protocol_on_stdout_logs_on_stderr(self):
        """stdout carries only JSON-RPC; log lines go to stderr."""
        session = self.start()
        session.initialize()
        session.call_tool("ping")
        self.assertTrue(
            wait_for(lambda: "stdio" in session.stderr()),
            f"expected a log line on stderr, got: {session.stderr()!r}",
        )


CASES = {
    "SseFramingTests.test_response_is_an_sse_message_frame": "transport.sse-framing",
    "SessionTests.test_session_id_is_issued_and_required_for_follow_up": "transport.session-id",
    "TokenSecretAuthTests.test_minted_token_is_accepted": "auth.token-secret",
    "TokenSecretAuthTests.test_identity_is_the_token_username": "auth.identity",
    "LegacyBearerTokenTests.test_legacy_bearer_token_is_accepted": "auth.legacy-bearer",
    "OpenAuthTests.test_no_token_configured_means_no_token_required": "auth.open",
    "CorsOriginTests.test_configured_origin_is_returned": "cors.origin",
    "CorsDisabledTests.test_empty_cors_origin_omits_the_header": "cors.disabled",
    "PreflightTests.test_preflight_needs_no_token": "transport.preflight-allowed",
    "StdioCallTests.test_tools_over_stdio": "stdio.call",
    "StdioNoAuthTests.test_stdio_does_not_authenticate": "stdio.no-auth",
    "StdioStreamTests.test_protocol_on_stdout_logs_on_stderr": "stdio.stdout-protocol",
}
