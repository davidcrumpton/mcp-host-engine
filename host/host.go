package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mcphe/config"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func MakeHostObject(cfg config.Config, ctx context.Context, pluginName string) map[string]interface{} {
	pluginConfig := map[string]interface{}{
		"mcp-version": config.Version,
		"mcp-commit":  config.Commit,
	}
	if pCfg, ok := cfg.Plugins[pluginName]; ok {
		for k, v := range pCfg {
			pluginConfig[k] = v
		}
	}

	return map[string]interface{}{
		"readFile":   func(path string) (string, error) { return ReadFile(path, cfg, pluginName) },
		"writeFile":  func(path string, content string) error { return WriteFile(path, content, cfg, pluginName) },
		"listFiles":  func(path string) ([]string, error) { return ListFiles(path, cfg, pluginName) },
		"deleteFile": func(path string) error { return DeleteFile(path, cfg, pluginName) },
		"renameFile": func(oldPath, newPath string) error {
			return RenameFile(oldPath, newPath, cfg, pluginName)
		},
		"runCommand": func(command string) (string, error) { return RunCommand(ctx, cfg, pluginName, command) },
		"httpGet": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
			return HTTPGet(ctx, urlStr, headers, cfg, pluginName)
		},
		"httpPost": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
			return HTTPPost(ctx, urlStr, headers, body, cfg, pluginName)
		},
		"httpPut": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
			return HTTPPut(ctx, urlStr, headers, body, cfg, pluginName)
		},
		"httpDelete": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
			return HTTPDelete(ctx, urlStr, headers, "", cfg, pluginName)
		},
		"getEnv": func(key string) (string, error) {
			return GetEnv(key, cfg, pluginName), nil
		},
		"config": pluginConfig,
	}
}

func isPathAllowed(path string, allowedPaths []string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, allowed := range allowedPaths {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			return false
		}
		if strings.HasPrefix(absPath, absAllowed) {
			return true
		}
	}
	return false
}

func ReadFile(path string, cfg config.Config, pluginName string) (string, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	
	if !allowed {
		return "", fmt.Errorf("access to file %q is not allowed", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func WriteFile(path string, content string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	
	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func ListFiles(path string, cfg config.Config, pluginName string) ([]string, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return nil, fmt.Errorf("access to directory %q is not allowed", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files, nil
}

func DeleteFile(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("deleting file %q is not allowed", path)
	}
	return os.Remove(path)
}

func RenameFile(oldPath, newPath string, cfg config.Config, pluginName string) error {
	allowedOld := isPathAllowed(oldPath, cfg.AllowedWriteFilePathsFor(pluginName))
	allowedNew := isPathAllowed(newPath, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowedOld || !allowedNew {
		return fmt.Errorf("renaming file from %q to %q is not allowed", oldPath, newPath)
	}
	return os.Rename(oldPath, newPath)
}

func RunCommand(ctx context.Context, cfg config.Config, pluginName string, command string) (string, error) {
	var cmd *exec.Cmd
	
	allowed := false
	for _, allowedCmd := range cfg.AllowedRunCommandsFor(pluginName) {
		if strings.HasPrefix(command, allowedCmd) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("command %q is not allowed", command)
	}
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func HTTPGet(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPGet %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isAllowedDomain(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
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

func HTTPPost(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPPost %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isAllowedDomain(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
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

func HTTPPut(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPPut called with URL: %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isAllowedDomain(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
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

func HTTPDelete(ctx context.Context, urlStr string, headers map[string]interface{}, body string, cfg config.Config, pluginName string) (map[string]interface{}, error) {
	cfg.Logf(3, "HTTPDelete called with URL: %s", urlStr)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		cfg.Logf(1, "Invalid URL %s: %v", urlStr, err)
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if !isAllowedDomain(parsedURL.Hostname(), cfg.AllowedDomainsFor(pluginName)) {
		cfg.Logf(1, "Blocked HTTP request to %s - not in allowed domains", parsedURL.Hostname())
		return nil, fmt.Errorf("access to %s is not allowed", parsedURL.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, urlStr, strings.NewReader(body))
	if err != nil {
		cfg.Logf(1, "Failed to create HTTP request: %v", err)
		return nil, err
	}

	for k, v := range headers {
		if str, ok := v.(string); ok {
			req.Header.Set(k, str)
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

func GetEnv(name string, cfg config.Config, pluginName string) string {
	allowedEnvs := cfg.AllowedENVsFor(pluginName)
	if len(allowedEnvs) == 0 {
		cfg.Logf(1, "Blocked access to environment variable %s for plugin %s - no allowed envs configured", name, pluginName)
		return ""
	}
	for _, env := range allowedEnvs {
		if env == name {
			return os.Getenv(name)
		}
	}
	cfg.Logf(1, "Blocked access to environment variable %s for plugin %s - not in allowed envs", name, pluginName)
	return ""
}

func isAllowedDomain(hostname string, allowed []string) bool {
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
