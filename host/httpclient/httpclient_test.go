package httpclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"mcphe/config"
)

func cfgWithDomains(pluginName string, domains []string) config.Config {
	iDomains := make([]interface{}, len(domains))
	for i, d := range domains {
		iDomains[i] = d
	}
	return config.Config{
		Plugins: map[string]map[string]interface{}{
			pluginName: {"allowed_domains": iDomains},
		},
	}
}

func TestIsAllowedDomain_Exact(t *testing.T) {
	if !isDomainAllowed("example.com", []string{"example.com"}) {
		t.Error("exact match should be allowed")
	}
}

func TestIsAllowedDomain_Subdomain(t *testing.T) {
	if !isDomainAllowed("api.example.com", []string{"example.com"}) {
		t.Error("subdomain should be allowed")
	}
}

func TestIsAllowedDomain_NoMatch(t *testing.T) {
	if isDomainAllowed("evil.com", []string{"example.com"}) {
		t.Error("non-matching domain should be denied")
	}
}

func TestIsAllowedDomain_EmptyList(t *testing.T) {
	if isDomainAllowed("example.com", nil) {
		t.Error("empty allowed list should deny everything")
	}
}

// L19: the old code trimmed a trailing ":" with the comment that url.Parse may
// produce "hostname:" for bare IPs. It does not -- url.URL.Hostname() strips the
// port and its colon, for IPs too -- so the trim was dead code dressed as a
// defence, and a hostname that really does end in ":" was matched by accident.
// What must hold is that a port never takes part in the domain comparison.
func TestIsAllowedDomain_PortIsNotPartOfTheHostname(t *testing.T) {
	for _, raw := range []string{"https://example.com:8443/x", "http://example.com:80/x"} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", raw, err)
		}
		if got := u.Hostname(); got != "example.com" {
			t.Fatalf("%q: Hostname() = %q, want %q", raw, got, "example.com")
		}
		if !isDomainAllowed(u.Hostname(), []string{"example.com"}) {
			t.Errorf("%q should match allowed domain example.com", raw)
		}
	}
	// A "hostname" that genuinely contains a colon is not a hostname, and must
	// not be normalised into one.
	if isDomainAllowed("example.com:", []string{"example.com"}) {
		t.Error(`"example.com:" should not match example.com`)
	}
}

// L19: allowed_domains bounds the host, not the port, so an allow-listed host
// was reachable on any port. allowed_ports (and the global allowed_http_ports)
// now bound that; unset still means any port, which the shipped plugins need.
func TestIsPortAllowed(t *testing.T) {
	mustParse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", raw, err)
		}
		return u
	}
	withPorts := func(ports []string) config.Config {
		return config.Config{
			Plugins: map[string]map[string]interface{}{
				"p": {"allowed_ports": ports},
			},
		}
	}

	// Unset anywhere: any port.
	if !isPortAllowed(mustParse("https://example.com:8443/"), config.Config{}, "p") {
		t.Error("unset allowed_ports should allow any port")
	}

	cfg := withPorts([]string{"443"})
	if !isPortAllowed(mustParse("https://example.com/x"), cfg, "p") {
		t.Error("https with no explicit port should count as 443")
	}
	if !isPortAllowed(mustParse("https://example.com:443/x"), cfg, "p") {
		t.Error("explicit 443 should be allowed")
	}
	if isPortAllowed(mustParse("https://example.com:8443/x"), cfg, "p") {
		t.Error("8443 should be refused when allowed_ports is [443]")
	}
	if isPortAllowed(mustParse("http://example.com/x"), cfg, "p") {
		t.Error("http with no explicit port is 80, which is not in [443]")
	}

	// The global list is the fallback when the plugin sets none.
	global := config.Config{AllowedHTTPPorts: []string{"80", "443"}}
	if !isPortAllowed(mustParse("http://example.com/x"), global, "p") {
		t.Error("global allowed_http_ports should allow port 80")
	}
	if isPortAllowed(mustParse("http://example.com:9200/x"), global, "p") {
		t.Error("global allowed_http_ports should refuse 9200")
	}

	// A plugin list overrides the global one rather than merging with it.
	override := config.Config{
		AllowedHTTPPorts: []string{"80"},
		Plugins: map[string]map[string]interface{}{
			"p": {"allowed_ports": []interface{}{9200}},
		},
	}
	if !isPortAllowed(mustParse("http://example.com:9200/x"), override, "p") {
		t.Error("plugin allowed_ports should allow 9200 (as a YAML number)")
	}
	if isPortAllowed(mustParse("http://example.com/x"), override, "p") {
		t.Error("plugin allowed_ports should override, not extend, the global list")
	}
}

