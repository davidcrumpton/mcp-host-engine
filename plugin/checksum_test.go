package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphe/config"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sha256Empty is the well-known sha256 of the empty byte string; used to pin the
// hashing path to a value that cannot drift with a refactor.
const sha256Empty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// pluginsMap builds a Config whose plugins map holds the given entries.
func pluginsMap(entries map[string]map[string]interface{}) config.Config {
	return config.Config{Verbosity: 0, Plugins: entries}
}

// digestOf returns the "sha256:<hex>" checksum of a file, failing the test if it
// cannot be read.
func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return HashBytes(data).String()
}

// writeFile writes content to dir/name, creating parent directories.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// schemaJS returns a minimal plugin with an inputSchema (the MCP SDK panics on a
// nil schema, so every test plugin carries one).
func schemaJS(name, body string) string {
	return `module.exports = { name: "` + name + `", description: "test", ` +
		`inputSchema: {type:"object"}, call: function(args){ ` + body + ` } };`
}

// ---------------------------------------------------------------------------
// ParseChecksum
// ---------------------------------------------------------------------------

func TestParseChecksum(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	hex128 := strings.Repeat("cd", 64)

	tests := []struct {
		name    string
		raw     interface{}
		want    string
		wantErr bool
	}{
		{name: "sha256 prefixed", raw: "sha256:" + hex64, want: "sha256:" + hex64},
		{name: "bare 64 hex is sha256", raw: hex64, want: "sha256:" + hex64},
		{name: "sha512 prefixed", raw: "sha512:" + hex128, want: "sha512:" + hex128},
		{name: "bare 128 hex is sha512", raw: hex128, want: "sha512:" + hex128},
		{name: "uppercase normalised", raw: "SHA256:" + strings.ToUpper(hex64), want: "sha256:" + hex64},
		{name: "whitespace trimmed", raw: "  sha256:" + hex64 + "\n", want: "sha256:" + hex64},
		{name: "unknown algorithm", raw: "md5:" + hex64, wantErr: true},
		{name: "sha256 with 128 hex", raw: "sha256:" + hex128, wantErr: true},
		{name: "bare wrong length", raw: "abc123", wantErr: true},
		{name: "not hex", raw: "sha256:" + strings.Repeat("zz", 32), wantErr: true},
		{name: "empty", raw: "", wantErr: true},
		{name: "non-string", raw: 12345, wantErr: true},
		{name: "nil", raw: nil, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseChecksum(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tc.want {
				t.Errorf("got %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestChecksumMatches(t *testing.T) {
	if got := HashBytes(nil).Hex; got != sha256Empty {
		t.Fatalf("sha256 of empty input = %s, want %s", got, sha256Empty)
	}
	c, err := ParseChecksum("sha256:" + sha256Empty)
	if err != nil {
		t.Fatalf("ParseChecksum: %v", err)
	}
	if !c.Matches(nil) {
		t.Error("empty checksum should match empty content")
	}
	if c.Matches([]byte("x")) {
		t.Error("empty checksum should not match non-empty content")
	}
	// A sha512 checksum must be compared with sha512, not sha256.
	s512, err := ParseChecksum("sha512:" + strings.Repeat("00", 64))
	if err != nil {
		t.Fatalf("ParseChecksum(sha512): %v", err)
	}
	if s512.Matches(nil) {
		t.Error("bogus sha512 checksum should not match")
	}
	if got := hashWith(ChecksumSHA512, nil); got.Algorithm != ChecksumSHA512 || len(got.Hex) != 128 {
		t.Errorf("hashWith(sha512) = %+v, want 128 hex chars", got)
	}
}

func TestVerifyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.js", "hello")
	want, err := ParseChecksum(digestOf(t, path))
	if err != nil {
		t.Fatalf("ParseChecksum: %v", err)
	}
	if err := VerifyFile(path, want); err != nil {
		t.Fatalf("VerifyFile on matching file: %v", err)
	}
	other := writeFile(t, dir, "b.js", "goodbye")
	if err := VerifyFile(other, want); err == nil {
		t.Fatal("expected mismatch error for different content")
	}
	if err := VerifyFile(filepath.Join(dir, "missing.js"), want); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// Manifest
// ---------------------------------------------------------------------------

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	hex64 := strings.Repeat("ab", 32)
	hex128 := strings.Repeat("cd", 64)
	manifest := strings.Join([]string{
		"# generated by mcphe digest",
		"",
		hex64 + "  ping.js",
		hex64 + " *lib/helper.js",
		"sha512:" + hex128 + "  lib/deep/other.js",
		"",
	}, "\n")
	path := writeFile(t, dir, "checksums.txt", manifest)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Len() != 3 {
		t.Fatalf("got %d entries, want 3: %v", m.Len(), m.Entries)
	}
	for _, key := range []string{"ping.js", "lib/helper.js", "lib/deep/other.js"} {
		if _, ok := m.Entries[key]; !ok {
			t.Errorf("missing entry %q in %v", key, m.Entries)
		}
	}
	if got := m.Entries["ping.js"]; got.Algorithm != ChecksumSHA256 || got.Hex != hex64 {
		t.Errorf("ping.js = %+v", got)
	}
	if got := m.Entries["lib/deep/other.js"]; got.Algorithm != ChecksumSHA512 {
		t.Errorf("lib/deep/other.js algorithm = %q, want sha512", got.Algorithm)
	}
}

func TestLoadManifestErrors(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: "# only a comment\n\n"},
		{name: "missing path", content: hex64 + "\n"},
		{name: "bad digest", content: "not-a-digest  ping.js\n"},
		{name: "missing name after marker", content: hex64 + "  \n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeFile(t, dir, "checksums.txt", tc.content)
			if _, err := LoadManifest(path); err == nil {
				t.Fatalf("expected error for %q", tc.content)
			}
		})
	}
	if _, err := LoadManifest(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Fatal("expected error for missing manifest file")
	}
}

func TestManifestVerify(t *testing.T) {
	dir := t.TempDir()
	content := []byte("module.exports = {};")
	path := writeFile(t, dir, filepath.Join("lib", "helper.js"), string(content))
	sum := HashBytes(content)

	m := &Manifest{Path: "checksums.txt", Entries: map[string]Checksum{"lib/helper.js": sum}}
	if err := m.Verify(dir, path, content); err != nil {
		t.Fatalf("Verify on matching file: %v", err)
	}
	if err := m.Verify(dir, path, []byte("tampered")); err == nil {
		t.Fatal("expected mismatch error")
	}
	unlisted := writeFile(t, dir, "other.js", "x")
	if err := m.Verify(dir, unlisted, []byte("x")); err == nil {
		t.Fatal("expected error for file not listed in manifest")
	}
	// A nil manifest verifies nothing, so plugins keep loading with no manifest
	// configured.
	var nilManifest *Manifest
	if err := nilManifest.Verify(dir, path, content); err != nil {
		t.Fatalf("nil manifest should be a no-op, got %v", err)
	}
	if nilManifest.Len() != 0 {
		t.Error("nil manifest should report 0 entries")
	}
}

// ---------------------------------------------------------------------------
// Startup enforcement
// ---------------------------------------------------------------------------

func TestLoadPluginsChecksumMatches(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"checksum": digestOf(t, path)},
	})
	cfg.PluginDir = dir

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins with a matching checksum: %v", err)
	}
	if len(pm.plugins) != 1 {
		t.Fatalf("got %d plugins, want 1", len(pm.plugins))
	}
}

func TestLoadPluginsChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"checksum": "sha256:" + strings.Repeat("00", 32)},
	})
	cfg.PluginDir = dir

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected a checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should mention the mismatch, got: %v", err)
	}
	if !strings.Contains(err.Error(), "config expects") {
		t.Errorf("error should say what was expected, got: %v", err)
	}
}

func TestLoadPluginsChecksumMismatchTampered(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"checksum": digestOf(t, path)},
	})
	cfg.PluginDir = dir

	// Tamper after the checksum was taken, which is the attack this defends
	// against.
	if err := os.WriteFile(path, []byte(schemaJS("ping", `return "pwned";`)), 0o644); err != nil {
		t.Fatalf("rewriting plugin: %v", err)
	}
	if _, err := LoadPlugins(cfg); err == nil {
		t.Fatal("expected a checksum mismatch error after tampering")
	}
}

func TestLoadPluginsBareHexChecksum(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	bare := strings.TrimPrefix(digestOf(t, path), "sha256:")
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"checksum": bare},
	})
	cfg.PluginDir = dir

	if _, err := LoadPlugins(cfg); err != nil {
		t.Fatalf("bare-hex checksum should be accepted: %v", err)
	}
}

func TestLoadPluginsRequireChecksums(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))

	t.Run("missing checksum is fatal", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
		cfg.PluginDir = dir
		cfg.RequirePluginChecksums = true
		_, err := LoadPlugins(cfg)
		if err == nil {
			t.Fatal("expected an error when a loaded plugin declares no checksum")
		}
		if !strings.Contains(err.Error(), "no checksum configured") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("declared checksum is accepted", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{
			"ping": {"checksum": digestOf(t, path)},
		})
		cfg.PluginDir = dir
		cfg.RequirePluginChecksums = true
		if _, err := LoadPlugins(cfg); err != nil {
			t.Fatalf("LoadPlugins: %v", err)
		}
	})

	t.Run("disabled plugin is exempt", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{
			"ping": {"enabled": false},
		})
		cfg.PluginDir = dir
		cfg.RequirePluginChecksums = true
		if _, err := LoadPlugins(cfg); err != nil {
			t.Fatalf("disabled plugin should not be required to declare a checksum: %v", err)
		}
	})

	t.Run("off by default", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
		cfg.PluginDir = dir
		if _, err := LoadPlugins(cfg); err != nil {
			t.Fatalf("checksums must stay optional by default: %v", err)
		}
	})
}

