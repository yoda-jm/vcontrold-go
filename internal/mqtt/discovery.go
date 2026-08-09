// Package mqtt - Home Assistant discovery for readings.
package mqtt

import (
	"fmt"
	"github.com/yoda-jm/vcontrold-go/internal/logging"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/xml"
)

// sensorPayload is the discovery message for a sensor or a binary sensor.
type sensorPayload struct {
	entityBase

	StateTopic        string `json:"state_topic"`
	UnitOfMeasurement string `json:"unit_of_measurement,omitempty"`
	DeviceClass       string `json:"device_class,omitempty"`
	StateClass        string `json:"state_class,omitempty"`
	PayloadOn         string `json:"payload_on,omitempty"`
	PayloadOff        string `json:"payload_off,omitempty"`
}

// sensorMetadata is what Home Assistant needs to know about a measurement.
type sensorMetadata struct {
	Unit        string
	DeviceClass string
	StateClass  string
	Icon        string
}

// metadataForUnit derives the Home Assistant classes from the vito.xml unit.
//
// state_class is the field that matters: without it Home Assistant keeps the
// entity out of long-term statistics, and the history vanishes with the
// recorder purge (ten days by default). That is what cost a year of readings
// the last time round.
//
//	total_increasing -> counters that only ever climb (hours, starts)
//	measurement      -> instantaneous values (temperatures, states)
func metadataForUnit(vitoUnit string) sensorMetadata {
	switch vitoUnit {
	case "CS": // CounterS: accumulated seconds, converted to hours (V/3600)
		return sensorMetadata{
			Unit:        "h",
			DeviceClass: "duration",
			StateClass:  "total_increasing",
			Icon:        "mdi:timer-outline",
		}
	case "CO": // Counter: accumulated events
		return sensorMetadata{
			StateClass: "total_increasing",
			Icon:       "mdi:counter",
		}
	case "UT", "UT1", "UT1U", "UTI": // Temperatures
		return sensorMetadata{
			Unit:        "°C",
			DeviceClass: "temperature",
			StateClass:  "measurement",
		}
	case "PR", "PR1": // Percentages
		return sensorMetadata{
			Unit:       "%",
			StateClass: "measurement",
		}
	case "UN": // Neigung: heating curve slope, unitless
		return sensorMetadata{
			StateClass: "measurement",
			Icon:       "mdi:chart-line",
		}
	case "ST": // Plain numeric status
		return sensorMetadata{StateClass: "measurement"}
	case "RT": // ReturnStatus: 0/1 enum
		return sensorMetadata{}
	case "BA": // BetriebsArt: textual enum, no state_class applies
		return sensorMetadata{Icon: "mdi:tune"}
	default:
		return sensorMetadata{}
	}
}

// resolveMetadata combines what vito.xml knows with what the configuration
// overrides. The XML is the default; the configuration has the last word.
func resolveMetadata(addr config.MQTTAddress, hexAddr uint16, binary bool) sensorMetadata {
	meta := sensorMetadata{}
	if mapping, exists := xml.GetAddressMapping(hexAddr); exists {
		meta = metadataForUnit(mapping.Unit)
	}

	if addr.Unit != "" {
		meta.Unit = addr.Unit
	}
	if addr.DeviceClass != "" {
		meta.DeviceClass = addr.DeviceClass
		// The icon guessed from the vito.xml unit is a stopgap: unit RT covers
		// the burner and the pumps alike. Once a device class is declared,
		// Home Assistant picks a better icon than we can.
		meta.Icon = ""
	}
	if addr.StateClass != "" {
		meta.StateClass = addr.StateClass
	}
	if addr.Icon != "" {
		meta.Icon = addr.Icon
	}

	if binary {
		// Neither applies to an on/off entity, and Home Assistant rejects the
		// configuration if state_class is left on one.
		meta.StateClass = ""
		meta.Unit = ""
	}

	return meta
}

// PublishDiscovery announces one retained entity per monitored address.
func (p *Publisher) PublishDiscovery() error {
	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled {
			return nil
		}
		return err
	}

	published := 0
	for _, addr := range p.config.Addresses {
		component, objectID, payload, err := p.buildSensorPayload(addr)
		if err != nil {
			logging.Warn("MQTT discovery: skipping %q: %v", addr.Address, err)
			continue
		}
		if p.publishDiscovery(component, objectID, payload) {
			published++
		}
	}

	logging.Info("MQTT discovery: published %d retained entity definitions", published)
	return nil
}

// buildSensorPayload turns one configured address into a discovery message.
//
// Kept separate from publishing so the payload — the part that silently breaks
// history when it is wrong — can be asserted without a broker.
func (p *Publisher) buildSensorPayload(addr config.MQTTAddress) (component, objectID string, payload sensorPayload, err error) {
	hexAddr, err := config.ParseHexAddress(addr.Address)
	if err != nil {
		return "", "", payload, fmt.Errorf("invalid address: %w", err)
	}

	alias := addr.Alias
	if alias == "" {
		alias = fmt.Sprintf("addr_%04x", hexAddr)
	}

	objectID = sanitizeObjectID(alias)
	if objectID == "" {
		return "", "", payload, fmt.Errorf("alias %q yields an empty object id", alias)
	}

	component = addr.Component
	if component == "" {
		component = "sensor"
	}
	binary := component == "binary_sensor"

	meta := resolveMetadata(addr, hexAddr, binary)

	displayName := addr.FriendlyName
	if displayName == "" {
		displayName = alias
	}

	payload = sensorPayload{
		entityBase:        newEntityBase(p, objectID, displayName, meta.Icon),
		StateTopic:        p.stateTopic(alias),
		UnitOfMeasurement: meta.Unit,
		DeviceClass:       meta.DeviceClass,
		StateClass:        meta.StateClass,
	}

	if binary {
		payload.PayloadOn = addr.PayloadOn
		payload.PayloadOff = addr.PayloadOff
		if payload.PayloadOn == "" {
			payload.PayloadOn = "1"
		}
		if payload.PayloadOff == "" {
			payload.PayloadOff = "0"
		}
	}

	return component, objectID, payload, nil
}
