package util

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

var (
	logger     *slog.Logger
	logFile    *os.File
	logEnabled bool
)

// InitLogger initializes the structured logger using Go's slog package
// It's enabled when verbose level >= 1 or debug flag is set
func InitLogger(verboseLevel int, enableDebug bool) error {
	// Enable logging if verbose or debug flag is set
	if verboseLevel >= 1 || enableDebug {
		logEnabled = true
	} else {
		logEnabled = false
		// Set a no-op logger
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		return nil
	}

	// Open debug.log file
	var err error
	logFile, err = os.OpenFile("debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open debug.log: %w", err)
	}

	// Determine log level and output destinations
	var logLevel slog.Level
	var writers []io.Writer

	writers = append(writers, logFile) // Always write to file

	switch {
	case verboseLevel >= 3 || enableDebug:
		logLevel = slog.LevelDebug
		writers = append(writers, os.Stderr) // Debug: also write to stderr
	case verboseLevel >= 2:
		logLevel = slog.LevelInfo
		writers = append(writers, os.Stderr) // Detailed: also write to stderr
	default:
		logLevel = slog.LevelInfo // Basic: file only
	}

	// Create multi-writer and structured logger
	multiWriter := io.MultiWriter(writers...)
	handler := slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		Level: logLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Simplify the timestamp format
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().Format("15:04:05"))
			}
			return a
		},
	})

	logger = slog.New(handler)

	// Log session start
	logger.Info("=== Dovetail Debug Session Started ===",
		"verbose_level", verboseLevel,
		"debug_enabled", enableDebug)

	return nil
}

// CleanupLogger closes the debug log file
func CleanupLogger() {
	if logEnabled && logger != nil {
		logger.Info("=== Dovetail Debug Session Ended ===")
	}

	if logFile != nil {
		logFile.Close()
		logFile = nil
	}

	logEnabled = false
	logger = nil
}

// DebugPrintf writes debug messages using structured logging (zero-alloc when disabled)
func DebugPrintf(format string, args ...interface{}) {
	if logger != nil && logger.Enabled(nil, slog.LevelDebug) {
		logger.Debug(fmt.Sprintf(format, args...))
	}
}

// LogInfo writes info-level messages using structured logging (zero-alloc when disabled)
func LogInfo(format string, args ...interface{}) {
	if logger != nil && logger.Enabled(nil, slog.LevelInfo) {
		logger.Info(fmt.Sprintf(format, args...))
	}
}

// LogError writes error messages to both log and stderr
func LogError(format string, args ...interface{}) {
	if logger != nil && logger.Enabled(nil, slog.LevelError) {
		logger.Error(fmt.Sprintf(format, args...))
	}
	// Always write errors to stderr regardless of logging
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
}

// LogWarning writes warning messages using structured logging (zero-alloc when disabled)
func LogWarning(format string, args ...interface{}) {
	if logger != nil && logger.Enabled(nil, slog.LevelWarn) {
		logger.Warn(fmt.Sprintf(format, args...))
	}
}

// LogProgress writes progress messages using structured logging (zero-alloc when disabled)
func LogProgress(format string, args ...interface{}) {
	if logger != nil && logger.Enabled(nil, slog.LevelInfo) {
		logger.Info(fmt.Sprintf(format, args...), "type", "progress")
	}
}

// === STRUCTURED LOGGING INTERFACES (Preferred for new code) ===

// Debug writes a debug message with optional structured attributes
func Debug(msg string, attrs ...any) {
	if logger != nil {
		logger.Debug(msg, attrs...)
	}
}

// Info writes an info message with optional structured attributes
func Info(msg string, attrs ...any) {
	if logger != nil {
		logger.Info(msg, attrs...)
	}
}

// Warn writes a warning message with optional structured attributes
func Warn(msg string, attrs ...any) {
	if logger != nil {
		logger.Warn(msg, attrs...)
	}
}

// Error writes an error message with optional structured attributes
func Error(msg string, attrs ...any) {
	if logger != nil {
		logger.Error(msg, attrs...)
	}
	// Also write to stderr
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", msg)
}

// Progress writes a progress message with structured attributes
func Progress(msg string, attrs ...any) {
	if logger != nil {
		attrs = append(attrs, "type", "progress")
		logger.Info(msg, attrs...)
	}
}

// === CONDITIONAL LOGGING FOR EXPENSIVE OPERATIONS ===

// DebugEnabled returns true if debug logging is enabled
func DebugEnabled() bool {
	return logger != nil && logger.Enabled(nil, slog.LevelDebug)
}

// InfoEnabled returns true if info logging is enabled
func InfoEnabled() bool {
	return logger != nil && logger.Enabled(nil, slog.LevelInfo)
}

// IsLogEnabled returns whether logging is currently enabled
func IsLogEnabled() bool {
	return logEnabled
}

// GetLogger returns the current slog.Logger instance for advanced usage
func GetLogger() *slog.Logger {
	return logger
}
