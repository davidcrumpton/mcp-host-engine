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

func RegisterPlugins(server *mcp.Server, pm *plugin.PluginManager, cfg config.Config) {
	for _, plugin := range pm.ListTools(cfg) {
		name := plugin["name"].(string)
		if !cfg.IsToolEnabled(name) {
			continue
		}

		// Convert inputSchema to map[string]interface{} for the SDK
		inputSchemaBytes, err := json.Marshal(plugin["inputSchema"])
		if err != nil {
			cfg.Logf(1, "Failed to marshal inputSchema for plugin %s: %v", name, err)
			continue
		}
		var inputSchema map[string]interface{}
		if err := json.Unmarshal(inputSchemaBytes, &inputSchema); err != nil {
			cfg.Logf(1, "Failed to unmarshal inputSchema for plugin %s: %v", name, err)
			continue
		}

		// Create tool with the correct API
		tool := &mcp.Tool{
			Name:        name,
			Description: plugin["description"].(string),
			InputSchema: inputSchema,
			Annotations: pluginAnnotationsToMCP(plugin["annotations"]),
		}

		// Use AddTool with generic types - we'll use "any" for both input and output
		// to pass through the raw parameters and results as-is
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
	}
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
