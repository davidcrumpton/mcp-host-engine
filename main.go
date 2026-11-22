// main.go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mcphe/auth"
	"mcphe/config"
	"mcphe/plugin"
	"mcphe/server"
	"mcphe/transport"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "token":
			runTokenCommand(os.Args[2:])
			return
		case "digest":
			runDigestCommand(os.Args[2:])
			return
		case "config":
			runConfigCommand(os.Args[2:])
			return
		}
	}

	var guard configGuard
	guard.register(flag.CommandLine)

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage: %s [-config-checksum <sha256:hex>] [-require-config-checksum] [config.yaml]\n", os.Args[0])
		fmt.Fprintf(out, "       %s token create|revoke ...   (see '%s token')\n", os.Args[0], os.Args[0])
		fmt.Fprintf(out, "       %s digest [-format ...] [-verify]\n", os.Args[0])
		fmt.Fprintf(out, "       %s config verify -checksum <sha256:hex> [-config path]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	configPath := "./config.yaml"
	explicitConfig := flag.NArg() > 0
	if explicitConfig {
		configPath = flag.Arg(0)
	}

	// The root check comes before the config is parsed, not after. Parsing
	// resolves env:// and file:// values and can log in to Vault, so the old
	// order performed all of that as root before deciding not to run as root.
	// Reading run_as_root out of the raw bytes is the one thing that has to
	// happen first; it has no side effects and can only make us refuse.
	if err := guard.checkRunAsRoot(configPath, !explicitConfig); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	cfg, err := guard.load(configPath, !explicitConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Check if running as root is allowed in config and log a warning if so
	if cfg.RunAsRoot {
		cfg.Logf(1, "WARN: Running as root is enabled in config. Please ensure you understand the security implications.")
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

	if err := server.RegisterPlugins(mcpServer, pluginManager, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register tools: %v\n", err)
		os.Exit(1)
	}

	// Take the single-instance lock before serving. A second instance used to
	// overwrite the pid file of a running one, and the first instance then
	// deleted the second's file on exit; two daemons on one config share a
	// revocation list and a plugin directory. Inert when pid_file is unset.
	lock, err := acquirePidLockForConfig(cfg.PidFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	if cfg.PidFile != "" {
		cfg.Logf(2, "Locked pid file %s (pid %d)", cfg.PidFile, os.Getpid())
	}

	switch cfg.Transport {
	case config.TransportStdio:
		runStdio(mcpServer, cfg, lock)
	default:
		runHTTP(mcpServer, pluginManager, cfg, lock)
	}
}

// runTokenCommand implements:
//
//	mcphe token create [-config path] <username> <duration> [label]
//	mcphe token revoke [-config path] <username>[:<label>] [ttl]
//
// label/ttl are both optional. Omitting label on create means that token
// can only be revoked at the username level. Omitting ttl on revoke just
// means the revocation entry has no self-prune hint and stays until removed
// by hand -- see auth.Revoker's doc comment.
func runTokenCommand(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	configPath := fs.String("config", "./config.yaml", "path to config.yaml")
	var guard configGuard
	guard.register(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  mcphe token create [-config path] [-config-checksum <sha256:hex>] <username> <duration> [label]")
		fmt.Fprintln(os.Stderr, "  mcphe token revoke [-config path] [-config-checksum <sha256:hex>] <username>[:<label>] [ttl]")
		fmt.Fprintln(os.Stderr, "  duration/ttl examples: 12h, 30d, 1y  (flags must come before positional args)")
	}

	if len(args) < 1 || (args[0] != "create" && args[0] != "revoke") {
		fs.Usage()
		os.Exit(1)
	}
	subcommand := args[0]
	fs.Parse(args[1:])
	rest := fs.Args()

	cfg, err := guard.load(*configPath, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %q: %v\n", *configPath, err)
		os.Exit(1)
	}

	switch subcommand {
	case "create":
		if len(rest) < 2 {
			fs.Usage()
			os.Exit(1)
		}
		username, durationStr := rest[0], rest[1]
		var label string
		if len(rest) >= 3 {
			label = rest[2]
		}

		ttl, err := parseTokenDuration(durationStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid duration %q: %v\n", durationStr, err)
			os.Exit(1)
		}
		if cfg.TokenSecret == "" {
			fmt.Fprintln(os.Stderr, "token_secret is not set in config — add one before minting tokens")
			os.Exit(1)
		}

		// Create token and purge from revocation list
		token, err := auth.Create("mcphe", config.Version, username, label, ttl, cfg.TokenSecret)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create token: %v\n", err)
			os.Exit(1)
		}
		r, err := auth.NewRevoker(cfg.TokenRevocationFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open revocation file %q: %v\n", cfg.TokenRevocationFile, err)
			os.Exit(1)
		}

		// Clear both the specific and the blanket revocation entries: a bare
		// "username" entry denies all of that user's tokens, so it must be
		// purged too or the token we just minted would be revoked on arrival.
		if err := r.UnrevokeIdentity(username, label); err != nil {
			fmt.Fprintf(os.Stderr, "failed to unrevoke token: %v\n", err)
			os.Exit(1)
		}
		// fmt.Printf("Successfully unrevoked token for %s\n", username)
		fmt.Println(token)

	case "revoke":
		if len(rest) < 1 {
			fs.Usage()
			os.Exit(1)
		}
		key := rest[0]

		var expiry int64
		if len(rest) >= 2 {
			ttl, err := parseTokenDuration(rest[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid ttl %q: %v\n", rest[1], err)
				os.Exit(1)
			}
			expiry = time.Now().Add(ttl).Unix()
		}
		if cfg.TokenRevocationFile == "" {
			fmt.Fprintln(os.Stderr, "token_revocation_file is not set in config — add one before revoking tokens")
			os.Exit(1)
		}

		r, err := auth.NewRevoker(cfg.TokenRevocationFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open revocation file %q: %v\n", cfg.TokenRevocationFile, err)
			os.Exit(1)
		}
		if err := r.Revoke(key, expiry); err != nil {
			fmt.Fprintf(os.Stderr, "failed to revoke %q: %v\n", key, err)
			os.Exit(1)
		}
		fmt.Printf("Revoked %q\n", key)
	}
}

// runDigestCommand implements:
//
//	mcphe digest [-config path] [-algo sha256|sha512] [-format text|yaml|json|manifest] [-o file] [path ...]
//	mcphe digest -verify [-config path]
//
// The first form prints content digests for plugin files: "text" for pasting a
// plugin's "checksum:" value, "yaml" for a ready-made plugins: block, "manifest"
// for a coreutils-compatible file usable as plugin_checksum_manifest, and "json"
// for other tooling. With no paths it digests plugin_dir recursively.
//
// The second form checks the checksums already in config against the files on
// disk without starting the server, which makes it usable as a CI or pre-deploy
// gate. Exit status is 0 when everything matches, 1 when anything does not.
func runDigestCommand(args []string) {
	fs := flag.NewFlagSet("digest", flag.ExitOnError)
	configPath := fs.String("config", "./config.yaml", "path to config.yaml")
	algo := fs.String("algo", plugin.ChecksumSHA256, "hash algorithm: sha256 or sha512")
	format := fs.String("format", "text", "output format: text, yaml, json, manifest")
	outFile := fs.String("o", "", "write output to this file instead of stdout")
	verify := fs.Bool("verify", false, "verify configured checksums against the files on disk and exit non-zero on any mismatch")
	var guard configGuard
	guard.register(fs)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  mcphe digest [-config path] [-algo sha256|sha512] [-format text|yaml|json|manifest] [-o file] [path ...]")
		fmt.Fprintln(os.Stderr, "  mcphe digest -verify [-config path]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "With no paths, plugin_dir from the config is scanned recursively for *.js.")
		fmt.Fprintln(os.Stderr, "  text      \"algo:hex  path\" lines (use as a plugin's checksum: value)")
		fmt.Fprintln(os.Stderr, "  yaml      a plugins: block keyed by exported plugin name")
		fmt.Fprintln(os.Stderr, "  manifest  coreutils-style \"hex  path\" lines for plugin_checksum_manifest")
		fmt.Fprintln(os.Stderr, "  json      JSON object keyed by path")
	}

	if len(args) < 1 {
		fs.Usage()
		os.Exit(2)
	}
	fs.Parse(args)
	rest := fs.Args()

	algorithm := strings.ToLower(*algo)
	if algorithm != plugin.ChecksumSHA256 && algorithm != plugin.ChecksumSHA512 {
		fmt.Fprintf(os.Stderr, "invalid -algo %q: want %s or %s\n", *algo, plugin.ChecksumSHA256, plugin.ChecksumSHA512)
		os.Exit(2)
	}

	cfg, err := guard.load(*configPath, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %q: %v\n", *configPath, err)
		os.Exit(1)
	}
	pluginDir := cfg.PluginDir
	if pluginDir == "" {
		pluginDir = config.DefaultConfig.PluginDir
	}

	if *verify {
		if len(rest) > 0 {
			fmt.Fprintln(os.Stderr, "-verify checks the checksums in the config; it takes no file arguments")
			os.Exit(2)
		}
		if *outFile != "" {
			fmt.Fprintln(os.Stderr, "-verify prints its report to stdout and cannot be combined with -o")
			os.Exit(2)
		}
		os.Exit(verifyPluginChecksums(cfg))
	}

	var entries []plugin.DigestEntry
	if len(rest) == 0 {
		entries, err = plugin.DigestDir(pluginDir, algorithm)
	} else {
		entries, err = plugin.DigestPaths(pluginDir, algorithm, rest)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "digest: %v\n", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "digest: no .js files found in %q\n", pluginDir)
		os.Exit(1)
	}

	var output string
	switch strings.ToLower(*format) {
	case "text":
		output = plugin.FormatDigestText(entries)
	case "yaml":
		output = plugin.FormatDigestYAML(entries)
	case "manifest":
		output = plugin.FormatDigestManifest(entries)
	case "json":
		output, err = plugin.FormatDigestJSON(entries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "digest: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "invalid -format %q: want text, yaml, json or manifest\n", *format)
		os.Exit(2)
	}

	if *outFile == "" {
		fmt.Print(output)
		return
	}
	if err := os.WriteFile(*outFile, []byte(output), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "digest: writing %s: %v\n", *outFile, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d digests to %s\n", len(entries), *outFile)
}

// verifyPluginChecksums prints a per-file integrity report and returns the
// process exit status: 0 when every configured checksum matches, 1 otherwise.
func verifyPluginChecksums(cfg config.Config) int {
	reports, err := plugin.VerifyConfiguredChecksums(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "digest: %v\n", err)
		return 1
	}
	if len(reports) == 0 {
		fmt.Fprintln(os.Stderr, "digest: no enabled plugins to verify")
		return 1
	}

	var failures, unchecked int
	for _, r := range reports {
		switch r.Status {
		case plugin.ChecksumStatusMismatch:
			failures++
			fmt.Printf("%-10s %-24s %s\n", r.Status, r.Plugin, r.File)
			fmt.Printf("%-10s %-24s expected %s, got %s\n", "", "", r.Expected, r.Actual)
		case plugin.ChecksumStatusMissing:
			failures++
			detail := "file not found"
			if r.Expected == "" {
				detail = "no checksum configured (require_plugin_checksums is true)"
			}
			fmt.Printf("%-10s %-24s %s: %s\n", r.Status, r.Plugin, r.File, detail)
		case plugin.ChecksumStatusUnchecked:
			unchecked++
			fmt.Printf("%-10s %-24s %s\n", r.Status, r.Plugin, r.File)
		default:
			fmt.Printf("%-10s %-24s %s\n", r.Status, r.Plugin, r.File)
		}
	}

	fmt.Printf("\n%d checked, %d failed, %d without a configured checksum\n", len(reports), failures, unchecked)
	if failures > 0 {
		return 1
	}
	return 0
}

// parseTokenDuration extends time.ParseDuration with day ("d") and year ("y")
// suffixes, since token lifetimes are usually given as human spans like
// "265d" or "1y" rather than raw hours — time.ParseDuration tops out at "h".
//
// The result is always positive and finite. It used to accept "-1h", "NaNd" and
// "1e400d": the last two because ParseFloat returns ±Inf/NaN and the float→
// Duration conversion then overflows to a negative number. A negative TTL is not
// a rejected input, it is a wrong answer -- `mcphe token revoke bob -1h` wrote an
// already-expired entry, which readEntries pruned on the next reload, so the
// revocation silently vanished and the token kept working.
func parseTokenDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("duration is empty")
	}

	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "d"):
		d, err = scaleDuration(s, strings.TrimSuffix(s, "d"), 24*float64(time.Hour))
	case strings.HasSuffix(s, "y"):
		d, err = scaleDuration(s, strings.TrimSuffix(s, "y"), 365*24*float64(time.Hour))
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive, got %q", s)
	}
	return d, nil
}

