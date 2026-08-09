// Package mqtt - rejecting implausible counter readings at the source.
package mqtt

import (
	"strconv"
	"sync"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/logging"
)

// counterGuard refuses readings that cannot be true for a monotonic counter.
//
// The KW protocol carries no checksum, so a disturbed frame can return a
// perfectly well-formed number that happens to be wrong. Published to a
// total_increasing entity, one such reading is permanent damage: Home Assistant
// treats a decrease as a meter reset and a jump as consumption, and the error
// is discovered months later in a chart nobody can repair.
//
// Filtering this in Home Assistant would be too late — the bad row is already
// written. It belongs here, before the value leaves the daemon.
type counterGuard struct {
	mutex sync.Mutex
	last  map[string]counterSample

	// consecutiveLower counts readings below the accepted value. A single one
	// is corruption; a persistent one is a genuine meter reset, which does
	// happen when a controller is serviced.
	consecutiveLower map[string]int
}

type counterSample struct {
	value float64
	at    time.Time
}

// resetConfirmations is how many consecutive lower readings are needed before a
// decrease is believed. Three readings of a slow counter group is several
// minutes of agreement — far beyond a one-frame glitch.
const resetConfirmations = 3

func newCounterGuard() *counterGuard {
	return &counterGuard{
		last:             make(map[string]counterSample),
		consecutiveLower: make(map[string]int),
	}
}

// accept reports whether a reading may be published.
//
// maxIncreasePerHour bounds the rise. The bound is not arbitrary: burner hours
// cannot climb faster than one hour per hour of wall time, and burner starts
// are limited by how fast the appliance can cycle. Zero disables the check, for
// quantities that are not counters.
func (g *counterGuard) accept(alias string, raw string, maxIncreasePerHour float64, now time.Time) bool {
	if maxIncreasePerHour <= 0 {
		return true
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		// Not a number, so not a counter. Enum topics land here.
		return true
	}

	g.mutex.Lock()
	defer g.mutex.Unlock()

	previous, seen := g.last[alias]
	if !seen {
		// Nothing to compare against. This is also the case after a restart,
		// where the counter has legitimately advanced while we were not
		// watching, so refusing it would be wrong.
		g.last[alias] = counterSample{value: value, at: now}
		return true
	}

	if value < previous.value {
		g.consecutiveLower[alias]++
		if g.consecutiveLower[alias] < resetConfirmations {
			logging.Warn("%s: refusing %.2f, below the last accepted %.2f "+
				"(reading %d of %d before this is treated as a meter reset)",
				alias, value, previous.value, g.consecutiveLower[alias], resetConfirmations)
			return false
		}

		logging.Warn("%s: %.2f confirmed below %.2f by %d consecutive readings; "+
			"accepting it as a meter reset",
			alias, value, previous.value, g.consecutiveLower[alias])
		g.consecutiveLower[alias] = 0
		g.last[alias] = counterSample{value: value, at: now}
		return true
	}

	elapsed := now.Sub(previous.at)
	if elapsed <= 0 {
		elapsed = time.Second
	}

	// One extra unit of tolerance absorbs rounding in the published decimals.
	allowed := maxIncreasePerHour*elapsed.Hours() + 1
	if value-previous.value > allowed {
		logging.Warn("%s: refusing %.2f, a rise of %.2f over %s exceeds the "+
			"plausible %.2f — treating it as a corrupt frame",
			alias, value, value-previous.value, elapsed.Truncate(time.Second), allowed)
		return false
	}

	g.consecutiveLower[alias] = 0
	g.last[alias] = counterSample{value: value, at: now}
	return true
}
