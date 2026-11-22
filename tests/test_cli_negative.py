"""Negative cases for the token sub-command and for CLI usage.

These are the mistakes an operator makes: a config with no secret, a duration
that is not a duration, a flag the binary does not have. Each asserts the exit
status as well as the message, because a wrapper script branches on the status
and a human reads the message.
"""


from tests.support import harness
from tests.support.cases import ConfigCase

SECRET = "suite-token-secret-0123456789"


class TokenCase(ConfigCase):
    plugins = ("ping",)

    def token(self, *args, **kwargs):
        return harness.run_cli(
            "token", *args[:1], "-config", self.config.path, *args[1:], **kwargs
        )


class NoSecretTests(TokenCase):
    def config_for(self):
        fields = super().config_for()
        fields["token_revocation_file"] = str(self.dir / "revocations.yaml")
        return fields

    def test_create_without_a_token_secret(self):
        """No token_secret means no token can be signed, and it says why."""
        completed = self.token("create", "alice", "1h")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("token_secret is not set", completed.stderr)
        self.assertEqual(completed.stdout, "")


class BadDurationTests(TokenCase):
    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_unparsable_duration(self):
        """A duration that is not a duration is refused before anything is minted."""
        completed = self.token("create", "alice", "banana")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn('invalid duration "banana"', completed.stderr)
        self.assertEqual(completed.stdout, "")


class MissingArgumentsTests(TokenCase):
    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_create_without_a_duration(self):
        """token create needs a username and a duration."""
        completed = self.token("create", "alice")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("Usage:", completed.stderr)
        self.assertEqual(completed.stdout, "")


class UnknownSubcommandTests(TokenCase):
    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_unknown_token_subcommand(self):
        """A sub-command that does not exist is a usage error."""
        completed = self.token("bogus")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("Usage:", completed.stderr)


class RevokeWithoutFileTests(TokenCase):
    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_revoke_without_a_revocation_file(self):
        """Revocation is opt-in, so revoking without the file is an error."""
        completed = self.token("revoke", "alice")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("token_revocation_file is not set", completed.stderr)


class UnknownFlagTests(ConfigCase):
    plugins = ("ping",)

    def test_unknown_flag_is_a_usage_error(self):
        """Go's flag parser rejects an unknown flag with exit 2."""
        completed = harness.run_cli("-no-such-flag")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("flag provided but not defined", completed.stderr)


class VerifyWithArgumentsTests(ConfigCase):
    plugins = ("ping",)

    def test_verify_refuses_file_arguments(self):
        """-verify checks the config's checksums; it does not take paths."""
        completed = harness.run_cli(
            "digest", "-verify", "-config", self.config.path, "ping.js"
        )
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("takes no file arguments", completed.stderr)


class VerifyWithOutputTests(ConfigCase):
    plugins = ("ping",)

    def test_verify_refuses_an_output_file(self):
        """-verify reports to stdout, so -o is a contradiction."""
        completed = harness.run_cli(
            "digest", "-verify", "-config", self.config.path, "-o", self.dir / "out.txt"
        )
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("cannot be combined with -o", completed.stderr)


CASES = {
    "NoSecretTests.test_create_without_a_token_secret": "token.no-secret",
    "BadDurationTests.test_unparsable_duration": "token.bad-duration",
    "MissingArgumentsTests.test_create_without_a_duration": "token.missing-args",
    "UnknownSubcommandTests.test_unknown_token_subcommand": "token.unknown-sub",
    "RevokeWithoutFileTests.test_revoke_without_a_revocation_file": "token.revoke-no-file",
    "UnknownFlagTests.test_unknown_flag_is_a_usage_error": "cli.unknown-flag",
    "VerifyWithArgumentsTests.test_verify_refuses_file_arguments": "cli.verify-with-args",
    "VerifyWithOutputTests.test_verify_refuses_an_output_file": "cli.verify-with-output",
}
