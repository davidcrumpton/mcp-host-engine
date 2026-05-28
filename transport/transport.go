package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"mcphe/config"
	"mcphe/plugin"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

type rawRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

type sseClient struct {
	send chan []byte
}

var (
	inflightMu sync.Mutex
	inflight   = map[string]context.CancelFunc{}

	sseClientsMu sync.Mutex
	sseClients   = map[*sseClient]struct{}{}
)


func ValidateBearerToken(next http.HandlerFunc, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func HandleHTTPRequest(w http.ResponseWriter, r *http.Request, cfg config.Config, pm *plugin.PluginManager) {
	setCORSHeaders(w)

	cfg.Logf(3, "Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		if r.URL.Path == "/rpc/openapi.json" {
			handleOpenAPI(w, r, cfg)
		} else {
			handleSSE(w, r, cfg)
		}

	case http.MethodPost:
		handlePost(w, r, cfg, pm)

	default:
		cfg.Logf(1, "Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleOpenAPI serves a minimal OpenAPI 3.0 spec so clients like OpenWebUI
// that probe GET /rpc/openapi.json can discover the available JSON-RPC methods.
func handleOpenAPI(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	cfg.Logf(3, "OpenAPI spec requested from %s", r.RemoteAddr)

	spec := map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":   "mcphe",
			"version": cfg.Version,
		},
		"paths": map[string]interface{}{
			"/": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "JSON-RPC endpoint",
					"description": "MCP JSON-RPC 2.0 endpoint. Supports methods: initialize, tools/list, tools/call, ping.",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"jsonrpc": map[string]interface{}{
											"type":    "string",
											"example": "2.0",
										},
										"method": map[string]interface{}{
											"type": "string",
										},
										"params": map[string]interface{}{
											"type": "object",
										},
										"id": map[string]interface{}{
											"type": "string",
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "JSON-RPC response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
									},
								},
							},
						},
					},
				},
				"get": map[string]interface{}{
					"summary":     "SSE stream",
					"description": "Server-Sent Events channel for server→client notifications.",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "text/event-stream",
						},
					},
				},
			},
		},
	}

	sendJSON(w, spec)
}

