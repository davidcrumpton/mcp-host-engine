// config/env_resolver.go
package config

import (
	"fmt"
	"os"
)

// EnvResolver resolves env:// references against the process environment.
// Unlike host.process.env() (used by plugins at call-time, gated by
// allowed_env_vars), this resolver runs once at config-load time and has
// no allowlist — it's expanding server config, not exposing arbitrary
// env access to plugin JS. Config authors already control what YAML says
// to read.
type EnvResolver struct{}

// ref is just the env var name, e.g. "DB_HOST" from "env://DB_HOST"
func (EnvResolver) Resolve(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("env:// reference missing variable name")
	}
	val, ok := os.LookupEnv(ref)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", ref)
	}
	return val, nil
}