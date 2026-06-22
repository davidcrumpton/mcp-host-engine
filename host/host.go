package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mcphe/config"
	"mcphe/host/crypto"
	"mcphe/host/env"
	"mcphe/host/exec"
	"mcphe/host/fs"
	"mcphe/host/httpclient"
	"mcphe/host/path"
	"mcphe/host/url"
	"mcphe/transport"
	"net/http"
	"os"
	"time"
)

func MakeHostObject(cfg config.Config, ctx context.Context, pluginName string) map[string]interface{} {
	httpHeaders := make(map[string]string)
	if h, ok := ctx.Value("http_headers").(http.Header); ok {
		for k, vs := range h {
			if len(vs) > 0 {
				if httpclient.IsHTTPHeaderAllowed(k, cfg) {
					httpHeaders[k] = vs[0]
				}
			}
		}
	}

	// Extract identity and sessionID from context for logging
	identity, _ := ctx.Value(transport.IdentityContextKey).(string)
	if identity == "" {
		identity = "-"
	}
	sessionID, _ := ctx.Value(transport.SessionIDContextKey).(string)
	if sessionID == "" {
		sessionID = "-"
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
		// Informational
		"logger": func(level int, format string, args ...interface{}) {
			cfg.LogfForPluginWithContext(pluginName, identity, sessionID, level, format, args...)
		},
		"config":      pluginConfig,
		"pid":         pid,
		"httpHeaders": httpHeaders,

		// JavaScript style functions and objects, will become the future format
		// making above functions and objects obsolete and providing porters
		// an easier pathway to port their plugin to GoJa JavsScript plugin.

		"crypto": map[string]interface{}{
			"randomBytes": func(n int) (string, error) {
				return crypto.RandomBytes(n)
			},
			"sha256": func(data string) (string, error) {
				return crypto.Sha256(data)
			},
		},
		"sleep": func(ms int) error {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return nil
		},
		"console": map[string]interface{}{
			"log": func(msg string) error {
				cfg.LogfWithContext(1, identity, sessionID, "%s: %s", pluginName, msg)
				return nil
			},
			"debug": func(msg string) error {
				cfg.LogfWithContext(4, identity, sessionID, "%s: %s", pluginName, msg)
				return nil
			},
			"warn": func(msg string) error {
				cfg.LogfWithContext(2, identity, sessionID, "%s: %s", pluginName, msg)
				return nil
			},
			"error": func(msg string) error {
				cfg.LogfWithContext(3, identity, sessionID, "%s: %s", pluginName, msg)
				return nil
			},
		},
		"path": map[string]interface{}{
			"basename": func(p string) (string, error) {
				return path.Basename(p), nil
			},
			// join: Joins path segments into a single path string, using the appropriate separator (e.g., path.join('/foo', 'bar', 'baz') -> '/foo/bar/baz').
			"join": func(paths ...string) (string, error) {
				return path.Join(paths...), nil
			},
			// resolve: Resolves a sequence of paths or path segments into an absolute path (e.g., path.resolve('/foo', '/bar', 'baz') -> '/bar/baz').
			"resolve": func(paths ...string) (string, error) {
				return path.Resolve(paths...), nil
			},
			// normalize: Normalizes a path, resolving '..' and '.' segments (e.g., path.normalize('/foo/bar/../baz') -> '/foo/baz').
			"normalize": func(p string) (string, error) {
				return path.Normalize(p), nil
			},
			// dirname: Returns the directory name of a path (e.g., path.dirname('/foo/bar/baz') -> '/foo/bar').
			"dirname": func(p string) (string, error) {
				return path.Dirname(p), nil

			},
			// extname: Returns the extension of a path (e.g., path.extname('/foo/bar.txt') -> '.txt').
			"extname": func(p string) (string, error) {
				return path.Extname(p), nil
			},
		},
		"fs": map[string]interface{}{
			"readFile":   func(path string) (string, error) { return fs.ReadFile(path, cfg, pluginName) },
			"writeFile":  func(path string, content string) error { return fs.WriteFile(path, content, cfg, pluginName) },
			"listFiles":  func(path string) ([]string, error) { return fs.ListFiles(path, cfg, pluginName) },
			"deleteFile": func(path string) error { return fs.DeleteFile(path, cfg, pluginName) },
			"renameFile": func(oldPath, newPath string) error {
				return fs.RenameFile(oldPath, newPath, cfg, pluginName)
			},
			"readStream": func(path string, options map[string]interface{}, callback func(chunk string) error) error {
				return fs.ReadStream(path, cfg, pluginName, options, callback)
			},
			"writeStream": func(path string, options map[string]interface{}, callback func() (string, error)) error {
				return fs.WriteStream(path, cfg, pluginName, options, callback)
			},
			"makeDir": func(path string) error {
				return fs.Mkdir(path, cfg, pluginName)
			},
			"rmDir": func(path string) error {
				return fs.RmDir(path, cfg, pluginName)
			},
			"isDir": func(path string) (bool, error) {
				return fs.IsDir(path, cfg, pluginName)
			},
			"exists": func(path string) (bool, error) {
				return fs.IsPathExisted(path, cfg, pluginName)
			},
		},
		"http": map[string]interface{}{
			"get": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
				return httpclient.Get(ctx, urlStr, headers, cfg, pluginName)
			},
			"post": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
				return httpclient.Post(ctx, urlStr, headers, body, cfg, pluginName)
			},
			"patch": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
				return httpclient.Patch(ctx, urlStr, headers, body, cfg, pluginName)
			},
			"put": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
				return httpclient.Put(ctx, urlStr, headers, body, cfg, pluginName)
			},
			"delete": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
				return httpclient.Delete(ctx, urlStr, headers, "", cfg, pluginName)
			},
			"options": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
				return httpclient.Options(ctx, urlStr, headers, cfg, pluginName)
			},
			"head": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
				return httpclient.Head(ctx, urlStr, headers, cfg, pluginName)
			},
			"rawPost": func(urlStr string, headers map[string]interface{}, body interface{}) (map[string]interface{}, error) {
				var bodyStr string
				switch v := body.(type) {
				case string:
					bodyStr = v
				case map[string]interface{}, []interface{}:
					b, err := json.Marshal(v)
					if err != nil {
						return nil, fmt.Errorf("failed to marshal body: %w", err)
					}
					bodyStr = string(b)
				default:
					bodyStr = fmt.Sprintf("%v", v)
				}
				return httpclient.Post(ctx, urlStr, headers, bodyStr, cfg, pluginName)
			},
			"request": func(opts map[string]interface{}) (map[string]interface{}, error) {
				urlVal, ok := opts["url"].(string)
				if !ok || urlVal == "" {
					return nil, fmt.Errorf("http.request: url option is required and must be a string")
				}
				methodVal, ok := opts["method"].(string)
				if !ok || methodVal == "" {
					methodVal = "GET"
				}
				var headersVal map[string]interface{}
				if h, ok := opts["headers"].(map[string]interface{}); ok {
					headersVal = h
				}
				var bodyStr string
				if body, exists := opts["body"]; exists && body != nil {
					switch v := body.(type) {
					case string:
						bodyStr = v
					case map[string]interface{}, []interface{}:
						b, err := json.Marshal(v)
						if err != nil {
							return nil, fmt.Errorf("failed to marshal body: %w", err)
						}
						bodyStr = string(b)
					default:
						bodyStr = fmt.Sprintf("%v", v)
					}
				}
				return httpclient.Request(ctx, methodVal, urlVal, headersVal, bodyStr, cfg, pluginName)
			},
		},
		"exec": map[string]interface{}{
			"runCommand": func(command string) (string, error) { return exec.RunCommand(ctx, cfg, pluginName, command) },
		},
		"process": map[string]interface{}{
			"config": func() map[string]interface{} { return pluginConfig },
			"env":    func(key string) (string, error) { return env.GetEnv(key, cfg, pluginName) },
		},
		"mcp": map[string]interface{}{
			"version": func() string { return config.Version },
			"commit":  func() string { return config.Commit },
			"logger":  cfg.LogfForPlugin(pluginName),
			"config":  func() map[string]interface{} { return pluginConfig },
		},
		"server": map[string]interface{}{
			"version": func() string { return config.Version },
			"commit":  func() string { return config.Commit },
			"logger":  cfg.LogfForPlugin(pluginName),
			"config": func(name string) map[string]interface{} {
				if len(name) == 0 {
					name = pluginName
				}
				pluginConfig := map[string]interface{}{
					"mcp-version": config.Version,
					"mcp-commit":  config.Commit,
				}
				if pCfg, ok := cfg.Plugins[name]; ok {
					for k, v := range pCfg {
						pluginConfig[k] = v
					}
				}
				return pluginConfig
			},
			"httpHeaders": httpHeaders,
		},
		"utils": map[string]interface{}{
			"btoa": func(str string) string { return base64.StdEncoding.EncodeToString([]byte(str)) },
			"atob": func(str string) string {
				bytes, _ := base64.StdEncoding.DecodeString(str)
				return string(bytes)
			},
		},
		"url": url.MakeURLNamespace(),
	}
}
