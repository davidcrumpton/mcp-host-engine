package httpclient

import (
	"context"
	"fmt"
	"io"
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

func TestIsAllowedDomain_TrailingColon(t *testing.T) {
	// url.Parse may produce "hostname:" for bare IPs without ports.
	if !isDomainAllowed("example.com:", []string{"example.com"}) {
		t.Error("trailing colon should be stripped")
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