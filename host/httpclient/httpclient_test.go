package httpclient

import (
	"context"
	"fmt"
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