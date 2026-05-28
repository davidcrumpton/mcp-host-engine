package config

import (
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

type Config struct {
	Port        string          `yaml:"port"`
	Host        string          `yaml:"host"`
	UseHTTPS    bool            `yaml:"use_https"`
	CertFile    string          `yaml:"cert_file"`
	KeyFile     string          `yaml:"key_file"`
	BearerToken string          `yaml:"bearer_token"`
	PluginDir   string          `yaml:"plugin_dir"`
	Tools       map[string]bool `yaml:"tools"`
	Verbosity   int             `yaml:"verbosity_level"`
	Version     string          `yaml:"version"`
	PidFile     string          `yaml:"pid_file"`
	Plugins     map[string]map[string]interface{} `yaml:"plugins"`
}

var DefaultConfig = Config{
	Port:      "8001",
	Host:      "127.0.0.1",
	UseHTTPS:  false,
	PluginDir: "plugins",
	Version:   "internal-default",
	Tools: map[string]bool{
		"ping":             true,
		"wikipedia_search": true,
		"google_search":    true,
		"get_ip":           true,
		"read_file":        false,
		"write_file":       false,
		"run_command":      false,
		"date_time":        true,	
		"http_request_get": false,
	},
	Verbosity: 0,
	Plugins: map[string]map[string]interface{}{
		"wikipedia_search": {
			"allowed_domains": []string{"en.wikipedia.org"},
		},
		"google_search": {
			"allowed_domains": []string{"google.com"},
		},
	},
}


func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("config file not found: %s", path)
		}
		return cfg, fmt.Errorf("error reading config: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig, fmt.Errorf("error parsing config: %w", err)
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = DefaultConfig.PluginDir
	}
	return cfg, nil
}

func (c Config) Verbose(level int) bool {
	return c.Verbosity >= level
}

func (c Config) Logf(level int, format string, args ...interface{}) {
	if c.Verbose(level) {
		fmt.Fprintf(os.Stderr, time.Now().Format("2006-01-02 15:04:05")+" "+format+"\n", args...)
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
	if c.Tools == nil {
		return false
	}
	enabled, ok := c.Tools[name]
	return ok && enabled
}

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

func (c Config) GetProcessUID() int {
	if c.PidFile == "" {
		return os.Getuid()
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

func (c Config) GetProcessGID() int {
	if c.PidFile == "" {
		return os.Getgid()
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

func (c Config) WritePidFile() error {
	if c.PidFile == "" {
		fmt.Printf("PidFile is not set %s", c.PidFile)
		return fmt.Errorf("PidFile is not set %s", c.PidFile)
	}
	err := os.WriteFile(c.PidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	if err != nil {
		fmt.Printf("Error writing pid file %s: %s", c.PidFile, err)
		return err
	}
	return nil
}