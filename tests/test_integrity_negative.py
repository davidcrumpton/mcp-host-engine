"""Negative cases for plugin and config integrity.

Every case here is a file that must not be trusted: a digest that disagrees with
the bytes, a checksum that cannot be parsed, a manifest that does not cover what
the loader reads. Each asserts both the refusal and the reason, because "the
server did not start" is not enough to distinguish a checksum failure from a
typo in the plugin directory.
"""

import hashlib

from tests.support import fixtures, harness
from tests.support.cases import ConfigCase, ServerCase, StartupCase


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class DigestCase(ConfigCase):
    plugins = ("ping", "boom")
    enabled = ("ping",)

    def digest(self, *args, **kwargs):
        return harness.run_cli("digest", "-config", self.config.path, *args, **kwargs)


class MissingPluginDirectoryTests(DigestCase):
    def test_digest_without_a_plugin_directory(self):
        """A plugin_dir that does not exist is an error, not an empty list."""
        self.config.rewrite(plugin_dir=str(self.dir / "no-such-dir"))
        completed = self.digest()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("plugin directory", completed.stderr)
        self.assertIn("no-such-dir", completed.stderr)


class EmptyPluginDirectoryTests(DigestCase):
    plugins = ()

    def test_digest_with_nothing_to_digest(self):
        """An empty plugin directory is reported rather than printing nothing."""
        completed = self.digest()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("no .js files found", completed.stderr)


class BadAlgorithmTests(DigestCase):
    def test_unknown_algorithm(self):
        """-algo accepts sha256 and sha512 only, and says so with exit 2."""
        completed = self.digest("-algo", "md5")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn('invalid -algo "md5"', completed.stderr)
        self.assertIn("sha256", completed.stderr)


class BadFormatTests(DigestCase):
    def test_unknown_format(self):
        """-format accepts text, yaml, json and manifest."""
        completed = self.digest("-format", "xml")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn('invalid -format "xml"', completed.stderr)


class NoArgumentsTests(DigestCase):
    def test_digest_with_no_arguments_prints_usage(self):
        """digest needs at least a flag, and says so instead of guessing."""
        completed = harness.run_cli("digest")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("Usage:", completed.stderr)
        self.assertIn("plugin_checksum_manifest", completed.stderr)


class VerifyMismatchTests(DigestCase):
    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"ping": {"checksum": "sha256:" + "a" * 64}}
        return fields

    def test_mismatch_is_reported_with_both_digests(self):
        """-verify prints what it expected and what it found, then exits 1."""
        completed = self.digest("-verify")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("mismatch", completed.stdout)
        self.assertIn(f"expected sha256:{'a' * 64}", completed.stdout)
        self.assertIn(f"got sha256:{sha256(self.plugin_dir / 'ping.js')}", completed.stdout)
        self.assertIn("1 failed", completed.stdout)


class VerifyUncheckedTests(DigestCase):
    def config_for(self):
        fields = super().config_for()
        fields["require_plugin_checksums"] = True
        fields["plugins"] = {"ping": {}}
        return fields

    def test_missing_checksum_is_a_failure_when_required(self):
        """With checksums mandatory, an unchecked plugin is reported as missing."""
        completed = self.digest("-verify")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("missing", completed.stdout)
        self.assertIn("require_plugin_checksums is true", completed.stdout)


class ChecksumMismatchStartupTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"ping": {"checksum": "sha256:" + "b" * 64}}
        return fields

    def test_changed_plugin_stops_startup(self):
        """A plugin whose bytes changed is refused, with both digests printed."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("checksum mismatch", completed.stderr)
        self.assertIn(f"config expects sha256:{'b' * 64}", completed.stderr)
        self.assertIn(f"file is sha256:{sha256(self.plugin_dir / 'ping.js')}", completed.stderr)


class MalformedChecksumStartupTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        # Deliberately on a disabled entry: a typo must not lie dormant.
        fields["plugins"] = {"ping": {"checksum": "sha256:xyz", "enabled": False}}
        return fields

    def test_malformed_checksum_stops_startup(self):
        """A digest of the wrong length is a config error, even when disabled."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("sha256 digest must be 64 hex chars, got 3", completed.stderr)


class UnsupportedAlgorithmStartupTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {"ping": {"checksum": "md5:" + "c" * 32}}
        return fields

    def test_unsupported_algorithm_stops_startup(self):
        """An algorithm nobody implements is refused, not silently ignored."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('unsupported algorithm "md5"', completed.stderr)


class RequiredChecksumMissingTests(StartupCase):
    plugins = ("ping", "boom")

    def config_for(self):
        fields = super().config_for()
        fields["require_plugin_checksums"] = True
        fields["plugins"] = {
            "ping": {"checksum": f"sha256:{sha256(self.plugin_dir / 'ping.js')}"},
            "boom": {},
        }
        return fields

    def test_required_checksum_missing_stops_startup(self):
        """An enabled plugin with no checksum is refused, with the hint."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('plugin "boom": no checksum configured', completed.stderr)
        self.assertIn("mcphe digest -format yaml", completed.stderr)


