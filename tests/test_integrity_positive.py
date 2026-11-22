"""Positive cases for plugin and config integrity.

Integrity is checked in two places and both are exercised here: the ``digest``
sub-command, which produces the values, and the server, which refuses to start
when a value does not match. The digests are recomputed in Python with
``hashlib`` so the suite proves the tool agrees with an independent
implementation rather than with itself.
"""

import hashlib
import json

from tests.support import fixtures, harness
from tests.support.cases import ConfigCase, ServerCase


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sha512(path):
    return hashlib.sha512(path.read_bytes()).hexdigest()


class DigestCase(ConfigCase):
    """A plugin directory with a mix of fixtures, and the digest command."""

    plugins = ("ping", "clock", "helper", "boom")
    enabled = ("ping",)

    def digest(self, *args, **kwargs):
        return harness.run_cli("digest", "-config", self.config.path, *args, **kwargs)

    def assertDigestOk(self, completed):
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        return completed.stdout


class DigestTextTests(DigestCase):
    def test_text_output_covers_every_js_file(self):
        """Every .js file, recursively, sorted, as 'algo:hex  path'."""
        output = self.assertDigestOk(self.digest())
        lines = [line for line in output.splitlines() if line.strip()]
        self.assertEqual(
            [line.split("  ", 1)[1] for line in lines],
            ["boom.js", "clock_file.js", "lib/helper.js", "ping.js"],
        )
        for line in lines:
            checksum, path = line.split("  ", 1)
            self.assertEqual(checksum, f"sha256:{sha256(self.plugin_dir / path)}")


class DigestYamlTests(DigestCase):
    def test_yaml_is_keyed_by_exported_name(self):
        """The key is the plugin's exported name, with the file as a comment."""
        output = self.assertDigestOk(self.digest("-format", "yaml"))
        self.assertTrue(output.startswith("plugins:\n"))
        self.assertIn("  get_clock:\n    # clock_file.js\n", output)
        self.assertIn("  ping:\n    # ping.js\n", output)
        # A library module exports no name, so the file stem is used.
        self.assertIn("  helper:\n    # lib/helper.js\n", output)
        self.assertIn(f'checksum: "sha256:{sha256(self.plugin_dir / "ping.js")}"', output)


class DigestManifestTests(DigestCase):
    def test_manifest_matches_coreutils_format(self):
        """'<hex>  <path>' lines, the input format of sha256sum -c."""
        output = self.assertDigestOk(self.digest("-format", "manifest"))
        entries = {}
        for line in output.splitlines():
            if not line.strip():
                continue
            digest, path = line.split("  ", 1)
            entries[path] = digest
        self.assertEqual(entries["ping.js"], sha256(self.plugin_dir / "ping.js"))
        self.assertEqual(entries["lib/helper.js"], sha256(self.plugin_dir / "lib/helper.js"))
        # No algorithm prefix: coreutils would not understand one.
        self.assertNotIn(":", entries["ping.js"])


class DigestJsonTests(DigestCase):
    def test_json_is_keyed_by_path(self):
        """A JSON object keyed by path, carrying path, name and checksum."""
        output = self.assertDigestOk(self.digest("-format", "json"))
        entries = json.loads(output)
        self.assertEqual(entries["ping.js"]["name"], "ping")
        self.assertEqual(entries["clock_file.js"]["name"], "get_clock")
        self.assertEqual(entries["ping.js"]["path"], "ping.js")
        self.assertEqual(
            entries["ping.js"]["checksum"], f"sha256:{sha256(self.plugin_dir / 'ping.js')}"
        )


class DigestSha512Tests(DigestCase):
    def test_sha512_digests(self):
        """-algo sha512 produces 128 hex characters per file."""
        output = self.assertDigestOk(self.digest("-algo", "sha512"))
        checksum, path = output.splitlines()[0].split("  ", 1)
        self.assertTrue(checksum.startswith("sha512:"))
        self.assertEqual(len(checksum.split(":", 1)[1]), 128)
        self.assertEqual(checksum, f"sha512:{sha512(self.plugin_dir / path)}")


class DigestOutFileTests(DigestCase):
    def test_output_can_be_written_to_a_file(self):
        """-o writes the digests and reports the count on stderr."""
        target = self.dir / "checksums.txt"
        completed = self.digest("-format", "manifest", "-o", target)
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn("wrote 4 digests", completed.stderr)
        self.assertEqual(completed.stdout, "")
        self.assertIn("ping.js", target.read_text())


