// main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"mcphe/auth"
	"mcphe/config"
	"mcphe/plugin"
	"mcphe/server"
	"mcphe/transport"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "token" {
		runTokenCommand(os.Args[2:])
		return
	}

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [config.yaml]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	configPath := "./config.yaml"
	var cfg config.Config
	if flag.NArg() > 0 {
		var err error
		configPath = flag.Arg(0)
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
	} else if flag.NArg() == 0 {
		if _, err := os.Stat(configPath); err == nil {
			cfg, err = config.LoadConfig(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
				os.Exit(1)
			}
		} else {
			cfg = config.DefaultConfig
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

	cfg.Logf(2, "Using config %s plugin dir=%s, verbosity=%d, transport=%s",
		configPath, cfg.PluginDir, cfg.Verbosity, cfg.Transport)

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

// runTokenCommand implements `mcphe token create <username> <duration>`.
// This is an offline admin action, not runtime server behavior, so it
// doesn't conflict with the "config.yaml controls everything" rule — it's
// the same category as the positional config-path argument main() already
// accepts.
func runTokenCommand(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	configPath := fs.String("config", "./config.yaml", "path to config.yaml")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mcphe token create [-config path] <username> <duration>")
		fmt.Fprintln(os.Stderr, "  duration examples: 12h, 30d, 1y  (flags must come before positional args)")
	}

	if len(args) < 1 || args[0] != "create" {
		fs.Usage()
		os.Exit(1)
	}
	fs.Parse(args[1:])

	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		os.Exit(1)
	}
	username, durationStr := rest[0], rest[1]

	ttl, err := parseTokenDuration(durationStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid duration %q: %v\n", durationStr, err)
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %q: %v\n", *configPath, err)
		os.Exit(1)
	}
	if cfg.TokenSecret == "" {
		fmt.Fprintln(os.Stderr, "token_secret is not set in config — add one before minting tokens")
		os.Exit(1)
	}

	token, err := auth.Create("mcphe", config.Version, username, ttl, cfg.TokenSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(token)
}

// parseTokenDuration extends time.ParseDuration with day ("d") and year ("y")
// suffixes, since token lifetimes are usually given as human spans like
// "265d" or "1y" rather than raw hours — time.ParseDuration tops out at "h".
func parseTokenDuration(s string) (time.Duration, error) {
	switch {
	case strings.HasSuffix(s, "d"):
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day duration: %w", err)
		}
		return time.Duration(days * 24 * float64(time.Hour)), nil
	case strings.HasSuffix(s, "y"):
		years, err := strconv.ParseFloat(strings.TrimSuffix(s, "y"), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid year duration: %w", err)
		}
		return time.Duration(years * 365 * 24 * float64(time.Hour)), nil
	default:
		return time.ParseDuration(s)
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
	if cfg.BearerToken == "" && cfg.TokenSecret == "" && !isLoopback(cfg.Host) {
		fmt.Fprintf(os.Stderr, "WARNING: neither token_secret nor bearer_token is configured and server is listening on %s — the API is unauthenticated\n", cfg.Host)
	}

	// Warn bearer_token will be obsolete if it used
	if cfg.BearerToken != "" && cfg.TokenSecret == "" {
		fmt.Fprintf(os.Stderr, "WARNING: bearer_token will be obsolete in the future. Please use token_secret instead.\n")
	}

	// Create the HTTP handlers with a function that returns the server
	serverFunc := func(r *http.Request) *mcp.Server {
		return mcpServer
	}
	streamableHandler := mcp.NewStreamableHTTPHandler(serverFunc, nil)
	sseHandler := mcp.NewSSEHandler(serverFunc, nil)

	// Route based on transport type
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject request headers into context so plugins can access them via host.server.httpHeaders
		r = r.WithContext(context.WithValue(r.Context(), "http_headers", r.Header))

		// Derive session ID for routing decisions (SSE vs StreamableHTTP)
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			sessionID = r.URL.Query().Get("sessionId")
		}
		if sessionID == "" {
			sessionID = r.URL.Query().Get("sessionid")
		}

		identity, _ := r.Context().Value(transport.IdentityContextKey).(string)
		if identity == "" {
			identity = "-"
		}
		cfg.Logf(4, "Incoming %s %s identity=%s sessionID=%s", r.Method, r.URL.Path, identity, sessionID)

		if sessionID != "" && r.Header.Get("Mcp-Session-Id") == "" {
			// SSE POST: sessionid comes from query param
			sseHandler.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && sessionID == "" {
			// SSE GET: initial connection
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
	if cfg.TokenSecret != "" || cfg.BearerToken != "" {
		finalHandler = transport.ValidateToken("mcphe", config.Version, finalHandler, cfg.TokenSecret, cfg.BearerToken)
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
