// Package logging provides the one logger every package here writes through.
//
// It lives in its own package for a reason. The logger used to sit inside
// internal/viessmann, out of reach of internal/mqtt and internal/web, so those
// packages fell back on the standard library. The result was two formats
// interleaved in one journal:
//
//	2026/08/09 10:42:19 Controller: V200KW2 ID=0x2098
//	[2026-08-09 10:42:19] INFO: Data point 0x0800 getTempA
//
// and, worse, a -verbose flag that reached only half the daemon.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Level orders messages by how much you want to see.
type Level int

const (
	// LevelDebug shows every serial transaction and every skipped publish.
	// Useful when a reading is wrong and you need to see the bytes.
	LevelDebug Level = iota

	// LevelInfo is the default: what the daemon did, not how.
	LevelInfo

	// LevelWarn covers a reading that failed but will be retried.
	LevelWarn

	// LevelError covers what the daemon could not do at all.
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel maps a configuration string onto a level.
func ParseLevel(name string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", name)
	}
}

// Logger writes levelled messages to one destination.
type Logger struct {
	mutex  sync.RWMutex
	level  Level
	out    *log.Logger
	prefix string
}

// New builds a logger writing to w.
func New(w io.Writer, level Level) *Logger {
	return &Logger{
		level: level,
		// No stdlib timestamp: the timestamp is formatted below, so that every
		// line looks the same whether it went through here or through journald.
		out: log.New(w, "", 0),
	}
}

// WithPrefix returns a logger tagging its messages with a component name, so a
// line says which part of the daemon produced it.
func (l *Logger) WithPrefix(prefix string) *Logger {
	child := New(io.Discard, l.Level())
	l.mutex.RLock()
	child.out = l.out
	l.mutex.RUnlock()
	child.prefix = prefix
	return child
}

// Level reports the current threshold.
func (l *Logger) Level() Level {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	return l.level
}

// SetLevel changes the threshold. Safe to call while the daemon runs.
func (l *Logger) SetLevel(level Level) {
	l.mutex.Lock()
	l.level = level
	l.mutex.Unlock()
}

// Enabled reports whether a level would be printed. Worth checking before
// building an expensive message such as a hex dump.
func (l *Logger) Enabled(level Level) bool {
	return level >= l.Level()
}

func (l *Logger) logAt(level Level, msg string, args ...interface{}) {
	if !l.Enabled(level) {
		return
	}

	text := msg
	if len(args) > 0 {
		text = fmt.Sprintf(msg, args...)
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if l.prefix != "" {
		l.out.Printf("[%s] %-5s %s: %s", timestamp, level, l.prefix, text)
		return
	}
	l.out.Printf("[%s] %-5s %s", timestamp, level, text)
}

// Debug logs the detail you only want while diagnosing something.
func (l *Logger) Debug(msg string, args ...interface{}) { l.logAt(LevelDebug, msg, args...) }

// Info logs what the daemon did.
func (l *Logger) Info(msg string, args ...interface{}) { l.logAt(LevelInfo, msg, args...) }

// Warn logs something that failed but is recoverable.
func (l *Logger) Warn(msg string, args ...interface{}) { l.logAt(LevelWarn, msg, args...) }

// Error logs something the daemon could not do.
func (l *Logger) Error(msg string, args ...interface{}) { l.logAt(LevelError, msg, args...) }

// defaultLogger is what the package-level helpers write through, so a call site
// needs no plumbing to log correctly.
var defaultLogger = New(os.Stdout, LevelInfo)

// Default returns the shared logger.
func Default() *Logger { return defaultLogger }

// SetLevel sets the shared logger's threshold. This is what -verbose drives.
func SetLevel(level Level) { defaultLogger.SetLevel(level) }

// For returns a shared logger tagged with a component name.
func For(component string) *Logger { return defaultLogger.WithPrefix(component) }

// Package-level shorthands for call sites that have no logger to hand.
func Debug(msg string, args ...interface{}) { defaultLogger.logAt(LevelDebug, msg, args...) }
func Info(msg string, args ...interface{})  { defaultLogger.logAt(LevelInfo, msg, args...) }
func Warn(msg string, args ...interface{})  { defaultLogger.logAt(LevelWarn, msg, args...) }
func Error(msg string, args ...interface{}) { defaultLogger.logAt(LevelError, msg, args...) }

// NoOp discards everything. Useful in tests, where log output is noise.
type NoOp struct{}

func (NoOp) Debug(string, ...interface{}) {}
func (NoOp) Info(string, ...interface{})  {}
func (NoOp) Warn(string, ...interface{})  {}
func (NoOp) Error(string, ...interface{}) {}
