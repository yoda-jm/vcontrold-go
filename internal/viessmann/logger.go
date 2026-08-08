// Package viessmann - Logging functionality
package viessmann

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogLevel represents different logging levels
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// String returns the string representation of LogLevel
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger implements structured logging for the viessmann package
type Logger struct {
	level  LogLevel
	logger *log.Logger
}

// NewLogger creates a new logger instance
func NewLogger(level LogLevel) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(os.Stdout, "", 0),
	}
}

// NewDefaultLogger creates a logger with INFO level
func NewDefaultLogger() *Logger {
	return NewLogger(INFO)
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, args ...interface{}) {
	if l.level <= DEBUG {
		l.logMessage(DEBUG, msg, args...)
	}
}

// Info logs an info message
func (l *Logger) Info(msg string, args ...interface{}) {
	if l.level <= INFO {
		l.logMessage(INFO, msg, args...)
	}
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, args ...interface{}) {
	if l.level <= WARN {
		l.logMessage(WARN, msg, args...)
	}
}

// Error logs an error message
func (l *Logger) Error(msg string, args ...interface{}) {
	if l.level <= ERROR {
		l.logMessage(ERROR, msg, args...)
	}
}

// logMessage formats and outputs the log message
func (l *Logger) logMessage(level LogLevel, msg string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	formattedMsg := fmt.Sprintf(msg, args...)
	l.logger.Printf("[%s] %s: %s", timestamp, level.String(), formattedMsg)
}

// SetLevel changes the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// NoOpLogger is a logger that does nothing - useful for testing
type NoOpLogger struct{}

// NewNoOpLogger creates a logger that discards all messages
func NewNoOpLogger() *NoOpLogger {
	return &NoOpLogger{}
}

// Debug does nothing
func (n *NoOpLogger) Debug(msg string, args ...interface{}) {}

// Info does nothing
func (n *NoOpLogger) Info(msg string, args ...interface{}) {}

// Warn does nothing
func (n *NoOpLogger) Warn(msg string, args ...interface{}) {}

// Error does nothing
func (n *NoOpLogger) Error(msg string, args ...interface{}) {}