func TestLoadPluginsBadChecksumFormat(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"checksum": "md5:d41d8cd98f00b204e9800998ecf8427e"},
	})
	cfg.PluginDir = dir

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected an error for an unsupported algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported algorithm") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadPluginsBadChecksumFormatDisabled(t *testing.T) {
	// A malformed checksum is a config bug even on a disabled entry: it would
	// otherwise lie dormant until the plugin is re-enabled.
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"enabled": false, "checksum": "not-a-checksum"},
	})
	cfg.PluginDir = dir

	if _, err := LoadPlugins(cfg); err == nil {
		t.Fatal("expected an error for a malformed checksum on a disabled plugin")
	}
}

func TestLoadPluginsAliasedInstanceInheritsChecksum(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "base", schemaJS("base", `return "base";`))

	t.Run("checksum declared on the source entry", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{
			"base": {"checksum": digestOf(t, path)},
			"prod": {"plugin": "base"},
		})
		cfg.PluginDir = dir
		pm, err := LoadPlugins(cfg)
		if err != nil {
			t.Fatalf("alias should inherit the module checksum: %v", err)
		}
		if len(pm.plugins) != 2 {
			t.Fatalf("got %d plugins, want 2", len(pm.plugins))
		}
	})

	t.Run("checksum declared on the alias", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{
			"base": {"enabled": false},
			"prod": {"plugin": "base", "checksum": digestOf(t, path)},
		})
		cfg.PluginDir = dir
		if _, err := LoadPlugins(cfg); err != nil {
			t.Fatalf("LoadPlugins: %v", err)
		}
	})

	t.Run("alias with a bad checksum fails", func(t *testing.T) {
		cfg := pluginsMap(map[string]map[string]interface{}{
			"base": {"enabled": false},
			"prod": {"plugin": "base", "checksum": "sha256:" + strings.Repeat("11", 32)},
		})
		cfg.PluginDir = dir
		if _, err := LoadPlugins(cfg); err == nil {
			t.Fatal("expected a checksum mismatch for the aliased instance")
		}
	})
}

