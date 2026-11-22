"""Positive cases for the token sub-command and for CLI usage.

The token command is the operator's only way to mint a credential, so these
tests look inside the token: it must name the user, carry the label it was given
and expire when it was asked to. The usage cases assert the exit status, because
that is what a script wrapping the CLI sees.
"""

import base64
import json
import threading
import time
import unittest

from tests.support import harness
from tests.support.cases import ConfigCase, ServerCase

SECRET = "suite-token-secret-0123456789"


def decode_token(token):
    """Return the payload of a token as JSON (the format is payload.signature)."""
    payload, _, signature = token.partition(".")
    padded = payload + "=" * (-len(payload) % 4)
    return json.loads(base64.urlsafe_b64decode(padded)), signature


class TokenCase(ConfigCase):
    plugins = ("ping",)

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        fields["token_revocation_file"] = str(self.dir / "revocations.yaml")
        return fields

    def token(self, *args, **kwargs):
        return harness.run_cli(
            "token", *args[:1], "-config", self.config.path, *args[1:], **kwargs
        )

    @property
    def revocation_file(self):
        return self.dir / "revocations.yaml"


class TokenCreateTests(TokenCase):
    def test_create_prints_a_token(self):
        """The token names the user and carries the requested lifetime."""
        before = time.time()
        completed = self.token("create", "alice", "1h")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))

        token = completed.stdout.strip()
        self.assertEqual(len(token.split(".")), 2, token)
        payload, signature = decode_token(token)
        self.assertEqual(payload["sub"], "alice")
        self.assertEqual(payload["prog"], "mcphe")
        self.assertTrue(signature)
        # One hour, within a minute of tolerance for clock granularity.
        self.assertAlmostEqual(payload["exp"] - payload["iat"], 3600, delta=60)
        self.assertGreaterEqual(payload["iat"], int(before) - 60)


class TokenDaysTests(TokenCase):
    def test_duration_in_days(self):
        """A day-based lifetime is accepted, which Go durations alone are not."""
        completed = self.token("create", "alice", "30d")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        payload, _ = decode_token(completed.stdout.strip())
        self.assertAlmostEqual(payload["exp"] - payload["iat"], 30 * 86400, delta=60)


class TokenLabelTests(ServerCase):
    plugins = ("ping", "echo")

    def config_for(self):
        fields = super().config_for()
        fields["token_secret"] = SECRET
        return fields

    def test_labelled_token_authenticates(self):
        """A labelled token works, and the server reports the username."""
        completed = harness.run_cli(
            "token", "create", "-config", self.config.path, "bob", "12h", "laptop"
        )
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        token = completed.stdout.strip()
        payload, _ = decode_token(token)
        self.assertEqual(payload["label"], "laptop")

        session = self.new_session(token=token)
        self.assertEqual(session.tool_names(), ["echo", "ping"])
        self.assertEqual(json.loads(session.call_tool("echo").text)["identity"], "bob")


class TokenRevokeTests(TokenCase):
    def test_revoke_writes_the_key(self):
        """Revoking a user records the username in the revocation file."""
        completed = self.token("revoke", "alice")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertIn('Revoked "alice"', completed.stdout)
        self.assertEqual(self.revocation_file.read_text().strip(), "alice")

    def test_revoke_with_a_ttl_records_an_expiry(self):
        """A ttl is stored beside the key so the entry can prune itself."""
        completed = self.token("revoke", "alice", "1h")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        key, _, expiry = self.revocation_file.read_text().strip().partition(" ")
        self.assertEqual(key, "alice")
        self.assertAlmostEqual(int(expiry), int(time.time()) + 3600, delta=120)


class TokenCreateClearsRevocationTests(TokenCase):
    def test_creating_a_token_unrevokes_the_user(self):
        """A freshly minted token must not be dead on arrival."""
        self.assertEqual(self.token("revoke", "alice").returncode, 0)
        self.assertIn("alice", self.revocation_file.read_text())

        completed = self.token("create", "alice", "1h")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        self.assertEqual(self.revocation_file.read_text().strip(), "")


# ---------------------------------------------------------------------------
# CLI usage
# ---------------------------------------------------------------------------


class HelpTests(unittest.TestCase):
    def test_help_lists_the_subcommands(self):
        """-h documents the whole surface and exits 0."""
        completed = harness.run_cli("-h")
        self.assertEqual(completed.returncode, 0, harness.output_of(completed))
        usage = completed.stderr
        self.assertIn("Usage:", usage)
        self.assertIn("token create|revoke", usage)
        self.assertIn("digest", usage)
        self.assertIn("config verify", usage)
        self.assertIn("-config-checksum", usage)


class DigestUsageTests(unittest.TestCase):
    def test_digest_usage_block(self):
        """digest with no arguments explains the four formats."""
        completed = harness.run_cli("digest")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        for fragment in ("Usage:", "text", "yaml", "manifest", "json"):
            self.assertIn(fragment, completed.stderr)


class ConfigUsageTests(unittest.TestCase):
    def test_config_usage_block(self):
        """config with no sub-command prints its usage."""
        completed = harness.run_cli("config")
        self.assertEqual(completed.returncode, 2, harness.output_of(completed))
        self.assertIn("config verify", completed.stderr)
        self.assertIn("-checksum", completed.stderr)


class TokenUsageTests(unittest.TestCase):
    def test_token_usage_block(self):
        """token with no sub-command prints its usage."""
        completed = harness.run_cli("token")
        self.assertEqual(completed.returncode, 1, harness.output_of(completed))
        self.assertIn("token create", completed.stderr)
        self.assertIn("token revoke", completed.stderr)


class ConcurrentRevokeTests(TokenCase):
    """Simultaneous revocations are serialised, so none is silently dropped."""

    def test_concurrent_revokes_all_survive(self):
        """Every entry is in the file after N revokes started at the same time.

        The file is read, mutated and rewritten; without a lock the last writer
        wins and the others vanish. A fixed temp name made it worse: two writers
        shared one scratch file, so a half-written list could be renamed into
        place.
        """
        keys = [f"user{i}" for i in range(6)]
        barrier = threading.Barrier(len(keys))
        results = {}

        def revoke(key):
            barrier.wait()
            results[key] = self.token("revoke", key)

        threads = [threading.Thread(target=revoke, args=(key,)) for key in keys]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join(timeout=60)

        for key, completed in results.items():
            self.assertEqual(completed.returncode, 0, harness.output_of(completed))

        entries = self.revocation_file.read_text().split()
        for key in keys:
            self.assertIn(key, entries, f"{key} was lost: {entries}")


CASES = {
    "ConcurrentRevokeTests.test_concurrent_revokes_all_survive": "token.revoke-concurrent",
    "TokenCreateTests.test_create_prints_a_token": "token.create",
    "TokenDaysTests.test_duration_in_days": "token.days",
    "TokenLabelTests.test_labelled_token_authenticates": "token.label",
    "TokenRevokeTests.test_revoke_writes_the_key": "token.revoke",
    "TokenRevokeTests.test_revoke_with_a_ttl_records_an_expiry": "token.revoke-ttl",
    "TokenCreateClearsRevocationTests.test_creating_a_token_unrevokes_the_user": "token.create-clears-revocation",
    "HelpTests.test_help_lists_the_subcommands": "cli.help",
    "DigestUsageTests.test_digest_usage_block": "cli.digest-usage",
    "ConfigUsageTests.test_config_usage_block": "cli.config-usage",
    "TokenUsageTests.test_token_usage_block": "cli.token-usage",
}
