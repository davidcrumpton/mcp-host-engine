// host/httpclient/httpclient.go
package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mcphe/config"
	"mcphe/transport"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// maxResponseBytes bounds one response body. The body is handed to plugin JS as
// a string, so an unbounded read is an unbounded allocation: a 10 GB response
// from an allow-listed (or compromised) host would take the daemon down before
// the plugin ever saw the data.
const maxResponseBytes = 32 << 20 // 32 MiB

// Request issues one HTTP request with an explicit method.
//
// Every entry point in this file funnels into do(), which is the single place
// the domain, method and header gates live. That is deliberate: the eight
// near-identical functions this replaced each had their own copy of the checks,
// and the seven convenience helpers had drifted so that only the domain check
// survived. allowed_http_methods: ["GET"] did not stop host.http.post, and
// allowed_headers did not stop an exfiltration header. One gate, one path.
func Request(ctx context.Context, method string, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, method, urlStr, headers, body, cfg, pluginName)
}

func Get(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodGet, urlStr, headers, "", cfg, pluginName)
}

func Post(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodPost, urlStr, headers, body, cfg, pluginName)
}

func Patch(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodPatch, urlStr, headers, body, cfg, pluginName)
}

func Put(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodPut, urlStr, headers, body, cfg, pluginName)
}

func Delete(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodDelete, urlStr, headers, body, cfg, pluginName)
}

func Options(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodOptions, urlStr, headers, "", cfg, pluginName)
}

func Head(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	return do(ctx, http.MethodHead, urlStr, headers, "", cfg, pluginName)
}

func do(ctx context.Context, method, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	identity, sessionID := logContext(ctx)
	method = strings.ToUpper(strings.TrimSpace(method))
	logURL := redactURL(urlStr)
	cfg.LogfWithContext(3, identity, sessionID, "HTTP %s %s", method, logURL)

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.LogfWithContext(1, identity, sessionID, "Invalid URL %s: %v", logURL, err)
		// The parse error quotes the input, which may carry a credential; the
		// redacted form is already in the log.
		return nil, fmt.Errorf("invalid URL")
	}
	if !isDomainAllowed(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.LogfWithContext(1, identity, sessionID, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}
	if !isMethodAllowed(method, cfg, pluginName) {
		cfg.LogfWithContext(1, identity, sessionID, "Blocked HTTP request to %s - method %s not allowed", parsedURL.Hostname(), method)
		return nil, fmt.Errorf("method %s is not allowed for %s", method, parsedURL.Hostname())
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		cfg.LogfWithContext(1, identity, sessionID, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	if err := applyHeaders(req, headers, cfg, pluginName, identity, sessionID); err != nil {
		return nil, err
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}

	resp, err := clientFor(cfg, pluginName).Do(req)
	if err != nil {
		cfg.LogfWithContext(1, identity, sessionID, "HTTP request to %s failed: %v", logURL, err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := readBounded(resp.Body)
	if err != nil {
		cfg.LogfWithContext(1, identity, sessionID, "Failed to read HTTP response body: %v", err)
		return nil, err
	}

	respHeaders := map[string]interface{}{}
	for k, v := range resp.Header {
		respHeaders[k] = v
	}

	return map[string]interface{}{
		"status":  resp.StatusCode,
		"headers": respHeaders,
		"body":    string(respBody),
	}, nil
}

// applyHeaders copies the plugin's headers onto req, refusing any that the
// plugin's allowed_headers list does not permit.
//
// A plugin may pass headers either directly ({"X-Foo": "bar"}) or wrapped in a
// "headers" object ({"headers": {"X-Foo": "bar"}}). The wrapper is a container,
// not a header, so the gate is applied to what it contains rather than to its
// name -- otherwise a plugin whose allow-list names only real headers could
// never use the wrapper form.
func applyHeaders(req *http.Request, headers map[string]interface{}, cfg config.Config, pluginName, identity, sessionID string) error {
	for k, v := range headers {
		cfg.LogfWithContext(4, identity, sessionID, "HTTP request header inbound: %s: %v", k, cfg.MaskKeyValue(k, v))
		switch val := v.(type) {
		case string:
			if !IsHTTPReqHeaderAllowed(k, cfg, pluginName) {
				cfg.LogfWithContext(1, identity, sessionID, "Blocked HTTP header: %s", k)
				return fmt.Errorf("header %s is not allowed", k)
			}
			req.Header.Set(k, val)
		case map[string]interface{}:
			if k != "headers" {
				continue
			}
			for hk, hv := range val {
				hstr, ok := hv.(string)
				if !ok {
					continue
				}
				if !IsHTTPReqHeaderAllowed(hk, cfg, pluginName) {
					cfg.LogfWithContext(1, identity, sessionID, "Blocked HTTP header: %s", hk)
					return fmt.Errorf("header %s is not allowed", hk)
				}
				req.Header.Set(hk, hstr)
			}
		}
	}
	return nil
}

// readBounded reads at most maxResponseBytes and reports an overrun as an
// error rather than returning a silently truncated body: a plugin that acted on
// half a response would be worse off than one that got a clear failure.
func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("response body exceeds the %d byte limit", maxResponseBytes)
	}
	return data, nil
}

// logContext pulls the identity and session id the auth middleware stored.
func logContext(ctx context.Context) (identity, sessionID string) {
	identity, _ = ctx.Value(transport.IdentityContextKey).(string)
	if identity == "" {
		identity = "-"
	}
	sessionID, _ = ctx.Value(transport.SessionIDContextKey).(string)
	if sessionID == "" {
		sessionID = "-"
	}
	return identity, sessionID
}

// redactURL removes credentials from a URL before it is logged: the userinfo
// ("user:pass@host") is dropped and the values of query parameters whose names
// look sensitive are masked. Scheme, host, path and parameter names survive, so
// the log still says which request was made.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparsable url>"
	}
	// url.User(...) would be percent-encoded into the rendered URL ("%2A%2A%2A"),
	// which is still recognisable but reads badly; drop the userinfo and put a
	// literal marker back in front of the host.
	hadUserInfo := u.User != nil
	u.User = nil
	if u.RawQuery != "" {
		q := u.Query()
		for name := range q {
			if config.IsSensitiveName(name) {
				q.Set(name, "***")
			}
		}
		u.RawQuery = q.Encode()
	}
	out := u.String()
	if hadUserInfo {
		if i := strings.Index(out, "://"); i >= 0 {
			out = out[:i+3] + "***@" + out[i+3:]
		}
	}
	return out
}

