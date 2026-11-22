"""Negative cases: failures are reported, not swallowed.

A startup failure, a refused request and a sandbox denial are all different
bugs, so each test asserts the *failure mode* -- the exit status, the message,
the JSON-RPC error code -- and, where it matters, the side effect that must not
happen (a file that must not be written, a request that must not be sent).
"""

import socket

from tests.support import harness
from tests.support.cases import ServerCase, StartupCase
from tests.support.upstream import Upstream


class StartupFailureTests(StartupCase):
    plugins = ("ping",)

    def test_missing_config_file(self):
        """An explicit config path that does not exist is a startup failure."""
        missing = self.dir / "does-not-exist.yaml"
        completed = harness.run_cli(missing)
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("config file not found", completed.stderr)
        self.assertIn(str(missing), completed.stderr)

    def test_unparsable_config(self):
        """A config that is not valid YAML is refused, naming the parse error."""
        self.config.path.write_text("transport: http\n  bad: [unclosed\n")
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("error parsing config", completed.stderr)

    def test_unknown_transport(self):
        """transport: bogus is refused instead of silently serving http."""
        self.config.rewrite(transport="bogus")
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('invalid transport "bogus"', completed.stderr)
        self.assertIn("http", completed.stderr)

    def test_missing_plugin_directory(self):
        """A plugin_dir that does not exist is a startup failure."""
        self.config.rewrite(plugin_dir=str(self.dir / "no-such-plugins"))
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("plugin directory", completed.stderr)
        self.assertIn("missing", completed.stderr)


class EmptyPluginDirectoryTests(StartupCase):
    plugins = ()

    def test_directory_without_plugins(self):
        """An empty plugin directory is refused rather than serving no tools."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("no JavaScript plugins found", completed.stderr)


class PortInUseTests(StartupCase):
    plugins = ("ping",)

    def test_port_already_bound(self):
        """A port another process holds is a startup failure, not a fallback."""
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as blocker:
            blocker.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            blocker.bind(("127.0.0.1", 0))
            blocker.listen(1)
            port = blocker.getsockname()[1]
            self.config.rewrite(port=str(port))
            completed = self.run_startup()

        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("address already in use", completed.stderr)
        self.assertIn(f"127.0.0.1:{port}", completed.stderr)


class MissingNameTests(StartupCase):
    """A plugin that cannot be named cannot be a tool."""

    plugins = ("no-name",)
    enabled = ()

    def test_plugin_without_a_name(self):
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("missing name", completed.stderr)
        self.assertIn("noname.js", completed.stderr)


class MissingCallTests(StartupCase):
    plugins = ("no-call",)
    enabled = ()

    def test_plugin_without_a_call_function(self):
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("call property is not a function", completed.stderr)


class DuplicateNameTests(StartupCase):
    plugins = ("duplicate",)
    enabled = ()

    def test_two_plugins_with_the_same_name(self):
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('duplicate plugin name "dupe"', completed.stderr)


class BadSchemaTypeTests(StartupCase):
    plugins = ("bad-schema-type",)
    enabled = ()

    def test_schema_type_must_be_object(self):
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn(
            'inputSchema "type" must be the string "object"', completed.stderr
        )


class BadSchemaValueTests(StartupCase):
    plugins = ("bad-schema-value",)
    enabled = ()

    def test_schema_must_be_an_object(self):
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("inputSchema must be a JSON object", completed.stderr)


class UnknownAliasTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"ghost": {"plugin": "nosuch"}}
        return fields

    def test_alias_naming_a_missing_module(self):
        """plugin: must name a module that exists in plugin_dir."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('source module "nosuch" not found', completed.stderr)


class UnknownToolTests(ServerCase):
    plugins = ("ping",)

    def test_unknown_tool_is_a_json_rpc_error(self):
        """Calling a tool that is not registered is an error, not empty output."""
        result = self.call("does_not_exist")
        self.assertIsNotNone(result.error)
        self.assertEqual(result.error["code"], -32602)
        self.assertIn('unknown tool "does_not_exist"', result.error["message"])


class DisabledToolTests(ServerCase):
    plugins = ("ping", "echo")

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"ping": {"enabled": False}, "echo": {}}
        return fields

    def test_disabled_tool_is_neither_listed_nor_callable(self):
        """enabled: false removes the tool from the surface entirely."""
        self.assertEqual(self.tool_names(), ["echo"])
        result = self.call("ping")
        self.assertEqual(result.error["code"], -32602)


class UnconfiguredPluginTests(ServerCase):
    plugins = ("ping",)
    enabled = ()

    def test_plugin_without_a_config_entry_is_not_registered(self):
        """A .js file alone does not enable a tool: the config decides."""
        self.assertEqual(self.tool_names(), [])
        result = self.call("ping")
        self.assertEqual(result.error["code"], -32602)


