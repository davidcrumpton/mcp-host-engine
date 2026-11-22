"""Positive cases: the documented behaviour of the server happens.

Every test here starts the real binary against a config and a plugin directory
built for it, then talks to it over HTTP. What is under test is the request the
client builds, the response the server produces, and the side effect on disk --
not a mocked return value.
"""

import json
import unittest

from tests.support import harness, mcp_client
from tests.support.cases import ServerCase
from tests.support.upstream import Upstream


class StartupTests(ServerCase):
    plugins = ("ping",)

    def test_valid_config_serves(self):
        """A valid config starts a server that answers MCP on /rpc."""
        self.assertEqual(self.session.server_info["serverInfo"]["name"], "mcphe")
        self.assertIn("protocolVersion", self.session.server_info)
        self.assertEqual(self.tool_names(), ["ping"])
        self.assertEqual(self.text_of("ping"), "pong")


class PidFileStartupTests(ServerCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["pid_file"] = str(self.dir / "mcphe.pid")
        return fields

    def test_pid_file_is_written(self):
        """pid_file names the process that is actually running."""
        pid_file = self.dir / "mcphe.pid"
        self.assertEqual(pid_file.read_text().strip(), str(self.server.pid))


class EnvResolutionTests(ServerCase):
    """A token_secret that comes from the environment, not the file."""

    plugins = ("ping",)
    autostart = False
    server_env = {"MCPHE_TEST_SECRET": "suite-secret"}

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = "env://MCPHE_TEST_SECRET"
        return fields

    def test_token_secret_is_read_from_the_environment(self):
        """env:// resolves at load, and the resolved secret is enforced."""
        self.server.start()
        anonymous = mcp_client.HttpSession(self.server.base_url)
        self.assertEqual(anonymous.initialize().status, 401)

        minted = harness.run_cli(
            "token", "create", "-config", self.config.path, "alice", "1h",
            env=self.server_env,
        )
        self.assertEqual(minted.returncode, 0, harness.output_of(minted))
        session = self.new_session(token=minted.stdout.strip())
        self.assertEqual(session.tool_names(), ["ping"])


class FileResolutionTests(ServerCase):
    """A plugin option that comes from a file, not the config."""

    plugins = ("counter",)

    def config_for(self):
        tag_file = self.dir / "tag.txt"
        tag_file.write_text("tag-from-file")
        fields = super().config_for()
        fields["plugins"] = {"counter": {"tag": f"file://{tag_file}"}}
        return fields

    def test_plugin_option_is_read_from_a_file(self):
        """file:// resolves to the file's contents before the plugin sees it."""
        self.assertIn("tag-from-file", self.text_of("counter"))


class DefaultsTests(unittest.TestCase):
    def test_digest_falls_back_to_built_in_defaults(self):
        """With no config file, the built-in defaults are used: plugin_dir=plugins."""
        directory = harness.temp_dir(self)
        harness.install_plugins(directory, "counter")
        completed = harness.run_cli("digest", "-format", "text", cwd=directory)
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn("counter.js", completed.stdout)


class DiscoveryTests(ServerCase):
    plugins = ("ping", "echo", "no-schema")

    def test_tools_list(self):
        """Every enabled plugin is advertised with a name, description and schema."""
        tools = self.session.list_tools()
        self.assertEqual([tool["name"] for tool in tools], ["bare", "echo", "ping"])
        for tool in tools:
            self.assertTrue(tool["description"])
            self.assertEqual(tool["inputSchema"]["type"], "object")

    def test_declared_schema_is_passed_through(self):
        """A plugin's own inputSchema reaches the client unchanged."""
        schema = self.tool("echo")["inputSchema"]
        self.assertEqual(schema["properties"]["text"]["type"], "string")
        self.assertEqual(schema["required"], [])

    def test_plugin_without_schema_gets_an_object_schema(self):
        """A missing inputSchema is normalised to {"type": "object"}."""
        self.assertEqual(self.tool("bare")["inputSchema"], {"type": "object"})

    def test_annotation_hints(self):
        """Declared hints appear; an undeclared destructive hint defaults to false."""
        ping = self.tool("ping")["annotations"]
        self.assertTrue(ping["readOnlyHint"])
        self.assertTrue(ping["idempotentHint"])
        self.assertFalse(ping["openWorldHint"])
        self.assertFalse(ping["destructiveHint"])

        bare = self.tool("bare")["annotations"]
        self.assertFalse(bare["destructiveHint"])
        self.assertNotIn("readOnlyHint", bare)

    def test_openapi_lists_plugin_metadata(self):
        """openapi.json carries the metadata tools/list has no room for."""
        response = self.session.openapi()
        self.assertEqual(response.status, 200)
        entries = json.loads(response.body)
        ping = next(entry for entry in entries if entry["name"] == "ping")
        self.assertEqual(ping["version"], "1.1.0")
        self.assertEqual(ping["commit"], "none")
        self.assertEqual(ping["tags"], ["utility"])
        self.assertIsNone(ping["_meta"])


class AliasTests(ServerCase):
    plugins = ("counter",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "counter_a": {"plugin": "counter", "tag": "A"},
            "counter_b": {"plugin": "counter", "tag": "B"},
        }
        return fields

    def test_one_module_two_independent_tools(self):
        """Aliases share a file but not state or options."""
        self.assertEqual(self.tool_names(), ["counter_a", "counter_b"])
        self.assertIn("calls=1 tag=A", self.text_of("counter_a"))
        self.assertIn("calls=2 tag=A", self.text_of("counter_a"))
        self.assertIn("calls=1 tag=B", self.text_of("counter_b"))


class LegacyToolsMapTests(ServerCase):
    plugins = ("counter",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {}
        fields["tools"] = {"counter": True}
        return fields

    def test_legacy_tools_map_still_registers_a_plugin(self):
        """The deprecated top-level tools map is still honoured."""
        self.assertEqual(self.tool_names(), ["counter"])
        self.assertTrue(self.text_of("counter").startswith("calls=1"))


class ResultShapeTests(ServerCase):
    plugins = ("ping", "echo")

    def test_string_result_is_text_content(self):
        """A string result arrives as one text content block."""
        result = self.call("ping")
        self.assertFalse(result.is_error)
        self.assertEqual(result.text, "pong")
        self.assertEqual(result.result["content"][0]["type"], "text")

    def test_object_result_is_json_text(self):
        """An object result arrives as JSON text, with & < > left alone."""
        result = self.call("echo", {"text": "a && b <c>"})
        self.assertFalse(result.is_error)
        payload = json.loads(result.text)
        self.assertEqual(payload["args"]["text"], "a && b <c>")
        self.assertNotIn("\\u0026", result.text)
        self.assertNotIn("\\u003c", result.text)

    def test_arguments_reach_the_plugin(self):
        """Call arguments arrive as given, and an argument-less call gets {}."""
        self.assertEqual(
            json.loads(self.text_of("echo", {"text": "hello"}))["args"],
            {"text": "hello"},
        )
        self.assertEqual(json.loads(self.text_of("echo"))["args"], {})


class FileSandboxTests(ServerCase):
    plugins = ("reader", "writer")

    def config_for(self):
        self.allowed = self.dir / "allowed"
        self.allowed.mkdir()
        self.outside = self.dir / "outside"
        self.outside.mkdir()
        fields = super().config_for()
        fields["plugins"] = {
            "reader": {"allowed_read_file_paths": [str(self.allowed)]},
            "writer": {"allowed_write_file_paths": [str(self.allowed)]},
        }
        return fields

    def test_read_inside_the_allowed_path(self):
        """A file under allowed_read_file_paths is readable."""
        target = self.allowed / "note.txt"
        target.write_text("inside the sandbox\n")
        self.assertEqual(
            self.text_of("reader", {"path": str(target)}), "inside the sandbox\n"
        )

    def test_write_inside_the_allowed_path(self):
        """A file under allowed_write_file_paths is written, and lands on disk."""
        target = self.allowed / "written.txt"
        self.assertIn(
            "wrote", self.text_of("writer", {"path": str(target), "content": "hello"})
        )
        self.assertEqual(target.read_text(), "hello")


class EnvSandboxTests(ServerCase):
    plugins = ("enver",)
    server_env = {"MCPHE_TEST_VAR": "visible-value"}

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"enver": {"allowed_env_vars": ["MCPHE_TEST_VAR"]}}
        return fields

    def test_allowed_variable_is_readable(self):
        """A variable in allowed_env_vars is returned."""
        self.assertEqual(
            self.text_of("enver", {"name": "MCPHE_TEST_VAR"}), "visible-value"
        )


class ExecSandboxTests(ServerCase):
    plugins = ("runner",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"runner": {"allowed_commands": ["echo"]}}
        return fields

    def test_allowed_command_runs(self):
        """A command in allowed_commands runs and its output is returned."""
        self.assertEqual(
            self.text_of("runner", {"command": "echo hello-from-exec"}),
            "hello-from-exec\n",
        )


class HttpSandboxTests(ServerCase):
    plugins = ("fetcher", "requester")

    def setUp(self):
        self.upstream = Upstream().start()
        self.addCleanup(self.upstream.stop)
        super().setUp()

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "fetcher": {"allowed_domains": [self.upstream.host]},
            "requester": {"allowed_domains": [self.upstream.host]},
        }
        return fields

    def test_get_to_an_allowed_domain(self):
        """The request really goes out, and status and body come back."""
        text = self.text_of("fetcher", {"url": self.upstream.url("/page")})
        self.assertIn("status=200", text)
        self.assertIn("stub page", text)
        self.assertEqual(self.upstream.last_request()["path"], "/page")


