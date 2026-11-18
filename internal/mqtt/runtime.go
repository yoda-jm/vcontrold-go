// Package mqtt - cumulative run-time counters for on/off datapoints.
package mqtt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
)

// runtimeTracker accumulates how long an on/off datapoint has been on.
//
// Home Assistant only keeps long-term statistics for sensors carrying a
// state_class; a binary_sensor lives in the states table and is purged after
// purge_keep_days. So the recirculation pump's run time — the quantity that
// decides whether it is quietly costing 1300–2600 kWh a year — would never
// accumulate. Publishing it as a numeric total_increasing counter is what makes
// it survive.
type runtimeTracker struct {
	mutex sync.Mutex

	// seconds accumulated per output alias.
	seconds map[string]float64

	// last sample seen per source alias: its state and when it was observed.
	lastState map[string]bool
	lastSeen  map[string]time.Time

	statePath string
	dirty     bool

	// published holds the last payload sent per alias, so an unchanged total
	// is not republished on every reading.
	published map[string]string
}

// changed reports whether the total differs from the one last published.
func (t *runtimeTracker) changed(alias, payload string) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if previous, ok := t.published[alias]; ok && previous == payload {
		return false
	}
	t.published[alias] = payload
	return true
}

// forget drops what was published, so every total goes out again after a
// reconnection.
func (t *runtimeTracker) forget() {
	t.mutex.Lock()
	t.published = make(map[string]string)
	t.mutex.Unlock()
}

// total returns the accumulated run time of an output alias, in hours.
func (t *runtimeTracker) total(alias string) float64 {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.seconds[alias] / 3600
}

// maxCreditableGap bounds how much unobserved time may be credited between two
// samples.
//
// If the daemon was down, or reads were failing, we do not know whether the
// pump was running, and crediting the gap would invent run time. Skipping it
// under-reports instead, which is the honest direction: a chart that says
// "less than reality" during a known outage is recoverable, an invented number
// is not.
const maxCreditableGap = 5 * time.Minute

func newRuntimeTracker(statePath string) *runtimeTracker {
	t := &runtimeTracker{
		seconds:   make(map[string]float64),
		lastState: make(map[string]bool),
		lastSeen:  make(map[string]time.Time),
		published: make(map[string]string),
		statePath: statePath,
	}
	t.load()
	return t
}

// observe records a sample of a source datapoint and returns the run time in
// hours for its output alias, plus whether it is worth publishing.
func (t *runtimeTracker) observe(spec config.MQTTRuntimeConfig, on bool, now time.Time) (float64, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	previousSeen, seen := t.lastSeen[spec.SourceAlias]
	previousOn := t.lastState[spec.SourceAlias]

	t.lastSeen[spec.SourceAlias] = now
	t.lastState[spec.SourceAlias] = on

	if !seen {
		// First sample of this session: nothing to integrate over yet. The
		// accumulated total is still published, so a restart does not look
		// like a meter reset.
		return t.seconds[spec.Alias] / 3600, true
	}

	gap := now.Sub(previousSeen)
	if gap <= 0 {
		return t.seconds[spec.Alias] / 3600, false
	}

	if gap > maxCreditableGap {
		// Expected after a restart or a spell of failed reads. It is worth
		// seeing when diagnosing a counter, but it is not a warning.
		logging.Debug("%s: %s since the previous sample, longer than the %s that can "+
			"be credited; the interval is skipped rather than assumed",
			spec.Alias, gap.Truncate(time.Second), maxCreditableGap)
		return t.seconds[spec.Alias] / 3600, false
	}

	if previousOn {
		t.seconds[spec.Alias] += gap.Seconds()
		t.dirty = true
	}

	return t.seconds[spec.Alias] / 3600, true
}

// runtimeState is what survives a restart.
type runtimeState struct {
	Seconds map[string]float64 `json:"seconds"`
	SavedAt time.Time          `json:"saved_at"`
}

// load restores the accumulated totals.
//
// Without this, every restart would publish 0 to a total_increasing entity, and
// Home Assistant would record a meter reset each time. A missing or unreadable
// file is not fatal: starting from zero loses history but keeps the daemon
// running, and the loss is announced rather than silent.
func (t *runtimeTracker) load() {
	if t.statePath == "" {
		return
	}

	body, err := os.ReadFile(t.statePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logging.Warn("Could not read the run-time state %s: %v; starting from zero", t.statePath, err)
		}
		return
	}

	var state runtimeState
	if err := json.Unmarshal(body, &state); err != nil {
		logging.Warn("Run-time state %s is unreadable (%v); starting from zero", t.statePath, err)
		return
	}

	for alias, seconds := range state.Seconds {
		t.seconds[alias] = seconds
	}
	logging.Info("Restored run-time counters from %s (saved %s)",
		t.statePath, state.SavedAt.Format(time.RFC3339))
}

