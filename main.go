package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"mcphe/config"
	"mcphe/plugin"
	"mcphe/transport"
)

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

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	cfg.Logf(1, "Using config %s version %s, plugin dir=%s, verbosity=%d", configPath, cfg.Version, cfg.PluginDir, cfg.Verbosity)
	
	pluginManager, err := plugin.LoadPlugins(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load plugins: %v\n", err)
		os.Exit(1)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		transport.HandleHTTPRequest(w, r, cfg, pluginManager)
	})
	
	if cfg.BearerToken != "" {
		handler = transport.ValidateBearerToken(handler, cfg.BearerToken)
	}

	cfg.Logf(1, "Starting server on %s:%s (HTTPS=%v)", cfg.Host, cfg.Port, cfg.UseHTTPS)

	http.Handle("/rpc", handler)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	if cfg.UseHTTPS {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			fmt.Fprintln(os.Stderr, "HTTPS enabled but cert_file or key_file is not configured")
			os.Exit(1)
		}
		fmt.Printf("MCP Server version %s starting...\n", config.Version)
		fmt.Printf("MCP Server listening with HTTPS\n")
		fmt.Printf("    on https://%s/rpc\n", addr)
		storePidFile(cfg)
		err = http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, nil)
	} else {
		fmt.Printf("MCP Server version %s starting...\n", config.Version)
		fmt.Printf("    on http://%s/rpc\n", addr)
		storePidFile(cfg)
		err = http.ListenAndServe(addr, nil)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
		os.Exit(1)
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