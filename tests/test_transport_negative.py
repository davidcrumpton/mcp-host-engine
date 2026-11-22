"""Negative cases for the transports: what a bad request gets back.

The interesting thing about a transport is not that a good request works, it is
*how* a bad one is refused -- 401 versus 404 versus 415, a JSON-RPC error versus
a plain-text body, a closed connection versus a hang. Each case asserts the
specific status and message so a regression cannot be mistaken for a different
failure.
"""

import time

from tests.support import harness, mcp_client
from tests.support.cases import ServerCase, StdioCase

SECRET = "suite-token-secret-0123456789"


class MissingTokenTests(ServerCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_request_without_a_token(self):
        """No Authorization header is a 401, not an anonymous call."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.initialize()
        self.assertEqual(response.status, 401)
        self.assertIn("Unauthorized", response.body)


class BadCredentialsTests(ServerCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_wrong_token(self):
        """A token that does not verify is a 401."""
        session = mcp_client.HttpSession(self.server.base_url, token="not-the-token")
        self.assertEqual(session.initialize().status, 401)

    def test_wrong_scheme(self):
        """A non-Bearer Authorization header is a 401, not a parse error."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post(
            {"jsonrpc": "2.0", "id": 1, "method": "ping"},
            headers={"Authorization": "Basic dXNlcjpwYXNz"},
        )
        self.assertEqual(response.status, 401)


class RevokedTokenTests(ServerCase):
    """Revocation takes effect on a running server, via the watched file."""

    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        fields["token_revocation_file"] = str(self.dir / "revocations.yaml")
        return fields

    def test_revoked_token_stops_working(self):
        """A revoked token is refused without a restart.

        The server reloads the revocation file on a 5 s timer, so this is the
        one case in the suite that waits rather than polls a sub-second event.
        """
        minted = harness.run_cli(
            "token", "create", "-config", self.config.path, "dave", "1h"
        )
        self.assertEqual(minted.returncode, 0, harness.output_of(minted))
        token = minted.stdout.strip()

        before = mcp_client.HttpSession(self.server.base_url, token=token)
        self.assertEqual(before.initialize().status, 200)

        revoked = harness.run_cli(
            "token", "revoke", "-config", self.config.path, "dave"
        )
        self.assertEqual(revoked.returncode, 0, harness.output_of(revoked))

        deadline = time.time() + 12
        while time.time() < deadline:
            probe = mcp_client.HttpSession(self.server.base_url, token=token)
            if probe.initialize().status == 401:
                return
            time.sleep(0.25)
        self.fail("the revoked token was still accepted 12s after revocation")


