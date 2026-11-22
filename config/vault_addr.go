// config/vault_addr.go
package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// vaultClientTimeout bounds every Vault request made while resolving config
// values. Without one, an unreachable vault_addr -- a host that accepts the TCP
// connection and never answers, which is what a firewalled or overloaded Vault
// looks like -- hung startup forever with no message.
const vaultClientTimeout = 30 * time.Second

// newVaultClient builds a client for addr after validating the address.
//
// The check exists because a Vault token is a bearer credential sent on every
// request: over http:// it travels in clear text, so pointing vault_addr at a
// remote http:// endpoint hands the token to anyone on the path. https:// is
// always accepted; http:// is accepted only for a loopback address, where there
// is no network to eavesdrop on (a local vault agent, the dev server). Anything
// else is refused at startup rather than silently downgraded.
func newVaultClient(addr string) (*vaultapi.Client, error) {
	if err := validateVaultAddr(addr); err != nil {
		return nil, err
	}
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr
	// One timeout for the whole request, connection included. The api package's
	// own default is no timeout at all.
	cfg.Timeout = vaultClientTimeout
	c, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating vault client: %w", err)
	}
	return c, nil
}

// validateVaultAddr accepts https:// anywhere and http:// only on loopback.
func validateVaultAddr(addr string) error {
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("vault_addr %q is not a URL: %w", addr, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("vault_addr %q uses http:// for a non-loopback host: the vault token would be sent in clear text; use https://", addr)
	case "":
		return fmt.Errorf("vault_addr %q has no scheme: use https://host:8200", addr)
	default:
		return fmt.Errorf("vault_addr %q: unsupported scheme %q (want https, or http for loopback only)", addr, u.Scheme)
	}
}
