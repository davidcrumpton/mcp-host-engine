// configguard_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphe/integrity"
)

// writeConfigFile writes content to a temp file and returns its path.
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

// checksumOf returns the canonical checksum of a file's bytes.
func checksumOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return integrity.HashBytes(data).String()
}

const minimalConfig = "transport: http\nport: \"8001\"\nplugins:\n  ping:\n"

// The happy path: the supplied digest matches, the config loads, and the values
// come from the file rather than the defaults.
func TestGuardLoadVerified(t *testing.T) {
	path := writeConfigFile(t, minimalConfig)
	guard := configGuard{expected: checksumOf(t, path)}

	cfg, err := guard.load(path, false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != "8001" {
		t.Errorf("port = %q, want 8001", cfg.Port)
	}
	if _, ok := cfg.Plugins["ping"]; !ok {
		t.Errorf("plugins = %v, want a ping entry", cfg.Plugins)
	}
}

// A file that does not match must not be loaded at all: the error has to name
// both digests, because that is the only way an operator can tell a stale pin
// from a tampered file.
func TestGuardLoadMismatch(t *testing.T) {
	path := writeConfigFile(t, minimalConfig)
	// Digest of different content, so the check fails.
	other := writeConfigFile(t, minimalConfig+"verbosity_level: 3\n")
	guard := configGuard{expected: checksumOf(t, other)}

	_, err := guard.load(path, false)
	if err == nil {
		t.Fatal("expected a mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should say the checksum mismatched: %v", err)
	}
	if !strings.Contains(err.Error(), "expected") || !strings.Contains(err.Error(), "got") {
		t.Errorf("error should show both digests: %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file: %v", err)
	}
}

// require_config_checksum is the hygiene switch: with it set, running without a
// supplied digest is a startup failure, not a silent pass.
func TestGuardLoadRequiresChecksumFromConfig(t *testing.T) {
	path := writeConfigFile(t, minimalConfig+"require_config_checksum: true\n")

	_, err := configGuard{}.load(path, false)
	if err == nil {
		t.Fatal("expected the gate to refuse an unverified config")
	}
	if !strings.Contains(err.Error(), "require_config_checksum") {
		t.Errorf("error should name the setting: %v", err)
	}
	if !strings.Contains(err.Error(), "-config-checksum") {
		t.Errorf("error should say how to fix it: %v", err)
	}

	// The same file passes once the digest is supplied.
	if _, err := (configGuard{expected: checksumOf(t, path)}).load(path, false); err != nil {
		t.Fatalf("verified load should succeed: %v", err)
	}
}

// The command-line switch enforces the same thing without trusting the file to
// say so, which is what a service unit should pin.
func TestGuardLoadRequiresChecksumFromFlag(t *testing.T) {
	path := writeConfigFile(t, minimalConfig) // config says nothing about it

	_, err := configGuard{required: true}.load(path, false)
	if err == nil {
		t.Fatal("expected -require-config-checksum to refuse an unverified config")
	}

	if _, err := (configGuard{required: true, expected: checksumOf(t, path)}).load(path, false); err != nil {
		t.Fatalf("verified load should succeed: %v", err)
	}
}

// A config that says nothing and an operator who asks for nothing keeps working
// exactly as before -- the gate is opt-in.
func TestGuardLoadUnverifiedByDefault(t *testing.T) {
	path := writeConfigFile(t, minimalConfig)
	cfg, err := configGuard{}.load(path, false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != "8001" {
		t.Errorf("port = %q, want 8001", cfg.Port)
	}
}

// A bare `mcphe` with no config file runs on the built-in defaults, but not when
// a checksum was demanded: there would be nothing to verify.
func TestGuardLoadMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := configGuard{}.load(missing, true)
	if err != nil {
		t.Fatalf("load with defaults: %v", err)
	}
	if cfg.Port != "8001" {
		t.Errorf("port = %q, want the built-in default", cfg.Port)
	}

	for _, guard := range []configGuard{
		{expected: "sha256:" + strings.Repeat("ab", 32)},
		{required: true},
	} {
		if _, err := guard.load(missing, true); err == nil {
			t.Errorf("guard %+v should refuse a missing config file", guard)
		}
	}

	// Without allowMissing a missing file is an error even with no gate.
	if _, err := (configGuard{}).load(missing, false); err == nil {
		t.Error("expected an error for a missing config file")
	}
}

// The expected digest may come from the environment or a file, so it does not
// have to be duplicated in the service unit.
func TestGuardLoadExpectedFromEnvAndFile(t *testing.T) {
	path := writeConfigFile(t, minimalConfig)
	sum := checksumOf(t, path)

	t.Setenv("MCPHE_CONFIG_SHA256", sum)
	if _, err := (configGuard{expected: "env://MCPHE_CONFIG_SHA256"}).load(path, false); err != nil {
		t.Fatalf("env:// reference: %v", err)
	}

	sumFile := filepath.Join(t.TempDir(), "config.sha256")
	if err := os.WriteFile(sumFile, []byte(sum+"\n"), 0o644); err != nil {
		t.Fatalf("writing checksum file: %v", err)
	}
	if _, err := (configGuard{expected: "file://" + sumFile}).load(path, false); err != nil {
		t.Fatalf("file:// reference: %v", err)
	}

	// A reference that cannot be resolved is an error, not a silent skip.
	if _, err := (configGuard{expected: "env://MCPHE_NOT_SET_ANYWHERE"}).load(path, false); err == nil {
		t.Error("expected an error for an unset env:// reference")
	}
}

func TestParseExpectedChecksum(t *testing.T) {
	sum := integrity.HashBytes([]byte("hello"))
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "prefixed", raw: sum.String(), want: sum.String()},
		{name: "bare hex", raw: sum.Hex, want: sum.String()},
		{name: "surrounding space", raw: "  " + sum.String() + "\n", want: sum.String()},
		{name: "sha512", raw: "sha512:" + strings.Repeat("ab", 64), want: "sha512:" + strings.Repeat("ab", 64)},
		{name: "empty", raw: "", wantErr: true},
		{name: "misspelled algorithm", raw: "sha265:" + sum.Hex, wantErr: true},
		{name: "short digest", raw: "sha256:abc", wantErr: true},
		{name: "not hex", raw: "sha256:" + strings.Repeat("zz", 32), wantErr: true},
		// A manifest-style file handed to file:// parses as "<hex>  <path>",
		// which is not a checksum -- better an error than a wrong value.
		{name: "manifest content", raw: sum.Hex + "  config.yaml", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExpectedChecksum(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExpectedChecksum(%q): %v", tc.raw, err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// The digest covers the file as deployed, not the resolved config: env:// and
// vault:// references stay as written, so the pin does not change when a secret
// does.
func TestGuardLoadDigestCoversRawBytes(t *testing.T) {
	path := writeConfigFile(t, "transport: http\ntoken_secret: \"env://MCPHE_TEST_SECRET\"\n")
	t.Setenv("MCPHE_TEST_SECRET", "s3cret")

	guard := configGuard{expected: checksumOf(t, path)}
	cfg, err := guard.load(path, false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The value was resolved for use...
	if cfg.TokenSecret != "s3cret" {
		t.Errorf("token_secret = %q, want the resolved value", cfg.TokenSecret)
	}
	// ...but the file on disk still holds the reference, so the pin still holds.
	if checksumOf(t, path) != guard.expected {
		t.Error("checksum changed after loading; it must describe the bytes on disk")
	}
}

// A config whose bytes cannot be parsed but whose digest matches must still fail
// -- verification says "these are the bytes I expected", not "they are valid".
func TestGuardLoadVerifiedButInvalid(t *testing.T) {
	path := writeConfigFile(t, "transport: \"not a transport\"\n")
	guard := configGuard{expected: checksumOf(t, path)}

	_, err := guard.load(path, false)
	if err == nil {
		t.Fatal("expected a parse/validation error")
	}
	if strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("the digest matches; the error should be about the content: %v", err)
	}
}
