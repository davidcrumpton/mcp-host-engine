// server/bridge.go
package server

import (
	"context"
	"encoding/json"

	"mcphe/config"
	"mcphe/plugin"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func RegisterPlugins(server *mcp.Server, pm *plugin.PluginManager, cfg config.Config) {
	for _, plugin := range pm.ListTools(cfg) {
		name := plugin["name"].(string)
		if !cfg.IsToolEnabled(name) {
			continue
		}

		// Convert inputSchema to map[string]interface{} for the SDK
		inputSchemaBytes, _ := json.Marshal(plugin["inputSchema"])
		var inputSchema map[string]interface{}
		json.Unmarshal(inputSchemaBytes, &inputSchema)

		// Create tool with the correct API
		tool := &mcp.Tool{
			Name:        name,
			Description: plugin["description"].(string),
			InputSchema: inputSchema,
		}

		// Use AddTool with generic types - we'll use "any" for both input and output
		// to pass through the raw parameters and results as-is
		mcp.AddTool(server, tool, func(ctx context.Context, request *mcp.CallToolRequest, input any) (*mcp.CallToolResult, any, error) {
			// Convert the raw parameters to a map
			var params map[string]interface{}
			if request.Params.Arguments != nil {
				// Convert raw JSON to map
				json.Unmarshal(request.Params.Arguments, &params)
			} else {
				params = make(map[string]interface{})
			}

			// Convert params back to raw JSON for plugin call
			rawArgs, err := json.Marshal(params)
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
		// For other types, we'll convert to JSON
		jsonData, _ := json.Marshal(value)
		result.Content = []mcp.Content{
			&mcp.TextContent{Text: string(jsonData)},
		}
	}

	return result
}