func isDomainAllowed(hostname string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	hostname = strings.TrimSuffix(hostname, ":")
	for _, domain := range allowed {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

// IsHTTPHeaderAllowed checks whether an inbound HTTP request header may be
// forwarded to plugins. It reads from the global allowed_http_headers config key.
func IsHTTPHeaderAllowed(header string, cfg config.Config) bool {
	for _, allowedHeader := range cfg.AllowedHTTPHeaders {
		if header == allowedHeader {
			return true
		}
	}
	cfg.Logf(4, "HTTP-Header: header: %s not in allowed_http_headers", header)
	return false
}

// IsHTTPReqHeaderAllowed checks whether a request header is permitted for the
// given plugin. If the plugin config has no "allowed_headers" key, all headers
// are allowed (open-by-default). An explicit empty list also allows all headers.
func IsHTTPReqHeaderAllowed(header string, cfg config.Config, pluginName string) bool {
	pCfg, ok := cfg.Plugins[pluginName]
	if !ok {
		return true
	}
	raw, ok := pCfg["allowed_headers"]
	if !ok {
		return true
	}
	var allowedHeaders []string
	switch v := raw.(type) {
	case []string:
		allowedHeaders = v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				allowedHeaders = append(allowedHeaders, s)
			}
		}
	}
	if len(allowedHeaders) == 0 {
		return true
	}
	for _, allowedHeader := range allowedHeaders {
		if strings.EqualFold(header, allowedHeader) {
			return true
		}
	}
	cfg.Logf(4, "HTTP-Header: header: %s not in allowed_headers for plugin %s", header, pluginName)
	return false
}

func isMethodAllowed(method string, cfg config.Config, pluginName string) bool {
	allowedMethods := cfg.AllowedHTTPMethodsFor(pluginName)
	if len(allowedMethods) == 0 {
		return true // no restriction configured → allow all
	}
	for _, allowedMethod := range allowedMethods {
		if strings.EqualFold(method, allowedMethod) {
			return true
		}
	}
	return false
}

// maxRedirects bounds a redirect chain. Go's default is 10; five is plenty for
// the legitimate case (http -> https, a moved page) and short enough that a
// redirect loop cannot hold a tool call for the whole timeout.
const maxRedirects = 5

// clientFor returns the client used for one plugin's requests.
//
// Every function above funnels through here, which is what makes this the right
// place for the redirect policy: isDomainAllowed is applied to the URL the
// plugin asked for, and without a CheckRedirect net/http would then follow up
// to ten more URLs nobody checked. A single open redirect on an allow-listed
// host would otherwise reach cloud metadata (169.254.169.254), this daemon's own
// API on 127.0.0.1, or any internal service -- and a cross-domain redirect also
// forwards custom headers (X-API-Key, PRIVATE-TOKEN, ...), since Go only strips
// Authorization and Cookie.
func clientFor(cfg config.Config, pluginName string) *http.Client {
	timeout := cfg.HTTPTimeoutSecondsFor(pluginName)
	allowedDomains := cfg.AllowedDomainsFor(pluginName)
	return &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: httpClient.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if !isDomainAllowed(req.URL.Hostname(), allowedDomains) {
				return fmt.Errorf("redirect to %q is not allowed", req.URL.Hostname())
			}
			return nil
		},
	}
}