func TestIsMethodAllowed_Success(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"get_ip": {"allowed_http_methods": []string{"GET"}},
		},
	}
	if !isMethodAllowed("GET", cfg, "get_ip") {
		t.Error("GET should be allowed")
	}
}

func TestIsMethodAllowed_NoMatch(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"get_ip": {"allowed_http_methods": []string{"POST"}},
		},
	}
	if isMethodAllowed("GET", cfg, "get_ip") {
		t.Error("GET should not be allowed if POST is allowed")
	}
}

func TestIsMethodAllowed_EmptyList(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"get_ip": {"allowed_http_methods": nil},
		},
	}
	// empty / nil list means no restriction → allow all
	if !isMethodAllowed("GET", cfg, "get_ip") {
		t.Error("GET should be allowed when allowed_http_methods is not configured")
	}
}

func TestIsMethodAllowed_NoPlugin(t *testing.T) {
	cfg := config.Config{}
	// plugin not in config at all → no restriction
	if !isMethodAllowed("DELETE", cfg, "unknown_plugin") {
		t.Error("method should be allowed when plugin has no config")
	}
}

func TestHTTPGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	// srv.URL is "http://127.0.0.1:<port>" – allow just the hostname.
	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_get", []string{parsed.Hostname()})

	res, err := Get(context.Background(), srv.URL, nil, cfg, "http_request_get")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "hello" {
		t.Errorf("body: got %q, want %q", res["body"], "hello")
	}
}

func TestHTTPGet_BlockedDomain(t *testing.T) {
	cfg := cfgWithDomains("http_request_get", []string{"allowed.example.com"}) // blocked.example.com not listed
	_, err := Get(context.Background(), "http://blocked.example.com/", nil, cfg, "http_request_get")
	if err == nil {
		t.Fatal("expected blocked-domain error")
	}
}

func TestHTTPGet_InvalidURL(t *testing.T) {
	cfg := config.Config{}
	_, err := Get(context.Background(), "://bad-url", nil, cfg, "http_request_get")
	if err == nil {
		t.Fatal("expected invalid-URL error")
	}
}

func TestHTTPPost_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 64)
		n, _ := r.Body.Read(body)
		fmt.Fprintf(w, "got:%s", body[:n])
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_post", []string{parsed.Hostname()})

	res, err := Post(context.Background(), srv.URL, nil, "payload", cfg, "http_request_post")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res["body"].(string), "payload") {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPPost_BlockedDomain(t *testing.T) {
	cfg := cfgWithDomains("http_request_post", []string{"other.example.com"})
	_, err := Post(context.Background(), "http://blocked.com/", nil, "", cfg, "http_request_post")
	if err == nil {
		t.Fatal("expected blocked-domain error")
	}
}

func TestHTTPPatch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "patch-ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_patch", []string{parsed.Hostname()})

	res, err := Patch(context.Background(), srv.URL, nil, "data", cfg, "http_request_patch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "patch-ok" {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPPut_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "put-ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_put", []string{parsed.Hostname()})

	res, err := Put(context.Background(), srv.URL, nil, "data", cfg, "http_request_put")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "put-ok" {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPDelete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "deleted")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_delete", []string{parsed.Hostname()})

	res, err := Delete(context.Background(), srv.URL, nil, "", cfg, "http_request_delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "deleted" {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPOptions_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "options-ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_options", []string{parsed.Hostname()})

	res, err := Options(context.Background(), srv.URL, nil, cfg, "http_request_options")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "options-ok" {
		t.Errorf("body: got %q", res["body"])
	}
}

func TestHTTPHead_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprint(w, "head-ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("http_request_head", []string{parsed.Hostname()})

	res, err := Head(context.Background(), srv.URL, nil, cfg, "http_request_head")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "" {
		t.Errorf("Request to %s body: got %q", srv.URL, res["body"])
	}
}

func TestHTTPRequest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST method, got %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "received:%s", string(body))
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	// domain must be allowed; method is unrestricted when not configured
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"http_request": {
				"allowed_domains": []string{parsed.Hostname()},
			},
		},
	}

	res, err := Request(context.Background(), "POST", srv.URL, nil, "payload", cfg, "http_request")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["body"] != "received:payload" {
		t.Errorf("got body %q, expected received:payload", res["body"])
	}
}

