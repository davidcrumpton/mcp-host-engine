# Issues requiring fixes

## ✅ Fixed Issues

### Logging

1. ✅ **Detailed Logs** - FIXED

The logging format now includes:

- Log level (e.g., [INFO], [WARN], [DEBUG])
- Identity (username or "-" if not authenticated)
- SessionID (or "-" if no session)
- Full message with context

Format: `2026-Jun-21 18:08:54 [INFO] bear WNV4OIG2LT5HY6JO4ISJHEKE33 - <message>`

This is now consistent across all logging calls in the HTTP transport, plugin execution, and HTTP client operations.

2. ✅ **Expired or revoked tokens** - FIXED

Token validation events are logged:

- Successful token validation: `Token validation successful`
- Failed validation: `Token validation failed: <error details>`
- Legacy token usage: `Using legacy bearer token`

Error messages include details about expiry or revocation status.

3. host.http.* fixed timeout issue

Fix `host.http.*` so the timeout is configurable.

### Plugin integrity

4. ✅ **Checksum enforcement for plugins** - FIXED

- Per-plugin `checksum:` (`sha256:`/`sha512:`/bare hex), verified against the exact bytes handed to goja.
- `plugin_checksum_manifest` for whole-directory coverage, including `require()`d modules read at call time.
- `require_plugin_checksums` to make a declared checksum mandatory.
- `mcphe digest [-format text|yaml|json|manifest] [-algo sha256|sha512]` to generate them, and `mcphe digest -verify` as a CI/pre-deploy gate.
- Mismatch is a startup failure (exit 1), exactly like a missing plugin directory.

## Upcoming Features

### Plugin integrity: cover config/metadata, not just file bytes

**Current limitation.** `checksum:` and `plugin_checksum_manifest` digest the plugin's `.js` bytes only. The config entry that actually grants the plugin its powers — `options`, `allowed_domains`, `allowed_read_file_paths`, `allowed_write_file_paths`, `allowed_commands`, `allowed_env_vars`, `allowed_http_methods`, `enabled`, and the `plugin:` alias — is **not** part of any digest. An edit to `config.yaml` can therefore widen a checksum-verified plugin's reach (new domain, new command, new file path, swapped credentials) without changing one byte of the file the checksum pins.

**Workaround today.** Pin the whole config file: `-config-checksum` / `require_config_checksum` (see README → Config Integrity). That covers the metadata, but it invalidates on any unrelated edit to the file and is a hygiene control rather than a boundary, since whoever edits the config can clear the flag.

**Proposed fix.** Extend a plugin's identity from "these bytes" to "these bytes **plus** the permissions they were reviewed with" — e.g. a digest over the plugin's *resolved* config (after `env://`/`file://`/`vault://` resolution), with secret-bearing values excluded or hashed by reference. Enforcement would sit where `checksumPolicy.verifyEntry` sits now, so a mismatch is still a startup failure.

**Open design questions.**

- What exactly counts as metadata: the whole resolved `plugins.<name>` block, or an explicit list of security-relevant keys? The latter is stable against cosmetic config changes but needs maintaining as new keys are added.
- How to keep `vault://`/`env://`-sourced secrets out of the hash, so rotating a credential does not force a re-pin, while still detecting that the *source* changed.
- Where the expected value lives: alongside `checksum:` as e.g. `config_checksum:`, or as manifest entries under a separate key space so `mcphe digest -format manifest` stays coreutils-compatible.
- Whether `mcphe digest -verify` should report config drift as a third status, distinct from `mismatch` (file changed) and `unchecked` (nothing declared).

Until this lands, the documented scope is deliberate: digests cover file bytes, and the README says so.
