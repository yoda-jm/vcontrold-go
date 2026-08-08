// Package mqtt - Entites climate Home Assistant
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// climatePayload est le message de decouverte d'une entite climate.
//
// Home Assistant refuse une entite climate sans au moins une consigne : les
// topics de commande sont donc obligatoires, ceux d'etat facultatifs.
type climatePayload struct {
	Name     string `json:"name"`
	UniqueID string `json:"unique_id"`
	ObjectID string `json:"object_id"`
	Icon     string `json:"icon,omitempty"`

	TemperatureCommandTopic string `json:"temperature_command_topic"`
	TemperatureStateTopic   string `json:"temperature_state_topic,omitempty"`
	CurrentTemperatureTopic string `json:"current_temperature_topic,omitempty"`

	ModeCommandTopic  string   `json:"mode_command_topic,omitempty"`
	ModeStateTopic    string   `json:"mode_state_topic,omitempty"`
	ModeStateTemplate string   `json:"mode_state_template,omitempty"`
	Modes             []string `json:"modes,omitempty"`

	ActionTopic    string `json:"action_topic,omitempty"`
	ActionTemplate string `json:"action_template,omitempty"`

	MinTemp         float64 `json:"min_temp"`
	MaxTemp         float64 `json:"max_temp"`
	TempStep        float64 `json:"temp_step"`
	TemperatureUnit string  `json:"temperature_unit"`
	Precision       float64 `json:"precision,omitempty"`
	Optimistic      bool    `json:"optimistic,omitempty"`

	Device       discoveryDevice     `json:"device"`
	Availability []map[string]string `json:"availability,omitempty"`
}

// PublishClimateDiscovery publie les entites climate declarees en configuration.
func (p *Publisher) PublishClimateDiscovery(climates []config.MQTTClimateConfig) error {
	if !p.enabled || p.config == nil || len(climates) == 0 {
		return nil
	}
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("cannot publish climate discovery: MQTT client not connected")
	}

	root := p.config.TopicRoot

	for _, c := range climates {
		if c.TargetAlias == "" || c.ObjectID == "" {
			log.Printf("MQTT climate: skipping %q, target_alias and object_id are required", c.Name)
			continue
		}

		step := c.TempStep
		if step == 0 {
			// Les consignes tiennent sur un octet entier, unite ST : proposer
			// un demi-degre laisserait croire a une finesse inexistante.
			step = 1
		}

		payload := climatePayload{
			Name:     c.Name,
			UniqueID: fmt.Sprintf("%s_%s", deviceIdentifier, c.ObjectID),
			ObjectID: c.ObjectID,
			Icon:     c.Icon,

			// La commande part sur le topic ecoute par le hook correspondant.
			TemperatureCommandTopic: fmt.Sprintf("%s/set%s", root, trimGetPrefix(c.TargetAlias)),
			TemperatureStateTopic:   fmt.Sprintf("%s/%s", root, c.TargetAlias),

			MinTemp:         c.MinTemp,
			MaxTemp:         c.MaxTemp,
			TempStep:        step,
			TemperatureUnit: "C",
			Precision:       0.1,

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

		if c.CurrentAlias != "" {
			payload.CurrentTemperatureTopic = fmt.Sprintf("%s/%s", root, c.CurrentAlias)
		}

		if len(c.Modes) > 0 && c.ModeAlias != "" {
			payload.Modes = c.Modes
			payload.ModeStateTopic = fmt.Sprintf("%s/%s", root, c.ModeAlias)
			payload.ModeStateTemplate = c.ModeTemplate

			// Le topic de commande n'est publie que si une adresse de mode est
			// declaree. Sans lui le mode est en lecture seule -- ce qui est
			// voulu tant que l'ecriture du mode n'a pas ete validee sur la
			// chaudiere, et honnete puisque le mode est GLOBAL a l'appareil :
			// le regler depuis une zone agirait sur l'autre.
			if c.ModeAddress != "" {
				payload.ModeCommandTopic = fmt.Sprintf("%s/set%s", root, trimGetPrefix(c.ModeAlias))
			}
		}

		// hvac_action : « chauffe » quand le bruleur est allume, « en veille »
		// sinon. C'est l'etat reel de la chaudiere, pas une deduction.
		if c.ActionAlias != "" {
			payload.ActionTopic = fmt.Sprintf("%s/%s", root, c.ActionAlias)
			payload.ActionTemplate = "{{ 'heating' if value == '1' else 'idle' }}"
		}

		body, err := json.Marshal(payload)
		if err != nil {
			log.Printf("MQTT climate: failed to marshal %q: %v", c.Name, err)
			continue
		}

		topic := fmt.Sprintf("homeassistant/climate/%s/%s/config", deviceIdentifier, c.ObjectID)
		if token := p.client.Publish(topic, 0, true, body); token.Wait() && token.Error() != nil {
			log.Printf("MQTT climate: failed to publish %s: %v", topic, token.Error())
			continue
		}

		log.Printf("MQTT climate: %s (%.0f-%.0f °C, step %.0f)", topic, c.MinTemp, c.MaxTemp, step)
	}

	return nil
}

// trimGetPrefix transforme un alias de lecture en suffixe de commande :
// getTempWWsoll -> TempWWsoll, d'ou le topic openv/setTempWWsoll deja ecoute
// par les hooks.
func trimGetPrefix(alias string) string {
	const prefix = "get"
	if len(alias) > len(prefix) && alias[:len(prefix)] == prefix {
		return alias[len(prefix):]
	}
	return alias
}