// scaleDuration parses a decimal number of unit-length spans into a Duration,
// rejecting values that are not finite or that overflow a Duration.
func scaleDuration(whole, number string, unit float64) (time.Duration, error) {
	n, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", whole, err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid duration %q: not a finite number", whole)
	}
	ns := n * unit
	// A float-to-int64 conversion out of range is implementation-defined and
	// yields a negative Duration here, so bound it explicitly.
	if ns >= float64(math.MaxInt64) || ns <= float64(math.MinInt64) {
		return 0, fmt.Errorf("invalid duration %q: out of range", whole)
	}
	return time.Duration(ns), nil
}

// runStdio serves MCP over stdin/stdout. Bearer token and CORS are HTTP-only
// concerns and are intentionally omitted here. The process exits when the
// client disconnects (server.Run returns) or when it is signalled.
func runStdio(mcpServer *mcp.Server, cfg config.Config, lock *pidLock) {
	cfg.Logf(1, "MCP Host Engine version %s starting on stdio transport", config.Version)

	// Without this a SIGINT/SIGTERM kills the process outright, so the pid file
	// is left behind. The context makes the shutdown go through Run's own
	// return path.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
		lock.Release()
		// A cancelled context is the shutdown we asked for, not a failure.
		if ctx.Err() != nil {
			return
		}
		fmt.Fprintf(os.Stderr, "Stdio server error: %v\n", err)
		os.Exit(1)
	}
	lock.Release()
}

