"""Opt-in live tests against a real, already-running mcphe server.

This is the only module that talks to something the suite did not start, so it
is excluded from the default run and from the inventory gate:

* it skips unless ``MCPHE_LIVE_BASE_URL`` is set,
* it skips again if the server does not answer,
* it creates nothing it would have to clean up.

    export MCPHE_LIVE_BASE_URL=http://127.0.0.1:8001
    export MCPHE_LIVE_TOKEN=<token>            # if the server authenticates
    export MCPHE_LIVE_TOOL=ping                # optional: a tool to call
    export MCPHE_LIVE_ARGS='{}'                # optional: its arguments
    python3 -m unittest tests.test_live -v

Use it to check a deployment -- a container image, a package install, a service
unit -- where the thing you want to prove is that *this* server, with *this*
config, serves the tools it should.
"""

import json
import os
import unittest
import urllib.error
import urllib.request

from tests.support import mcp_client

LIVE_BASE_URL = os.environ.get("MCPHE_LIVE_BASE_URL", "").rstrip("/")
LIVE_TOKEN = os.environ.get("MCPHE_LIVE_TOKEN", "")
LIVE_TOOL = os.environ.get("MCPHE_LIVE_TOOL", "")
LIVE_ARGS = os.environ.get("MCPHE_LIVE_ARGS", "{}")

SKIP_REASON = "live tests need a running server: set MCPHE_LIVE_BASE_URL (and MCPHE_LIVE_TOKEN if it authenticates)"


def setUpModule():
    """Skip the whole module unless a real server is configured and reachable."""
    if not LIVE_BASE_URL:
        raise unittest.SkipTest(SKIP_REASON)
    try:
        # Any answer at all -- including 401 -- proves something is listening.
        urllib.request.urlopen(f"{LIVE_BASE_URL}/rpc/openapi.json", timeout=5)
    except urllib.error.HTTPError:
        pass
    except Exception as exc:  # noqa: BLE001 - any transport failure means "skip"
        raise unittest.SkipTest(f"no mcphe server at {LIVE_BASE_URL}: {exc}")


def live_session(token=None):
    """An initialised session against the deployment under test."""
    session = mcp_client.HttpSession(LIVE_BASE_URL, token=token or LIVE_TOKEN or None)
    session.initialize()
    return session


@unittest.skipUnless(LIVE_BASE_URL, SKIP_REASON)
class LiveServerTests(unittest.TestCase):
    def test_server_answers_and_lists_tools(self):
        """The deployment speaks MCP and has at least one tool registered."""
        session = live_session()
        self.assertEqual(session.server_info["serverInfo"]["name"], "mcphe")
        tools = session.list_tools()
        self.assertTrue(tools, "the server lists no tools")
        for tool in tools:
            self.assertEqual(tool["inputSchema"]["type"], "object")

    def test_openapi_is_served(self):
        """The tool list is available as JSON as well as over MCP."""
        session = live_session()
        response = session.openapi()
        self.assertEqual(response.status, 200)
        self.assertIsInstance(json.loads(response.body), list)

    def test_configured_tool_can_be_called(self):
        """Call MCPHE_LIVE_TOOL, if the deployment names one."""
        if not LIVE_TOOL:
            self.skipTest("set MCPHE_LIVE_TOOL to call a specific tool")
        session = live_session()
        result = session.call_tool(LIVE_TOOL, json.loads(LIVE_ARGS))
        self.assertFalse(result.is_error, result.text)


@unittest.skipUnless(LIVE_BASE_URL and LIVE_TOKEN, SKIP_REASON)
class LiveAuthTests(unittest.TestCase):
    def test_bad_token_is_rejected(self):
        """A wrong token cannot reach the tool list."""
        session = live_session(token="definitely-not-the-token")
        self.assertIn(session.initialize().status, (401, 403))
