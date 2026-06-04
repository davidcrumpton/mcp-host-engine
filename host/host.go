// host/host.go
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
	httpHeaders := make(map[string]string)
	if h, ok := ctx.Value("http_headers").(http.Header); ok {
		for k, vs := range h {
			if len(vs) > 0 {
				if IsHTTPHeaderAllowed(k, cfg) {
					httpHeaders[k] = vs[0]
				}
			}
		}
	}

	pluginConfig := map[string]interface{}{
		"mcp-version": config.Version,
		"mcp-commit":  config.Commit,
	}
	if pCfg, ok := cfg.Plugins[pluginName]; ok {
		for k, v := range pCfg {
			pluginConfig[k] = v
		}
	}

	pid := fmt.Sprintf("%d", os.Getpid())

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
			return GetEnv(key, cfg, pluginName)
		},
		"readStream": func(path string, options map[string]interface{}, callback func(chunk string) error) error {
			return ReadStream(path, cfg, pluginName, options, callback)
		},
		"writeStream": func(path string, options map[string]interface{}, callback func() (string, error)) error {
			return WriteStream(path, cfg, pluginName, options, callback)
		},
		"makeDir": func(path string) error {
			return Mkdir(path, cfg, pluginName)
		},
		"rmDir": func(path string) error {
			return rmDir(path, cfg, pluginName)
		},
		"isdir": func(path string) (bool, error) {
			return IsDir(path, cfg, pluginName)
		},
		"isexisted": func(path string) (bool, error) {
			return isPathExisted(path, cfg, pluginName)
		},
		"config":      pluginConfig,
		"pid":         pid,
		"httpHeaders": httpHeaders,
	}
}

func IsHTTPHeaderAllowed(header string, cfg config.Config) bool {
	for _, allowedHeader := range cfg.AllowedHTTPHeaders {
		if header == allowedHeader {
			return true
		}
	}
	cfg.Logf(4, "header: %s not in allowed headers", header)
	return false
}

// isPathAllowed checks whether path falls under one of allowedPaths.
// It resolves symlinks (via filepath.EvalSymlinks) before comparing, preventing
// a symlink inside an allowed directory from pointing outside it.
//
// For paths that do not exist yet (e.g. write targets), we resolve symlinks on
// the parent directory and re-join the filename. This correctly handles systems
// like macOS where os.TempDir() returns a symlink (e.g. /var/folders → /private/var/folders)
// so that a non-existent child path still resolves to the same real root as the
// allowed directory.
func isPathAllowed(path string, allowedPaths []string) bool {
	resolved := resolvePath(path)
	if resolved == "" {
		return false
	}

	for _, allowed := range allowedPaths {
		resolvedAllowed := resolvePath(allowed)
		if resolvedAllowed == "" {
			continue
		}
		// Ensure the prefix match ends on a directory boundary to prevent
		// /allowed/foo matching /allowed/foobar.
		if resolved == resolvedAllowed || strings.HasPrefix(resolved, resolvedAllowed+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolvePath returns the real absolute path for p, resolving symlinks.
// If p does not exist, it resolves symlinks on the parent directory and
// re-joins the base name, so that a non-existent child of a symlinked
// directory (e.g. macOS /var/folders) still compares correctly.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	// p doesn't exist yet — resolve the parent and re-join.
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	parent := filepath.Dir(abs)
	base := filepath.Base(abs)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// Parent doesn't exist either; best effort with Abs.
		return abs
	}
	return filepath.Join(resolvedParent, base)
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

// commandName returns the leading word of a shell command string, i.e. the
// executable name without arguments. This is used for allow-list matching so
// that "git" does not accidentally permit "git-evil".
func commandName(command string) string {
	// Trim leading whitespace, then take everything up to the first space/tab.
	command = strings.TrimLeft(command, " \t")
	if idx := strings.IndexAny(command, " \t"); idx >= 0 {
		return command[:idx]
	}
	return command
}

func RunCommand(ctx context.Context, cfg config.Config, pluginName string, command string) (string, error) {
	var cmd *exec.Cmd

	// Match on the command name (first whitespace-delimited token) rather than
	// a raw prefix, preventing "git-evil" from matching an allowlisted "git".
	cmdName := commandName(command)
	allowed := false
	for _, allowedCmd := range cfg.AllowedRunCommandsFor(pluginName) {
		if cmdName == allowedCmd {
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

// ReadStream reads a file in chunks and passes each chunk to the callback
func ReadStream(path string, cfg config.Config, pluginName string, options map[string]interface{}, callback func(chunk string) error) error {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))

	if !allowed {
		return fmt.Errorf("access to file %q is not allowed", path)
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Default chunk size is 4KB
	chunkSize := 4096
	if options != nil {
		if size, ok := options["chunkSize"].(float64); ok {
			chunkSize = int(size)
		}
	}

	buffer := make([]byte, chunkSize)
	for {
		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if err := callback(string(buffer[:n])); err != nil {
			return err
		}

		if err == io.EOF {
			break
		}
	}

	return nil
}

// WriteStream writes data to a file in chunks by calling the callback function
func WriteStream(path string, cfg config.Config, pluginName string, options map[string]interface{}, callback func() (string, error)) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("writing to file %q is not allowed", path)
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	for {
		chunk, err := callback()
		if err != nil {
			return err
		}
		if chunk == "" {
			// End of stream
			break
		}

		// Write the chunk to file
		if _, err := file.WriteString(chunk); err != nil {
			return err
		}
	}

	return nil
}

func rmDir(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("removing directory %q is not allowed", path)
	}
	return os.RemoveAll(path)
}

func Mkdir(path string, cfg config.Config, pluginName string) error {
	allowed := isPathAllowed(path, cfg.AllowedWriteFilePathsFor(pluginName))
	if !allowed {
		return fmt.Errorf("writing to directory %q is not allowed", path)
	}
	return os.MkdirAll(path, 0755)
}

func IsDir(path string, cfg config.Config, pluginName string) (bool, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return false, fmt.Errorf("access to directory %q is not allowed", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func isPathExisted(path string, cfg config.Config, pluginName string) (bool, error) {
	allowed := isPathAllowed(path, cfg.AllowedReadFilePathsFor(pluginName))
	if !allowed {
		return false, fmt.Errorf("access to file %q is not allowed", path)
	}
	_, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return true, nil
}

func HTTPGet(ctx context.Context, urlStr string, headers map[string]interface{}, cfg config.Config, pluginName string) (map[string]interface{}, error) {
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

func GetEnv(name string, cfg config.Config, pluginName string) (string, error) {
	allowedEnvs := cfg.AllowedENVsFor(pluginName)
	if len(allowedEnvs) == 0 {
		cfg.Logf(1, "Blocked access to environment variable %s for plugin %s - no allowed envs configured", name, pluginName)
		return "", fmt.Errorf("access to environment variable %s for plugin %s is not allowed", name, pluginName)
	}
	for _, env := range allowedEnvs {
		if env == name {
			return os.Getenv(name), nil
		}
	}
	cfg.Logf(1, "Blocked access to environment variable %s for plugin %s - not in allowed envs", name, pluginName)
	return "", fmt.Errorf("access to environment variable %s for plugin %s is not allowed", name, pluginName)
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