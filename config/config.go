package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// CORSAllowAny is the explicit opt-in for cors_origin: "*". A wildcard
	// origin plus a bearer token is one misconfiguration away from a cross-site
	// tool-invocation channel, so the wildcard is refused unless this is set.
	CORSAllowAny bool `yaml:"cors_allow_any"`
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
	// AllowedHTTPPorts is the global default for which destination ports a
	// plugin may reach. Empty means any port. See AllowedPortsFor.
	AllowedHTTPPorts []string `yaml:"allowed_http_ports"`
	HTTPClientTimeoutSeconds int `yaml:"http_client_timeout_seconds"`
	TokenSecret        string                            `yaml:"token_secret"`
	TokenRevocationFile  string                            `yaml:"token_revocation_file"`
	// Plugin integrity. Per-plugin checksums live in the plugins map under
	// "checksum:"; these two are the global switches. See plugin/checksum.go.
	RequirePluginChecksums bool   `yaml:"require_plugin_checksums"`
	PluginChecksumManifest string `yaml:"plugin_checksum_manifest"`
	// Config integrity. When true the process refuses to start unless this
	// file's checksum was verified against a value supplied from outside the
	// file (the -config-checksum flag). It is a hygiene control, not a security
	// boundary: whoever can edit this file can also clear the flag. See
	// integrity/checksum.go and the "Config Integrity" section of the README.
	RequireConfigChecksum bool `yaml:"require_config_checksum"`
	VaultAddr          string                            `yaml:"vault_addr"`
	VaultToken         string                            `yaml:"vault_token"` 
	VaultRoleID        string                            `yaml:"vault_role_id"`
	VaultSecretID      string                            `yaml:"vault_secret_id"`
	VaultApprole       string                            `yaml:"vault_approle"`

	// sourcePath is the file this config was loaded from. Unexported and not
	// part of the YAML schema: it exists so the loader can refuse a plugin
	// entry that points a file:// reference at the daemon's own config.
	sourcePath string

	// resolvers are the scheme resolvers in effect for this config, including
	// the vault client built from *this* file. Unexported and not part of the
	// YAML schema. Keeping them here rather than in a package global is what
	// stops a second load from inheriting the first one's vault credentials.
	resolvers resolverSet
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
	HTTPClientTimeoutSeconds: 60,
	Verbosity: 0,
	Plugins: map[string]map[string]interface{}{
		"wikipedia_search": {
			"enabled": true,
			// Must stay in sync with LANG_NAMES in the plugin: the plugin
			// rejects any language code not in that map, and this list is the
			// set of hosts those codes resolve to. Previously only "en" was
			// listed, so 19 of the 20 advertised language editions failed with
			// "access to <lang>.wikipedia.org is not allowed".
			"allowed_domains": []string{
				"en.wikipedia.org", "fr.wikipedia.org", "de.wikipedia.org",
				"es.wikipedia.org", "it.wikipedia.org", "pt.wikipedia.org",
				"nl.wikipedia.org", "ru.wikipedia.org", "ja.wikipedia.org",
				"zh.wikipedia.org", "ar.wikipedia.org", "ko.wikipedia.org",
				"pl.wikipedia.org", "sv.wikipedia.org", "fa.wikipedia.org",
				"tr.wikipedia.org", "uk.wikipedia.org", "vi.wikipedia.org",
				"he.wikipedia.org", "id.wikipedia.org",
			},
		},
		"ping": {
			"enabled": true,
		},
	},
}

// LoadConfig doesn't set defaults.  Main now does only if no argument is provided.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("config file not found: %s", path)
		}
		return Config{}, fmt.Errorf("error reading config: %w", err)
	}
	return LoadConfigBytes(path, data)
}

// LoadConfigBytes parses config data the caller has already read from path.
//
// It exists so the config-integrity gate can verify a file and then parse the
// very same bytes: reading the file twice would leave a window in which it could
// change between the check and its use. Everything LoadConfig does after reading
// -- resolution of env://, vault:// and file:// references, the vault client,
// plugin value expansion -- happens here.
func LoadConfigBytes(path string, data []byte) (Config, error) {
	cfg := Config{sourcePath: path}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: error parsing config: %w", path, err)
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
		cfg.resolvers = builtinResolvers().with("vault", &VaultResolver{Client: client})
	} else {
		cfg.resolvers = builtinResolvers()
	}
	if err := cfg.resolveTopLevelFields(); err != nil {
		return cfg, err
	}

	if err := cfg.resolvePluginValues(); err != nil { // Plugins map
		return cfg, err
	}

	return cfg, nil
}

