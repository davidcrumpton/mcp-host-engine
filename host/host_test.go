package host

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphe/config"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// cfgWithPaths builds a Config that allows read/write to the given dir for
// the named plugin.
func cfgWithPaths(t *testing.T, pluginName, dir string) config.Config {
	t.Helper()
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			pluginName: {
				"allowed_read_file_paths":  []interface{}{dir},
				"allowed_write_file_paths": []interface{}{dir},
			},
		},
	}
}

func cfgWithCommands(pluginName string, cmds []string) config.Config {
	iCmds := make([]interface{}, len(cmds))
	for i, c := range cmds {
		iCmds[i] = c
	}
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			pluginName: {"allowed_commands": iCmds},
		},
	}
}

func cfgWithDomains(domains []string) config.Config {
	iDomains := make([]interface{}, len(domains))
	for i, d := range domains {
		iDomains[i] = d
	}
	// HTTPGet/Post/Put/Delete all call cfg.AllowedDomainsFor("http_request_get").
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			"http_request_get": {"allowed_domains": iDomains},
		},
	}
}

// ---------------------------------------------------------------------------
// isPathAllowed (tested indirectly through ReadFile / WriteFile)
// ---------------------------------------------------------------------------

func TestIsPathAllowed_AllowedSubdir(t *testing.T) {
	dir := t.TempDir()
	allowed := isPathAllowed(filepath.Join(dir, "subfile.txt"), []string{dir})
	if !allowed {
		t.Error("path inside allowed dir should be allowed")
	}
}

func TestIsPathAllowed_NotAllowed(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	allowed := isPathAllowed(filepath.Join(other, "file.txt"), []string{dir})
	if allowed {
		t.Error("path outside allowed dir should not be allowed")
	}
}

func TestIsPathAllowed_EmptyList(t *testing.T) {
	if isPathAllowed("/tmp/foo", nil) {
		t.Error("empty allowed list should deny everything")
	}
}

// ---------------------------------------------------------------------------
// ReadFile
// ---------------------------------------------------------------------------

func TestReadFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	os.WriteFile(path, []byte("world"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	got, err := ReadFile(path, cfg, "myplugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "world" {
		t.Errorf("got %q, want %q", got, "world")
	}
}

func TestReadFile_AccessDenied(t *testing.T) {
	_, err := ReadFile("/etc/passwd", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

func TestReadFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithPaths(t, "myplugin", dir)
	_, err := ReadFile(filepath.Join(dir, "nope.txt"), cfg, "myplugin")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// WriteFile
// ---------------------------------------------------------------------------

func TestWriteFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	cfg := cfgWithPaths(t, "myplugin", dir)

	if err := WriteFile(path, "content", cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "content" {
		t.Errorf("file content: got %q, want %q", string(data), "content")
	}
}

func TestWriteFile_AccessDenied(t *testing.T) {
	err := WriteFile("/etc/passwd", "x", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

// ---------------------------------------------------------------------------
// ListFiles
// ---------------------------------------------------------------------------

func TestListFiles_Success(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	files, err := ListFiles(dir, cfg, "myplugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2", len(files))
	}
}

func TestListFiles_AccessDenied(t *testing.T) {
	_, err := ListFiles("/etc", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

// ---------------------------------------------------------------------------
// DeleteFile
// ---------------------------------------------------------------------------

func TestDeleteFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "del.txt")
	os.WriteFile(path, []byte("bye"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	if err := DeleteFile(path, cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

func TestDeleteFile_AccessDenied(t *testing.T) {
	err := DeleteFile("/etc/passwd", config.Config{}, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error")
	}
}

// ---------------------------------------------------------------------------
// RenameFile
// ---------------------------------------------------------------------------

func TestRenameFile_Success(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("data"), 0644)

	cfg := cfgWithPaths(t, "myplugin", dir)
	if err := RenameFile(src, dst, cfg, "myplugin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("destination should exist after rename")
	}
}

func TestRenameFile_AccessDeniedSrc(t *testing.T) {
	dir := t.TempDir()
	cfg := cfgWithPaths(t, "myplugin", dir)
	err := RenameFile("/etc/passwd", filepath.Join(dir, "out.txt"), cfg, "myplugin")
	if err == nil {
		t.Fatal("expected access-denied error for src outside allowed paths")
	}
}

// ---------------------------------------------------------------------------
// RunCommand
// ---------------------------------------------------------------------------

func TestRunCommand_Success(t *testing.T) {
	cfg := cfgWithCommands("myplugin", []string{"echo"})
	out, err := RunCommand(context.Background(), cfg, "myplugin", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got %q", out)
	}
}

func TestRunCommand_NotAllowed(t *testing.T) {
	cfg := cfgWithCommands("myplugin", []string{"echo"})
	_, err := RunCommand(context.Background(), cfg, "myplugin", "rm -rf /")
	if err == nil {
		t.Fatal("expected command-not-allowed error")
	}
}

func TestRunCommand_NoAllowedCommands(t *testing.T) {
	_, err := RunCommand(context.Background(), config.Config{}, "myplugin", "echo hi")
	if err == nil {
		t.Fatal("expected error when no commands configured")
	}
}

// ---------------------------------------------------------------------------
// GetEnv
// ---------------------------------------------------------------------------

func TestGetEnv_Allowed(t *testing.T) {
	os.Setenv("MY_TEST_VAR", "secret")
	defer os.Unsetenv("MY_TEST_VAR")

	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_env_vars": []interface{}{"MY_TEST_VAR"}},
		},
	}
	val := GetEnv("MY_TEST_VAR", cfg, "myplugin")
	if val != "secret" {
		t.Errorf("got %q, want %q", val, "secret")
	}
}

func TestGetEnv_NotAllowed(t *testing.T) {
	os.Setenv("SECRET", "shh")
	defer os.Unsetenv("SECRET")

	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_env_vars": []interface{}{"HOME"}},
		},
	}
	val := GetEnv("SECRET", cfg, "myplugin")
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}

func TestGetEnv_NoAllowedEnvs(t *testing.T) {
	os.Setenv("ANYTHING", "value")
	defer os.Unsetenv("ANYTHING")

	val := GetEnv("ANYTHING", config.Config{}, "myplugin")
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}

// ---------------------------------------------------------------------------
// isAllowedDomain
// ---------------------------------------------------------------------------

func TestIsAllowedDomain_Exact(t *testing.T) {
	if !isAllowedDomain("example.com", []string{"example.com"}) {
		t.Error("exact match should be allowed")
	}
}

func TestIsAllowedDomain_Subdomain(t *testing.T) {
	if !isAllowedDomain("api.example.com", []string{"example.com"}) {
		t.Error("subdomain should be allowed")
	}
}

func TestIsAllowedDomain_NoMatch(t *testing.T) {
	if isAllowedDomain("evil.com", []string{"example.com"}) {
		t.Error("non-matching domain should be denied")
	}
}

func TestIsAllowedDomain_EmptyList(t *testing.T) {
	if isAllowedDomain("example.com", nil) {
		t.Error("empty allowed list should deny everything")
	}
}

func TestIsAllowedDomain_TrailingColon(t *testing.T) {
	// url.Parse may produce "hostname:" for bare IPs without ports.
	if !isAllowedDomain("example.com:", []string{"example.com"}) {
		t.Error("trailing colon should be stripped")
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers (HTTPGet, HTTPPost, HTTPPut, HTTPDelete)
// ---------------------------------------------------------------------------

func TestHTTPGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	// srv.URL is "http://127.0.0.1:<port>" – allow just the hostname.
	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains([]string{parsed.Hostname()})

	res, err := HTTPGet(context.Background(), srv.URL, nil, cfg, "http_request_get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "hello" {
		t.Errorf("body: got %q, want %q", res["body"], "hello")
	}
}

func TestHTTPGet_BlockedDomain(t *testing.T) {
	cfg := cfgWithDomains([]string{"allowed.example.com"}) // blocked.example.com not listed
	_, err := HTTPGet(context.Background(), "http://blocked.example.com/", nil, cfg, "http_request_get")
	if err == nil {
		t.Fatal("expected blocked-domain error")
	}
}

func TestHTTPGet_InvalidURL(t *testing.T) {
	cfg := config.Config{}
	_, err := HTTPGet(context.Background(), "://bad-url", nil, cfg, "http_request_get")
	if err == nil {
		t.Fatal("expected invalid-URL error")
	}
}

func TestHTTPPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 64)
		n, _ := r.Body.Read(body)
		fmt.Fprintf(w, "got:%s", body[:n])
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains([]string{parsed.Hostname()})

	res, err := HTTPPost(context.Background(), srv.URL, nil, "payload", cfg, "http_request_get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res["body"].(string), "payload") {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPPost_BlockedDomain(t *testing.T) {
	cfg := cfgWithDomains([]string{"other.example.com"})
	_, err := HTTPPost(context.Background(), "http://blocked.com/", nil, "", cfg, "http_request_get")
	if err == nil {
		t.Fatal("expected blocked-domain error")
	}
}

func TestHTTPPut_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "put-ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains([]string{parsed.Hostname()})

	res, err := HTTPPut(context.Background(), srv.URL, nil, "data", cfg, "http_request_get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "put-ok" {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPDelete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "deleted")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains([]string{parsed.Hostname()})

	res, err := HTTPDelete(context.Background(), srv.URL, nil, "", cfg, "http_request_get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "deleted" {
		t.Errorf("body: got %q", res["body"])
	}
}

// ---------------------------------------------------------------------------
// MakeHostObject
// ---------------------------------------------------------------------------

func TestMakeHostObject_Keys(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"foo": "bar"},
		},
	}
	obj := MakeHostObject(cfg, context.Background(), "myplugin")

	expectedKeys := []string{
		"readFile", "writeFile", "listFiles", "deleteFile",
		"renameFile", "runCommand", "httpGet", "httpPost",
		"httpPut", "httpDelete", "getEnv", "config",
	}
	for _, k := range expectedKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("host object missing key %q", k)
		}
	}
}

func TestMakeHostObject_ConfigMerge(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"custom_key": "custom_val"},
		},
	}
	obj := MakeHostObject(cfg, context.Background(), "myplugin")
	pluginCfg, ok := obj["config"].(map[string]interface{})
	if !ok {
		t.Fatal("config key should be a map")
	}
	if pluginCfg["custom_key"] != "custom_val" {
		t.Errorf("custom_key not found in plugin config: %v", pluginCfg)
	}
	if _, ok := pluginCfg["mcp-version"]; !ok {
		t.Error("mcp-version should be present in plugin config")
	}
}