class ToolFailureTests(ServerCase):
    plugins = ("ping", "echo", "boom", "strict")

    def test_a_throwing_plugin_is_an_error_result(self):
        """A plugin exception is reported, and the server keeps serving."""
        result = self.call("boom")
        self.assertToolError(result, "tool error", "kaboom")
        # The session still works: one bad call must not take the server down.
        self.assertEqual(self.text_of("ping"), "pong")

    def test_arguments_that_are_not_an_object(self):
        """Non-object arguments are rejected by validation, not by the plugin."""
        result = self.call("echo", "a string, not an object")
        self.assertToolError(result, "arguments")

    def test_required_arguments_are_validated(self):
        """A call that does not satisfy the inputSchema never reaches the plugin."""
        self.assertToolError(
            self.call("strict", {}), 'missing properties: ["text"]'
        )
        self.assertToolError(
            self.call("strict", {"text": 42}), 'has type "integer", want "string"'
        )
        # The same tool works when the schema is satisfied, so the refusals above
        # are the schema and not the plugin.
        self.assertEqual(self.text_of("strict", {"text": "ok"}), "got ok")


class FileSandboxFailureTests(ServerCase):
    plugins = ("reader", "writer")

    def config_for(self):
        self.allowed = self.dir / "allowed"
        self.allowed.mkdir()
        self.outside = self.dir / "outside"
        self.outside.mkdir()
        self.sibling = self.dir / "allowed_extra"
        self.sibling.mkdir()
        fields = super().config_for()
        fields["plugins"] = {
            "reader": {"allowed_read_file_paths": [str(self.allowed)]},
            "writer": {"allowed_write_file_paths": [str(self.allowed)]},
        }
        return fields

    def test_read_outside_the_allowed_path(self):
        """A path outside allowed_read_file_paths is refused, not read."""
        target = self.outside / "secret.txt"
        target.write_text("do not read me\n")
        self.assertToolError(
            self.call("reader", {"path": str(target)}),
            "access to file",
            "not allowed",
        )

    def test_read_through_a_traversing_path(self):
        """A path that climbs out with .. is refused, not resolved."""
        target = self.allowed / ".." / "outside" / "secret.txt"
        (self.outside / "secret.txt").write_text("do not read me\n")
        self.assertToolError(
            self.call("reader", {"path": str(target)}), "not allowed"
        )

    def test_write_outside_the_allowed_path(self):
        """A write outside allowed_write_file_paths is refused and creates nothing."""
        target = self.outside / "planted.txt"
        self.assertToolError(
            self.call("writer", {"path": str(target), "content": "x"}),
            "writing to file",
            "not allowed",
        )
        self.assertFalse(target.exists())

    def test_allowed_directory_stops_at_a_name_boundary(self):
        """'/tmp/x' must not authorise '/tmp/x_extra'."""
        target = self.sibling / "note.txt"
        target.write_text("sibling\n")
        self.assertToolError(
            self.call("reader", {"path": str(target)}), "not allowed"
        )


class EnvSandboxFailureTests(ServerCase):
    plugins = ("enver",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"enver": {"allowed_env_vars": ["MCPHE_TEST_VAR"]}}
        return fields

    def test_variable_not_on_the_allow_list(self):
        """An environment variable that is not allowed is refused."""
        self.assertToolError(
            self.call("enver", {"name": "PATH"}),
            "access to environment variable",
            "not allowed",
        )


class ExecSandboxFailureTests(ServerCase):
    plugins = ("runner",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"runner": {"allowed_commands": ["echo"]}}
        return fields

    def test_command_not_on_the_allow_list(self):
        """A command that is not allowed never runs."""
        self.assertToolError(
            self.call("runner", {"command": "su"}), 'command "su" is not allowed'
        )

    def test_allow_list_matches_the_command_name_only(self):
        """An allow-list entry does not extend to a longer command name."""
        self.assertToolError(
            self.call("runner", {"command": "echo-evil"}),
            'command "echo-evil" is not allowed',
        )


class HttpSandboxFailureTests(ServerCase):
    plugins = ("fetcher", "requester")

    def setUp(self):
        self.upstream = Upstream().start()
        self.addCleanup(self.upstream.stop)
        super().setUp()

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "fetcher": {
                "allowed_domains": ["allowed.example"],
                "allowed_http_methods": ["GET"],
            },
            "requester": {
                "allowed_domains": [self.upstream.host],
                "allowed_http_methods": ["GET"],
            },
        }
        return fields

    def test_domain_not_on_the_allow_list(self):
        """A host outside allowed_domains is refused, and nothing is sent."""
        result = self.call("fetcher", {"url": self.upstream.url("/page")})
        self.assertToolError(result, f"access to {self.upstream.host} is not allowed")
        self.assertEqual(self.upstream.requests, [])

    def test_http_request_enforces_allowed_methods(self):
        """host.http.request refuses a method that allowed_http_methods omits."""
        self.assertToolError(
            self.call(
                "requester",
                {"url": self.upstream.url("/echo"), "method": "POST"},
            ),
            "not allowed",
        )
        self.assertEqual(self.upstream.requests, [])