func TestLoadPluginsConflictingChecksums(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "base", schemaJS("base", `return "base";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"base": {"checksum": digestOf(t, path)},
		"prod": {"plugin": "base", "checksum": "sha256:" + strings.Repeat("22", 32)},
	})
	cfg.PluginDir = dir

	_, err := LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected an error when two entries disagree about one module")
	}
	if !strings.Contains(err.Error(), "same checksum") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Manifest enforcement, including require()'d modules
// ---------------------------------------------------------------------------

func TestLoadPluginsManifestCoversEntryAndRequiredFiles(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	writeFile(t, dir, filepath.Join("lib", "helper.js"),
		`module.exports = { greet: function(){ return "hi"; } };`)
	writePlugin(t, dir, "user", schemaJS("user", `return require("./lib/helper.js").greet();`))

	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	manifestPath := writeFile(t, dir, "checksums.txt", FormatDigestManifest(entries))

	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}, "user": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = manifestPath

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins with a matching manifest: %v", err)
	}
	got, err := pm.CallTool(context.Background(), "user", json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got != "hi" {
		t.Errorf("CallTool returned %v, want %q", got, "hi")
	}
}

func TestLoadPluginsManifestRejectsTamperedEntry(t *testing.T) {
	dir := t.TempDir()
	path := writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	manifestPath := writeFile(t, dir, "checksums.txt", FormatDigestManifest(entries))

	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = manifestPath

	if err := os.WriteFile(path, []byte(schemaJS("ping", `return "pwned";`)), 0o644); err != nil {
		t.Fatalf("rewriting plugin: %v", err)
	}
	if _, err := LoadPlugins(cfg); err == nil {
		t.Fatal("expected startup to fail for a tampered entry file")
	}
}

func TestLoadPluginsManifestRejectsUnlistedFile(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	manifestPath := writeFile(t, dir, "checksums.txt", FormatDigestManifest(entries))
	// A plugin dropped in after the manifest was generated.
	writePlugin(t, dir, "sneaky", schemaJS("sneaky", `return "x";`))

	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = manifestPath

	_, err = LoadPlugins(cfg)
	if err == nil {
		t.Fatal("expected an error for a file missing from the manifest")
	}
	if !strings.Contains(err.Error(), "not listed in checksum manifest") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolManifestRejectsTamperedRequiredModule(t *testing.T) {
	// The entry file is verified at startup, but require() reads other files at
	// call time -- the gap the manifest exists to close.
	dir := t.TempDir()
	writePlugin(t, dir, "user", schemaJS("user", `return require("./lib/helper.js").greet();`))
	libPath := writeFile(t, dir, filepath.Join("lib", "helper.js"),
		`module.exports = { greet: function(){ return "hi"; } };`)

	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	manifestPath := writeFile(t, dir, "checksums.txt", FormatDigestManifest(entries))

	cfg := pluginsMap(map[string]map[string]interface{}{"user": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = manifestPath

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}

	// Tamper with the library after startup, then call the tool.
	if err := os.WriteFile(libPath, []byte(`module.exports = { greet: function(){ return "pwned"; } };`), 0o644); err != nil {
		t.Fatalf("rewriting lib: %v", err)
	}
	if _, err := pm.CallTool(context.Background(), "user", json.RawMessage(`{}`), cfg); err == nil {
		t.Fatal("expected CallTool to fail for a tampered required module")
	} else if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCallToolWithoutManifestAllowsRequiredModules(t *testing.T) {
	// No manifest configured: require() behaves exactly as before, so existing
	// deployments are unaffected.
	dir := t.TempDir()
	writePlugin(t, dir, "user", schemaJS("user", `return require("./lib/helper.js").greet();`))
	writeFile(t, dir, filepath.Join("lib", "helper.js"),
		`module.exports = { greet: function(){ return "hi"; } };`)

	cfg := pluginsMap(map[string]map[string]interface{}{"user": {}})
	cfg.PluginDir = dir

	pm, err := LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	got, err := pm.CallTool(context.Background(), "user", json.RawMessage(`{}`), cfg)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got != "hi" {
		t.Errorf("CallTool returned %v, want %q", got, "hi")
	}
}

func TestLoadPluginsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "pong";`))
	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = filepath.Join(dir, "no-such-manifest.txt")

	if _, err := LoadPlugins(cfg); err == nil {
		t.Fatal("expected an error when the configured manifest cannot be read")
	}
}

// ---------------------------------------------------------------------------
// VerifyConfiguredChecksums
// ---------------------------------------------------------------------------