// handleSSE holds GET connections open indefinitely.
func handleSSE(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	cfg.Logf(3, "SSE client connected from %s", r.RemoteAddr)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	client := &sseClient{
		send: make(chan []byte, 32),
	}

	sseClientsMu.Lock()
	sseClients[client] = struct{}{}
	sseClientsMu.Unlock()

	defer func() {
		sseClientsMu.Lock()
		delete(sseClients, client)
		sseClientsMu.Unlock()

		cfg.Logf(3, "SSE client disconnected from %s", r.RemoteAddr)
	}()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case msg := <-client.send:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func broadcastSSE(msg interface{}, cfg config.Config) {
	b, err := json.Marshal(msg)
	if err != nil {
		cfg.Logf(1, "broadcastSSE marshal error: %v", err)
		return
	}

	cfg.Logf(3, "Broadcasting SSE: %s", string(b))

	sseClientsMu.Lock()
	defer sseClientsMu.Unlock()

	for client := range sseClients {
		select {
		case client.send <- b:
		default:
			cfg.Logf(2, "SSE client buffer full, dropping message")
		}
	}
}

// handlePost processes all JSON-RPC POST requests synchronously.
func handlePost(w http.ResponseWriter, r *http.Request, cfg config.Config, pm *plugin.PluginManager) {
	cfg.Logf(3, "Incoming request POST %s", r.URL.Path)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read request body: %v", err)
		sendRPCError(w, jsonrpc.Request{}, -32700, "Parse error")
		return
	} 

	cfg.Logf(3, "Request body: %s", string(body))

	var raw rawRequest

	if err := json.Unmarshal(body, &raw); err != nil {
		cfg.Logf(1, "Failed to parse request body: %v", err)
		sendRPCError(w, jsonrpc.Request{}, -32700, "Parse error")
		return
	}

	var req jsonrpc.Request

	req.Method = raw.Method
	req.Params = raw.Params

	// Preserve numeric OR string IDs
	if len(raw.ID) > 0 {
		req.ID = jsonrpc.ID(req.ID)
	}

	// Notifications: 202, no body.
	if strings.HasPrefix(req.Method, "notifications/") {
		handleNotification(w, req, cfg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	key := fmt.Sprintf("%v", req.ID)

	inflightMu.Lock()
	inflight[key] = cancel
	inflightMu.Unlock()

	defer func() {
		inflightMu.Lock()
		delete(inflight, key)
		inflightMu.Unlock()
	}()

	res := jsonrpc.Response{
		ID: req.ID,
	}

	handleRequest(ctx, req, &res, cfg, pm)

	cfg.Logf(3, "Sending response id=%v", req.ID)

	sendJSON(w, res)

	if req.Method != "initialize" && req.Method != "ping" {
		broadcastSSE(res, cfg)
	}
}

func handleNotification(w http.ResponseWriter, req jsonrpc.Request, cfg config.Config) {
	switch req.Method {

	case "notifications/cancelled":
		var p struct {
			RequestID interface{} `json:"requestId"`
		}

		if err := json.Unmarshal(req.Params, &p); err == nil {
			key := fmt.Sprintf("%v", p.RequestID)

			inflightMu.Lock()

			if cancel, ok := inflight[key]; ok {
				cancel()
				delete(inflight, key)
			}

			inflightMu.Unlock()

			cfg.Logf(3, "Cancelled in-flight request id=%v", p.RequestID)
		}

	case "notifications/initialized":
		cfg.Logf(3, "Client sent initialized notification")

	default:
		cfg.Logf(2, "Unknown notification: %s", req.Method)
	}

	w.WriteHeader(http.StatusAccepted)
}

func handleRequest(
	ctx context.Context,
	req jsonrpc.Request,
	res *jsonrpc.Response,
	cfg config.Config,
	pm *plugin.PluginManager,
) {
	switch req.Method {

	case "initialize":
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}

		if err := json.Unmarshal(req.Params, &initParams); err != nil || initParams.ProtocolVersion == "" {
			initParams.ProtocolVersion = "2025-03-26"
		}

		cfg.Logf(2, "Client requested protocolVersion=%s", initParams.ProtocolVersion)

		res.Result = mustRaw(map[string]interface{}{
			"protocolVersion": initParams.ProtocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "mcphe",
				"version": cfg.Version,
			},
		})

	case "tools/list":
		res.Result = mustRaw(map[string]interface{}{
			"tools": normalizeTools(pm.ListTools(cfg)),
		})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}

		if err := json.Unmarshal(req.Params, &params); err != nil {
			cfg.Logf(1, "Failed to parse tools/call parameters: %v", err)

			res.Error = &jsonrpc.Error{
				Code:    -32600,
				Message: "Invalid Request",
			}

			return
		}

		result, err := pm.CallTool(ctx, params.Name, params.Arguments, cfg)

		cfg.Logf(3, "Tool %s returned result: %v", params.Name, result)

		if err != nil {
			res.Error = &jsonrpc.Error{
				Code:    -32603,
				Message: err.Error(),
			}

			return
		}

		res.Result = mustRaw(normalizeToolResult(result))

	case "ping":
		res.Result = mustRaw(map[string]interface{}{})

	default:
		cfg.Logf(2, "Unknown method: %s", req.Method)

		res.Error = &jsonrpc.Error{
			Code:    -32601,
			Message: "Method not found",
		}
	}
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control")
}

func sendJSON(w http.ResponseWriter, res interface{}) {
	w.Header().Set("Content-Type", "application/json")

	b, err := json.Marshal(res)
	if err != nil {
		http.Error(
			w,
			`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`,
			http.StatusInternalServerError,
		)
		return
	}

	w.Write(b)
}

func mustRaw(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func normalizeTools(raw interface{}) []map[string]interface{} {
	b, err := json.Marshal(raw)
	if err != nil {
		return []map[string]interface{}{}
	}

	var tools []map[string]interface{}

	if err := json.Unmarshal(b, &tools); err != nil {
		return []map[string]interface{}{}
	}

	out := make([]map[string]interface{}, 0, len(tools))

	for _, t := range tools {
		out = append(out, map[string]interface{}{
			"name":        t["name"],
			"description": t["description"],
			"inputSchema": t["inputSchema"],
		})
	}

	return out
}

func toolResult(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": false,
	}
}

func toolError(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": true,
	}
}

func normalizeToolResult(value interface{}) interface{} {
	if value == nil {
		return toolResult("")
	}

	if obj, ok := value.(map[string]interface{}); ok {
		if _, hasContent := obj["content"]; hasContent {
			return obj
		}
	}

	if str, ok := value.(string); ok {
		return toolResult(str)
	}

	if bytesValue, ok := value.([]byte); ok {
		return toolResult(string(bytesValue))
	}

	if marshaled, err := json.Marshal(value); err == nil {
		return toolResult(string(marshaled))
	}

	return toolResult(fmt.Sprintf("%v", value))
}

func sendRPCError(w http.ResponseWriter, req jsonrpc.Request, code int, message string) {
	errRes := jsonrpc.Response{
		ID: req.ID,
	}

	w.Header().Set("Content-Type", "application/json")

	// JSON-RPC errors are usually returned with 200 OK
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(errRes)
}
