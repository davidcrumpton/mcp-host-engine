// server/bridge.go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"mcphe/config"
	"mcphe/plugin"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// marshalNoEscape encodes v as JSON without HTML-escaping &, <, and >.
// encoding/json escapes those by default ("a && b" -> "a \u0026\u0026 b").
// In an MCP pipeline the escaped form leaks into raw-arg logs and into the
// text content returned to the model, which then reproduces the literal
// escape sequence in later tool calls and breaks shell commands.
func marshalNoEscape(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// RegisterPlugins registers every enabled plugin as an MCP tool.
//
// It returns an error rather than letting a bad plugin take the process down:
// mcp.AddTool reports a malformed tool by panicking, and a plugin must not be
// able to crash the server with a nil or invalid inputSchema. Plugins are
// validated when they are loaded, so reaching the error paths here means the
// tool list was built some other way -- the guard is cheap insurance against a
// startup segfault either way.
func RegisterPlugins(server *mcp.Server, pm *plugin.PluginManager, cfg config.Config) error {
	for _, toolInfo := range pm.ListTools(cfg) {
		if err := registerToolInfo(server, toolInfo, pm, cfg); err != nil {
			cfg.Logf(1, "Cannot register tool: %v", err)
			return err
		}
	}
	return nil
}

// registerToolInfo registers one entry of the tool list returned by ListTools.
func registerToolInfo(server *mcp.Server, toolInfo map[string]interface{}, pm *plugin.PluginManager, cfg config.Config) error {
	name, _ := toolInfo["name"].(string)
	if name == "" {
		return fmt.Errorf("plugin with no name cannot be registered as a tool")
	}
	if !cfg.IsToolEnabled(name) {
		return nil
	}
	description, _ := toolInfo["description"].(string)

	inputSchema, err := schemaForSDK(toolInfo["inputSchema"])
	if err != nil {
		return fmt.Errorf("tool %q: %w", name, err)
	}

	// Create tool with the correct API
	tool := &mcp.Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Annotations: pluginAnnotationsToMCP(toolInfo["annotations"]),
	}

	// Use AddTool with generic types - we'll use "any" for both input and output
	// to pass through the raw parameters and results as-is
	if err := addTool(server, tool, name, pm, cfg); err != nil {
		return fmt.Errorf("tool %q: %w", name, err)
	}
	return nil
}

// schemaForSDK converts a plugin's inputSchema into the map the SDK expects.
//
// A nil schema, or one that is not a JSON object, becomes an empty object
// schema instead of a nil map: a nil map inside a non-nil interface is what
// made the SDK dereference a nil *jsonschema.Schema and crash.
func schemaForSDK(raw interface{}) (map[string]interface{}, error) {
	if raw == nil {
		return map[string]interface{}{"type": "object"}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshalling inputSchema: %w", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, fmt.Errorf("inputSchema must be a JSON object: %w", err)
	}
	if schema == nil {
		return map[string]interface{}{"type": "object"}, nil
	}
	if _, ok := schema["type"]; !ok {
		schema["type"] = "object"
	}
	return schema, nil
}

// addTool registers one tool, converting an SDK panic into an error.
//
// mcp.AddTool's only failure channel is panic (see its implementation: it wraps
// toolForErr's error in a panic), so recovering is the documented way to handle
// a schema the SDK rejects. Without this a single malformed plugin schema
// aborts the process with a stack trace instead of a message naming the plugin.
func addTool(server *mcp.Server, tool *mcp.Tool, name string, pm *plugin.PluginManager, cfg config.Config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("MCP SDK rejected the tool definition: %v", r)
		}
	}()

	mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
		// Convert the raw parameters to a map
		var params map[string]interface{}
		if request.Params.Arguments != nil {
			if err := json.Unmarshal(request.Params.Arguments, &params); err != nil {
				cfg.Logf(1, "Failed to unmarshal arguments for tool %s: %v", name, err)
				result := &mcp.CallToolResult{}
				result.SetError(fmt.Errorf("invalid arguments: %w", err))
				return result, nil, nil
			}
		} else {
			params = make(map[string]interface{})
		}

		// Convert params back to raw JSON for plugin call
		rawArgs, err := marshalNoEscape(params)
		if err != nil {
			result := &mcp.CallToolResult{}
			result.SetError(err)
			return result, nil, nil
		}

		result, err := pm.CallTool(ctx, name, rawArgs, cfg)
		if err != nil {
			result := &mcp.CallToolResult{}
			result.SetError(err)
			return result, nil, nil
		}

		// Convert result to SDK format
		sdkResult := ResultToSDK(result)
		return sdkResult, nil, nil
	})
	return nil
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

// pluginAnnotationsToMCP converts a PluginAnnotations value (passed as interface{}
// from ListTools) into mcp.ToolAnnotations. When the plugin declares no annotations
// the MCP spec defaults apply (destructiveHint=true, openWorldHint=true,
// readOnlyHint=false), which can be misleading for read-only tools. We therefore
// fall back to the most conservative explicit set: not destructive, open-world.
func pluginAnnotationsToMCP(raw interface{}) *mcp.ToolAnnotations {
	ann, _ := raw.(*plugin.PluginAnnotations)

	out := &mcp.ToolAnnotations{}
	hasAny := false

	if ann != nil && ann.ReadOnlyHint != nil {
		out.ReadOnlyHint = *ann.ReadOnlyHint
		hasAny = true
	}
	if ann != nil && ann.DestructiveHint != nil {
		out.DestructiveHint = ann.DestructiveHint
		hasAny = true
	} else {
		// Override the spec default of true — omit a hint that says "destructive"
		// unless the plugin explicitly opted in.
		out.DestructiveHint = boolPtr(false)
		hasAny = true
	}
	if ann != nil && ann.IdempotentHint != nil {
		out.IdempotentHint = *ann.IdempotentHint
		hasAny = true
	}
	if ann != nil && ann.OpenWorldHint != nil {
		out.OpenWorldHint = ann.OpenWorldHint
		hasAny = true
	}

	if !hasAny {
		return nil
	}
	return out
}

func ResultToSDK(value interface{}) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}

	if value == nil {
		result.Content = []mcp.Content{
			&mcp.TextContent{Text: ""},
		}
		return result
	}

	switch v := value.(type) {
	case string:
		result.Content = []mcp.Content{
			&mcp.TextContent{Text: v},
		}
	case []byte:
		result.Content = []mcp.Content{
			&mcp.TextContent{Text: string(v)},
		}
	default:
		// For other types, we'll convert to JSON. Use a non-HTML-escaping
		// encoder so ampersands and angle brackets in tool output are shown
		// verbatim to the model instead of as \u0026, \u003c, \u003e.
		jsonData, _ := marshalNoEscape(value)
		result.Content = []mcp.Content{
			&mcp.TextContent{Text: string(jsonData)},
		}
	}

	return result
}
