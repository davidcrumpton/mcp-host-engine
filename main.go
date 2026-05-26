package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Port           string          `yaml:"port"`
	Host           string          `yaml:"host"`
	UseHTTPS       bool            `yaml:"use_https"`
	CertFile       string          `yaml:"cert_file"`
	KeyFile        string          `yaml:"key_file"`
	BearerToken    string          `yaml:"bearer_token"`
	PluginDir      string          `yaml:"plugin_dir"`
	GoogleAPIKey   string          `yaml:"google_api_key"`
	GoogleCXID     string          `yaml:"google_cx_id"`
	Tools          map[string]bool `yaml:"tools"`
	AllowedDomains []string        `yaml:"allowed_domains"`
	Verbosity      int             `yaml:"verbosity_level"`
}

var defaultConfig = Config{
	Port:      "8000",
	Host:      "127.0.0.1",
	UseHTTPS:  false,
	PluginDir: "plugins",
	Tools: map[string]bool{
		"ping":             true,
		"wikipedia_search": true,
		"google_search":    true,
		"get_ip":           true,
		"read_file":        false,
		"run_command":      false,
		"date_time":   		true,
		"http_request_get": true,
	},
	AllowedDomains: []string{},
	Verbosity:      0,
}

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

type Plugin struct {
	Name        string
	Description string
	InputSchema interface{}
	Call        goja.Callable
	VM          *goja.Runtime
}

type PluginManager struct {
	plugins []*Plugin
	byName  map[string]*Plugin
}

var (
	inflightMu sync.Mutex
	inflight   = map[string]context.CancelFunc{}
	writeMu    sync.Mutex
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [config.yaml]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	configPath := "config.yaml"
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
	}

	config := loadConfig(configPath)
	config.Logf(1, "Using config %s, plugin dir=%s, verbosity=%d", configPath, config.PluginDir, config.Verbosity)
	pluginManager, err := loadPlugins(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load plugins: %v\n", err)
		os.Exit(1)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleHTTPRequest(w, r, config, pluginManager)
	})
	if config.BearerToken != "" {
		handler = validateBearerToken(handler, config.BearerToken)
	}

	config.Logf(1, "Starting server on %s:%s (HTTPS=%v)", config.Host, config.Port, config.UseHTTPS)

	http.Handle("/rpc", handler)

	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	if config.UseHTTPS {
		if config.CertFile == "" || config.KeyFile == "" {
			fmt.Fprintln(os.Stderr, "HTTPS enabled but cert_file or key_file is not configured")
			os.Exit(1)
		}
		fmt.Printf("MCP Server listening on https://%s/rpc\n", addr)
		err = http.ListenAndServeTLS(addr, config.CertFile, config.KeyFile, nil)
	} else {
		fmt.Printf("MCP Server listening on http://%s/rpc\n", addr)
		err = http.ListenAndServe(addr, nil)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig(path string) Config {
	cfg := defaultConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		}
		return cfg
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config: %v\n", err)
		return defaultConfig
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = defaultConfig.PluginDir
	}
	return cfg
}

func validateBearerToken(next http.HandlerFunc, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") || strings.TrimPrefix(authHeader, "Bearer ") != token {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func handleHTTPRequest(w http.ResponseWriter, r *http.Request, config Config, pm *PluginManager) {
	config.Logf(3, "Incoming request %s %s", r.Method, r.URL.Path)
	if r.Method != http.MethodPost {
		config.Logf(1, "Method not allowed: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		config.Logf(1, "Failed to read request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{-32700, "Parse error"}, ID: nil})
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		config.Logf(1, "Failed to parse request body: %v", err)
		sendJSON(w, ResponseWithErr{JSONRPC: "2.0", Error: &ErrorResponse{-32700, "Parse error"}, ID: nil})
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
	config.Logf(3, "Handling request id=%v method=%s", req.ID, req.Method)
	inflightMu.Lock()
	inflight[key] = cancel
	inflightMu.Unlock()
	defer func() {
		inflightMu.Lock()
		delete(inflight, key)
		inflightMu.Unlock()
	}()

	res := &ResponseWithErr{JSONRPC: "2.0", ID: req.ID}
	handleRequest(ctx, req, res, config, pm)
	sendJSON(w, res)
}