class DigestPathsTests(DigestCase):
    def test_explicit_paths_are_digested_as_given(self):
        """File arguments limit the output; paths keep the form they were given."""
        loose = self.dir / "loose.js"
        loose.write_text(
            'module.exports = { name: "loose", call: function () { return 1; } };\n'
        )
        output = self.assertDigestOk(self.digest(str(loose)))
        lines = [line for line in output.splitlines() if line.strip()]
        self.assertEqual(len(lines), 1)
        self.assertEqual(lines[0], f"sha256:{sha256(loose)}  {loose}")

        # A file under plugin_dir is reported relative to it, which is the form
        # a manifest needs.
        inside = self.assertDigestOk(self.digest(str(self.plugin_dir / "ping.js")))
        self.assertEqual(
            inside.strip(),
            f"sha256:{sha256(self.plugin_dir / 'ping.js')}  ping.js",
        )


class DigestVerifyTests(DigestCase):
    plugins = ("ping", "boom")
    enabled = ("ping", "boom")

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "ping": {"checksum": f"sha256:{sha256(self.plugin_dir / 'ping.js')}"},
            "boom": {"checksum": f"sha256:{sha256(self.plugin_dir / 'boom.js')}"},
        }
        return fields

    def test_verify_reports_ok_and_exits_zero(self):
        """Matching checksums are reported per file, with a summary."""
        completed = self.digest("-verify")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn("ok", completed.stdout)
        self.assertIn("ping", completed.stdout)
        self.assertIn("2 checked, 0 failed", completed.stdout)


class PerPluginChecksumTests(ServerCase):
    plugins = ("ping", "boom")

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "ping": {"checksum": f"sha256:{sha256(self.plugin_dir / 'ping.js')}"},
            "boom": {},
        }
        return fields

    def test_matching_checksum_lets_the_server_start(self):
        """A verified plugin is served, and one with no checksum still is."""
        self.assertEqual(self.tool_names(), ["boom", "ping"])
        self.assertEqual(self.text_of("ping"), "pong")


class AliasChecksumTests(ServerCase):
    plugins = ("counter",)

    def config_for(self):
        fields = super().config_for()
        checksum = f"sha256:{sha256(self.plugin_dir / 'counter.js')}"
        fields["plugins"] = {
            "counter": {"checksum": checksum, "tag": "source"},
            "counter_alias": {"plugin": "counter", "tag": "alias"},
        }
        return fields

    def test_alias_inherits_the_module_checksum(self):
        """Declaring the checksum once covers every entry using that module."""
        self.assertEqual(self.tool_names(), ["counter", "counter_alias"])
        self.assertIn("tag=alias", self.text_of("counter_alias"))


class RequiredChecksumDisabledEntryTests(ServerCase):
    plugins = ("ping", "boom")

    def config_for(self):
        fields = super().config_for()
        fields["require_plugin_checksums"] = True
        fields["plugins"] = {
            "ping": {"checksum": f"sha256:{sha256(self.plugin_dir / 'ping.js')}"},
            "boom": {"enabled": False},
        }
        return fields

    def test_disabled_entry_is_exempt(self):
        """require_plugin_checksums applies to loaded plugins only."""
        self.assertEqual(self.tool_names(), ["ping"])


class ManifestCoversRequiredFileTests(ServerCase):
    """A manifest that covers the file a plugin require()s at call time."""

    plugins = ("needs-late",)

    def prepare(self):
        # The required file has to exist before the manifest is generated.
        fixtures.write_late_lib(self.plugin_dir)
        self.manifest = self.dir / "checksums.txt"
        digest_config = harness.write_config(
            self.dir, plugins={"needs_late": {}}
        )
        completed = harness.run_cli(
            "digest", "-config", digest_config.path, "-format", "manifest",
            "-o", self.manifest,
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))

    def config_for(self):
        fields = super().config_for()
        fields["plugin_checksum_manifest"] = str(self.manifest)
        return fields

    def test_required_file_is_verified_at_call_time(self):
        """A require()d file covered by the manifest is allowed."""
        self.assertEqual(self.text_of("needs_late"), "late-export")


# ---------------------------------------------------------------------------
# Config integrity
# ---------------------------------------------------------------------------


class ConfigVerifyCase(ConfigCase):
    plugins = ("ping",)

    def digest(self):
        return f"sha256:{sha256(self.config.path)}"

    def verify(self, *args, **kwargs):
        return harness.run_cli("config", "verify", "-config", self.config.path, *args, **kwargs)


class ConfigVerifyPrintTests(ConfigVerifyCase):
    def test_printing_the_digest(self):
        """With no -checksum, print the digest and how to pin it."""
        completed = self.verify()
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn(self.digest(), completed.stderr)
        self.assertIn("pin it with:", completed.stderr)


class ConfigVerifyMatchTests(ConfigVerifyCase):
    def test_matching_digest(self):
        """A matching digest exits 0 and says so."""
        digest = self.digest()
        completed = self.verify("-checksum", digest)
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn(f"ok: {self.config.path} matches {digest}", completed.stdout)