func TestVerifyConfiguredChecksums(t *testing.T) {
	dir := t.TempDir()
	good := writePlugin(t, dir, "good", schemaJS("good", `return "ok";`))
	bad := writePlugin(t, dir, "bad", schemaJS("bad", `return "ok";`))
	writePlugin(t, dir, "bare", schemaJS("bare", `return "ok";`))

	cfg := pluginsMap(map[string]map[string]interface{}{
		"good": {"checksum": digestOf(t, good)},
		"bad":  {"checksum": "sha256:" + strings.Repeat("33", 32)},
		"bare": {},
	})
	cfg.PluginDir = dir

	reports, err := VerifyConfiguredChecksums(cfg)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	byPlugin := map[string]ChecksumReport{}
	for _, r := range reports {
		byPlugin[r.Plugin] = r
	}
	if got := byPlugin["good"].Status; got != ChecksumStatusOK {
		t.Errorf("good status = %q, want %q", got, ChecksumStatusOK)
	}
	if got := byPlugin["bad"].Status; got != ChecksumStatusMismatch {
		t.Errorf("bad status = %q, want %q", got, ChecksumStatusMismatch)
	}
	if got := byPlugin["bare"].Status; got != ChecksumStatusUnchecked {
		t.Errorf("bare status = %q, want %q", got, ChecksumStatusUnchecked)
	}
	if !byPlugin["bad"].Failed() || byPlugin["good"].Failed() {
		t.Error("Failed() should report only the mismatch")
	}
	if byPlugin["bad"].Expected == "" || byPlugin["bad"].Actual == "" {
		t.Error("a mismatch should report both the expected and actual digest")
	}
	_ = bad
}

func TestVerifyConfiguredChecksumsRequiredAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "ok";`))

	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
	cfg.PluginDir = dir
	cfg.RequirePluginChecksums = true
	reports, err := VerifyConfiguredChecksums(cfg)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	if len(reports) != 1 || reports[0].Status != ChecksumStatusMissing {
		t.Fatalf("got %+v, want a single missing report", reports)
	}

	// A declared checksum whose file is gone is also a failure.
	cfg2 := pluginsMap(map[string]map[string]interface{}{
		"gone": {"checksum": "sha256:" + strings.Repeat("44", 32)},
	})
	cfg2.PluginDir = dir
	reports2, err := VerifyConfiguredChecksums(cfg2)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	if len(reports2) != 1 || reports2[0].Status != ChecksumStatusMissing {
		t.Fatalf("got %+v, want a single missing report", reports2)
	}
}

func TestVerifyConfiguredChecksumsCoversManifest(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "ok";`))
	writeFile(t, dir, filepath.Join("lib", "helper.js"), `module.exports = {};`)

	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	manifestPath := writeFile(t, dir, "checksums.txt", FormatDigestManifest(entries))

	cfg := pluginsMap(map[string]map[string]interface{}{"ping": {}})
	cfg.PluginDir = dir
	cfg.PluginChecksumManifest = manifestPath

	reports, err := VerifyConfiguredChecksums(cfg)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	var manifestRows int
	for _, r := range reports {
		if r.Plugin == "-" {
			manifestRows++
			if r.Status != ChecksumStatusOK {
				t.Errorf("manifest row %s = %q", r.File, r.Status)
			}
		}
	}
	if manifestRows != 2 {
		t.Errorf("got %d manifest rows, want 2 (entry + lib)", manifestRows)
	}

	// Tampering with a lib file is caught by the manifest rows.
	if err := os.WriteFile(filepath.Join(dir, "lib", "helper.js"), []byte("tampered"), 0o644); err != nil {
		t.Fatalf("rewriting lib: %v", err)
	}
	reports, err = VerifyConfiguredChecksums(cfg)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	var failed int
	for _, r := range reports {
		if r.Failed() {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("got %d failures, want 1: %+v", failed, reports)
	}
}

func TestVerifyConfiguredChecksumsSkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "ping", schemaJS("ping", `return "ok";`))
	cfg := pluginsMap(map[string]map[string]interface{}{
		"ping": {"enabled": false, "checksum": "sha256:" + strings.Repeat("55", 32)},
	})
	cfg.PluginDir = dir
	reports, err := VerifyConfiguredChecksums(cfg)
	if err != nil {
		t.Fatalf("VerifyConfiguredChecksums: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("disabled plugins should not be verified, got %+v", reports)
	}
}

// ---------------------------------------------------------------------------
// Digest generation
// ---------------------------------------------------------------------------