func TestHTTPRequest_BlockedDomain(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {
				"allowed_http_methods": []string{"GET"},
				"allowed_domains":      []string{"allowed.example.com"},
			},
		},
	}
	_, err := Request(context.Background(), "GET", "http://blocked.example.com/", nil, "", cfg, "myplugin")
	if err == nil {
		t.Fatal("expected error for blocked domain")
	}
}

func TestHTTPRequest_BlockedMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	// explicit list: only GET allowed → POST must be rejected
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {
				"allowed_http_methods": []string{"GET"},
				"allowed_domains":      []string{parsed.Hostname()},
			},
		},
	}
	_, err := Request(context.Background(), "POST", srv.URL, nil, "body", cfg, "myplugin")
	if err == nil {
		t.Fatal("expected error for method not in explicit allowlist")
	}
}

func TestHTTPRequest_BlockedHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {
				"allowed_http_methods": []string{"GET"},
				"allowed_domains":      []string{parsed.Hostname()},
				"allowed_headers":      []string{"X-Allowed-Header"},
			},
		},
	}
	_, err := Request(context.Background(), "GET", srv.URL,
		map[string]interface{}{"X-Blocked-Header": "value"}, "", cfg, "myplugin")
	if err == nil {
		t.Fatal("expected error for blocked header")
	}
}

func TestIsHTTPReqHeaderAllowed_NilPlugin(t *testing.T) {
	cfg := config.Config{}
	if !IsHTTPReqHeaderAllowed("Any-Header", cfg, "myplugin") {
		t.Error("should allow all headers when plugin has no config")
	}
}

func TestIsHTTPReqHeaderAllowed_NoKey(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{"myplugin": {}},
	}
	if !IsHTTPReqHeaderAllowed("Any-Header", cfg, "myplugin") {
		t.Error("should allow all headers when allowed_headers key is absent")
	}
}

func TestIsHTTPReqHeaderAllowed_EmptyList(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_headers": []string{}},
		},
	}
	if !IsHTTPReqHeaderAllowed("Any-Header", cfg, "myplugin") {
		t.Error("should allow all headers when allowed_headers list is empty")
	}
}

func TestIsHTTPReqHeaderAllowed_Allowed(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_headers": []string{"X-Custom", "Authorization"}},
		},
	}
	if !IsHTTPReqHeaderAllowed("X-Custom", cfg, "myplugin") {
		t.Error("X-Custom should be allowed")
	}
}

func TestIsHTTPReqHeaderAllowed_CaseInsensitive(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_headers": []string{"x-custom"}},
		},
	}
	if !IsHTTPReqHeaderAllowed("X-Custom", cfg, "myplugin") {
		t.Error("header match should be case-insensitive")
	}
}

func TestIsHTTPReqHeaderAllowed_Blocked(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_headers": []string{"X-Allowed"}},
		},
	}
	if IsHTTPReqHeaderAllowed("X-Secret", cfg, "myplugin") {
		t.Error("X-Secret should be blocked")
	}
}