class HttpMethodGapTests(ServerCase):
    """allowed_http_methods, seen from the convenience helper that ignores it."""

    plugins = ("fetcher",)

    def setUp(self):
        self.upstream = Upstream().start()
        self.addCleanup(self.upstream.stop)
        super().setUp()

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "fetcher": {
                "allowed_domains": [self.upstream.host],
                "allowed_http_methods": ["GET"],
            }
        }
        return fields

    def test_http_post_ignores_allowed_methods(self):
        """host.http.post is not covered by allowed_http_methods -- a recorded gap.

        The check lives in host.http.request only, so the convenience helpers
        (get/post/put/patch/delete/head/options) enforce the domain list but not
        the method list. This case pins the behaviour so that fixing it is a
        deliberate change here rather than a silent one.
        """
        text = self.text_of(
            "fetcher", {"url": self.upstream.url("/echo"), "method": "POST"}
        )
        self.assertIn("status=200", text)
        self.assertEqual(self.upstream.last_request()["method"], "POST")


class RequireFailureTests(ServerCase):
    plugins = ("requirer", "cycle", "data")
    enabled = ("requirer",)

    def test_path_traversal(self):
        """require() refuses a specifier that leaves plugin_dir."""
        self.assertToolError(
            self.call("requirer", {"what": "../../etc/passwd"}),
            "path traversal not allowed",
        )

    def test_non_javascript_module(self):
        """Only .js modules may be required."""
        self.assertToolError(
            self.call("requirer", {"what": "./lib/data.json"}),
            "only .js modules may be required",
        )

    def test_circular_import(self):
        """A require() cycle is reported instead of recursing."""
        self.assertToolError(
            self.call("requirer", {"what": "./lib/cycle_a.js"}), "circular import"
        )


CASES = {
    "StartupFailureTests.test_missing_config_file": "startup.missing-config",
    "StartupFailureTests.test_unparsable_config": "startup.bad-yaml",
    "StartupFailureTests.test_unknown_transport": "startup.bad-transport",
    "StartupFailureTests.test_missing_plugin_directory": "startup.missing-plugin-dir",
    "EmptyPluginDirectoryTests.test_directory_without_plugins": "startup.no-plugins",
    "PortInUseTests.test_port_already_bound": "startup.port-in-use",
    "MissingNameTests.test_plugin_without_a_name": "discover.missing-name",
    "MissingCallTests.test_plugin_without_a_call_function": "discover.missing-call",
    "DuplicateNameTests.test_two_plugins_with_the_same_name": "discover.duplicate-name",
    "BadSchemaTypeTests.test_schema_type_must_be_object": "discover.bad-schema-type",
    "BadSchemaValueTests.test_schema_must_be_an_object": "discover.schema-not-object",
    "UnknownAliasTests.test_alias_naming_a_missing_module": "discover.unknown-alias",
    "UnknownToolTests.test_unknown_tool_is_a_json_rpc_error": "discover.unknown-tool",
    "DisabledToolTests.test_disabled_tool_is_neither_listed_nor_callable": "discover.disabled",
    "UnconfiguredPluginTests.test_plugin_without_a_config_entry_is_not_registered": "discover.unconfigured",
    "ToolFailureTests.test_a_throwing_plugin_is_an_error_result": "tool.error",
    "ToolFailureTests.test_arguments_that_are_not_an_object": "tool.bad-arguments",
    "ToolFailureTests.test_required_arguments_are_validated": "tool.schema-validation",
    "FileSandboxFailureTests.test_read_outside_the_allowed_path": "sandbox.fs-read-denied",
    "FileSandboxFailureTests.test_read_through_a_traversing_path": "sandbox.fs-traversal",
    "FileSandboxFailureTests.test_write_outside_the_allowed_path": "sandbox.fs-write-denied",
    "FileSandboxFailureTests.test_allowed_directory_stops_at_a_name_boundary": "sandbox.fs-prefix-boundary",
    "EnvSandboxFailureTests.test_variable_not_on_the_allow_list": "sandbox.env-denied",
    "ExecSandboxFailureTests.test_command_not_on_the_allow_list": "sandbox.exec-denied",
    "ExecSandboxFailureTests.test_allow_list_matches_the_command_name_only": "sandbox.exec-name-only",
    "HttpSandboxFailureTests.test_domain_not_on_the_allow_list": "sandbox.http-domain-denied",
    "HttpSandboxFailureTests.test_http_request_enforces_allowed_methods": "sandbox.http-method-request",
    "HttpMethodGapTests.test_http_post_ignores_allowed_methods": "sandbox.http-method-convenience",
    "RequireFailureTests.test_path_traversal": "sandbox.require-traversal",
    "RequireFailureTests.test_non_javascript_module": "sandbox.require-non-js",
    "RequireFailureTests.test_circular_import": "sandbox.require-cycle",
}