func TestDetectPluginName(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "esbuild const", content: "const plugin = {\n  name: \"ping\",\n  call(){}\n};", want: "ping"},
		{name: "inline object", content: `module.exports = { name: "date_time", call(){} };`, want: "date_time"},
		{name: "no space", content: `module.exports = {name:"tight"};`, want: "tight"},
		{name: "description before name", content: "var o = { description: \"x\", name: \"after\" };", want: "after"},
		{name: "not a name key", content: `var o = { fileName: "nope" };`, want: ""},
		{name: "module.exports.name", content: `module.exports.name = "assigned";`, want: ""},
		{name: "absent", content: "var x = 1;", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeFile(t, dir, strings.ReplaceAll(tc.name, " ", "_")+".js", tc.content)
			if got := DetectPluginName(path); got != tc.want {
				t.Errorf("DetectPluginName = %q, want %q", got, tc.want)
			}
		})
	}
	if got := DetectPluginName(filepath.Join(dir, "missing.js")); got != "" {
		t.Errorf("missing file should yield \"\", got %q", got)
	}
}

func TestDigestDirIsRecursiveAndSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "b.js", "b")
	writeFile(t, dir, "a.js", "a")
	writeFile(t, dir, filepath.Join("lib", "c.js"), "c")
	writeFile(t, dir, "notes.txt", "ignored")

	entries, err := DigestDir(dir, ChecksumSHA256)
	if err != nil {
		t.Fatalf("DigestDir: %v", err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	want := []string{"a.js", "b.js", "lib/c.js"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", paths, want)
	}
	if entries[0].Checksum != HashBytes([]byte("a")).String() {
		t.Errorf("a.js checksum = %s", entries[0].Checksum)
	}
	if _, err := DigestDir(filepath.Join(dir, "nope"), ChecksumSHA256); err == nil {
		t.Error("expected an error for a missing directory")
	}
}

func TestDigestPaths(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.js", "a")
	b := writeFile(t, dir, filepath.Join("lib", "b.js"), "b")

	entries, err := DigestPaths(dir, ChecksumSHA512, []string{a, b})
	if err != nil {
		t.Fatalf("DigestPaths: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Checksum, "sha512:") {
			t.Errorf("%s checksum = %s, want a sha512 digest", e.Path, e.Checksum)
		}
	}
	if entries[1].Path != "lib/b.js" {
		t.Errorf("path %q should be relative to the plugin dir", entries[1].Path)
	}
	if _, err := DigestPaths(dir, ChecksumSHA256, []string{filepath.Join(dir, "missing.js")}); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestFormatDigestOutputs(t *testing.T) {
	entries := []DigestEntry{
		{Path: "date_time.js", Name: "get_datetime", Checksum: "sha256:aaa"},
		{Path: "ping.js", Name: "ping", Checksum: "sha256:bbb"},
		{Path: "lib/helper.js", Name: "", Checksum: "sha256:ccc"},
		{Path: "ping_copy.js", Name: "ping", Checksum: "sha256:ddd"},
	}

	text := FormatDigestText(entries)
	if !strings.Contains(text, "sha256:aaa  date_time.js") {
		t.Errorf("text output: %q", text)
	}

	manifest := FormatDigestManifest(entries)
	if !strings.Contains(manifest, "aaa  date_time.js") || strings.Contains(manifest, "sha256:") {
		t.Errorf("manifest output should be coreutils-style: %q", manifest)
	}

	// The YAML block must key on the exported name (config.yaml's key), not the
	// file name, and must stay parseable when two files export the same name.
	yamlOut := FormatDigestYAML(entries)
	for _, want := range []string{
		"plugins:",
		"  get_datetime:",
		"    # date_time.js",
		`    checksum: "sha256:aaa"`,
		"  ping:",
		"  ping_2:",
		"  helper:", // file-stem fallback when no name is detectable
	} {
		if !strings.Contains(yamlOut, want) {
			t.Errorf("yaml output missing %q:\n%s", want, yamlOut)
		}
	}

	jsonOut, err := FormatDigestJSON(entries)
	if err != nil {
		t.Fatalf("FormatDigestJSON: %v", err)
	}
	var decoded map[string]DigestEntry
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("json output is not valid JSON: %v\n%s", err, jsonOut)
	}
	if decoded["ping.js"].Checksum != "sha256:bbb" {
		t.Errorf("json entry = %+v", decoded["ping.js"])
	}
}
