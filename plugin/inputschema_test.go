package plugin

import (
	"reflect"
	"strings"
	"testing"

	"mcphe/config"
)

// A plugin that omits inputSchema used to crash the process at tool
// registration: the missing schema became a nil map inside a non-nil interface,
// which the MCP SDK dereferenced. It must instead get an empty object schema.
func TestLoadPluginMissingInputSchemaDefaultsToObject(t *testing.T) {
	pm, cfg := loadOne(t, "ping", `
module.exports = {
  name: "ping",
  description: "no schema",
  call: function() { return "pong"; }
};
`)

	p, ok := pm.byName["ping"]
	if !ok {
		t.Fatal("plugin not loaded")
	}
	want := map[string]interface{}{"type": "object"}
	if !reflect.DeepEqual(p.InputSchema, want) {
		t.Fatalf("InputSchema = %#v, want %#v", p.InputSchema, want)
	}

	// ListTools feeds the server bridge, so it has to carry the default too.
	tools := pm.ListTools(cfg)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if !reflect.DeepEqual(tools[0]["inputSchema"], want) {
		t.Errorf("ListTools inputSchema = %#v, want %#v", tools[0]["inputSchema"], want)
	}
}

func TestLoadPluginNullInputSchemaDefaultsToObject(t *testing.T) {
	pm, _ := loadOne(t, "ping", `
module.exports = {
  name: "ping",
  description: "null schema",
  inputSchema: null,
  call: function() { return "pong"; }
};
`)
	p := pm.byName["ping"]
	if !reflect.DeepEqual(p.InputSchema, map[string]interface{}{"type": "object"}) {
		t.Fatalf("InputSchema = %#v, want an empty object schema", p.InputSchema)
	}
}

// A schema with properties but no "type" is what an author would naturally
// write; the SDK requires type "object", so it is added rather than rejected.
func TestLoadPluginInputSchemaWithoutTypeGetsObject(t *testing.T) {
	pm, _ := loadOne(t, "adder", `
module.exports = {
  name: "adder",
  description: "adds",
  inputSchema: { properties: { x: { type: "number" } } },
  call: function(args) { return args.x; }
};
`)
	schema, ok := pm.byName["adder"].InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema is %T, want a map", pm.byName["adder"].InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf(`schema["type"] = %v, want "object"`, schema["type"])
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is %T, want a map", schema["properties"])
	}
	if _, ok := props["x"]; !ok {
		t.Errorf("properties lost the x parameter: %#v", props)
	}
}

func TestLoadPluginInputSchemaPreserved(t *testing.T) {
	pm, _ := loadOne(t, "searcher", `
module.exports = {
  name: "searcher",
  description: "searches",
  inputSchema: {
    type: "object",
    properties: { q: { type: "string", description: "query" } },
    required: ["q"]
  },
  call: function(args) { return args.q; }
};
`)
	schema, ok := pm.byName["searcher"].InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema is %T, want a map", pm.byName["searcher"].InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf(`type = %v, want "object"`, schema["type"])
	}
	required, ok := schema["required"].([]interface{})
	if !ok || len(required) != 1 || required[0] != "q" {
		t.Errorf("required = %#v, want [q]", schema["required"])
	}
}

// The SDK panics on a schema it does not like, so a bad one has to be caught at
// load time, where the error can name the file.
func TestLoadPluginInvalidInputSchema(t *testing.T) {
	tests := []struct {
		name      string
		schema    string
		wantInErr string
	}{
		{name: "string schema", schema: `"nonsense"`, wantInErr: "must be a JSON object"},
		{name: "number schema", schema: `42`, wantInErr: "must be a JSON object"},
		{name: "array schema", schema: `[{ type: "object" }]`, wantInErr: "must be a JSON object"},
		{name: "wrong type", schema: `{ type: "string" }`, wantInErr: `"type" must be the string "object"`},
		{name: "non-string type", schema: `{ type: 7 }`, wantInErr: `"type" must be the string "object"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writePlugin(t, dir, "bad", `
module.exports = {
  name: "bad",
  description: "bad schema",
  inputSchema: `+tc.schema+`,
  call: function() { return "x"; }
};
`)
			_, err := LoadPlugins(configForDir(t, dir, "bad"))
			if err == nil {
				t.Fatal("expected a load error for an invalid inputSchema")
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q should mention %q", err, tc.wantInErr)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q should name the offending file %q", err, path)
			}
		})
	}
}

// A non-object schema must not be accepted silently either -- the whole point is
// that it would otherwise reach the SDK and panic.
func TestLoadPluginInputSchemaArrayOfTypes(t *testing.T) {
	// JSON Schema allows a union of types, but an MCP tool's arguments are a
	// single object, and the SDK rejects the union. Fail loudly instead.
	dir := t.TempDir()
	writePlugin(t, dir, "union", `
module.exports = {
  name: "union",
  description: "union type",
  inputSchema: { type: ["object", "null"] },
  call: function() { return "x"; }
};
`)
	if _, err := LoadPlugins(configForDir(t, dir, "union")); err == nil {
		t.Fatal("expected a load error for a non-object inputSchema type")
	}
}

// configForDir builds a Config that loads exactly the named plugins from dir.
func configForDir(t *testing.T, dir string, names ...string) config.Config {
	t.Helper()
	plugins := make(map[string]map[string]interface{}, len(names))
	for _, n := range names {
		plugins[n] = map[string]interface{}{}
	}
	cfg := allEnabled(names...)
	cfg.Plugins = plugins
	cfg.PluginDir = dir
	return cfg
}