class AliasChecksumConflictTests(StartupCase):
    plugins = ("counter",)

    def config_for(self):
        fields = super().config_for()
        fields["plugins"] = {
            "counter": {"checksum": f"sha256:{sha256(self.plugin_dir / 'counter.js')}"},
            "counter_alias": {"plugin": "counter", "checksum": "sha256:" + "d" * 64},
        }
        return fields

    def test_two_entries_disagreeing_about_one_file(self):
        """A checksum describes a file, so disagreement is a config error."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("must declare the same checksum", completed.stderr)


class ManifestUnlistedPluginTests(StartupCase):
    plugins = ("ping",)

    def prepare(self):
        self.manifest = self.dir / "checksums.txt"
        digest_config = harness.write_config(self.dir, plugins={"ping": {}})
        completed = harness.run_cli(
            "digest", "-config", digest_config.path, "-format", "manifest",
            "-o", self.manifest,
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        # A plugin dropped in after the manifest was generated, and enabled.
        fixtures.install(self.plugin_dir, "boom")

    def config_for(self):
        fields = super().config_for()
        fields["plugin_checksum_manifest"] = str(self.manifest)
        fields["plugins"] = {"ping": {}, "boom": {}}
        return fields

    def test_plugin_not_in_the_manifest_is_refused(self):
        """A file the manifest does not list must not load."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("boom.js is not listed in checksum manifest", completed.stderr)


