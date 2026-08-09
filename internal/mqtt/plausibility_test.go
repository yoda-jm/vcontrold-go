package mqtt

import (
	"testing"
	"time"
)

// The burner hours counter cannot climb faster than one hour per hour of wall
// time. That bound is physical, not a guess, which is what makes it safe to
// enforce.
const burnerHoursPerHour = 1.05

func TestCounterGuard_AcceptsPlausibleProgress(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	if !g.accept("getBrennerStunden1", "11066.20", burnerHoursPerHour, now) {
		t.Fatal("the first reading must be accepted: there is nothing to compare it with")
	}
	// Five minutes later, at most five minutes of burner time can have elapsed.
	if !g.accept("getBrennerStunden1", "11066.28", burnerHoursPerHour, now.Add(5*time.Minute)) {
		t.Error("a plausible rise was refused")
	}
}

// A restart leaves no baseline, and the counter has legitimately advanced while
// nobody was watching. Refusing that would lose real data.
func TestCounterGuard_FirstReadingAfterRestartIsAccepted(t *testing.T) {
	g := newCounterGuard()
	if !g.accept("getBrennerStarts", "206315", 120, time.Now()) {
		t.Error("the first reading of a session must be accepted")
	}
}

// The failure that already happened on this system: a counter that jumps by
// thousands in one interval. Published, it becomes a permanent false step in
// the energy history.
func TestCounterGuard_RejectsImplausibleJump(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	g.accept("getBrennerStarts", "206315", 120, now)

	if g.accept("getBrennerStarts", "215651", 120, now.Add(5*time.Minute)) {
		t.Error("a jump of 9336 starts in five minutes must be refused")
	}

	// The guard must not have moved its baseline to the bad value, or the next
	// genuine reading would look like a huge decrease.
	if !g.accept("getBrennerStarts", "206317", 120, now.Add(10*time.Minute)) {
		t.Error("a genuine reading after a refused one must still be accepted")
	}
}

// Burner hours are the foundation of every other figure, and the bound on them
// is the tightest available: wall time itself.
func TestCounterGuard_RejectsHoursRisingFasterThanTime(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	g.accept("getBrennerStunden1", "11066.2", burnerHoursPerHour, now)

	// Ten hours of burner time cannot pass in one hour.
	if g.accept("getBrennerStunden1", "11076.2", burnerHoursPerHour, now.Add(time.Hour)) {
		t.Error("burner hours rising ten times faster than wall time must be refused")
	}
}

// A single decrease is corruption. Publishing it would make Home Assistant
// record a meter reset and start a new cycle.
func TestCounterGuard_RejectsSingleDecrease(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	g.accept("getBrennerStarts", "206315", 120, now)

	if g.accept("getBrennerStarts", "9707", 120, now.Add(5*time.Minute)) {
		t.Error("a lone decrease must be refused: it is the truncated-counter signature")
	}
}

// A decrease that persists is a real meter reset, which happens when a
// controller is serviced. Refusing it forever would freeze the series.
func TestCounterGuard_AcceptsConfirmedReset(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	g.accept("getBrennerStarts", "206315", 120, now)

	var accepted bool
	for i := 1; i <= resetConfirmations; i++ {
		accepted = g.accept("getBrennerStarts", "12", 120, now.Add(time.Duration(i)*5*time.Minute))
	}
	if !accepted {
		t.Errorf("a decrease confirmed by %d consecutive readings must be accepted as a reset",
			resetConfirmations)
	}

	// And the series must continue from the new baseline.
	if !g.accept("getBrennerStarts", "13", 120, now.Add(time.Hour)) {
		t.Error("readings after a confirmed reset must be accepted")
	}
}

// Quantities that are not counters must pass through untouched: a temperature
// falls all the time.
func TestCounterGuard_IgnoresNonCounters(t *testing.T) {
	g := newCounterGuard()
	now := time.Now()

	for _, v := range []string{"27.5", "26.1", "24.0", "27.9"} {
		if !g.accept("getTempA", v, 0, now) {
			t.Errorf("temperature %s was refused; the guard must not apply without a bound", v)
		}
	}
}

// Enum topics publish text. The guard must not choke on them.
func TestCounterGuard_IgnoresNonNumeric(t *testing.T) {
	g := newCounterGuard()
	if !g.accept("getBetriebArtM1", "WW", 120, time.Now()) {
		t.Error("a non-numeric payload must pass through")
	}
}
