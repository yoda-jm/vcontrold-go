// Package mqtt - values computed from published readings.
package mqtt

import (
	"log"
	"strconv"
	"sync"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// derivedState remembers the latest numeric value of each alias, so a ratio
// can be recomputed as soon as either term changes.
type derivedState struct {
	mutex  sync.Mutex
	values map[string]float64

	// Last payload published per derived value. A computed value changes far
	// less often than the readings behind it, and republishing it on every
	// read would flood the log without telling anyone anything.
	published map[string]string
}

func newDerivedState() *derivedState {
	return &derivedState{
		values:    make(map[string]float64),
		published: make(map[string]string),
	}
}

// record stores a published value. Non-numeric ones — enums such as the
// operating mode — are ignored.
func (d *derivedState) record(alias, value string) {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return
	}
	d.mutex.Lock()
	d.values[alias] = f
	d.mutex.Unlock()
}

func (d *derivedState) get(alias string) (float64, bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	v, ok := d.values[alias]
	return v, ok
}

// changed reports whether the payload differs from the last published one, and
// remembers it.
func (d *derivedState) changed(alias, payload string) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if previous, ok := d.published[alias]; ok && previous == payload {
		return false
	}
	d.published[alias] = payload
	return true
}

// compute evaluates one derived value. It returns false when either term is
// still unknown, or when the ratio is undefined.
func computeDerived(d config.MQTTDerivedConfig, state *derivedState) (string, bool) {
	num, okN := state.get(d.Numerator)
	den, okD := state.get(d.Denominator)
	if !okN || !okD {
		return "", false // One of the two terms has not been read yet.
	}
	if den == 0 {
		return "", false // Undefined ratio: publishing nothing beats publishing zero.
	}

	factor := d.Factor
	if factor == 0 {
		factor = 1
	}

	precision := d.Precision
	if precision == 0 {
		precision = 2
	}

	return strconv.FormatFloat(num*factor/den, 'f', precision, 64), true
}

// publishDerived recomputes and publishes the derived values whose terms are
// both known and whose result has changed.
func (p *Publisher) publishDerived() {
	if p.config == nil || len(p.derivedConfigs) == 0 || p.derived == nil {
		return
	}
	if p.client == nil || !p.client.IsConnected() {
		return
	}

	for _, d := range p.derivedConfigs {
		payload, ok := computeDerived(d, p.derived)
		if !ok || !p.derived.changed(d.Alias, payload) {
			continue
		}

		topic := p.stateTopic(d.Alias)
		if token := p.client.Publish(topic, 0, true, payload); token.Wait() && token.Error() != nil {
			log.Printf("MQTT derived: failed to publish %s: %v", topic, token.Error())
			continue
		}
		log.Printf("MQTT DERIVED: %s = %s", topic, payload)
	}
}

// PublishDerivedDiscovery announces the derived values to Home Assistant.
func (p *Publisher) PublishDerivedDiscovery(derived []config.MQTTDerivedConfig) error {
	if len(derived) == 0 {
		return nil
	}
	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled {
			return nil
		}
		return err
	}

	// Keep the configuration around for the recomputation on every reading.
	p.derivedConfigs = derived
	if p.derived == nil {
		p.derived = newDerivedState()
	}

	for _, d := range derived {
		objectID := sanitizeObjectID(d.Alias)
		if objectID == "" {
			log.Printf("MQTT derived: alias %q yields an empty object id, skipping", d.Alias)
			continue
		}

		displayName := d.FriendlyName
		if displayName == "" {
			displayName = d.Alias
		}

		payload := sensorPayload{
			entityBase:        newEntityBase(p, objectID, displayName, d.Icon),
			StateTopic:        p.stateTopic(d.Alias),
			UnitOfMeasurement: d.Unit,
			DeviceClass:       d.DeviceClass,
			StateClass:        d.StateClass,
		}

		p.publishDiscovery("sensor", objectID, payload)
	}

	return nil
}