// save writes the accumulated totals, atomically.
//
// A partially written file would be worse than none: it would be parsed as a
// smaller total on the next start, and Home Assistant would see a decrease.
func (t *runtimeTracker) save() error {
	t.mutex.Lock()
	if !t.dirty || t.statePath == "" {
		t.mutex.Unlock()
		return nil
	}
	state := runtimeState{Seconds: make(map[string]float64, len(t.seconds)), SavedAt: time.Now()}
	for alias, seconds := range t.seconds {
		state.Seconds[alias] = seconds
	}
	t.dirty = false
	t.mutex.Unlock()

	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode run-time state: %w", err)
	}

	temporary := t.statePath + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, t.statePath); err != nil {
		return fmt.Errorf("failed to replace %s: %w", t.statePath, err)
	}
	return nil
}

// PublishRuntimeDiscovery announces the run-time counters to Home Assistant.
func (p *Publisher) PublishRuntimeDiscovery(runtimes []config.MQTTRuntimeConfig, statePath string) error {
	if len(runtimes) == 0 {
		return nil
	}

	// The tracker runs whether or not the broker answers: run time missed
	// while it does not could never be recovered.
	p.runtimeConfigs = runtimes
	p.runtimeStatePath = statePath
	if p.runtime == nil {
		p.runtime = newRuntimeTracker(statePath)
	}

	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled || err == errNotConnected {
			return nil
		}
		return err
	}

	for _, r := range runtimes {
		objectID := sanitizeObjectID(r.Alias)
		if objectID == "" {
			logging.Warn("Run-time counter %q yields an empty object id, skipping", r.Alias)
			continue
		}

		displayName := r.FriendlyName
		if displayName == "" {
			displayName = r.Alias
		}

		payload := sensorPayload{
			entityBase:        newEntityBase(p, objectID, displayName, r.Icon),
			StateTopic:        p.stateTopic(r.Alias),
			UnitOfMeasurement: "h",
			DeviceClass:       "duration",
			// total_increasing so Home Assistant keeps it in long-term
			// statistics, which is the entire point of this counter.
			StateClass: "total_increasing",
		}

		p.publishDiscovery("sensor", objectID, payload)
	}

	return nil
}

// handleRuntime updates and publishes the run-time counters fed by one source
// datapoint.
func (p *Publisher) handleRuntime(sourceAlias, cleanedValue string, now time.Time) {
	if p.runtime == nil || len(p.runtimeConfigs) == 0 {
		return
	}

	// The source is a 0/1 datapoint; anything else is not a state we can
	// integrate.
	var on bool
	switch cleanedValue {
	case "1":
		on = true
	case "0":
		on = false
	default:
		return
	}

	for _, r := range p.runtimeConfigs {
		if r.SourceAlias != sourceAlias {
			continue
		}

		hours, worth := p.runtime.observe(r, on, now)
		if !worth {
			continue
		}

		payload := fmt.Sprintf("%.4f", hours)
		entry := ledgerEntry{Time: now, Alias: r.Alias, Value: payload}

		// Checked before changed(), which would otherwise record as sent a
		// total that never left. It goes out on reconnection instead.
		if !p.online() {
			p.ledger.record(entry, false)
			continue
		}

		// While the pump is off the total does not move, and republishing it
		// every minute would be noise.
		if !p.runtime.changed(r.Alias, payload) {
			continue
		}

		p.ledger.record(entry, p.publishRuntime(r.Alias, payload) && p.haOnline.Load())
	}

	if err := p.runtime.save(); err != nil {
		logging.Warn("Could not persist the run-time counters: %v", err)
	}
}

// publishRuntime sends one run-time total and reports whether it left.
func (p *Publisher) publishRuntime(alias, payload string) bool {
	topic := p.stateTopic(alias)
	if token := p.client.Publish(topic, 0, true, payload); token.Wait() && token.Error() != nil {
		logging.Error("Failed to publish run-time %s: %v", topic, token.Error())
		return false
	}
	logging.Debug("RUNTIME: %s = %s h", topic, payload)
	return true
}

// republishRuntime sends every total again after a reconnection.
func (p *Publisher) republishRuntime() {
	if p.runtime == nil || !p.online() {
		return
	}
	p.runtime.forget()
	for _, r := range p.runtimeConfigs {
		payload := fmt.Sprintf("%.4f", p.runtime.total(r.Alias))
		if p.runtime.changed(r.Alias, payload) {
			p.publishRuntime(r.Alias, payload)
		}
	}
}

// DefaultRuntimeStatePath places the state file beside the configuration, which
// is the directory the daemon already owns on the target.
func DefaultRuntimeStatePath(configPath string) string {
	if configPath == "" {
		return "vcontrold-runtime.json"
	}
	return filepath.Join(filepath.Dir(configPath), "vcontrold-runtime.json")
}
