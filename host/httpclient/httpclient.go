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
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func Get(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPGet %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isDomainAllowed(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		cfg.Logf(4, "HTTPGet header inbound: %s: %v", k, v)
		if str, ok := v.(string); ok {
			cfg.Logf(4, "HTTPGet header set: %s: %s", k, str)
			req.Header.Set(k, str)
		} else if nested, ok := v.(map[string]interface{}); ok && k == "headers" {
			for hk, hv := range nested {
				if hstr, ok := hv.(string); ok {
					cfg.Logf(4, "HTTPGet header set (nested): %s: %s", hk, hstr)
					req.Header.Set(hk, hstr)
				}
			}
		}
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}
	req.Header.Set("X-Debugging-Plugin", "yes")
	resp, err := httpClient.Do(req)
	if err != nil {
		cfg.Logf(1, "HTTP request to %s failed: %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read HTTP response body: %v", err)
		return nil, err
	}

	resHeaders := map[string]interface{}{}
	for k, v := range resp.Header {
		resHeaders[k] = v
	}

	return map[string]interface{}{
		"status":  resp.StatusCode,
		"headers": resHeaders,
		"body":    string(body),
	}, nil
}

func Post(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPPost %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isDomainAllowed(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		cfg.Logf(4, "HTTPGet header inbound: %s: %v", k, v)
		if str, ok := v.(string); ok {
			cfg.Logf(4, "HTTPGet header set: %s: %s", k, str)
			req.Header.Set(k, str)
		} else if nested, ok := v.(map[string]interface{}); ok && k == "headers" {
			for hk, hv := range nested {
				if hstr, ok := hv.(string); ok {
					cfg.Logf(4, "HTTPGet header set (nested): %s: %s", hk, hstr)
					req.Header.Set(hk, hstr)
				}
			}
		}
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		cfg.Logf(1, "HTTP request to %s failed: %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read HTTP response body: %v", err)
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

func Put(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPPut called with URL: %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isDomainAllowed(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		cfg.Logf(4, "HTTPGet header inbound: %s: %v", k, v)
		if str, ok := v.(string); ok {
			cfg.Logf(4, "HTTPGet header set: %s: %s", k, str)
			req.Header.Set(k, str)
		} else if nested, ok := v.(map[string]interface{}); ok && k == "headers" {
			for hk, hv := range nested {
				if hstr, ok := hv.(string); ok {
					cfg.Logf(4, "HTTPGet header set (nested): %s: %s", hk, hstr)
					req.Header.Set(hk, hstr)
				}
			}
		}
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		cfg.Logf(1, "HTTP request to %s failed: %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read HTTP response body: %v", err)
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

func Delete(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPDelete called with URL: %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isDomainAllowed(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		cfg.Logf(4, "HTTPGet header inbound: %s: %v", k, v)
		if str, ok := v.(string); ok {
			cfg.Logf(4, "HTTPGet header set: %s: %s", k, str)
			req.Header.Set(k, str)
		} else if nested, ok := v.(map[string]interface{}); ok && k == "headers" {
			for hk, hv := range nested {
				if hstr, ok := hv.(string); ok {
					cfg.Logf(4, "HTTPGet header set (nested): %s: %s", hk, hstr)
					req.Header.Set(hk, hstr)
				}
			}
		}
	}

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mcphe/1.0")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		cfg.Logf(1, "HTTP request to %s failed: %v", urlStr, err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		cfg.Logf(1, "Failed to read HTTP response body: %v", err)
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

func IsHTTPHeaderAllowed(header string, cfg config.Config) bool {
	for _, allowedHeader := range cfg.AllowedHTTPHeaders {
		if header == allowedHeader {
			return true
		}
	}
	cfg.Logf(4, "HTTP-Header: header: %s not in allowed to be shared with plugin", header)
	return false
}
