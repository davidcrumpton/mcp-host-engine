package host

import (
	"context"
	"encoding/json"
	"fmt"
	"mcphe/config"
	"mcphe/host/env"
	"mcphe/host/exec"
	"mcphe/host/fs"
	"mcphe/host/httpclient"
	"net/http"
	"os"
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
		// Legacy names will be deprecated in the future
		"readFile":   func(path string) (string, error) { return fs.ReadFile(path, cfg, pluginName) },
		"writeFile":  func(path string, content string) error { return fs.WriteFile(path, content, cfg, pluginName) },
		"listFiles":  func(path string) ([]string, error) { return fs.ListFiles(path, cfg, pluginName) },
		"deleteFile": func(path string) error { return fs.DeleteFile(path, cfg, pluginName) },
		"renameFile": func(oldPath, newPath string) error {
			return fs.RenameFile(oldPath, newPath, cfg, pluginName)
		},
		"runCommand": func(command string) (string, error) { return exec.RunCommand(ctx, cfg, pluginName, command) },
		"httpGet": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
			return httpclient.Get(ctx, urlStr, headers, cfg, pluginName)
		},
		"httpPost": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
			return httpclient.Post(ctx, urlStr, headers, body, cfg, pluginName)
		},
		"httpPut": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) {
			return httpclient.Put(ctx, urlStr, headers, body, cfg, pluginName)
		},
		"httpDelete": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) {
			return httpclient.Delete(ctx, urlStr, headers, "", cfg, pluginName)
		},
		"getEnv": func(key string) (string, error) {
			return env.GetEnv(key, cfg, pluginName)
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
		"isdir": func(path string) (bool, error) {
			return fs.IsDir(path, cfg, pluginName)
		},
		"isexisted": func(path string) (bool, error) {
			return fs.IsPathExisted(path, cfg, pluginName)
		},

		// Informational
		"logger":      cfg.LogfForPlugin(pluginName),
		"pluginConfig": pluginConfig,
		"config":      pluginConfig,
		"pid":         pid,
		"httpHeaders": httpHeaders,

		// JavaScript style functions and objects, will become the future format
		// making above functions and objects obsolete and providing porters
		// an easier pathway to port their plugin to GoJa JavsScript plugin.
		"fs": map[string]interface{}{
			"readFile": func(path string) (string, error) { return fs.ReadFile(path, cfg, pluginName) },
			"writeFile": func(path string, content string) error { return fs.WriteFile(path, content, cfg, pluginName) },
			"listFiles": func(path string) ([]string, error) { return fs.ListFiles(path, cfg, pluginName) },
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
			"get": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) { return httpclient.Get(ctx, urlStr, headers, cfg, pluginName) },
			"post": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) { return httpclient.Post(ctx, urlStr, headers, body, cfg, pluginName) },
			"put": func(urlStr string, headers map[string]interface{}, body string) (map[string]interface{}, error) { return httpclient.Put(ctx, urlStr, headers, body, cfg, pluginName) },
			"delete": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) { return httpclient.Delete(ctx, urlStr, headers, "", cfg, pluginName) },
			"options": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) { return httpclient.Options(ctx, urlStr, headers, cfg, pluginName) },
			"head": func(urlStr string, headers map[string]interface{}) (map[string]interface{}, error) { return httpclient.Head(ctx, urlStr, headers, cfg, pluginName) },
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
			"commit": func() string { return config.Commit },
			"logger": cfg.LogfForPlugin(pluginName),
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
	}
}