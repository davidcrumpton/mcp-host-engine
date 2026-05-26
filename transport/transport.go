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

var (
	inflightMu sync.Mutex
	inflight   = map[string]context.CancelFunc{}
	writeMu    sync.Mutex
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
	cfg.Logf(3, "Incoming request %s %s", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		cfg.Logf(1, "Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{Code: -32700, Message: "Parse error"}, ID: nil})
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		cfg.Logf(1, "Failed to parse request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{Code: -32700, Message: "Parse error"}, ID: nil})
		return
	}

	if req.Method == "notifications/cancelled" {
		var p struct {
			RequestID interface{} `json:"requestId"`
		}
		json.Unmarshal(req.Params, &p)
		key := fmt.Sprintf("%v", p.RequestID)
		inflightMu.Lock()
		if cancel, ok := inflight[key]; ok {
			cancel()
			delete(inflight, key)
		}
		inflightMu.Unlock()
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Result: "Cancellation acknowledged", ID: nil})
		return
	}

	if req.Method == "notifications/initialized" {
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Result: "Initialization acknowledged", ID: nil})
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
	sendJSON(w, res)
}

func handleRequest(ctx context.Context, req Request, res *ResponseWithErr, cfg config.Config, pm *plugin.PluginManager) {
	switch req.Method {
	case "initialize":
		res.Result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{"name": "mcp-server-go", "version": "1.0.0"},
		}
	case "tools/list":
		res.Result = map[string]interface{}{"tools": pm.ListTools(cfg)}
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
		toolResult, err := pm.CallTool(ctx, params.Name, params.Arguments, cfg)
		cfg.Logf(3, "Tool %s returned result: %v", params.Name, toolResult)
		if err != nil {
			res.Error = &ErrorResponse{Code: -32000, Message: err.Error()}
			return
		}
		res.Result = normalizeToolResult(toolResult)
	default:
		res.Error = &ErrorResponse{Code: -32601, Message: "Method " + req.Method + " not found"}
	}
}

func sendJSON(w http.ResponseWriter, res interface{}) {
	writeMu.Lock()
	defer writeMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(res)
	w.Write(b)
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