// runHTTP serves MCP over HTTP (streamable + SSE). This is the original
// transport and supports bearer-token auth and CORS middleware.
func runHTTP(mcpServer *mcp.Server, pluginManager *plugin.PluginManager, cfg config.Config, lock *pidLock) {
	// Warn if the server is exposed on a non-loopback address without a bearer token.
	if cfg.BearerToken == "" && cfg.TokenSecret == "" && !isLoopback(cfg.Host) {
		fmt.Fprintf(os.Stderr, "WARNING: neither token_secret nor bearer_token is configured and server is listening on %s — the API is unauthenticated\n", cfg.Host)
	}

	// Warn bearer_token will be obsolete if it used
	if cfg.BearerToken != "" && cfg.TokenSecret == "" {
		fmt.Fprintf(os.Stderr, "WARNING: bearer_token will be obsolete in the future. Please use token_secret instead.\n")
	}

	// A wildcard CORS origin is refused unless it is explicitly opted into: with
	// a bearer token in play, "*" lets any website drive this server from a
	// visitor's browser.
	if cfg.CORSOrigin == "*" && !cfg.CORSAllowAny {
		fmt.Fprintln(os.Stderr, "ERROR: cors_origin: \"*\" allows any website to call this server from a browser.")
		fmt.Fprintln(os.Stderr, "Name the single origin that should be allowed, or set cors_allow_any: true to accept the risk.")
		os.Exit(1)
	}

	// Create the HTTP handlers with a function that returns the server
	serverFunc := func(r *http.Request) *mcp.Server {
		return mcpServer
	}
	streamableHandler := mcp.NewStreamableHTTPHandler(serverFunc, nil)
	sseHandler := mcp.NewSSEHandler(serverFunc, nil)
	openapiHandler := plugin.OpenapiHandler(cfg, pluginManager)

	// Route based on transport type
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject request headers into context so plugins can access them via host.server.httpHeaders
		r = r.WithContext(context.WithValue(r.Context(), transport.HTTPHeadersContextKey, r.Header))

		// Derive session ID for routing decisions (SSE vs StreamableHTTP). A
		// value that is not a plausible session id is dropped: it is used both
		// in log lines and to pick a transport, so it must not be able to forge
		// either.
		sessionID := transport.SessionIDFromRequest(r)

		identity, _ := r.Context().Value(transport.IdentityContextKey).(string)
		if identity == "" {
			identity = "-"
		}
		cfg.LogfWithContext(4, identity, sessionID, "Incoming %s %s", r.Method, r.URL.Path)

		// The tool inventory is served here rather than from its own mux entry,
		// so that it sits behind the same auth and CORS chain as /rpc. It used
		// to be registered directly on the mux, which meant any anonymous
		// client could read every tool name, description and input schema.
		if r.URL.Path == "/rpc/openapi.json" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			openapiHandler.ServeHTTP(w, r)
			return
		}

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

	// Apply bearer token middleware if configured
	if cfg.TokenSecret != "" || cfg.BearerToken != "" {
		var revoker auth.Revoked
		if cfg.TokenRevocationFile != "" {
			r, err := auth.NewRevoker(cfg.TokenRevocationFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load revocation file: %v\n", err)
				os.Exit(1)
			}
			revoker = r
			// Watch the revocation file for changes
			stopWatch := r.Watch(5*time.Second, cfg.Logf)
			defer stopWatch()
		}
		finalHandler = transport.ValidateToken("mcphe", config.Version, finalHandler, cfg.TokenSecret, cfg.BearerToken, revoker, cfg)
	}

	// CORS is applied outside auth so that a preflight -- which carries no
	// Authorization header by design -- is answered instead of challenged. See
	// transport.CORSMiddleware for what that costs and how it is contained.
	finalHandler = transport.CORSMiddleware(finalHandler, cfg.CORSOrigin, cfg.CORSAllowAny)

	cfg.Logf(1, "Starting server on %s:%s (HTTPS=%v)", cfg.Host, cfg.Port, cfg.UseHTTPS)

	// Build a dedicated ServeMux so we can pass it to ListenAndServe/TLS and
	// have the middleware chain (CORS, bearer token) apply to all routes.
	mux := http.NewServeMux()
	mux.Handle("/rpc/openapi.json", finalHandler)
	mux.Handle("/rpc", finalHandler)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)

	// A bare ListenAndServe has no timeouts at all: ReadHeaderTimeout is the
	// specific defence against a slowloris client holding a connection open
	// forever by dribbling headers. WriteTimeout stays zero on purpose -- SSE
	// responses are long-lived, and a deadline would cut the stream rather than
	// protect anything.
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if cfg.UseHTTPS && (cfg.CertFile == "" || cfg.KeyFile == "") {
		fmt.Fprintln(os.Stderr, "HTTPS enabled but cert_file or key_file is not configured")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("MCP Host Engine version %s starting...\n", config.Version)
	if cfg.UseHTTPS {
		fmt.Printf("MCP Host Engine listening with HTTPS\n")
		fmt.Printf("    on https://%s/rpc\n", addr)
	} else {
		fmt.Printf("    on http://%s/rpc\n", addr)
	}


	serveErr := make(chan error, 1)
	go func() {
		if cfg.UseHTTPS {
			serveErr <- srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else {
			serveErr <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-serveErr:
		lock.Release()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		// Run the shutdown before the deferred stopWatch/stop, so the watcher
		// is still live while in-flight requests drain.
		cfg.Logf(1, "Signal received, shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Graceful shutdown failed: %v\n", err)
			srv.Close()
		}
		lock.Release()
	}
}

// shutdownGracePeriod bounds how long in-flight requests may take to finish
// after SIGTERM/SIGINT before the listener is closed under them.
const shutdownGracePeriod = 30 * time.Second


// isLoopback reports whether host names a loopback destination.
//
// The old version compared against three literals, so the most common way to
// write "listen locally" -- host: localhost -- was treated as a public
// interface and produced a spurious "the API is unauthenticated" warning on
// every start. A name that resolves only to loopback addresses counts too.
// Anything that does not resolve is *not* loopback: the warning should err
// towards firing.
func isLoopback(host string) bool {
	h := strings.TrimSpace(host)
	// Accept the bracketed IPv6 form ("[::1]") that a listen address uses.
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if h == "" {
		return false
	}
	if strings.EqualFold(h, "localhost") || strings.HasSuffix(strings.ToLower(h), ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, h)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !a.IP.IsLoopback() {
			return false
		}
	}
	return true
}

// isRoot reports whether the current process is running as root.
func isRoot() bool {
	return os.Getuid() == 0
}