func handleRequest(ctx context.Context, req Request, res *ResponseWithErr, config Config, pm *PluginManager) {
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
		res.Result = map[string]interface{}{"tools": pm.ListTools(config)}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			config.Logf(1, "Failed to parse tools/call parameters: %v", err)
			res.Error = &ErrorResponse{-32602, "Invalid params"}
			return
		}
		toolResult, err := pm.CallTool(ctx, params.Name, params.Arguments, config)
		config.Logf(3, "Tool %s returned result: %v", params.Name, toolResult)
		if err != nil {
			res.Error = &ErrorResponse{-32000, err.Error()}
			return
		}
		res.Result = normalizeToolResult(toolResult)
	default:
		res.Error = &ErrorResponse{-32601, "Method " + req.Method + " not found"}
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

func (c Config) Verbose(level int) bool {
	return c.Verbosity >= level
}

func (c Config) Logf(level int, format string, args ...interface{}) {
	if c.Verbose(level) {
		fmt.Fprintf(os.Stderr, time.Now().Format("2006-01-02 15:04:05")+" "+format+"\n", args...)
	}
}

func (c Config) IsToolEnabled(name string) bool {
	if c.Tools == nil {
		return true
	}
	enabled, ok := c.Tools[name]
	return !ok || enabled
}

func loadPlugins(config Config) (*PluginManager, error) {
	config.Logf(2, "Scanning plugin directory %s", config.PluginDir)
	dir := config.PluginDir
	if dir == "" {
		dir = "plugins"
	}
	if _, err := os.Stat(dir); err != nil {
		config.Logf(1, "Plugin directory %q missing: %v", dir, err)
		return nil, fmt.Errorf("plugin directory %q missing: %w", dir, err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.js"))
	if err != nil {
		config.Logf(1, "Failed to scan plugin directory: %v", err)
		return nil, fmt.Errorf("failed to scan plugin directory: %w", err)
	}
	if len(files) == 0 {
		config.Logf(1, "No JavaScript plugin files found in %q", dir)
		return nil, fmt.Errorf("no JavaScript plugins found in %q", dir)
	}

	plugins := make([]*Plugin, 0, len(files))
	byName := make(map[string]*Plugin, len(files))

	for _, path := range files {
		config.Logf(2, "Loading plugin %s", path)
		plugin, err := loadPlugin(path, config)
		if err != nil {
			return nil, fmt.Errorf("loading plugin %q: %w", path, err)
		}
		if !config.IsToolEnabled(plugin.Name) {
			config.Logf(2, "Plugin %s disabled by config", plugin.Name)
			continue
		}
		plugins = append(plugins, plugin)
		byName[plugin.Name] = plugin
	}

	sort.SliceStable(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })
	return &PluginManager{plugins: plugins, byName: byName}, nil
}

func loadPlugin(path string, config Config) (*Plugin, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		config.Logf(1, "Failed to read plugin file %s: %v", path, err)
		return nil, err
	}

	vm := goja.New()
	hostValue := makeHostObject(config, context.Background())
	if err := vm.Set("host", hostValue); err != nil {
		config.Logf(1, "Failed to set host API for plugin %s: %v", path, err)
		return nil, err
	}

	script := fmt.Sprintf("var module = { exports: {} }; var exports = module.exports;\n%s\nmodule.exports", content)
	value, err := vm.RunString(script)
	if err != nil {
		config.Logf(1, "Error executing plugin %s: %v", path, err)
		return nil, err
	}

	obj := value.ToObject(vm)
	nameVal := obj.Get("name")
	callVal := obj.Get("call")
	inputSchemaVal := obj.Get("inputSchema")

	name, ok := nameVal.Export().(string)
	if !ok || name == "" {
		config.Logf(1, "Plugin %q missing name", path)
		return nil, fmt.Errorf("plugin %q missing name", path)
	}
	if callVal == nil {
		config.Logf(1, "Plugin %q missing call function", name)
		return nil, fmt.Errorf("plugin %q missing call function", name)
	}
	callFunc, ok := goja.AssertFunction(callVal)
	if !ok {
		config.Logf(1, "Plugin %q call property is not a function", name)
		return nil, fmt.Errorf("plugin %q call property is not a function", name)
	}

	return &Plugin{
		Name:        name,
		Description: obj.Get("description").String(),
		InputSchema: inputSchemaVal.Export(),
		Call:        callFunc,
		VM:          vm,
	}, nil
}

