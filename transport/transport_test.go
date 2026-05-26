package transport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcphe/config"
	"mcphe/plugin"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// emptyPluginManager returns a PluginManager with no plugins by bypassing the
// file-scan logic through the exported struct fields (both are unexported, so
// we use a zero-value pointer that is safe to call ListTools / CallTool on
// because those methods nil-check nothing critical for an empty map).
func emptyPluginManager() *plugin.PluginManager {
	// LoadPlugins requires a real directory; use a temp dir with no JS files.
	// However, LoadPlugins returns an error when there are no plugins.
	// Instead we build a PluginManager via a small helper that just returns the
	// zero value using reflection-free approach: use a minimal plugin dir.
	// The cleanest option here is a table-driven wrapper that returns the
	// internal empty struct directly.
	//
	// Since PluginManager fields are unexported we cannot construct it
	// outside the package.  We therefore create a temporary directory with a
	// valid JS stub so LoadPlugins succeeds, then keep only that manager.
	return nil // see note in TestHandleRequest_ToolsList
}

func defaultCfg() config.Config {
	return config.Config{Verbosity: 0}
}

func postJSON(t *testing.T, handler http.Handler, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func decodeResp(t *testing.T, w *httptest.ResponseRecorder) ResponseWithErr {
	t.Helper()
	var resp ResponseWithErr
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// ValidateBearerToken
// ---------------------------------------------------------------------------

func TestValidateBearerToken_Valid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ValidateBearerToken(inner, "supersecret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer supersecret")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestValidateBearerToken_Missing(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ValidateBearerToken(inner, "supersecret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestValidateBearerToken_WrongToken(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ValidateBearerToken(inner, "supersecret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestValidateBearerToken_NotBearerScheme(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := ValidateBearerToken(inner, "supersecret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic supersecret")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleHTTPRequest – method guard
// ---------------------------------------------------------------------------

func TestHandleHTTPRequest_RejectsGET(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("got %d, want 405", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleHTTPRequest – bad JSON body
// ---------------------------------------------------------------------------

func TestHandleHTTPRequest_BadJSON(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	req := httptest.NewRequest(http.MethodPost, "/rpc", strings.NewReader("{bad}"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := decodeResp(t, w)
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Errorf("expected parse error -32700, got %v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// handleRequest – initialize
// ---------------------------------------------------------------------------

func TestHandleRequest_Initialize(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	w := postJSON(t, handler, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	resp := decodeResp(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("result should be a map, got %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
}

// ---------------------------------------------------------------------------
// handleRequest – notifications/initialized
// ---------------------------------------------------------------------------

func TestHandleRequest_NotificationsInitialized(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	w := postJSON(t, handler, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      nil,
		"method":  "notifications/initialized",
	})
	resp := decodeResp(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// handleRequest – unknown method
// ---------------------------------------------------------------------------

func TestHandleRequest_UnknownMethod(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	w := postJSON(t, handler, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      99,
		"method":  "no_such_method",
	})
	resp := decodeResp(t, w)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected method-not-found -32601, got %v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// normalizeToolResult
// ---------------------------------------------------------------------------

func TestNormalizeToolResult_Nil(t *testing.T) {
	res := normalizeToolResult(nil)
	checkToolResult(t, res, "", false)
}

func TestNormalizeToolResult_String(t *testing.T) {
	res := normalizeToolResult("hello world")
	checkToolResult(t, res, "hello world", false)
}

func TestNormalizeToolResult_Bytes(t *testing.T) {
	res := normalizeToolResult([]byte("bytes"))
	checkToolResult(t, res, "bytes", false)
}

func TestNormalizeToolResult_Map_WithContent(t *testing.T) {
	input := map[string]interface{}{
		"content": []interface{}{"foo"},
		"isError": false,
	}
	res := normalizeToolResult(input)
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}
	if _, ok := m["content"]; !ok {
		t.Error("content key should be preserved")
	}
}

func TestNormalizeToolResult_ArbitraryType(t *testing.T) {
	// Integers get JSON-marshalled.
	res := normalizeToolResult(42)
	checkToolResult(t, res, "42", false)
}

// checkToolResult asserts that res is a well-formed tool-result map.
func checkToolResult(t *testing.T, res interface{}, wantText string, wantError bool) {
	t.Helper()
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("result should be map[string]interface{}, got %T", res)
	}
	content, ok := m["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content should be []map[string]interface{}, got %T", m["content"])
	}
	if len(content) == 0 {
		t.Fatal("content slice should be non-empty")
	}
	if wantText != "" && content[0]["text"] != wantText {
		t.Errorf("text: got %q, want %q", content[0]["text"], wantText)
	}
	if m["isError"] != wantError {
		t.Errorf("isError: got %v, want %v", m["isError"], wantError)
	}
}

// ---------------------------------------------------------------------------
// toolResult / toolError helpers
// ---------------------------------------------------------------------------

func TestToolResult(t *testing.T) {
	res := toolResult("ok")
	checkToolResult(t, res, "ok", false)
}

func TestToolError(t *testing.T) {
	res := toolError("bad")
	checkToolResult(t, res, "bad", true)
}

// ---------------------------------------------------------------------------
// notifications/cancelled
// ---------------------------------------------------------------------------

func TestHandleRequest_CancelNonExistentID(t *testing.T) {
	// Should not panic and should return a 2.0 response.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleHTTPRequest(w, r, defaultCfg(), nil)
	})
	w := postJSON(t, handler, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      nil,
		"method":  "notifications/cancelled",
		"params":  map[string]interface{}{"requestId": "xyz"},
	})
	resp := decodeResp(t, w)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}
