package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	Port               string                            `yaml:"port"`
	Host               string                            `yaml:"host"`
	UseHTTPS           bool                              `yaml:"use_https"`
	CertFile           string                            `yaml:"cert_file"`
	KeyFile            string                            `yaml:"key_file"`
	BearerToken        string                            `yaml:"bearer_token"`
	CORSOrigin         string                            `yaml:"cors_origin"`
	PluginDir          string                            `yaml:"plugin_dir"`
	Tools              map[string]bool                   `yaml:"tools"`
	Verbosity          int                               `yaml:"verbosity_level"`
	PluginVersion      string                            `yaml:"version"`
	PidFile            string                            `yaml:"pid_file"`
	Plugins            map[string]map[string]interface{} `yaml:"plugins"`
	Meta               map[string]interface{}            `yaml:"meta"`
	RunAsRoot          bool                              `yaml:"run_as_root"`
	Transport          string                            `yaml:"transport"`
	LogsAsJSON         bool                              `yaml:"logs_as_json"`
	AllowedHTTPHeaders []string                          `yaml:"allowed_http_headers"`
	TokenSecret        string                            `yaml:"token_secret"`
	TokenRevocationFile  string                            `yaml:"token_revocation_file"`
	VaultAddr          string                            `yaml:"vault_addr"`
	VaultToken         string                            `yaml:"vault_token"` 
	VaultRoleID        string                            `yaml:"vault_role_id"`
	VaultSecretID      string                            `yaml:"vault_secret_id"`
	VaultApprole       string                            `yaml:"vault_approle"`
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
	VaultApprole:  "approle",

	Verbosity: 0,
	Plugins: map[string]map[string]interface{}{
		"wikipedia_search": {
			"enabled":         true,
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
	if cfg.VaultAddr != "" {
		cfg.VaultRoleID = resolveVaultRoleID(cfg)
		cfg.VaultSecretID = resolveVaultSecretID(cfg)
		var client VaultClient
		var err error
		token := ""

		if cfg.VaultRoleID != "" && cfg.VaultSecretID != "" {
			cfg.Logf(1, "Using roleid and secretid for vault authentication")
			client, err = NewVaultClientAppRole(cfg.VaultAddr, cfg.VaultRoleID, cfg.VaultSecretID, cfg.VaultApprole)
		} else {
			cfg.Logf(1, "Using token for vault authentication is not recommended practice. Consider using roleid and secretid.")
			token = resolveVaultToken(cfg)
			client, err = NewRealVaultClient(cfg.VaultAddr, token)
		}
		
		if err != nil {
			return cfg, fmt.Errorf("initializing vault client: %w", err)
		}
		RegisterResolver("vault", &VaultResolver{Client: client})
	}

	RegisterResolver("env", EnvResolver{})
	RegisterResolver("file", FileResolver{})
	if err := cfg.resolveTopLevelFields(); err != nil {
		return cfg, err
	}

	if err := cfg.resolvePluginValues(); err != nil { // Plugins map
		return cfg, err
	}

	return cfg, nil
}

func (c Config) Verbose(level int) bool {
	return c.Verbosity >= level
}

func (c Config) Logf(level int, format string, args ...interface{}) {
	c.LogfWithContext(level, "", "", format, args...)
}

func (c Config) LogfWithContext(level int, identity, sessionID, format string, args ...interface{}) {
	if c.Verbose(level) {
		message := fmt.Sprintf(format, args...)
		if c.LogsAsJSON {
			logEntry := map[string]interface{}{
				"level":     level,
				"timestamp": time.Now().Format(time.RFC3339Nano),
				"message":   message,
			}
			if identity != "" {
				logEntry["identity"] = identity
			}
			if sessionID != "" {
				logEntry["sessionID"] = sessionID
			}
			logJSON, _ := json.Marshal(logEntry)
			fmt.Fprintln(os.Stderr, string(logJSON))
		} else {
			timestamp := time.Now().Format("2006-Jan-02 15:04:05")
			levelStr := levelToString(level)
			if identity == "" {
				identity = "-"
			}
			if sessionID == "" {
				sessionID = "-"
			}
			fmt.Fprintf(os.Stderr, "%s [%s] %s %s - %s\n", timestamp, levelStr, identity, sessionID, message)
		}
	}
}

func levelToString(level int) string {
	switch level {
	case 1:
		return "WARN"
	case 2:
		return "INFO"
	case 3:
		return "ERR"
	case 4:
		return "DEBUG"
	default:
		return "INFO"
	}
}

func (c Config) LogfForPlugin(pluginName string) func(level int, format string, args ...interface{}) {
	return func(level int, format string, args ...interface{}) {
		c.LogfForPluginWithContext(pluginName, "", "", level, format, args...)
	}
}

func (c Config) LogfForPluginWithContext(pluginName, identity, sessionID string, level int, format string, args ...interface{}) {
	if c.Verbose(level) {
		message := fmt.Sprintf(format, args...)
		if c.LogsAsJSON {
			logEntry := map[string]interface{}{
				"level":     level,
				"timestamp": time.Now().Format(time.RFC3339Nano),
				"plugin":    pluginName,
				"message":   message,
			}
			if identity != "" {
				logEntry["identity"] = identity
			}
			if sessionID != "" {
				logEntry["sessionID"] = sessionID
			}
			logJSON, _ := json.Marshal(logEntry)
			fmt.Fprintln(os.Stderr, string(logJSON))
		} else {
			timestamp := time.Now().Format("2006-Jan-02 15:04:05")
			levelStr := levelToString(level)
			if identity == "" {
				identity = "-"
			}
			if sessionID == "" {
				sessionID = "-"
			}
			fmt.Fprintf(os.Stderr, "%s [%s] %s %s - %s: %s\n", timestamp, levelStr, identity, sessionID, pluginName, message)
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

func (c Config) AllowedHTTPMethodsFor(pluginName string) []string {
	pCfg, ok := c.Plugins[pluginName]
	if !ok {
		return nil
	}
	raw, ok := pCfg["allowed_http_methods"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		methods := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				methods = append(methods, s)
			}
		}
		return methods
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

func (c Config) MaskKeyValue(key string, value any) any {
	key = strings.ToLower(key)
	if key == "authorization" || key == "cookie" || key == "token" || key == "password" {
		return "***"
	}
	return value
}


// listPluginKeys are the plugin config keys that the AllowedXFor accessors
// (AllowedENVsFor, AllowedDomainsFor, etc.) expect to be lists. Their
// type-switches only handle []string / []interface{} and silently return
// nil for anything else — so if a scheme ref (env://, vault://, file://)
// for one of these keys resolves to a scalar string, we split it on commas
// rather than let it vanish.
var listPluginKeys = map[string]bool{
	"allowed_read_file_paths":  true,
	"allowed_write_file_paths": true,
	"allowed_domains":          true,
	"allowed_commands":         true,
	"allowed_env_vars":         true,
	"allowed_http_methods":     true,
}

// splitListValue splits a resolved scalar string on commas, trims whitespace,
// and drops empty entries. A value with no commas becomes a single-element
// slice, so "OPENSEARCH_BASE_URL" and "A,B,C" both normalize consistently.
func splitListValue(s string) []interface{} {
	if s == "" {
		return []interface{}{}
	}
	parts := strings.Split(s, ",")
	out := make([]interface{}, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) resolvePluginValues() error {
	for pluginName, pluginCfg := range c.Plugins {
		for k, v := range pluginCfg {
			switch val := v.(type) {
			case string:
				resolved, err := resolveString(val)
				if err != nil {
					return fmt.Errorf("plugin %q key %q: %w", pluginName, k, err)
				}
				if listPluginKeys[k] {
					// e.g. allowed_env_vars: "env://OPENSEARCH_ALLOWED_ENVS"
					// resolving to "A,B,C" — must become a list, not a scalar,
					// or AllowedENVsFor's type-switch silently drops it.
					pluginCfg[k] = splitListValue(resolved)
				} else {
					pluginCfg[k] = resolved
				}

			case []interface{}:
				// Literal YAML list. Resolve each element individually in
				// case an entry is itself a scheme ref, e.g.
				// allowed_env_vars: ["env://SOME_ALIAS", "PLAIN_NAME"].
				// Plain literals (the common case) pass through resolveString
				// unchanged.
				resolvedList := make([]interface{}, len(val))
				for i, item := range val {
					s, ok := item.(string)
					if !ok {
						resolvedList[i] = item
						continue
					}
					resolved, err := resolveString(s)
					if err != nil {
						return fmt.Errorf("plugin %q key %q[%d]: %w", pluginName, k, i, err)
					}
					resolvedList[i] = resolved
				}
				pluginCfg[k] = resolvedList
			}
		}
	}
	return nil
}