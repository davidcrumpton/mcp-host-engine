"""Base classes the test modules build on.

Two shapes cover every case in the suite:

* ``ServerCase`` -- a throwaway plugin directory, a config, and the real binary
  running as a child process, ready to be talked to over HTTP;
* ``StartupCase`` -- a config that must be *refused*, so there is nothing to
  talk to: only an exit status and a message to assert on.

Neither asserts anything about behaviour; they exist so the test modules read as
statements about mcphe rather than statements about subprocesses.
"""

import unittest

from tests.support import fixtures, harness, mcp_client
from tests.support.mcp_client import ToolResult


class ConfigCase(unittest.TestCase):
    """A temporary directory with a plugin directory and a config file in it."""

    # Fixture names from support.fixtures to install.
    plugins = ()
    # Fixtures to give a plugins: entry to. Defaults to all of them; a fixture
    # that is only a library module (a require() target, not a tool) is
    # installed but not enabled.
    enabled = None
    # tool name -> extra plugin options, merged into the generated plugins: map.
    plugin_options = {}
    # Extra top-level config fields.
    config_fields = {}

    def setUp(self):
        self.dir = harness.temp_dir(self)
        self.plugin_dir = harness.install_plugins(self.dir, *self.plugins)
        self.prepare()
        self.config = harness.write_config(self.dir, **self.config_for())

    def prepare(self):
        """Hook for files that must exist before the config is written."""

    def config_for(self):
        fields = dict(self.config_fields)
        if "plugins" not in fields:
            fields["plugins"] = self.plugins_map()
        return fields

    def plugins_map(self):
        """Build the plugins: mapping for the installed fixtures."""
        block = {}
        for name in self.plugins if self.enabled is None else self.enabled:
            tool = fixtures.tool_name(name)
            block[tool] = dict(self.plugin_options.get(tool, {}))
        block.update(self.extra_plugins())
        return block

    def extra_plugins(self):
        """Plugin entries that are not derived from an installed fixture."""
        return {}

    def mint_token(self, username, duration="1h"):
        """Mint a token against this config and return it."""
        minted = harness.run_cli(
            "token", "create", "-config", self.config.path, username, duration
        )
        self.assertEqual(minted.returncode, 0, harness.output_of(minted))
        return minted.stdout.strip()


class StartupCase(ConfigCase):
    """A config that is expected to be refused, plus a way to run it."""

    def run_startup(self, *args, **kwargs):
        """Run the binary against this config and wait for it to exit."""
        return harness.run_server(self.config, *args, **kwargs)


class ServerCase(ConfigCase):
    """The real binary, serving HTTP, for one test."""

    # Set False when the test drives startup itself (a bad config, a second
    # server, an env var that must be set before the process starts).
    autostart = True
    # Extra environment for the server process.
    server_env = {}

    def setUp(self):
        super().setUp()
        self.server = harness.Server(self.config, env=self.server_env)
        self.addCleanup(self.server.stop)
        self.session = None
        if self.autostart:
            self.start()

    # -- sessions ----------------------------------------------------------

    def start(self, token=None, **kwargs):
        """Start the server if it is not running and return a live session."""
        if not self.server.started:
            self.server.start()
        self.session = mcp_client.http_session(self.server, token=token, **kwargs)
        return self.session

    def new_session(self, token=None):
        """A second, independent session against the same server."""
        return mcp_client.http_session(self.server, token=token)

    # -- assertions --------------------------------------------------------

    def call(self, name, arguments=None):
        return self.session.call_tool(name, arguments)

    def text_of(self, name, arguments=None):
        """Call a tool, require success, and return its text content."""
        result = self.call(name, arguments)
        self.assertFalse(
            result.is_error,
            f"expected {name} to succeed, got an error: {result.text}",
        )
        return result.text

    def assertToolError(self, result, *fragments):
        """Assert a tool call failed, mentioning each fragment in its message."""
        self.assertIsInstance(result, ToolResult)
        self.assertTrue(result.is_error, f"expected isError true, got: {result!r}")
        for fragment in fragments:
            self.assertIn(fragment, result.text)

    def tool_names(self):
        return self.session.tool_names()

    def tool(self, name):
        """Return the tools/list entry for name."""
        for entry in self.session.list_tools():
            if entry["name"] == name:
                return entry
        raise AssertionError(f"tool {name!r} is not registered")


class StdioCase(ConfigCase):
    """A config served over stdio instead of HTTP."""

    autostart = False

    def config_for(self):
        fields = super().config_for()
        fields["transport"] = "stdio"
        fields["port"] = None
        return fields

    def setUp(self):
        super().setUp()
        self.session = None

    def start(self):
        self.session = mcp_client.StdioSession(self.config.path)
        self.session.start()
        self.addCleanup(self.session.stop)
        return self.session
