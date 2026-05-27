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
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponseWithErr struct {
	JSONRPC string         `json:"jsonrpc"`
	Result  interface{}    `json:"result,omitempty"`
	Error   *ErrorResponse `json:"error,omitempty"`
	ID      interface{}    `json:"id"`
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
	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		handleSSE(w, r, cfg)
	case http.MethodPost:
		handlePost(w, r, cfg, pm)
	default:
		cfg.Logf(1, "Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSSE holds GET connections open indefinitely.
// n8n opens these on startup and keeps them alive as a persistent
// server→client channel. Responses are broadcast here AND returned
// on the POST so either transport style works.
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

	client := &sseClient{send: make(chan []byte, 32)}

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
// The response is written directly on the POST (works for LM Studio and
// any plain HTTP client) AND broadcast over SSE (works for n8n which
// may read from either channel).
func handlePost(w http.ResponseWriter, r *http.Request, cfg config.Config, pm *plugin.PluginManager) {
	cfg.Logf(3, "Incoming request POST %s", r.URL.Path)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{Code: -32700, Message: "Parse error"}, ID: nil})
		return
	}
	cfg.Logf(3, "Request body: %s", string(body))

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		cfg.Logf(1, "Failed to parse request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{Code: -32700, Message: "Parse error"}, ID: nil})
		return
	}

	// Notifications: 202, no body.
	if strings.HasPrefix(req.Method, "notifications/") {
		handleNotification(w, req, cfg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	key := fmt.Sprintf("%v", req.ID)
	cfg.Logf(3, "Handling request id=%v method=%s", req.ID, req.Method)

	inflightMu.Lock()
	inflight[key] = cancel
	inflightMu.Unlock()
	defer func() {
		inflightMu.Lock()
		delete(inflight, key)
		inflightMu.Unlock()
	}()

	res := &ResponseWithErr{JSONRPC: "2.0", ID: req.ID}
	handleRequest(ctx, req, res, cfg, pm)

	// Respond on the POST — required for LM Studio and spec-compliant for all clients.
	cfg.Logf(3, "Sending response id=%v", req.ID)
	sendJSON(w, res)

	// Broadcast over SSE for clients like n8n that listen on the GET channel.
	// Skip initialize/ping — those are handshake responses that belong only on
	// the POST; broadcasting them to persistent SSE listeners confuses clients.
	if req.Method != "initialize" && req.Method != "ping" {
		broadcastSSE(res, cfg)
	}
}

func handleNotification(w http.ResponseWriter, req Request, cfg config.Config) {
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

func handleRequest(ctx context.Context, req Request, res *ResponseWithErr, cfg config.Config, pm *plugin.PluginManager) {
	switch req.Method {
	case "initialize":
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(req.Params, &initParams); err != nil || initParams.ProtocolVersion == "" {
			initParams.ProtocolVersion = "2025-03-26"
		}
		cfg.Logf(2, "Client requested protocolVersion=%s", initParams.ProtocolVersion)
		res.Result = map[string]interface{}{
			"protocolVersion": initParams.ProtocolVersion,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "mcphe", "version": cfg.Version},
		}

	case "tools/list":
		res.Result = map[string]interface{}{"tools": normalizeTools(pm.ListTools(cfg))}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			cfg.Logf(1, "Failed to parse tools/call parameters: %v", err)
			res.Error = &ErrorResponse{Code: -32602, Message: "Invalid params"}
			return
		}
		result, err := pm.CallTool(ctx, params.Name, params.Arguments, cfg)
		cfg.Logf(3, "Tool %s returned result: %v", params.Name, result)
		if err != nil {
			res.Error = &ErrorResponse{Code: -32000, Message: err.Error()}
			return
		}
		res.Result = normalizeToolResult(result)

	case "ping":
		res.Result = map[string]interface{}{}

	default:
		cfg.Logf(2, "Unknown method: %s", req.Method)
		res.Error = &ErrorResponse{Code: -32601, Message: "Method " + req.Method + " not found"}
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
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":null}`, http.StatusInternalServerError)
		return
	}
	w.Write(b)
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
		"content": []map[string]interface{}{{"type": "text", "text": text}},
		"isError": false,
	}
}

func toolError(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
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