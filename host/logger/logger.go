package logger

import "mcphe/config"

// NewLogger creates a new Logger instance based on the provided configuration and plugin name. It can be extended to return different Logger implementations based on the config.
func NewLogger(cfg config.Config, pluginName string) Logger {
	// For now, we return a NoOpLogger, but this can be extended to return different loggers based on the config.
	return NoOpLogger{}
}

// Logger is an interface that defines methods for logging messages at different levels. This allows for flexible logging implementations that can be swapped out as needed.
type Logger interface {
	Logf(level int, format string, args ...interface{})
}

// NoOpLogger is a Logger implementation that does nothing. This can be used when logging is disabled or not needed.
type NoOpLogger struct{}

// Logf for NoOpLogger does nothing, effectively disabling logging.
func (l NoOpLogger) Logf(level int, format string, args ...interface{}) {
	// No operation performed
}