func TestIsHTTPReqHeaderAllowed_InterfaceSlice(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_headers": []interface{}{"X-Allowed"}},
		},
	}
	if !IsHTTPReqHeaderAllowed("X-Allowed", cfg, "myplugin") {
		t.Error("X-Allowed should be allowed via interface slice")
	}
	if IsHTTPReqHeaderAllowed("X-Other", cfg, "myplugin") {
		t.Error("X-Other should be blocked when not in interface slice")
	}
}

// ---------------------------------------------------------------------------
// Redirects (H4)
// ---------------------------------------------------------------------------

// isDomainAllowed covers the URL the plugin asked for. Without a CheckRedirect
// net/http follows up to ten more URLs nobody checked, so one open redirect on
// an allow-listed host reaches anything the daemon can reach.
func TestHTTPGet_RedirectOffAllowListIsRefused(t *testing.T) {
	var targetHits int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
		fmt.Fprint(w, "should never be reached")
	}))
	t.Cleanup(target.Close)

	// 127.0.0.1 is allowed; localhost is the same machine under a different
	// name, and is deliberately not in the allow-list.
	_, targetPort, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	offList := "http://localhost:" + targetPort + "/hit"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, offList, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("p", []string{parsed.Hostname()})

	res, err := Get(context.Background(), srv.URL, nil, cfg, "p")
	if err == nil {
		t.Fatalf("expected the redirect to be refused, got %v", res)
	}
	if !strings.Contains(err.Error(), "redirect") || !strings.Contains(err.Error(), "localhost") {
		t.Errorf("error should name the refused redirect, got: %v", err)
	}
	if targetHits != 0 {
		t.Errorf("the off-allow-list host was contacted %d times", targetHits)
	}
}

func TestHTTPGet_RedirectOnAllowListIsFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			fmt.Fprint(w, "arrived")
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("p", []string{parsed.Hostname()})

	res, err := Get(context.Background(), srv.URL+"/start", nil, cfg, "p")
	if err != nil {
		t.Fatalf("an on-allow-list redirect should be followed: %v", err)
	}
	if res["body"] != "arrived" {
		t.Errorf("body: got %q, want %q", res["body"], "arrived")
	}
}

func TestHTTPGet_RedirectLoopIsCapped(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()

	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("p", []string{parsed.Hostname()})

	if _, err := Get(context.Background(), srv.URL, nil, cfg, "p"); err == nil {
		t.Fatal("expected a redirect-loop error")
	}
	if hits > maxRedirects+1 {
		t.Errorf("followed %d redirects, want at most %d", hits, maxRedirects+1)
	}
}

// ---------------------------------------------------------------------------
// M1 -- the method and header gates apply to every entry point
//
// Before the fix the gates lived in Request() only, so allowed_http_methods:
// ["GET"] did not stop host.http.post and allowed_headers did not stop an
// exfiltration header on host.http.get.
// ---------------------------------------------------------------------------

func cfgGated(pluginName, hostname string, methods, headers []string) config.Config {
	block := map[string]interface{}{"allowed_domains": []string{hostname}}
	if methods != nil {
		block["allowed_http_methods"] = methods
	}
	if headers != nil {
		block["allowed_headers"] = headers
	}
	return config.Config{Plugins: map[string]map[string]interface{}{pluginName: block}}
}

