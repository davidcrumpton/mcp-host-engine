// main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"mcphe/config"
	"mcphe/plugin"
	"mcphe/server"
	"mcphe/transport"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [config.yaml]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	var configPath string
	cfg := config.DefaultConfig
	if flag.NArg() > 0 {
		configPath = flag.Arg(0)
		var err error
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
	}

	// Check if running as root is allowed in config and log a warning if so
	if cfg.RunAsRoot {
		cfg.Logf(1, "WARN: Running as root is enabled in config. Please ensure you understand the security implications.")
	}

	// Check if running as root but not explicitly allowed
	if isRoot() && !cfg.RunAsRoot {
		fmt.Fprintf(os.Stderr, "ERROR: This program is not designed to run as root.\n")
		fmt.Fprintf(os.Stderr, "If you really want to run as root, add 'run_as_root: true' to your config file.\n")
		os.Exit(1)
	}

	cfg.Logf(1, "Using config %s plugin version %s, MCP version %s, plugin dir=%s, verbosity=%d, transport=%s",
		configPath, cfg.PluginVersion, config.Version, cfg.PluginDir, cfg.Verbosity, cfg.Transport)

	pluginManager, err := plugin.LoadPlugins(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load plugins: %v\n", err)
		os.Exit(1)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "mcphe",
		Version: config.Version,
	}, nil)

	server.RegisterPlugins(mcpServer, pluginManager, cfg)

	switch cfg.Transport {
	case config.TransportStdio:
		runStdio(mcpServer, cfg)
	default:
		runHTTP(mcpServer, pluginManager, cfg)
	}
}

// runStdio serves MCP over stdin/stdout. Bearer token and CORS are HTTP-only
// concerns and are intentionally omitted here. The process exits when the
// client disconnects (server.Run returns).
func runStdio(mcpServer *mcp.Server, cfg config.Config) {
	cfg.Logf(1, "MCP Host Engine version %s starting on stdio transport", config.Version)
	if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "Stdio server error: %v\n", err)
		removePidFile(cfg)
		os.Exit(1)
	}
	removePidFile(cfg)
}

// runHTTP serves MCP over HTTP (streamable + SSE). This is the original
// transport and supports bearer-token auth and CORS middleware.
func runHTTP(mcpServer *mcp.Server, pluginManager *plugin.PluginManager, cfg config.Config) {
	// Warn if the server is exposed on a non-loopback address without a bearer token.
	if cfg.BearerToken == "" && !isLoopback(cfg.Host) {
		fmt.Fprintf(os.Stderr, "WARNING: bearer_token is not configured and server is listening on %s — the API is unauthenticated\n", cfg.Host)
	}

	// Create the HTTP handlers with a function that returns the server
	serverFunc := func(r *http.Request) *mcp.Server {
		return mcpServer
	}
	streamableHandler := mcp.NewStreamableHTTPHandler(serverFunc, nil)
	sseHandler := mcp.NewSSEHandler(serverFunc, nil)

	// Route based on transport type
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sessionid") != "" {
			sseHandler.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && r.Header.Get("Mcp-Session-Id") == "" {
			sseHandler.ServeHTTP(w, r)
			return
		}
		streamableHandler.ServeHTTP(w, r)
	})

	// Wrap with middleware using standard HTTP middleware approach
	var finalHandler http.Handler = handler

	// Apply CORS middleware. Use the configured origin (empty string disables
	// the header; "*" opts into wide-open CORS; any other value restricts to
	// that origin).
	finalHandler = transport.CORSMiddleware(finalHandler, cfg.CORSOrigin)

	// Apply bearer token middleware if configured
	if cfg.BearerToken != "" {
		finalHandler = transport.ValidateBearerToken(finalHandler, cfg.BearerToken)
	}

	cfg.Logf(1, "Starting server on %s:%s (HTTPS=%v)", cfg.Host, cfg.Port, cfg.UseHTTPS)

	// Build a dedicated ServeMux so we can pass it to ListenAndServe/TLS and
	// have the middleware chain (CORS, bearer token) apply to all routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/openapi.json", plugin.OpenapiHandler(cfg, pluginManager))
	mux.Handle("/rpc", finalHandler)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	var err error
	if cfg.UseHTTPS {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			fmt.Fprintln(os.Stderr, "HTTPS enabled but cert_file or key_file is not configured")
			os.Exit(1)
		}
		fmt.Printf("MCP Host Engine version %s starting...\n", config.Version)
		fmt.Printf("MCP Host Engine listening with HTTPS\n")
		fmt.Printf("    on https://%s/rpc\n", addr)
		storePidFile(cfg)
		err = http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, mux)
		if err != nil {
			removePidFile(cfg)
		}
	} else {
		fmt.Printf("MCP Host Engine version %s starting...\n", config.Version)
		fmt.Printf("    on http://%s/rpc\n", addr)
		storePidFile(cfg)
		err = http.ListenAndServe(addr, mux)
		if err != nil {
			removePidFile(cfg)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
	}
}

func removePidFile(cfg config.Config) {
	if cfg.PidFile != "" {
		err := os.Remove(cfg.PidFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing pid file: %v\n", err)
		}
	}
}

func storePidFile(cfg config.Config) {
	cfg.Logf(4, "Checking pid file %s", cfg.PidFile)
	pid := cfg.GetProcessPID()
	if pid != -1 {
		cfg.Logf(4, "Server process ID: %d\n", pid)
	}
	err := cfg.WritePidFile()
	if err != nil {
		cfg.Logf(4, "Error writing pid file %s", err)
	}
}

// isLoopback reports whether host is a loopback address (127.x.x.x or ::1).
func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || strings.HasPrefix(host, "127.")
}

// isRoot reports whether the current process is running as root.
func isRoot() bool {
	return os.Getuid() == 0
}