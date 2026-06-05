package logger

import (
	"testing"

	"mcphe/config"
)

func TestNoOpLogger(t *testing.T) {
	cfg := config.Config{}
	logger := NewLogger(cfg, "testplugin")

	// This should not panic or do anything
	logger.Logf(1, "This is a test log message: %d", 42)
}

func TestLoggerInterface(t *testing.T) {
	cfg := config.Config{}
	logger := NewLogger(cfg, "testplugin")

	// Check that logger implements the Logger interface
	var _ Logger = logger
}

func TestNewLogger_ReturnsNoOp(t *testing.T) {
	cfg := config.Config{}
	logger := NewLogger(cfg, "testplugin")

	// Check that the returned logger is a NoOpLogger
	if _, ok := logger.(NoOpLogger); !ok {
		t.Errorf("expected NewLogger to return NoOpLogger, got %T", logger)
	}
}

func TestNewLogger_ConfigDoesNotAffectNoOp(t *testing.T) {
	cfg := config.Config{
		Plugins: map[string]map[string]interface{}{
			"testplugin": {"some_option": "some_value"},
		},
	}
	logger := NewLogger(cfg, "testplugin")

	// This should still not panic or do anything, even with config options
	logger.Logf(1, "Testing logger with config options")
}
