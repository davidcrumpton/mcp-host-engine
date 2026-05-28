// transport/transport.go
package transport

import (
	"encoding/json"
	"net/http"
	"strings"
)

func ValidateBearerToken(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control")
		next.ServeHTTP(w, r)
	})
}

func OpenapiHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spec := map[string]interface{}{
			"openapi": "3.1.0",
			"info": map[string]interface{}{
				"title":   "MCP Server API",
				"description": "This is the API documentation for the MCP Server",
				"version": "1.0.0",
			},
			"paths": map[string]interface{}{
				"/rpc": map[string]interface{}{
					"post": map[string]interface{}{
					"operationId": "rpc",					"summary":     "MCP JSON-RPC endpoint",
					"description": "Handles MCP protocol messages via JSON-RPC 2.0",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/JSONRPCRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Successful JSON-RPC response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/JSONRPCResponse",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"JSONRPCRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"jsonrpc", "method"},
					"properties": map[string]interface{}{
						"jsonrpc": map[string]interface{}{"type": "string", "enum": []string{"2.0"}},
						"id":      map[string]interface{}{"oneOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "number"}}},
						"method":  map[string]interface{}{"type": "string"},
						"params":  map[string]interface{}{"type": "object"},
					},
				},
				"JSONRPCResponse": map[string]interface{}{
					"type":     "object",
					"required": []string{"jsonrpc", "id"},
					"properties": map[string]interface{}{
						"jsonrpc": map[string]interface{}{"type": "string", "enum": []string{"2.0"}},
						"id":      map[string]interface{}{"oneOf": []interface{}{map[string]interface{}{"type": "string"}, map[string]interface{}{"type": "number"}}},
						"result":  map[string]interface{}{"type": "object"},
						"error": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"code":    map[string]interface{}{"type": "integer"},
								"message": map[string]interface{}{"type": "string"},
								"data":    map[string]interface{}{"type": "object"},
							},
						},
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spec) 
	}
}