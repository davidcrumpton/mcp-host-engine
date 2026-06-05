package env

import (
	"os"
	"testing"

	"mcphe/config"
)

func TestGetEnv_Allowed(t *testing.T) {
	os.Setenv("MY_TEST_VAR", "secret")
	defer os.Unsetenv("MY_TEST_VAR")

	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_env_vars": []interface{}{"MY_TEST_VAR"}},
		},
	}
	val, _ := GetEnv("MY_TEST_VAR", cfg, "myplugin")
	if val != "secret" {
		t.Errorf("got %q, want %q", val, "secret")
	}
}

func TestGetEnv_NotAllowed(t *testing.T) {
	os.Setenv("SECRET", "shh")
	defer os.Unsetenv("SECRET")

	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"myplugin": {"allowed_env_vars": []interface{}{"HOME"}},
		},
	}
	val, _ := GetEnv("SECRET", cfg, "myplugin")
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}

func TestGetEnv_NoAllowedEnvs(t *testing.T) {
	os.Setenv("ANYTHING", "value")
	defer os.Unsetenv("ANYTHING")

	val, _ := GetEnv("ANYTHING", config.Config{}, "myplugin")
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}