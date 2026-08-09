package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLevel_FiltersBelowThreshold(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelWarn)

	l.Debug("a debug line")
	l.Info("an info line")
	l.Warn("a warning")
	l.Error("an error")

	out := buf.String()
	for _, hidden := range []string{"a debug line", "an info line"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q should be filtered out at level warn", hidden)
		}
	}
	for _, shown := range []string{"a warning", "an error"} {
		if !strings.Contains(out, shown) {
			t.Errorf("%q should be printed at level warn", shown)
		}
	}
}

// The whole reason this package exists: one format for every component, so a
// journal does not interleave two shapes of line.
func TestLogger_FormatIsConsistent(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)

	l.Info("plain message")
	l.WithPrefix("mqtt").Info("prefixed message")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	for _, line := range lines {
		if !strings.HasPrefix(line, "[") {
			t.Errorf("line does not start with a timestamp: %q", line)
		}
		if !strings.Contains(line, "INFO") {
			t.Errorf("line does not carry its level: %q", line)
		}
	}
	if !strings.Contains(lines[1], "mqtt:") {
		t.Errorf("prefixed line should name its component: %q", lines[1])
	}
}

// -verbose must reach every component, which is what it failed to do when each
// package logged through the standard library.
func TestSetLevel_AppliesToPrefixedLoggers(t *testing.T) {
	var buf bytes.Buffer
	parent := New(&buf, LevelInfo)
	child := parent.WithPrefix("serial")

	child.Debug("hidden at info")
	if strings.Contains(buf.String(), "hidden at info") {
		t.Error("debug should be filtered at info level")
	}

	child.SetLevel(LevelDebug)
	child.Debug("visible at debug")
	if !strings.Contains(buf.String(), "visible at debug") {
		t.Error("debug should appear once the level allows it")
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]Level{
		"debug":   LevelDebug,
		"DEBUG":   LevelDebug,
		"info":    LevelInfo,
		"":        LevelInfo,
		" warn ":  LevelWarn,
		"warning": LevelWarn,
		"error":   LevelError,
	}
	for in, want := range tests {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q) returned %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}

	if _, err := ParseLevel("chatty"); err == nil {
		t.Error("expected an error for an unknown level")
	}
}

// Enabled lets a caller skip building an expensive message, such as a hex dump
// of a serial frame, when it would only be discarded.
func TestLogger_Enabled(t *testing.T) {
	l := New(&bytes.Buffer{}, LevelInfo)

	if l.Enabled(LevelDebug) {
		t.Error("debug should not be enabled at info level")
	}
	if !l.Enabled(LevelWarn) {
		t.Error("warn should be enabled at info level")
	}
}

// The daemon logs from several refresh goroutines at once.
func TestLogger_ConcurrentUse(t *testing.T) {
	l := New(&bytes.Buffer{}, LevelInfo)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Info("message %d", n)
			l.SetLevel(LevelInfo)
			_ = l.Level()
		}(i)
	}
	wg.Wait()
}

func TestNoOp_DiscardsEverything(t *testing.T) {
	// Compiles only if NoOp satisfies the same shape the reader expects.
	var l interface {
		Debug(string, ...interface{})
		Info(string, ...interface{})
		Warn(string, ...interface{})
		Error(string, ...interface{})
	} = NoOp{}

	l.Debug("x")
	l.Info("x")
	l.Warn("x")
	l.Error("x")
}