func TestConvenienceHelpersEnforceMethodAllowList(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()
	parsed, _ := url.Parse(srv.URL)

	cfg := cfgGated("p", parsed.Hostname(), []string{"GET"}, nil)
	ctx := context.Background()

	// The permitted method still works.
	if _, err := Get(ctx, srv.URL, nil, cfg, "p"); err != nil {
		t.Fatalf("GET should be allowed: %v", err)
	}

	calls := map[string]func() (map[string]interface{}, error){
		"post":   func() (map[string]interface{}, error) { return Post(ctx, srv.URL, nil, "x", cfg, "p") },
		"put":    func() (map[string]interface{}, error) { return Put(ctx, srv.URL, nil, "x", cfg, "p") },
		"patch":  func() (map[string]interface{}, error) { return Patch(ctx, srv.URL, nil, "x", cfg, "p") },
		"delete": func() (map[string]interface{}, error) { return Delete(ctx, srv.URL, nil, "x", cfg, "p") },
		"head":   func() (map[string]interface{}, error) { return Head(ctx, srv.URL, nil, cfg, "p") },
		"options": func() (map[string]interface{}, error) {
			return Options(ctx, srv.URL, nil, cfg, "p")
		},
	}
	before := hits
	for name, call := range calls {
		if _, err := call(); err == nil {
			t.Errorf("%s should be refused when only GET is listed in allowed_http_methods", name)
		}
	}
	if hits != before {
		t.Errorf("a refused method still reached the server: %d extra request(s)", hits-before)
	}
}

func TestConvenienceHelpersEnforceHeaderAllowList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Exfil") != "" {
			t.Errorf("X-Exfil reached the upstream server: %q", r.Header.Get("X-Exfil"))
		}
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()
	parsed, _ := url.Parse(srv.URL)

	cfg := cfgGated("p", parsed.Hostname(), nil, []string{"Accept"})
	ctx := context.Background()

	if _, err := Get(ctx, srv.URL, map[string]interface{}{"Accept": "application/json"}, cfg, "p"); err != nil {
		t.Fatalf("an allow-listed header should be permitted: %v", err)
	}
	if _, err := Get(ctx, srv.URL, map[string]interface{}{"X-Exfil": "secret"}, cfg, "p"); err == nil {
		t.Error("host.http.get must refuse a header outside allowed_headers")
	}
	// The nested {"headers": {...}} form is gated on what it contains, not on
	// the wrapper's name.
	if _, err := Get(ctx, srv.URL, map[string]interface{}{
		"headers": map[string]interface{}{"X-Exfil": "secret"},
	}, cfg, "p"); err == nil {
		t.Error("a header inside the nested headers object must be gated too")
	}
	if _, err := Post(ctx, srv.URL, map[string]interface{}{"X-Exfil": "secret"}, "", cfg, "p"); err == nil {
		t.Error("host.http.post must refuse a header outside allowed_headers")
	}
}

// ---------------------------------------------------------------------------
// M2 -- bounded response bodies
// ---------------------------------------------------------------------------

func TestHTTPGet_OversizedBodyIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// One byte over the cap, streamed so the test does not hold it twice.
		w.Header().Set("Content-Length", fmt.Sprint(maxResponseBytes+1))
		io.CopyN(w, zeroReader{}, maxResponseBytes+1)
	}))
	t.Cleanup(srv.Close)
	httpClient = srv.Client()
	parsed, _ := url.Parse(srv.URL)
	cfg := cfgWithDomains("p", []string{parsed.Hostname()})

	if _, err := Get(context.Background(), srv.URL, nil, cfg, "p"); err == nil {
		t.Fatalf("a body over %d bytes must be an error, not a truncated success", maxResponseBytes)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// ---------------------------------------------------------------------------
// M3 -- credentials are not written to the log
// ---------------------------------------------------------------------------

func TestRedactURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		gone string
	}{
		{"https://user:hunter2@example.com/path", "https://***@example.com/path", "hunter2"},
		{"https://example.com/x?api_key=abcdef123&page=2", "", "abcdef123"},
		{"https://example.com/x?access_token=zzz", "", "zzz"},
		{"https://example.com/plain", "https://example.com/plain", ""},
	}
	for _, tc := range cases {
		got := redactURL(tc.in)
		if tc.want != "" && got != tc.want {
			t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if tc.gone != "" && strings.Contains(got, tc.gone) {
			t.Errorf("redactURL(%q) = %q still contains %q", tc.in, got, tc.gone)
		}
	}
}