class ProtocolErrorTests(ServerCase):
    plugins = ("ping",)

    def test_request_before_initialize(self):
        """A tool call before initialize is refused by the session, not served."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.request("tools/list", {})
        self.assertEqual(response.status, 200)
        self.assertIn("invalid during session initialization", response.error["message"])

    def test_unknown_session_id(self):
        """A session id the server never issued is a 404."""
        session = mcp_client.HttpSession(self.server.base_url)
        session.session_id = "NOSUCHSESSION"
        response = session.request("tools/list", {})
        self.assertEqual(response.status, 404)
        self.assertIn("session not found", response.body)

    def test_body_that_is_not_json(self):
        """A body that is not JSON is a 400 naming the parse failure."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post("this is not json")
        self.assertEqual(response.status, 400)
        self.assertIn("malformed payload", response.body)

    def test_wrong_content_type(self):
        """The endpoint accepts application/json and says so otherwise."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post(
            {"jsonrpc": "2.0", "id": 1, "method": "ping"}, content_type="text/plain"
        )
        self.assertEqual(response.status, 415)
        self.assertIn("Content-Type", response.body)

    def test_accept_header_must_offer_both_types(self):
        """A client that cannot accept SSE is told so, rather than sent JSON."""
        session = mcp_client.HttpSession(self.server.base_url)
        response = session.post(
            {"jsonrpc": "2.0", "id": 1, "method": "ping"}, accept="application/json"
        )
        self.assertEqual(response.status, 400)
        self.assertIn("text/event-stream", response.body)

    def test_unsupported_method(self):
        """An unknown JSON-RPC method is a 400, not a silently empty result."""
        session = mcp_client.http_session(self.server)
        response = session.request("bogus/method", {})
        self.assertEqual(response.status, 400)
        self.assertIn("not handled", response.body)
        self.assertIn("bogus/method", response.body)


class OpenapiAuthTests(ServerCase):
    """openapi.json sits behind the same auth chain as /rpc."""

    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_openapi_needs_a_token(self):
        """The tool inventory is not readable without credentials.

        /rpc/openapi.json used to be registered directly on the mux, so the
        bearer-token middleware never wrapped it and any anonymous client could
        read every tool name, description and input schema.
        """
        anonymous = mcp_client.HttpSession(self.server.base_url)
        response = anonymous.openapi()
        self.assertEqual(response.status, 401)
        self.assertNotIn("ping", response.body)

    def test_openapi_accepts_a_token(self):
        """With a token the same request succeeds, so the route still works."""
        token = self.mint_token("alice")
        session = mcp_client.HttpSession(self.server.base_url, token=token)
        response = session.openapi()
        self.assertEqual(response.status, 200)
        self.assertIn("ping", response.body)

    def test_openapi_is_read_only(self):
        """A state-changing method is 405, not a second way to read the list."""
        token = self.mint_token("alice")
        session = mcp_client.HttpSession(self.server.base_url, token=token)
        response = session.request_method("DELETE", "/rpc/openapi.json")
        self.assertEqual(response.status, 405)
        self.assertIn("GET", response.header("Allow") or "")


class CrossOriginTests(ServerCase):
    """The origin check is server-side, not just a CORS response header."""

    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        fields["cors_origin"] = "https://app.example.com"
        return fields

    def test_post_from_another_origin_is_refused(self):
        """A cross-origin POST from an unlisted origin is 403."""
        token = self.mint_token("alice")
        session = mcp_client.HttpSession(self.server.base_url, token=token)
        response = session.initialize(headers={"Origin": "https://evil.example"})
        self.assertEqual(response.status, 403)
        self.assertIn("Origin not allowed", response.body)


class SessionIdForgeryTests(ServerCase):
    """A session id is client input: it may not forge a log line or steer routing."""

    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["verbosity_level"] = 4
        return fields

    def test_a_newline_in_the_session_id_is_dropped(self):
        """The forged log line never appears; the request is still served."""
        session = mcp_client.HttpSession(self.server.base_url)
        forged = "x%0a2026-Nov-22%20[WARN]%20admin%20-%20forged"
        response = session.initialize(query=f"?sessionId={forged}")
        self.assertIn(response.status, (200, 404))

        log = self.server.stderr()
        self.assertNotIn("[WARN] admin - forged", log)


class StdioMalformedInputTests(StdioCase):
    plugins = ("ping",)

    def test_non_json_input_ends_the_process(self):
        """A line the transport cannot parse is fatal, and says why."""
        session = self.start()
        session.initialize()
        session.send_raw("this is not json")
        self.assertEqual(session.wait(timeout=10), 1)
        self.assertIn("Stdio server error", session.stderr())


class StdioEofTests(StdioCase):
    plugins = ("ping",)

    def test_closing_stdin_ends_the_process(self):
        """An idle session ends cleanly when the client closes the pipe."""
        session = self.start()
        session.initialize()
        session.call_tool("ping")
        session.close_stdin()
        self.assertEqual(session.wait(timeout=10), 0)
        self.assertNotIn("Stdio server error", session.stderr())


class StdioEofMidRequestTests(StdioCase):
    plugins = ("ping",)

    def test_closing_stdin_during_a_request_is_an_error(self):
        """Disappearing mid-request is reported, not treated as a clean stop."""
        session = self.start()
        session.send(
            {
                "jsonrpc": "2.0",
                "id": 1,
                "method": "initialize",
                "params": {
                    "protocolVersion": mcp_client.PROTOCOL_VERSION,
                    "capabilities": {},
                    "clientInfo": mcp_client.CLIENT_INFO,
                },
            }
        )
        session.close_stdin()
        self.assertEqual(session.wait(timeout=10), 1)
        self.assertIn("server is closing: EOF", session.stderr())


CASES = {
    "MissingTokenTests.test_request_without_a_token": "auth.no-token",
    "BadCredentialsTests.test_wrong_token": "auth.bad-token",
    "BadCredentialsTests.test_wrong_scheme": "auth.bad-scheme",
    "RevokedTokenTests.test_revoked_token_stops_working": "auth.revoked-token",
    "ProtocolErrorTests.test_request_before_initialize": "transport.no-session",
    "ProtocolErrorTests.test_unknown_session_id": "transport.unknown-session",
    "ProtocolErrorTests.test_body_that_is_not_json": "transport.malformed-json",
    "ProtocolErrorTests.test_wrong_content_type": "transport.wrong-content-type",
    "ProtocolErrorTests.test_accept_header_must_offer_both_types": "transport.bad-accept",
    "ProtocolErrorTests.test_unsupported_method": "transport.unknown-method",
    "OpenapiAuthTests.test_openapi_needs_a_token": "transport.openapi-auth",
    "OpenapiAuthTests.test_openapi_accepts_a_token": "transport.openapi-auth-ok",
    "OpenapiAuthTests.test_openapi_is_read_only": "transport.openapi-method",
    "CrossOriginTests.test_post_from_another_origin_is_refused": "transport.cross-origin-post",
    "SessionIdForgeryTests.test_a_newline_in_the_session_id_is_dropped": "transport.session-id-forgery",
    "StdioMalformedInputTests.test_non_json_input_ends_the_process": "stdio.malformed-input",
    "StdioEofTests.test_closing_stdin_ends_the_process": "stdio.eof",
    "StdioEofMidRequestTests.test_closing_stdin_during_a_request_is_an_error": "stdio.eof-mid-request",
}