// PeekRequireConfigChecksum reports the require_config_checksum setting in raw
// config bytes, without resolving references or contacting anything.
//
// The gate needs this one boolean from a file it has not verified yet, which is
// otherwise exactly what it must not do. Reading a single flag is safe in a way
// that acting on a whole config is not: it has no side effects, and the value
// can only ever make the process stricter. A file that turns the requirement
// off cannot make an unverified config acceptable -- that requires the operator
// to have supplied no expected checksum in the first place.
func PeekRequireConfigChecksum(data []byte) (bool, error) {
	var probe struct {
		RequireConfigChecksum bool `yaml:"require_config_checksum"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("error parsing config: %w", err)
	}
	return probe.RequireConfigChecksum, nil
}

// PeekRunAsRoot reports the run_as_root setting in raw config bytes, without
// resolving references, contacting a vault, or building anything.
//
// The root check has to happen *before* the config is acted on: parsing it can
// log in to Vault and resolve env:// values, and doing that as root before
// refusing to run as root is the wrong order. Only this one boolean is read out
// of the unparsed bytes, and it can only ever make the process refuse to start.
func PeekRunAsRoot(data []byte) (bool, error) {
	var probe struct {
		RunAsRoot bool `yaml:"run_as_root"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false, fmt.Errorf("error parsing config: %w", err)
	}
	return probe.RunAsRoot, nil
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
			fmt.Fprintf(os.Stderr, "%s [%s] %s %s - %s\n", timestamp, levelStr, escapeLogField(identity), escapeLogField(sessionID), message)
		}
	}
}