func makeHostObject(config Config, ctx context.Context) map[string]interface{} {
	hostConfig := map[string]interface{}{
		"google_api_key":  config.GoogleAPIKey,
		"google_cx_id":    config.GoogleCXID,
		"allowed_domains": config.AllowedDomains,
	}
	return map[string]interface{}{
		"readFile":   func(path string) (string, error) { return hostReadFile(path) },
		"runCommand": func(command string) (string, error) { return hostRunCommand(ctx, command) },
		"httpGet": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
    		return hostHTTPGet(ctx, urlStr, headers, config)
},
		"getEnv":     func(name string) string { return os.Getenv(name) },
		"config":     hostConfig,
	}
}

func hostReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func hostRunCommand(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func hostHTTPGet(ctx context.Context, urlStr string, headers map[string]interface{}, config Config) (map[string]interface{}, error) {
	config.Logf(3, "hostHTTPGet %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		config.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isAllowedDomain(parsedURL.Hostname(), config.AllowedDomains) {
		config.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		config.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	// Apply headers from JS, if any
	for k, v := range headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
		}
	}

	// Set a default User-Agent only if JS didn't provide one
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		config.Logf(1, "HTTP request to %s failed: %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		config.Logf(1, "Failed to read HTTP response body: %v", err)
		return nil, err
	}

	headers = map[string]interface{}{}
	for k, v := range resp.Header {
		headers[k] = v
	}

	return map[string]interface{}{
		"status":  resp.StatusCode,
		"headers": headers,
		"body":    string(body),
	}, nil
}

func isAllowedDomain(hostname string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	hostname = strings.TrimSuffix(hostname, ":")
	for _, domain := range allowed {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

func (pm *PluginManager) ListTools(config Config) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(pm.plugins))
	for _, plugin := range pm.plugins {
		if !config.IsToolEnabled(plugin.Name) {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"name":        plugin.Name,
			"description": plugin.Description,
			"inputSchema": plugin.InputSchema,
		})
	}
	return tools
}

func (pm *PluginManager) CallTool(ctx context.Context, name string, rawArgs json.RawMessage, config Config) (interface{}, error) {
	config.Logf(2, "Calling tool %s with raw args %s", name, string(rawArgs))
	plugin, ok := pm.byName[name]
	if !ok {
		config.Logf(1, "Tool %s not found", name)
		return nil, fmt.Errorf("tool %q not found", name)
	}

	var args interface{}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			config.Logf(1, "Failed to parse arguments for tool %s: %v", name, err)
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	} else {
		args = map[string]interface{}{}
	}

	if err := plugin.VM.Set("host", makeHostObject(config, ctx)); err != nil {
		config.Logf(1, "Failed to set host API for tool %s: %v", name, err)
		return nil, fmt.Errorf("failed to set host API: %w", err)
	}

	scriptArgs := plugin.VM.ToValue(args)
	result, err := plugin.Call(goja.Undefined(), scriptArgs)
	if err != nil {
		config.Logf(1, "Error executing tool %s: %v", name, err)
		return nil, fmt.Errorf("tool error: %w", err)
	}
	return result.Export(), nil
}
