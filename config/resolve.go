// config/resolve.go
package config

import (
	"fmt"
	"reflect"
	"strings"
)

type SecretResolver interface {
	Resolve(ref string) (string, error) // ref = everything after "scheme://"
}

var resolvers = map[string]SecretResolver{}

func RegisterResolver(scheme string, r SecretResolver) {
	resolvers[scheme] = r
}

// registerBuiltinResolvers registers the two schemes that never depend on
// anything else in the config. Idempotent: assigning the same value twice is
// harmless, and both LoadConfigBytes and ResolveValue call it so that either
// entry point works on its own.
func registerBuiltinResolvers() {
	RegisterResolver("env", EnvResolver{})
	RegisterResolver("file", FileResolver{})
}

// ResolveValue expands a "scheme://" reference through the registered resolvers
// and returns anything else unchanged. Unlike the config fields, it is callable
// before any config has been loaded.
//
// It exists for values that must be known *before* the config is parsed -- in
// practice the expected checksum of the config file itself, which by definition
// cannot be read out of the file it describes. That lets an operator keep the
// expected digest in an EnvironmentFile or a root-owned file instead of in the
// service unit's argv:
//
//	mcphe -config-checksum file:///etc/mcphe/config.sha256 /etc/mcphe/config.yaml
//
// Only env:// and file:// are available here. vault:// needs a vault client,
// which needs a config, which is the thing being verified.
func ResolveValue(s string) (string, error) {
	registerBuiltinResolvers()
	return resolveString(s)
}

// resolveString expands a single string value if it matches a registered scheme.
// Non-matching strings pass through unchanged — this is critical so normal
// config values (URLs, plain text) aren't misinterpreted.
func resolveString(val string) (string, error) {
	for scheme, r := range resolvers {
		prefix := scheme + "://"
		if strings.HasPrefix(val, prefix) {
			resolved, err := r.Resolve(strings.TrimPrefix(val, prefix))
			if err != nil {
				return "", fmt.Errorf("resolving %s: %w", val, err)
			}
			return resolved, nil
		}
	}
	return val, nil
}

// resolvableFields lists the top-level Config struct fields eligible for
// env://, vault://, etc. resolution. Deliberately a fixed allowlist rather
// than walking all string fields — config values like Host, Port, Transport
// aren't secrets and shouldn't silently be reinterpreted if they happen to
// contain "://" (e.g. someone sets host to a value that looks schemey by
// accident). Only fields that plausibly hold secrets or paths go here.
var resolvableFields = []string{
	"BearerToken",
	"TokenSecret",
	"TokenRevocationFile",
	"PidFile",
	"VaultAddr",
	"VaultToken",
	"VaultRoleID",
	"VaultSecretID",
	"VaultApprole",
	// A manifest path is often outside the config directory, so allow it to be
	// given as file:///etc/mcphe/plugins.sha256 rather than only a literal path.
	"PluginChecksumManifest",
}

// resolveTopLevelFields walks resolvableFields and, for each, resolves its
// current string value through the registered scheme resolvers (env://,
// vault://, ...). Non-matching values (no recognized scheme prefix) pass
// through unchanged via resolveString. Errors are wrapped with the field
// name so LoadConfig failures point at exactly which config key is broken.
func (c *Config) resolveTopLevelFields() error {
	v := reflect.ValueOf(c).Elem()

	for _, fieldName := range resolvableFields {
		field := v.FieldByName(fieldName)
		if !field.IsValid() {
			// Field name typo in resolvableFields itself — a programmer
			// error, not a user config error. Fail loudly rather than
			// silently skipping.
			return fmt.Errorf("resolveTopLevelFields: no such Config field %q", fieldName)
		}
		if field.Kind() != reflect.String {
			return fmt.Errorf("resolveTopLevelFields: field %q is not a string (%s)", fieldName, field.Kind())
		}
		if !field.CanSet() {
			return fmt.Errorf("resolveTopLevelFields: field %q is not settable", fieldName)
		}

		current := field.String()
		if current == "" {
			continue // nothing to resolve
		}

		resolved, err := resolveString(current)
		if err != nil {
			return fmt.Errorf("resolving field %q: %w", fieldName, err)
		}
		field.SetString(resolved)
	}

	return nil
}