// escapeLogField makes a caller-supplied value safe to interpolate into a single
// log line. The session id arrives as a query parameter, so without this
// "?sessionId=x%0a2026-Nov-22%20[WARN]%20admin" writes a fabricated log line in
// text mode (JSON mode escapes it already). Control characters are escaped
// rather than stripped so the forgery stays visible in the log.
func escapeLogField(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
			fmt.Fprintf(os.Stderr, "%s [%s] %s %s - %s: %s\n", timestamp, levelStr, escapeLogField(identity), escapeLogField(sessionID), pluginName, message)
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

// AllowedPortsFor returns the ports pluginName may reach, or nil for "any
// port".
//
// allowed_domains bounds the host, not the service: with
// allowed_domains: ["internal.example.com"] a plugin could still reach
// internal.example.com:6379 (redis) or :22. Set allowed_ports on the plugin --
// or allowed_http_ports globally -- to bound that too. Unset means any port,
// which is what the shipped plugins need (proxmox 8006, elastic 9200, ...).
func (c Config) AllowedPortsFor(pluginName string) []string {
	if pCfg, ok := c.Plugins[pluginName]; ok {
		if ports := portList(pCfg["allowed_ports"]); len(ports) > 0 {
			return ports
		}
	}
	return portList(c.AllowedHTTPPorts)
}

// portList normalises a YAML port list. Ports are written as numbers in every
// config file that has one, so an int (or float, which is what YAML gives for a
// bare number in some decoders) is accepted alongside a string.
func portList(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		ports := make([]string, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case string:
				if n != "" {
					ports = append(ports, n)
				}
			case int:
				ports = append(ports, strconv.Itoa(n))
			case float64:
				ports = append(ports, strconv.Itoa(int(n)))
			}
		}
		return ports
	case int:
		return []string{strconv.Itoa(v)}
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

// PluginSource returns the JS module to instantiate for a config entry. It
// defaults to the entry name when no "plugin:" alias is present. This is what
// allows one plugin file to be exposed as multiple named tools, e.g.
//
//	plugins:
//	  mcwsd-prod:    { plugin: mcwsd, options: {...} }
//	  mcwsd-staging: { plugin: mcwsd, options: {...} }
func (c Config) PluginSource(instanceName string) string {
	if pCfg, ok := c.Plugins[instanceName]; ok {
		if s, ok := pCfg["plugin"].(string); ok && s != "" {
			return s
		}
	}
	return instanceName
}

// PluginChecksum returns the raw "checksum" value declared for instanceName.
//
// A checksum describes a *file*, so an entry that instantiates another entry
// through "plugin:" inherits that entry's checksum when it declares none of its
// own -- declare it once on the module's own entry and every alias is covered.
//
// The value is returned unparsed (interface{}) so the plugin package can report
// a precise error for a malformed checksum instead of this accessor silently
// returning "" and the plugin loading unverified.
func (c Config) PluginChecksum(instanceName string) (interface{}, bool) {
	if pCfg, ok := c.Plugins[instanceName]; ok {
		if raw, ok := pCfg["checksum"]; ok {
			return raw, true
		}
	}
	source := c.PluginSource(instanceName)
	if source != instanceName {
		if pCfg, ok := c.Plugins[source]; ok {
			if raw, ok := pCfg["checksum"]; ok {
				return raw, true
			}
		}
	}
	return nil, false
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

// WritePidFile was removed: writing the pid file is the single-instance lock's
// job (see pidfile.go). An unconditional overwrite is exactly the bug that let a
// second instance clobber a running one's pid and then have that running
// instance delete the second instance's file on exit. Callers now take the lock
// instead, which cannot be acquired twice.

// sensitiveNameParts are the substrings that mark a header, query parameter or
// config key as carrying a credential. Matching is on substrings rather than on
// a fixed list of names because the previous exact-match version masked exactly
// four keys and let everything else through: x-api-key, private-token,
// proxy-authorization, set-cookie, x-auth-token and api_key all logged in clear.
var sensitiveNameParts = []string{
	"auth",
	"key",
	"token",
	"secret",
	"cookie",
	"password",
	"passwd",
	"credential",
	"signature",
	"session",
}

// IsSensitiveName reports whether a header/parameter/config key name looks like
// it carries a credential. Callers use it to decide whether a value may be
// logged verbatim.
func IsSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range sensitiveNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// MaskKeyValue returns value unless key looks sensitive, in which case it
// returns "***". Used wherever a header or option is logged.
func (c Config) MaskKeyValue(key string, value any) any {
	if IsSensitiveName(key) {
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

// refuseSelfReference rejects a plugin entry whose "config" key reads the
// daemon's own config file:
//
//	plugins:
//	  proxmox_api:
//	    config: "file:///etc/mcphe/config.yaml"
//
// That value is resolved into the plugin's config block, which host.config
// hands to the plugin -- so the plugin (and any tool argument that can make it
// log its config) receives token_secret, bearer_token and every other plugin's
// credentials. A plugin's own options belong in an options: block or the
// environment; pointing at the daemon's config is never what was meant.
func (c *Config) refuseSelfReference(pluginName, ref string) error {
	const prefix = "file://"
	if !strings.HasPrefix(ref, prefix) || c.sourcePath == "" {
		return nil
	}
	refPath, err := filepath.Abs(strings.TrimPrefix(ref, prefix))
	if err != nil {
		return nil
	}
	selfPath, err := filepath.Abs(c.sourcePath)
	if err != nil {
		return nil
	}
	if filepath.Clean(refPath) == filepath.Clean(selfPath) {
		return fmt.Errorf("plugin %q: config: %q is the daemon's own config file; put plugin options in an options: block or read them from the environment", pluginName, ref)
	}
	return nil
}

func (c *Config) resolvePluginValues() error {
	for pluginName, pluginCfg := range c.Plugins {
		for k, v := range pluginCfg {
			switch val := v.(type) {
			case string:
				if k == "config" {
					if err := c.refuseSelfReference(pluginName, val); err != nil {
						return err
					}
				}
				resolved, err := c.resolveString(val)
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
					resolved, err := c.resolveString(s)
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

// HTTPTimeoutSecondsFor returns the per-request HTTP client timeout for pluginName.
// Precedence: plugin-level "http_timeout_seconds" > global http_client_timeout_seconds > 15s default.
//
// The value is validated rather than passed through: 0 means "no timeout at all"
// to net/http, not "use the default", and a negative value makes every request
// fail immediately. Either is a config mistake that would be invisible until a
// request hung forever, so a value outside (0, maxHTTPTimeoutSeconds] falls back
// to the next source in the chain.
func (c Config) HTTPTimeoutSecondsFor(pluginName string) int {
	if pCfg, ok := c.Plugins[pluginName]; ok {
		if raw, ok := pCfg["http_timeout_seconds"]; ok {
			switch v := raw.(type) {
			case int:
				if validTimeoutSeconds(v) {
					return v
				}
			case int64:
				if v > 0 && v <= maxHTTPTimeoutSeconds {
					return int(v)
				}
			case float64:
				if v == float64(int(v)) && validTimeoutSeconds(int(v)) {
					return int(v)
				}
			}
		}
	}
	if validTimeoutSeconds(c.HTTPClientTimeoutSeconds) {
		return c.HTTPClientTimeoutSeconds
	}
	return defaultHTTPTimeoutSeconds
}

const (
	// defaultHTTPTimeoutSeconds is used when neither the plugin nor the config
	// names a usable value.
	defaultHTTPTimeoutSeconds = 15
	// maxHTTPTimeoutSeconds is an upper bound on a single request. A longer
	// timeout than this is almost always a typo (milliseconds written as
	// seconds) and would pin a plugin instance's serialised call queue.
	maxHTTPTimeoutSeconds = 3600
)

func validTimeoutSeconds(v int) bool {
	return v > 0 && v <= maxHTTPTimeoutSeconds
}