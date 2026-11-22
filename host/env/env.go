// host/env/env.go
package env

import (
	"fmt"
	"os"

	"mcphe/config"
)

func GetEnv(name string, cfg config.Config, pluginName string) (string, error) {
	allowedEnvs := cfg.AllowedENVsFor(pluginName)
	if len(allowedEnvs) == 0 {
		cfg.Logf(1, "Blocked access to environment variable %q - no environment variables are allowed", name)
		return "", fmt.Errorf("access to environment variable %q is not allowed", name)
	}
	allowed := false
	for _, allowedEnv := range allowedEnvs {
		if name == allowedEnv {
			allowed = true
			break
		}
	}
	if !allowed {
		cfg.Logf(1, "Blocked access to environment variable %q - not in allowed list", name)
		return "", fmt.Errorf("access to environment variable %q is not allowed", name)
	}
	return os.Getenv(name), nil
}
