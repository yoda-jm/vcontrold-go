// Package mqtt - the pieces every Home Assistant entity shares.
package mqtt

import (
	"encoding/json"
	"fmt"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
	"strings"
)

// The appliance every entity belongs to. Home Assistant groups entities by
// this identifier, so all of them appear under a single device rather than as
// two dozen loose sensors.
const (
	deviceIdentifier = "vitola200"
	deviceName       = "Vitola 200"
	deviceModel      = "Vitola 200 VB2A / Vitotronic 200 KW2"
	deviceVendor     = "Viessmann"
)

// States published on the availability topic.
const (
	availabilityOnline  = "online"
	availabilityOffline = "offline"
)

type discoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
}

// entityBase carries what a sensor, a binary sensor and a thermostat all need.
// Embedding it keeps a new entity type from having to rediscover how this
// installation names things.
type entityBase struct {
	Name         string              `json:"name"`
	UniqueID     string              `json:"unique_id"`
	ObjectID     string              `json:"object_id"`
	Icon         string              `json:"icon,omitempty"`
	Device       discoveryDevice     `json:"device"`
	Availability []map[string]string `json:"availability,omitempty"`
}

// device returns the block identifying the appliance.
func device() discoveryDevice {
	return discoveryDevice{
		Identifiers:  []string{deviceIdentifier},
		Name:         deviceName,
		Model:        deviceModel,
		Manufacturer: deviceVendor,
	}
}

// newEntityBase builds the common part of a discovery message.
//
// displayName drives object_id, and therefore the entity_id Home Assistant
// settles on. Deriving it here rather than at each call site is what makes
// those identifiers predictable enough for a dashboard to reference.
func newEntityBase(p *Publisher, objectID, displayName, icon string) entityBase {
	return entityBase{
		Name:     displayName,
		UniqueID: fmt.Sprintf("%s_%s", deviceIdentifier, objectID),
		ObjectID: fmt.Sprintf("vitola_%s", slugify(displayName)),
		Icon:     icon,
		Device:   device(),
		Availability: []map[string]string{{
			"topic":                 p.availabilityTopic(),
			"payload_available":     availabilityOnline,
			"payload_not_available": availabilityOffline,
		}},
	}
}

// stateTopic returns the topic carrying a reading, under the configured root.
func (p *Publisher) stateTopic(alias string) string {
	return fmt.Sprintf("%s/%s", p.config.TopicRoot, alias)
}

// commandTopic maps a read alias to the topic a hook listens on:
// getTempWWsoll -> openv/setTempWWsoll.
func (p *Publisher) commandTopic(alias string) string {
	return fmt.Sprintf("%s/set%s", p.config.TopicRoot, strings.TrimPrefix(alias, "get"))
}

// publishDiscovery sends one retained discovery message.
//
// The retain flag is what makes entities survive a Home Assistant restart:
// without it they would only reappear on our next publish.
func (p *Publisher) publishDiscovery(component, objectID string, payload any) bool {
	body, err := json.Marshal(payload)
	if err != nil {
		logging.Error("MQTT discovery: failed to marshal %s/%s: %v", component, objectID, err)
		return false
	}

	topic := fmt.Sprintf("homeassistant/%s/%s/%s/config", component, deviceIdentifier, objectID)
	if token := p.client.Publish(topic, 0, true, body); token.Wait() && token.Error() != nil {
		logging.Error("MQTT discovery: failed to publish %s: %v", topic, token.Error())
		return false
	}

	logging.Info("MQTT discovery: %s", topic)
	return true
}

// readyToDiscover reports whether discovery can be published at all.
func (p *Publisher) readyToDiscover() error {
	if !p.enabled || p.config == nil {
		return errDiscoveryDisabled
	}
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("cannot publish discovery: MQTT client not connected")
	}
	return nil
}

// errDiscoveryDisabled means MQTT is off; callers treat it as "nothing to do"
// rather than as a failure.
var errDiscoveryDisabled = fmt.Errorf("mqtt disabled")

// slugify reduces a label to a Home Assistant identifier: lowercase, no
// accents, words joined by underscores.
func slugify(label string) string {
	// The labels are French, so transliterate rather than drop: otherwise
	// "Brûleur" would become "brleur".
	replacer := strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i",
		"ô", "o", "ö", "o",
		"ù", "u", "û", "u", "ü", "u",
		"ç", "c",
	)
	folded := replacer.Replace(strings.ToLower(label))

	var b strings.Builder
	lastUnderscore := false
	for _, r := range folded {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	return strings.Trim(b.String(), "_")
}

// sanitizeObjectID makes an alias usable inside a topic and a unique_id.
func sanitizeObjectID(alias string) string {
	return slugify(alias)
}
