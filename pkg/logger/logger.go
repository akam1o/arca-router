package logger

import (
	"log/slog"
	"os"
	"time"
)

// Logger provides structured logging for arca-router components
type Logger struct {
	*slog.Logger
	component string
}

// Config holds logger configuration
type Config struct {
	Level     slog.Level
	AddSource bool
}

// DefaultConfig returns default logger configuration
func DefaultConfig() *Config {
	return &Config{
		Level:     slog.LevelInfo,
		AddSource: true,
	}
}

// New creates a new logger for a specific component
func New(component string, cfg *Config) *Logger {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	opts := &slog.HandlerOptions{
		Level:     cfg.Level,
		AddSource: cfg.AddSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Format time as RFC3339
			if a.Key == slog.TimeKey {
				t := a.Value.Time()
				a.Value = slog.StringValue(t.Format(time.RFC3339))
			}
			return a
		},
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	baseLogger := slog.New(handler)

	// Add component name to all log entries
	logger := baseLogger.With(slog.String("component", component))

	return &Logger{
		Logger:    logger,
		component: component,
	}
}

// WithField adds a field to the logger context
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		Logger:    l.Logger.With(slog.Any(key, value)),
		component: l.component,
	}
}

// ErrorWithCause logs an error with cause and suggested action
func (l *Logger) ErrorWithCause(msg string, err error, cause string, action string) {
	l.Error(msg,
		slog.Any("error", err),
		slog.String("cause", cause),
		slog.String("action", action),
	)
}

// Component returns the logger's component name
func (l *Logger) Component() string {
	return l.component
}
