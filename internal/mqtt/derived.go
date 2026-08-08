// Package mqtt - Grandeurs calculees a partir des mesures publiees
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// derivedState retient la derniere valeur numerique de chaque alias, pour
// pouvoir recalculer un rapport des qu'un des deux termes change.
type derivedState struct {
	mutex  sync.Mutex
	values map[string]float64

	// Derniere valeur publiee par grandeur derivee : une grandeur calculee
	// change bien plus rarement que les mesures dont elle depend, et la
	// republier a chaque lecture noierait le journal sans rien apporter.
	published map[string]string
}

func newDerivedState() *derivedState {
	return &derivedState{
		values:    make(map[string]float64),
		published: make(map[string]string),
	}
}

// changed indique si la valeur differe de la derniere publiee, et la retient.
func (d *derivedState) changed(alias, payload string) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if previous, ok := d.published[alias]; ok && previous == payload {
		return false
	}
	d.published[alias] = payload
	return true
}

// record memorise une valeur publiee. Les valeurs non numeriques -- les enums
// comme le mode de fonctionnement -- sont ignorees.
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

// publishDerived recalcule et publie les grandeurs derivees dont les deux
// termes sont connus.
func (p *Publisher) publishDerived() {
	if p.config == nil || len(p.derivedConfigs) == 0 || p.derived == nil {
		return
	}
	if p.client == nil || !p.client.IsConnected() {
		return
	}

	for _, d := range p.derivedConfigs {
		num, okN := p.derived.get(d.Numerator)
		den, okD := p.derived.get(d.Denominator)
		if !okN || !okD {
			continue // Un des deux termes n'a pas encore ete lu.
		}
		if den == 0 {
			continue // Rapport indefini : ne rien publier vaut mieux qu'un zero.
		}

		factor := d.Factor
		if factor == 0 {
			factor = 1
		}

		value := num * factor / den
		precision := d.Precision
		if precision == 0 {
			precision = 2
		}

		topic := fmt.Sprintf("%s/%s", p.config.TopicRoot, d.Alias)
		payload := strconv.FormatFloat(value, 'f', precision, 64)

		if !p.derived.changed(d.Alias, payload) {
			continue
		}

		if token := p.client.Publish(topic, 0, true, payload); token.Wait() && token.Error() != nil {
			log.Printf("MQTT derived: failed to publish %s: %v", topic, token.Error())
			continue
		}
		log.Printf("MQTT DERIVED: %s = %s", topic, payload)
	}
}

// PublishDerivedDiscovery annonce les grandeurs derivees a Home Assistant.
func (p *Publisher) PublishDerivedDiscovery(derived []config.MQTTDerivedConfig) error {
	if !p.enabled || p.config == nil || len(derived) == 0 {
		return nil
	}
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("cannot publish derived discovery: MQTT client not connected")
	}

	// Memoriser la configuration pour le calcul a chaque mesure.
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

		name := d.FriendlyName
		if name == "" {
			name = d.Alias
		}

		payload := discoveryPayload{
			Name:              name,
			UniqueID:          fmt.Sprintf("%s_%s", deviceIdentifier, objectID),
			ObjectID:          fmt.Sprintf("vitola_%s", slugify(name)),
			StateTopic:        fmt.Sprintf("%s/%s", p.config.TopicRoot, d.Alias),
			UnitOfMeasurement: d.Unit,
			DeviceClass:       d.DeviceClass,
			StateClass:        d.StateClass,
			Icon:              d.Icon,
			Device: discoveryDevice{
				Identifiers:  []string{deviceIdentifier},
				Name:         deviceName,
				Model:        deviceModel,
				Manufacturer: deviceVendor,
			},
			Availability: []map[string]string{{
				"topic":                 p.availabilityTopic(),
				"payload_available":     availabilityOnline,
				"payload_not_available": availabilityOffline,
			}},
		}

		body, err := json.Marshal(payload)
		if err != nil {
			log.Printf("MQTT derived: failed to marshal %q: %v", name, err)
			continue
		}

		topic := fmt.Sprintf("homeassistant/sensor/%s/%s/config", deviceIdentifier, objectID)
		if token := p.client.Publish(topic, 0, true, body); token.Wait() && token.Error() != nil {
			log.Printf("MQTT derived: failed to publish %s: %v", topic, token.Error())
			continue
		}
		log.Printf("MQTT derived discovery: %s (state_class=%q)", topic, d.StateClass)
	}

	return nil
}
