// Package mqtt - Home Assistant climate entities.
package mqtt

import (
	"log"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// climatePayload is the discovery message for a thermostat.
//
// Home Assistant will not accept a climate entity without a setpoint, so the
// temperature command topic is required; the state topics are not.
type climatePayload struct {
	entityBase

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
}

// defaultTempStep is one degree: the setpoints are single bytes in whole
// degrees (unit ST), so offering half a degree would promise a precision the
// appliance does not have.
const defaultTempStep = 1

// PublishClimateDiscovery announces the thermostats declared in configuration.
func (p *Publisher) PublishClimateDiscovery(climates []config.MQTTClimateConfig) error {
	if len(climates) == 0 {
		return nil
	}
	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled {
			return nil
		}
		return err
	}

	for _, c := range climates {
		if c.TargetAlias == "" || c.ObjectID == "" {
			log.Printf("MQTT climate: skipping %q, target_alias and object_id are required", c.Name)
			continue
		}

		step := c.TempStep
		if step == 0 {
			step = defaultTempStep
		}

		payload := climatePayload{
			entityBase: newEntityBase(p, c.ObjectID, c.Name, c.Icon),

			TemperatureCommandTopic: p.commandTopic(c.TargetAlias),
			TemperatureStateTopic:   p.stateTopic(c.TargetAlias),

			MinTemp:         c.MinTemp,
			MaxTemp:         c.MaxTemp,
			TempStep:        step,
			TemperatureUnit: "C",
			Precision:       0.1,
		}

		if c.CurrentAlias != "" {
			payload.CurrentTemperatureTopic = p.stateTopic(c.CurrentAlias)
		}

		if len(c.Modes) > 0 && c.ModeAlias != "" {
			payload.Modes = c.Modes
			payload.ModeStateTopic = p.stateTopic(c.ModeAlias)
			payload.ModeStateTemplate = c.ModeTemplate

			// The command topic only appears when a mode address is declared.
			// Without it the mode is read-only, which is deliberate: the mode
			// is global to the appliance, so setting it from one zone would
			// move the other.
			if c.ModeAddress != "" {
				payload.ModeCommandTopic = p.commandTopic(c.ModeAlias)
			}
		}

		// hvac_action: "heating" while the burner fires, "idle" otherwise.
		// That is the appliance's own state, not an inference.
		if c.ActionAlias != "" {
			payload.ActionTopic = p.stateTopic(c.ActionAlias)
			payload.ActionTemplate = "{{ 'heating' if value == '1' else 'idle' }}"
		}

		p.publishDiscovery("climate", c.ObjectID, payload)
	}

	return nil
}
