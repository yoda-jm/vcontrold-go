package mqtt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/config"
)

func testRuntimeSpec() config.MQTTRuntimeConfig {
	return config.MQTTRuntimeConfig{
		SourceAlias:  "getPumpeStatusZirku",
		Alias:        "pumpZirkuHours",
		FriendlyName: "Heures pompe de bouclage",
	}
}

func TestRuntimeTracker_AccumulatesWhileOn(t *testing.T) {
	tracker := newRuntimeTracker("")
	spec := testRuntimeSpec()
	start := time.Now()

	// First sample establishes the baseline; nothing has elapsed yet.
	hours, _ := tracker.observe(spec, true, start)
	if hours != 0 {
		t.Fatalf("first sample = %v h, want 0", hours)
	}

	// One minute later, still on: one minute credited.
	hours, _ = tracker.observe(spec, true, start.Add(time.Minute))
	if want := 1.0 / 60; hours < want*0.99 || hours > want*1.01 {
		t.Errorf("after a minute on = %v h, want about %v", hours, want)
	}

	// Another minute, now off. The interval that just ended was on, so it
	// counts; what follows will not.
	hours, _ = tracker.observe(spec, false, start.Add(2*time.Minute))
	if want := 2.0 / 60; hours < want*0.99 || hours > want*1.01 {
		t.Errorf("after two minutes on = %v h, want about %v", hours, want)
	}

	// A minute off adds nothing.
	hours, _ = tracker.observe(spec, false, start.Add(3*time.Minute))
	if want := 2.0 / 60; hours < want*0.99 || hours > want*1.01 {
		t.Errorf("a minute off changed the total to %v h, want it unchanged at %v", hours, want)
	}
}

// The honesty rule: a gap we did not observe must not be credited. During an
// outage nobody knows whether the pump was running, and inventing run time is
// the one error that cannot be undone later.
func TestRuntimeTracker_DoesNotCreditUnobservedGaps(t *testing.T) {
	tracker := newRuntimeTracker("")
	spec := testRuntimeSpec()
	start := time.Now()

	tracker.observe(spec, true, start)

	// Two hours later — the daemon was down, or reads were failing.
	hours, worth := tracker.observe(spec, true, start.Add(2*time.Hour))

	if hours != 0 {
		t.Errorf("credited %v h across an unobserved two-hour gap; it must credit nothing", hours)
	}
	if worth {
		t.Error("a skipped interval should not be published as if it were a fresh measurement")
	}
}

// The counters are total_increasing. If a restart published zero, Home
// Assistant would record a meter reset every time the daemon is restarted.
func TestRuntimeTracker_SurvivesRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "runtime.json")
	spec := testRuntimeSpec()
	start := time.Now()

	first := newRuntimeTracker(statePath)
	first.observe(spec, true, start)
	first.observe(spec, true, start.Add(time.Minute))
	if err := first.save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// A new process, as after a restart.
	second := newRuntimeTracker(statePath)
	hours, _ := second.observe(spec, true, start.Add(2*time.Minute))

	if want := 1.0 / 60; hours < want*0.99 || hours > want*1.01 {
		t.Errorf("after restart = %v h, want the accumulated %v to be restored", hours, want)
	}
}

// A truncated state file would be read back as a smaller total, which Home
// Assistant would see as a decrease. The write must be atomic.
func TestRuntimeTracker_SaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "runtime.json")
	spec := testRuntimeSpec()
	start := time.Now()

	tracker := newRuntimeTracker(statePath)
	tracker.observe(spec, true, start)
	tracker.observe(spec, true, start.Add(time.Minute))
	if err := tracker.save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("could not list the state directory: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temporary file %s left behind; the rename did not complete", e.Name())
		}
	}
}

// A missing state file is the normal first run, not an error.
func TestRuntimeTracker_MissingStateFileStartsFromZero(t *testing.T) {
	tracker := newRuntimeTracker(filepath.Join(t.TempDir(), "absent.json"))
	spec := testRuntimeSpec()
	start := time.Now()

	tracker.observe(spec, true, start)
	hours, _ := tracker.observe(spec, true, start.Add(time.Minute))

	if want := 1.0 / 60; hours < want*0.99 || hours > want*1.01 {
		t.Errorf("= %v h, want about %v starting from zero", hours, want)
	}
}

// A corrupt state file must not take the daemon down; losing the accumulation
// is bad, refusing to run is worse.
func TestRuntimeTracker_CorruptStateFileIsSurvivable(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("could not write the corrupt file: %v", err)
	}

	tracker := newRuntimeTracker(statePath)
	spec := testRuntimeSpec()
	start := time.Now()

	tracker.observe(spec, true, start)
	if hours, _ := tracker.observe(spec, true, start.Add(time.Minute)); hours <= 0 {
		t.Error("the tracker should still work after a corrupt state file")
	}
}