class ManifestUnlistedRequiredFileTests(ServerCase):
    """The manifest is what covers require()d files, at call time."""

    plugins = ("needs-late",)

    def prepare(self):
        # The manifest is generated while lib/late.js does not exist, so the
        # file is unlisted when the plugin asks for it at call time.
        self.manifest = self.dir / "checksums.txt"
        digest_config = harness.write_config(self.dir, plugins={"needs_late": {}})
        completed = harness.run_cli(
            "digest", "-config", digest_config.path, "-format", "manifest",
            "-o", self.manifest,
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        fixtures.write_late_lib(self.plugin_dir)

    def config_for(self):
        fields = super().config_for()
        fields["plugin_checksum_manifest"] = str(self.manifest)
        return fields

    def test_unlisted_required_file_fails_the_call(self):
        """The call fails, and the message names the manifest."""
        self.assertToolError(
            self.call("needs_late"), "is not listed in checksum manifest"
        )


class ManifestTamperedRequiredFileTests(ServerCase):
    plugins = ("needs-late",)

    def prepare(self):
        fixtures.write_late_lib(self.plugin_dir)
        self.manifest = self.dir / "checksums.txt"
        digest_config = harness.write_config(self.dir, plugins={"needs_late": {}})
        completed = harness.run_cli(
            "digest", "-config", digest_config.path, "-format", "manifest",
            "-o", self.manifest,
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        # Tampered after the manifest was generated, before the server starts.
        fixtures.write_late_lib(
            self.plugin_dir, source='module.exports = { value: "tampered" };\n'
        )

    def config_for(self):
        fields = super().config_for()
        fields["plugin_checksum_manifest"] = str(self.manifest)
        return fields

    def test_tampered_required_file_fails_the_call(self):
        """The call fails with the expected and actual digests."""
        self.assertToolError(
            self.call("needs_late"),
            "checksum mismatch",
            "manifest",
            "expects sha256:",
            "file is sha256:",
        )


# ---------------------------------------------------------------------------
# Config integrity
# ---------------------------------------------------------------------------


class ConfigVerifyCase(ConfigCase):
    plugins = ("ping",)

    def digest(self):
        return f"sha256:{sha256(self.config.path)}"

    def verify(self, *args, **kwargs):
        return harness.run_cli(
            "config", "verify", "-config", self.config.path, *args, **kwargs
        )


class ConfigVerifyMismatchTests(ConfigVerifyCase):
    def test_wrong_digest(self):
        """A wrong digest exits 1 and shows both values."""
        completed = self.verify("-checksum", "sha256:" + "e" * 64)
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn(f"expected sha256:{'e' * 64}", completed.stderr)
        self.assertIn(f"got {self.digest()}", completed.stderr)


class ConfigVerifyMissingFileTests(ConfigVerifyCase):
    def test_missing_config(self):
        """A config that does not exist cannot be verified."""
        missing = self.dir / "nope.yaml"
        completed = harness.run_cli(
            "config", "verify", "-config", missing, "-checksum", self.digest()
        )
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("failed to read config", completed.stderr)


class ConfigVerifyBadValueTests(ConfigVerifyCase):
    def test_value_that_is_not_a_digest(self):
        """-checksum with something that is not a digest is a usage error."""
        completed = self.verify("-checksum", "banana")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("bare digest must be 64 hex chars", completed.stderr)


class ConfigVerifyPositionalTests(ConfigVerifyCase):
    def test_positional_arguments_are_refused(self):
        """config verify verifies the -config file; it takes no arguments."""
        completed = self.verify("-checksum", self.digest(), "extra")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("takes no positional arguments", completed.stderr)
        self.assertIn("extra", completed.stderr)


class ServeMismatchTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        # If the file were parsed before being verified, this would be the
        # error we saw. It must not be.
        fields["transport"] = "bogus"
        return fields

    def test_config_that_does_not_match_never_starts(self):
        """A rejected config is not parsed and not served."""
        completed = self.run_startup("-config-checksum", "sha256:" + "f" * 64)
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("config integrity check failed", completed.stderr)
        self.assertNotIn("invalid transport", completed.stderr)


class ServeRequiresChecksumTests(StartupCase):
    plugins = ("ping",)

    def test_requirement_without_a_checksum(self):
        """-require-config-checksum alone is a refusal, not a warning."""
        completed = self.run_startup("-require-config-checksum")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("no expected checksum was supplied", completed.stderr)
        self.assertIn("mcphe config verify", completed.stderr)


class ServeRequireKeyWithoutChecksumTests(StartupCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["require_config_checksum"] = True
        return fields

    def test_requirement_in_the_file_without_a_checksum(self):
        """The flag in the config is enough to demand a checksum."""
        completed = self.run_startup()
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("no expected checksum was supplied", completed.stderr)


class ServeNoConfigFileTests(StartupCase):
    plugins = ("ping",)

    def test_checksum_gate_needs_a_file(self):
        """Asking for a pin with no file to pin is refused, not ignored."""
        empty = harness.temp_dir(self)
        completed = harness.run_cli(
            "-config-checksum", "sha256:" + "a" * 64, cwd=empty
        )
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("needs a file to verify", completed.stderr)


CASES = {
    "MissingPluginDirectoryTests.test_digest_without_a_plugin_directory": "digest.no-plugin-dir",
    "EmptyPluginDirectoryTests.test_digest_with_nothing_to_digest": "digest.empty-dir",
    "BadAlgorithmTests.test_unknown_algorithm": "digest.bad-algo",
    "BadFormatTests.test_unknown_format": "digest.bad-format",
    "NoArgumentsTests.test_digest_with_no_arguments_prints_usage": "digest.no-arguments",
    "VerifyMismatchTests.test_mismatch_is_reported_with_both_digests": "digest.verify-mismatch",
    "VerifyUncheckedTests.test_missing_checksum_is_a_failure_when_required": "digest.verify-unchecked",
    "ChecksumMismatchStartupTests.test_changed_plugin_stops_startup": "checksum.mismatch",
    "MalformedChecksumStartupTests.test_malformed_checksum_stops_startup": "checksum.malformed",
    "UnsupportedAlgorithmStartupTests.test_unsupported_algorithm_stops_startup": "checksum.unsupported-algo",
    "RequiredChecksumMissingTests.test_required_checksum_missing_stops_startup": "checksum.required-missing",
    "AliasChecksumConflictTests.test_two_entries_disagreeing_about_one_file": "checksum.alias-conflict",
    "ManifestUnlistedPluginTests.test_plugin_not_in_the_manifest_is_refused": "checksum.manifest-unlisted-plugin",
    "ManifestUnlistedRequiredFileTests.test_unlisted_required_file_fails_the_call": "checksum.manifest-unlisted-require",
    "ManifestTamperedRequiredFileTests.test_tampered_required_file_fails_the_call": "checksum.manifest-tampered-require",
    "ConfigVerifyMismatchTests.test_wrong_digest": "configverify.mismatch",
    "ConfigVerifyMissingFileTests.test_missing_config": "configverify.missing-file",
    "ConfigVerifyBadValueTests.test_value_that_is_not_a_digest": "configverify.bad-value",
    "ConfigVerifyPositionalTests.test_positional_arguments_are_refused": "configverify.positional",
    "ServeMismatchTests.test_config_that_does_not_match_never_starts": "serve.mismatch",
    "ServeRequiresChecksumTests.test_requirement_without_a_checksum": "serve.require-flag-no-value",
    "ServeRequireKeyWithoutChecksumTests.test_requirement_in_the_file_without_a_checksum": "serve.require-key-no-value",
    "ServeNoConfigFileTests.test_checksum_gate_needs_a_file": "serve.no-file",
}
