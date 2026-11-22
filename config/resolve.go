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

// resolverSet is the set of scheme resolvers in effect for one config load.
//
// It is a value carried by the Config, not a package global. The previous
// version registered the vault client into a process-wide map, which meant a
// second LoadConfigBytes silently inherited the first one's client (and its
// credentials) -- harmless while a process loads one config, wrong the moment a
// reload or a parallel test exists. A per-load set also means two configs can
// point at two different vault servers.
type resolverSet map[string]SecretResolver

// builtinResolvers returns the two schemes that never depend on anything else
// in the config.
func builtinResolvers() resolverSet {
	return resolverSet{"env": EnvResolver{}, "file": FileResolver{}}
}

// with returns rs extended with scheme, or rs itself when r is nil. A fresh map
// is returned so that a caller cannot mutate a set another config is using.
func (rs resolverSet) with(scheme string, r SecretResolver) resolverSet {
	if r == nil {
		return rs
	}
	out := make(resolverSet, len(rs)+1)
	for k, v := range rs {
		out[k] = v
	}
	out[scheme] = r
	return out
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
	return builtinResolvers().resolveString(s)
}

// resolveString expands a single string value if it matches a registered scheme.
// Non-matching strings pass through unchanged — this is critical so normal
// config values (URLs, plain text) aren't misinterpreted.
func (rs resolverSet) resolveString(val string) (string, error) {
	for scheme, r := range rs {
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

// resolveString expands val through this config's resolver set.
func (c Config) resolveString(val string) (string, error) {
	return c.resolversFor().resolveString(val)
}

// resolversFor returns the resolver set to use, falling back to the builtins for
// a Config that was not produced by LoadConfigBytes (DefaultConfig, a test
// literal, ...). A zero Config must still resolve env:// the way it always did.
func (c Config) resolversFor() resolverSet {
	if c.resolvers == nil {
		return builtinResolvers()
	}
	return c.resolvers
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

		resolved, err := c.resolveString(current)
		if err != nil {
			return fmt.Errorf("resolving field %q: %w", fieldName, err)
		}
		field.SetString(resolved)
	}

	return nil
}