package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	Version = "development"
	Commit  = "none"
)

// Transport mode constants.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

type Config struct {
	Port          string                            `yaml:"port"`
	Host          string                            `yaml:"host"`
	UseHTTPS      bool                              `yaml:"use_https"`
	CertFile      string                            `yaml:"cert_file"`
	KeyFile       string                            `yaml:"key_file"`
	BearerToken   string                            `yaml:"bearer_token"`
	CORSOrigin    string                            `yaml:"cors_origin"`
	PluginDir     string                            `yaml:"plugin_dir"`
	Tools         map[string]bool                   `yaml:"tools"`
	Verbosity     int                               `yaml:"verbosity_level"`
	PluginVersion string                            `yaml:"version"`
	PidFile       string                            `yaml:"pid_file"`
	Plugins       map[string]map[string]interface{} `yaml:"plugins"`
	Meta          map[string]interface{}            `yaml:"meta"`
	RunAsRoot     bool                              `yaml:"run_as_root"`
	Transport     string                            `yaml:"transport"`
	LogsAsJSON    bool                              `yaml:"logs_as_json"`
}

var DefaultConfig = Config{
	Port:          "8001",
	Host:          "127.0.0.1",
	UseHTTPS:      false,
	CORSOrigin:    "", // Empty disables CORS header by default; set explicitly in config.
	PluginDir:     "plugins",
	PluginVersion: "internal-default",
	RunAsRoot:     false,
	Transport:     TransportHTTP,
	LogsAsJSON:    false,

	Verbosity: 0,
	Plugins: map[string]map[string]interface{}{
		"wikipedia_search": {
			"enabled": true,
			"allowed_domains": []string{"en.wikipedia.org"},
		},
		"ping": {
			"enabled": true,
		},
	},
}

// LoadConfig doesn't set defaults.  Main now does only if no argument is provided.
func LoadConfig(path string) (Config, error) {
	cfg := Config{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("config file not found: %s", path)
		}
		return cfg, fmt.Errorf("error reading config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("error parsing config: %w", err)
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = DefaultConfig.PluginDir
	}
	if cfg.Transport == "" {
		cfg.Transport = DefaultConfig.Transport
	}
	if cfg.Transport != TransportHTTP && cfg.Transport != TransportStdio {
		return cfg, fmt.Errorf("invalid transport %q: must be %q or %q", cfg.Transport, TransportHTTP, TransportStdio)
	}
	return cfg, nil
}

func (c Config) Verbose(level int) bool {
	return c.Verbosity >= level
}

func (c Config) Logf(level int, format string, args ...interface{}) {
	if c.Verbose(level) {
		if c.LogsAsJSON {
			logEntry := map[string]interface{}{
				"level":     level,
				"timestamp": time.Now().Format(time.RFC3339Nano),
				"message":   fmt.Sprintf(format, args...),
			}
			logJSON, _ := json.Marshal(logEntry)
			fmt.Fprintln(os.Stderr, string(logJSON))
		} else {
			fmt.Fprintf(os.Stderr, time.Now().Format("2006-JAN-02 15:04:05")+" "+format+"\n", args...)
		}
	}
}

func (c Config) AllowedReadFilePathsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_read_file_paths"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		paths := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				paths = append(paths, s)
			}
		}
		return paths
	default:
		return nil
	}
}

func (c Config) AllowedWriteFilePathsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_write_file_paths"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		paths := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				paths = append(paths, s)
			}
		}
		return paths
	default:
		return nil
	}
}

func (c Config) AllowedDomainsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_domains"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		domains := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				domains = append(domains, s)
			}
		}
		return domains
	}
	return nil
}

func (c Config) AllowedRunCommandsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_commands"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		cmds := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				cmds = append(cmds, s)
			}
		}
		return cmds
	default:
		return nil
	}
}

func (c Config) AllowedENVsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_env_vars"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		envs := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				envs = append(envs, s)
			}
		}
		return envs
	default:
		return nil
	}
}

func (c Config) IsToolEnabled(name string) bool {
	// New-style: presence in the plugins map enables a tool.
	// An explicit  enabled: false  inside the plugin block disables it.
	if pCfg, ok := c.Plugins[name]; ok {
		if v, exists := pCfg["enabled"]; exists {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		// Present in plugins but no "enabled" key → enabled by default.
		return true
	}

	// Legacy fall-back: honour the top-level tools map when the plugin has no
	// entry in the plugins map (old config files / built-in defaults).
	// WILL BE REMOVED IN FUTURE RELEASE.
	if c.Tools != nil {
		enabled, ok := c.Tools[name]
		return ok && enabled
	}

	return false
}

// GetProcessPID returns the PID stored in the pid file, or the current process
// PID if no pid file is configured, or -1 if the file cannot be read/parsed.
func (c Config) GetProcessPID() int {
	if c.PidFile == "" {
		return os.Getpid()
	}
	data, err := os.ReadFile(c.PidFile)
	if err != nil {
		return -1
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return -1
	}
	return pid
}

// GetProcessUID returns the UID of the current process.
func (c Config) GetProcessUID() int {
	return os.Getuid()
}

// GetProcessGID returns the GID of the current process.
func (c Config) GetProcessGID() int {
	return os.Getgid()
}

func (c Config) WritePidFile() error {
	if c.PidFile == "" {
		c.Logf(1, "PidFile is not set, skipping pid file write")
		return fmt.Errorf("PidFile is not set")
	}
	err := os.WriteFile(c.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		c.Logf(1, "Error writing pid file %s: %v", c.PidFile, err)
		return err
	}
	return nil
}