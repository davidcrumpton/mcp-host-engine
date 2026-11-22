// server/bridge_test.go
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphe/config"
	"mcphe/plugin"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMarshalNoEscapeKeepsAmpersands(t *testing.T) {
	got, err := marshalNoEscape(map[string]interface{}{"command": "apt-get update && apt-get install -y x"})
	if err != nil {
		t.Fatalf("marshalNoEscape: %v", err)
	}
	want := `{"command":"apt-get update && apt-get install -y x"}`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMarshalNoEscapeKeepsAngleBrackets(t *testing.T) {
	got, err := marshalNoEscape(map[string]interface{}{"code": "if (a < b && c > d)"})
	if err != nil {
		t.Fatalf("marshalNoEscape: %v", err)
	}
	for _, escaped := range []string{`\u0026`, `\u003c`, `\u003e`} {
		if strings.Contains(string(got), escaped) {
			t.Fatalf("output %s unexpectedly contains %s", got, escaped)
		}
	}
}

func TestResultToSDKDoesNotEscapeAmpersands(t *testing.T) {
	res := ResultToSDK(map[string]interface{}{"stdout": "if (a && b)"})
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	if strings.Contains(tc.Text, `\u0026`) {
		t.Fatalf("text content %q still contains escaped ampersands", tc.Text)
	}
	if tc.Text != `{"stdout":"if (a && b)"}` {
		t.Fatalf("unexpected text content: %q", tc.Text)
	}
}

func TestResultToSDKPassesStringsThrough(t *testing.T) {
	res := ResultToSDK("plain && string")
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	if tc.Text != "plain && string" {
		t.Fatalf("unexpected text content: %q", tc.Text)
	}
}

// ---------------------------------------------------------------------------
// Tool registration
// ---------------------------------------------------------------------------

// loadPluginsFrom writes each js body to dir as <name>.js and loads them.
func loadPluginsFrom(t *testing.T, dir string, plugins map[string]string) (*plugin.PluginManager, config.Config) {
	t.Helper()
	entries := make(map[string]map[string]interface{}, len(plugins))
	for name, body := range plugins {
		if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(body), 0o644); err != nil {
			t.Fatalf("writing plugin %s: %v", name, err)
		}
		entries[name] = map[string]interface{}{}
	}
	cfg := config.Config{PluginDir: dir, Plugins: entries}
	pm, err := plugin.LoadPlugins(cfg)
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	return pm, cfg
}

// A plugin with no inputSchema used to segfault the process inside mcp.AddTool.
// It must register and be callable instead.
func TestRegisterPluginsToolWithoutInputSchema(t *testing.T) {
	dir := t.TempDir()
	pm, cfg := loadPluginsFrom(t, dir, map[string]string{
		"ping": `
module.exports = {
  name: "ping",
  description: "no schema",
  call: function() { return "pong"; }
};
`,
	})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	if err := RegisterPlugins(srv, pm, cfg); err != nil {
		t.Fatalf("RegisterPlugins: %v", err)
	}

	// Round-trip through a real client to prove the tool is usable, not merely
	// registered without crashing.
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "ping" {
		t.Fatalf("ListTools returned %+v, want the ping tool", listed.Tools)
	}
	schema, ok := listed.Tools[0].InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema is %T, want a map", listed.Tools[0].InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf(`InputSchema type = %v, want "object"`, schema["type"])
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ping", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("got %d content items, want 1", len(res.Content))
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok && tc.Text != "pong" {
		t.Errorf("CallTool returned %q, want %q", tc.Text, "pong")
	}
}

// schemaForSDK must never return a nil map inside a non-nil interface: that is
// the shape that made the SDK dereference a nil *jsonschema.Schema.
func TestSchemaForSDK(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want map[string]interface{}
	}{
		{
			name: "nil becomes an empty object schema",
			raw:  nil,
			want: map[string]interface{}{"type": "object"},
		},
		{
			name: "missing type is added",
			raw:  map[string]interface{}{"properties": map[string]interface{}{}},
			want: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
		{
			name: "existing schema is passed through",
			raw:  map[string]interface{}{"type": "object", "required": []interface{}{"q"}},
			want: map[string]interface{}{"type": "object", "required": []interface{}{"q"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := schemaForSDK(tc.raw)
			if err != nil {
				t.Fatalf("schemaForSDK: %v", err)
			}
			if got == nil {
				t.Fatal("schemaForSDK returned a nil map; the SDK panics on that")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			for k, want := range tc.want {
				if _, ok := got[k]; !ok {
					t.Errorf("missing key %q in %#v", k, got)
				} else if k == "type" && got[k] != want {
					t.Errorf("type = %v, want %v", got[k], want)
				}
			}
		})
	}

	if _, err := schemaForSDK("not an object"); err == nil {
		t.Error("expected an error for a non-object schema")
	}
}

// mcp.AddTool reports a rejected schema by panicking. addTool must turn that
// into an error so a bad plugin cannot take the process down at startup.
func TestAddToolRecoversFromSDKPanic(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	// An unresolvable $ref is a schema the SDK rejects.
	tool := &mcp.Tool{
		Name:        "broken",
		InputSchema: map[string]interface{}{"type": "object", "$ref": "#/nope"},
	}

	err := addTool(srv, tool, "broken", nil, config.Config{})
	if err == nil {
		t.Fatal("expected addTool to return an error instead of panicking")
	}
	if !strings.Contains(err.Error(), "broken") && !strings.Contains(err.Error(), "SDK") {
		t.Errorf("unexpected error: %v", err)
	}
}

// A tool the SDK rejects must surface as an error from RegisterPlugins, not a
// crash, and must name the offending tool.
func TestRegisterPluginsRejectsInvalidSchema(t *testing.T) {
	dir := t.TempDir()
	pm, cfg := loadPluginsFrom(t, dir, map[string]string{
		"ping": `
module.exports = {
  name: "ping",
  description: "ok",
  inputSchema: { type: "object" },
  call: function() { return "pong"; }
};
`,
	})

	// Reach past the loader's validation to simulate a tool list built some
	// other way, which is the case the bridge guard exists for.
	tools := pm.ListTools(cfg)
	tools[0]["inputSchema"] = map[string]interface{}{"type": "object", "$ref": "#/nope"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	err := registerToolInfo(srv, tools[0], pm, cfg)
	if err == nil {
		t.Fatal("expected an error for a schema the SDK rejects")
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Errorf("error should name the tool, got: %v", err)
	}
}