class RequireTests(ServerCase):
    plugins = ("requirer", "helper")
    enabled = ("requirer",)

    def test_require_loads_a_module_from_the_plugin_dir(self):
        """require() resolves against plugin_dir and its exports are usable."""
        self.assertEqual(
            self.text_of("requirer", {"what": "./lib/helper.js"}), "helper-export"
        )


class PluginConfigTests(ServerCase):
    plugins = ("counter",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"counter": {"tag": "alpha"}}
        return fields

    def test_plugin_sees_its_own_options(self):
        """host.server.config() returns the options of the calling instance."""
        self.assertIn("tag=alpha", self.text_of("counter"))


CASES = {
    "StartupTests.test_valid_config_serves": "startup.serves",
    "PidFileStartupTests.test_pid_file_is_written": "startup.pid-file",
    "EnvResolutionTests.test_token_secret_is_read_from_the_environment": "startup.env-resolution",
    "FileResolutionTests.test_plugin_option_is_read_from_a_file": "startup.file-resolution",
    "DefaultsTests.test_digest_falls_back_to_built_in_defaults": "startup.defaults",
    "DiscoveryTests.test_tools_list": "discover.tools-list",
    "DiscoveryTests.test_declared_schema_is_passed_through": "discover.schema-passthrough",
    "DiscoveryTests.test_plugin_without_schema_gets_an_object_schema": "discover.default-schema",
    "DiscoveryTests.test_annotation_hints": "discover.annotations",
    "DiscoveryTests.test_openapi_lists_plugin_metadata": "discover.openapi-metadata",
    "AliasTests.test_one_module_two_independent_tools": "discover.alias",
    "LegacyToolsMapTests.test_legacy_tools_map_still_registers_a_plugin": "discover.legacy-tools",
    "ResultShapeTests.test_string_result_is_text_content": "tool.text-result",
    "ResultShapeTests.test_object_result_is_json_text": "tool.json-result",
    "ResultShapeTests.test_arguments_reach_the_plugin": "tool.arguments",
    "FileSandboxTests.test_read_inside_the_allowed_path": "sandbox.fs-read",
    "FileSandboxTests.test_write_inside_the_allowed_path": "sandbox.fs-write",
    "EnvSandboxTests.test_allowed_variable_is_readable": "sandbox.env",
    "ExecSandboxTests.test_allowed_command_runs": "sandbox.exec",
    "HttpSandboxTests.test_get_to_an_allowed_domain": "sandbox.http-get",
    "RequireTests.test_require_loads_a_module_from_the_plugin_dir": "sandbox.require",
    "PluginConfigTests.test_plugin_sees_its_own_options": "sandbox.config",
}