class ConfigVerifyBareHexTests(ConfigVerifyCase):
    def test_bare_hex_is_accepted_as_sha256(self):
        """A 64-character digest with no algorithm prefix means sha256."""
        completed = self.verify("-checksum", self.digest().split(":", 1)[1])
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))


class ConfigVerifyFileRefTests(ConfigVerifyCase):
    def test_digest_read_from_a_file(self):
        """file:// keeps the expected value out of the service unit."""
        reference = self.dir / "config.sha256"
        reference.write_text(self.digest() + "\n")
        completed = self.verify("-checksum", f"file://{reference}")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))


class ConfigVerifyEnvRefTests(ConfigVerifyCase):
    def test_digest_read_from_the_environment(self):
        """env:// reads the expected value from the process environment."""
        completed = self.verify(
            "-checksum", "env://MCPHE_TEST_CONFIG_SUM",
            env={"MCPHE_TEST_CONFIG_SUM": self.digest()},
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))


class ConfigVerifyNoParseTests(ConfigVerifyCase):
    def test_unparsable_config_still_verifies(self):
        """Integrity is about bytes: the file is never parsed."""
        self.config.path.write_text("this: [is not: valid yaml\n")
        digest = f"sha256:{sha256(self.config.path)}"
        completed = self.verify("-checksum", digest)
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn("matches", completed.stdout)


class ServePinnedTests(ServerCase):
    plugins = ("ping",)
    autostart = False

    def config_for(self):
        fields = super().config_for()
        fields["verbosity_level"] = 1
        return fields

    def test_server_starts_on_a_matching_digest(self):
        """A pinned config starts, and says it verified the pin."""
        self.server.args = ("-config-checksum", f"sha256:{sha256(self.config.path)}")
        self.start()
        self.assertEqual(self.tool_names(), ["ping"])
        self.assertIn("Config checksum verified", self.server.stderr())


class ServeRequireFlagTests(ServerCase):
    plugins = ("ping",)
    autostart = False

    def test_requirement_from_the_command_line(self):
        """-require-config-checksum plus a matching digest starts the server."""
        self.server.args = (
            "-require-config-checksum",
            "-config-checksum",
            f"sha256:{sha256(self.config.path)}",
        )
        self.start()
        self.assertEqual(self.tool_names(), ["ping"])


class ServeRequireKeyTests(ServerCase):
    plugins = ("ping",)
    autostart = False

    def config_for(self):
        fields = super().config_for()
        fields["require_config_checksum"] = True
        return fields

    def test_requirement_from_the_config_file(self):
        """require_config_checksum plus a matching digest starts the server."""
        self.server.args = ("-config-checksum", f"sha256:{sha256(self.config.path)}")
        self.start()
        self.assertEqual(self.tool_names(), ["ping"])


CASES = {
    "DigestTextTests.test_text_output_covers_every_js_file": "digest.text",
    "DigestYamlTests.test_yaml_is_keyed_by_exported_name": "digest.yaml",
    "DigestManifestTests.test_manifest_matches_coreutils_format": "digest.manifest",
    "DigestJsonTests.test_json_is_keyed_by_path": "digest.json",
    "DigestSha512Tests.test_sha512_digests": "digest.sha512",
    "DigestOutFileTests.test_output_can_be_written_to_a_file": "digest.out-file",
    "DigestPathsTests.test_explicit_paths_are_digested_as_given": "digest.paths",
    "DigestVerifyTests.test_verify_reports_ok_and_exits_zero": "digest.verify-ok",
    "PerPluginChecksumTests.test_matching_checksum_lets_the_server_start": "checksum.per-plugin",
    "AliasChecksumTests.test_alias_inherits_the_module_checksum": "checksum.alias-inherits",
    "RequiredChecksumDisabledEntryTests.test_disabled_entry_is_exempt": "checksum.required-disabled-exempt",
    "ManifestCoversRequiredFileTests.test_required_file_is_verified_at_call_time": "checksum.manifest-require",
    "ConfigVerifyPrintTests.test_printing_the_digest": "configverify.print",
    "ConfigVerifyMatchTests.test_matching_digest": "configverify.match",
    "ConfigVerifyBareHexTests.test_bare_hex_is_accepted_as_sha256": "configverify.bare-hex",
    "ConfigVerifyFileRefTests.test_digest_read_from_a_file": "configverify.file-ref",
    "ConfigVerifyEnvRefTests.test_digest_read_from_the_environment": "configverify.env-ref",
    "ConfigVerifyNoParseTests.test_unparsable_config_still_verifies": "configverify.no-parse",
    "ServePinnedTests.test_server_starts_on_a_matching_digest": "serve.pinned",
    "ServeRequireFlagTests.test_requirement_from_the_command_line": "serve.require-flag",
    "ServeRequireKeyTests.test_requirement_from_the_config_file": "serve.require-key",
}
