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


class NegativeTtlTests(TokenCase):
    """L3: a non-positive ttl is refused instead of written as an expired entry.

    ``mcphe token revoke bob -1h`` used to be accepted: ``parseTokenDuration``
    turned it into a negative Duration, the expiry written to the file was
    already in the past, and the next ``readEntries`` pruned it -- so the
    revocation vanished with no error at any point.
    """

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        fields["token_revocation_file"] = str(self.dir / "revocations.yaml")
        return fields

    def test_revoke_with_a_non_positive_ttl(self):
        """A ttl that is already expired is refused, and the file is untouched."""
        revocations = self.dir / "revocations.yaml"
        revocations.write_text("carol\n")
        before = revocations.read_text()

        for ttl in ("-1h", "0s", "-1d", "NaNd", "1e400d"):
            completed = self.token("revoke", "bob", ttl)
            self.assertEqual(completed.returncode, 1, harness.output_of(completed))
            self.assertIn(f'invalid ttl "{ttl}"', completed.stderr)
            self.assertEqual(completed.stdout, "")

            # Refusing must not be half-applied: the pre-existing entry is
            # still there and no already-expired entry was written for bob.
            self.assertEqual(
                revocations.read_text(),
                before,
                f"a refused ttl ({ttl}) rewrote the revocation file",
            )


class BadIdentityTests(TokenCase):
    """L13: an identity that cannot be written to the revocation file is refused.

    A username containing ``:`` made ``alice:laptop`` mean both "the user
    alice:laptop" and "alice's laptop token", and whitespace broke the
    line-based file format outright. Minting is where the check belongs: a
    token that cannot be revoked unambiguously should never exist.
    """

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        fields["token_revocation_file"] = str(self.dir / "revocations.yaml")
        return fields

    def test_create_with_an_unrepresentable_identity(self):
        """A colon or a space in either half of the identity is refused."""
        cases = [
            ("alice:laptop", None),  # colon in the username
            ("al ice", None),  # space in the username
            ("alice\nlaptop", None),  # newline would forge a second entry
            ("alice", "la bel"),  # space in the label
            ("alice", "a:b"),  # colon in the label
        ]
        for username, label in cases:
            args = ["create", username, "1h"]
            if label is not None:
                args.append(label)
            completed = self.token(*args)
            self.assertEqual(completed.returncode, 1, harness.output_of(completed))
            self.assertIn("failed to create token", completed.stderr)
            self.assertIn("use letters, digits", completed.stderr)
            self.assertEqual(
                completed.stdout,
                "",
                f"a refused identity ({username!r}, {label!r}) still printed a token",
            )
            self.assertFalse(
                (self.dir / "revocations.yaml").exists(),
                "a refused identity created the revocation file",
            )

        # Control: a representable identity still mints, so the assertions
        # above cannot be passing because every create fails.
        self.assertEqual(self.mint_token("alice").count("."), 1)


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
    "NegativeTtlTests.test_revoke_with_a_non_positive_ttl": "token.negative-ttl",
    "BadIdentityTests.test_create_with_an_unrepresentable_identity": "token.bad-identity",
    "UnknownFlagTests.test_unknown_flag_is_a_usage_error": "cli.unknown-flag",
    "VerifyWithArgumentsTests.test_verify_refuses_file_arguments": "cli.verify-with-args",
    "VerifyWithOutputTests.test_verify_refuses_an_output_file": "cli.verify-with-output",
}
