// Package mqtt provides MQTT client functionality for publishing readings and receiving commands
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
)

// Client represents an MQTT client for Viessmann integration
type Client struct {
	config           *config.Config
	reader           *viessmann.Reader
	climateController *viessmann.ClimateController
	client            mqtt.Client
	connected         bool
}

// Message represents an MQTT message
type Message struct {
	Topic     string      `json:"topic"`
	Payload   interface{} `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// Command represents a command received via MQTT
type Command struct {
	Action    string                 `json:"action"`    // e.g., "read", "set", "schedule"
	Parameter string                 `json:"parameter"` // parameter name or address
	Value     interface{}            `json:"value,omitempty"`
	Options   map[string]interface{} `json:"options,omitempty"`
}

// NewClient creates a new MQTT client
func NewClient(cfg *config.Config, reader *viessmann.Reader) *Client {
	return &Client{
		config:            cfg,
		reader:            reader,
		climateController: viessmann.NewClimateController(reader),
		connected:         false,
	}
}

// Connect establishes connection to the MQTT broker
func (c *Client) Connect() error {
	// TODO: Implement actual MQTT connection
	log.Printf("MQTT: Connecting to broker %s:%d (placeholder)", c.config.MQTTBroker, c.config.MQTTPort)
	
	// Placeholder implementation
	if c.config.MQTTBroker == "" {
		return fmt.Errorf("MQTT broker not configured")
	}
	
	// TODO: Initialize MQTT client with broker, credentials, etc.
	/*
	opts := MQTT.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", c.config.MQTTBroker, c.config.MQTTPort))
	opts.SetClientID("vcontrold-go")
	if c.config.MQTTUsername != "" {
		opts.SetUsername(c.config.MQTTUsername)
		opts.SetPassword(c.config.MQTTPassword)
	}
	
	c.client = MQTT.NewClient(opts)
	if token := c.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT connection failed: %v", token.Error())
	}
	*/
	
	c.connected = true
	log.Printf("MQTT: Connected to broker (placeholder)")
	
	// Subscribe to command topics
	c.subscribeToCommands()
	
	return nil
}

// Disconnect closes the MQTT connection
func (c *Client) Disconnect() {
	if !c.connected {
		return
	}
	
	// TODO: Implement actual disconnection
	// c.client.Disconnect(250)
	c.connected = false
	log.Printf("MQTT: Disconnected from broker")
}

// IsConnected returns whether the client is connected
func (c *Client) IsConnected() bool {
	return c.connected
}

// PublishReading publishes a single parameter reading to MQTT
func (c *Client) PublishReading(reading viessmann.Reading) error {
	if !c.connected {
		return fmt.Errorf("MQTT client not connected")
	}
	
	// Create topic based on parameter name and address
	topic := fmt.Sprintf("%s/sensor/%s/state", c.config.MQTTTopic, c.sanitizeForTopic(reading.Name))
	
	// Create Home Assistant compatible payload
	payload := map[string]interface{}{
		"value":     reading.FormattedValue,
		"address":   fmt.Sprintf("0x%04X", reading.Address),
		"timestamp": reading.Timestamp.Unix(),
		"success":   reading.Success,
		"unit_type": reading.UnitType,
		"in_xml":    reading.InXML,
	}
	
	// Add raw value if available
	if reading.RawValue != nil {
		payload["raw_value"] = reading.RawValue
	}
	
	// Add error if present
	if reading.Error != "" {
		payload["error"] = reading.Error
	}
	
	// TODO: Implement actual publishing
	log.Printf("MQTT: Would publish to %s: %v", topic, payload)
	
	/*
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}
	
	token := c.client.Publish(topic, 0, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish: %v", token.Error())
	}
	*/
	
	return nil
}

// PublishReadings publishes multiple readings to MQTT
func (c *Client) PublishReadings(readings []viessmann.Reading) error {
	for _, reading := range readings {
		if err := c.PublishReading(reading); err != nil {
			log.Printf("MQTT: Failed to publish reading %s: %v", reading.Name, err)
			// Continue with other readings
		}
	}
	return nil
}

// PublishDiscovery publishes Home Assistant discovery messages for parameters
func (c *Client) PublishDiscovery(dataPoints []config.DataPoint) error {
	if !c.connected {
		return fmt.Errorf("MQTT client not connected")
	}
	
	log.Printf("MQTT: Would publish discovery for %d data points", len(dataPoints))
	
	for _, dp := range dataPoints {
		// Create Home Assistant discovery payload
		deviceName := c.sanitizeForTopic(dp.Name)
		uniqueID := fmt.Sprintf("viessmann_%04x", dp.Address)
		
		discoveryPayload := map[string]interface{}{
			"name":        dp.Name,
			"unique_id":   uniqueID,
			"state_topic": fmt.Sprintf("%s/sensor/%s/state", c.config.MQTTTopic, deviceName),
			"device": map[string]interface{}{
				"identifiers":  []string{"viessmann_v200kw2"},
				"name":         "Viessmann V200KW2",
				"manufacturer": "Viessmann",
				"model":        "V200KW2",
			},
			"json_attributes_topic": fmt.Sprintf("%s/sensor/%s/attributes", c.config.MQTTTopic, deviceName),
		}
		
		// Add unit of measurement if available
		if dp.Unit != "" {
			discoveryPayload["unit_of_measurement"] = dp.Unit
		}
		
		// Determine device class based on parameter type
		if deviceClass := c.determineDeviceClass(dp.Name); deviceClass != "" {
			discoveryPayload["device_class"] = deviceClass
		}
		
		discoveryTopic := fmt.Sprintf("homeassistant/sensor/%s/%s/config", uniqueID, deviceName)
		
		// TODO: Implement actual discovery publishing
		log.Printf("MQTT: Would publish discovery to %s", discoveryTopic)
		
		/*
		payloadBytes, err := json.Marshal(discoveryPayload)
		if err != nil {
			log.Printf("Failed to marshal discovery payload for %s: %v", dp.Name, err)
			continue
		}
		
		token := c.client.Publish(discoveryTopic, 0, true, payloadBytes) // retain=true for discovery
		token.Wait()
		if token.Error() != nil {
			log.Printf("Failed to publish discovery for %s: %v", dp.Name, token.Error())
		}
		*/
	}
	
	return nil
}

// subscribeToCommands subscribes to command topics
func (c *Client) subscribeToCommands() {
	// Subscribe to general command topic
	commandTopic := fmt.Sprintf("%s/command", c.config.MQTTTopic)
	log.Printf("MQTT: Would subscribe to commands on %s", commandTopic)
	
	// Subscribe to climate control topics
	c.subscribeToClimateCommands()
	
	/*
	token := c.client.Subscribe(commandTopic, 0, c.handleCommand)
	token.Wait()
	if token.Error() != nil {
		log.Printf("Failed to subscribe to commands: %v", token.Error())
	}
	*/
}

// subscribeToClimateCommands subscribes to Home Assistant climate control topics
func (c *Client) subscribeToClimateCommands() {
	// Hot water temperature control
	hotWaterTempTopic := "viessmann/climate/hotwater/temperature/set"
	hotWaterModeTopic := "viessmann/climate/hotwater/mode/set"
	
	// Heating temperature control
	heatingTempTopic := "viessmann/climate/heating/temperature/set"
	heatingModeTopic := "viessmann/climate/heating/mode/set"
	
	log.Printf("MQTT: Would subscribe to climate topics:")
	log.Printf("  - %s", hotWaterTempTopic)
	log.Printf("  - %s", hotWaterModeTopic)
	log.Printf("  - %s", heatingTempTopic)
	log.Printf("  - %s", heatingModeTopic)
	
	/*
	// Hot water temperature control
	token := c.client.Subscribe(hotWaterTempTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		c.handleClimateTemperatureCommand("hotwater", string(msg.Payload()))
	})
	token.Wait()
	
	// Hot water mode control
	token = c.client.Subscribe(hotWaterModeTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		c.handleClimateModeCommand("hotwater", string(msg.Payload()))
	})
	token.Wait()
	
	// Heating temperature control
	token = c.client.Subscribe(heatingTempTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		c.handleClimateTemperatureCommand("heating", string(msg.Payload()))
	})
	token.Wait()
	
	// Heating mode control
	token = c.client.Subscribe(heatingModeTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		c.handleClimateModeCommand("heating", string(msg.Payload()))
	})
	token.Wait()
	*/
}

// handleCommand processes incoming MQTT commands
func (c *Client) handleCommand(topic string, payload []byte) {
	log.Printf("MQTT: Received command on %s: %s", topic, string(payload))
	
	var command Command
	if err := json.Unmarshal(payload, &command); err != nil {
		log.Printf("MQTT: Invalid command format: %v", err)
		return
	}
	
	switch command.Action {
	case "read":
		c.handleReadCommand(command)
	case "set":
		c.handleSetCommand(command)
	case "schedule":
		c.handleScheduleCommand(command)
	default:
		log.Printf("MQTT: Unknown command action: %s", command.Action)
	}
}

// handleClimateTemperatureCommand processes climate temperature control commands
func (c *Client) handleClimateTemperatureCommand(zone string, temperature string) {
	log.Printf("MQTT: Processing climate temperature command: zone=%s, temperature=%s", zone, temperature)
	
	// Validate temperature format
	temp, err := strconv.ParseFloat(temperature, 64)
	if err != nil {
		log.Printf("MQTT: Invalid temperature format '%s': %v", temperature, err)
		return
	}
	
	// Create climate command
	command := viessmann.ClimateCommand{
		Type:  "temperature",
		Zone:  zone,
		Value: temperature,
	}
	
	// Process the command through the climate controller
	if err := c.climateController.ProcessClimateCommand(command); err != nil {
		log.Printf("MQTT: Failed to process temperature command for %s: %v", zone, err)
		
		// Optionally publish an error status back to MQTT
		errorTopic := fmt.Sprintf("%s/climate/%s/temperature/error", c.config.MQTTTopic, zone)
		errorPayload := map[string]interface{}{
			"error":       err.Error(),
			"temperature": temp,
			"zone":        zone,
			"timestamp":   time.Now().Unix(),
		}
		c.publishError(errorTopic, errorPayload)
		return
	}
	
	// Success - optionally publish confirmation
	confirmTopic := fmt.Sprintf("%s/climate/%s/temperature/status", c.config.MQTTTopic, zone)
	confirmPayload := map[string]interface{}{
		"success":     true,
		"temperature": temp,
		"zone":        zone,
		"timestamp":   time.Now().Unix(),
	}
	c.publishStatus(confirmTopic, confirmPayload)
	
	log.Printf("MQTT: Successfully set %s temperature to %.1f°C", zone, temp)
}

// handleClimateModeCommand processes climate mode control commands
func (c *Client) handleClimateModeCommand(zone string, mode string) {
	log.Printf("MQTT: Processing climate mode command: zone=%s, mode=%s", zone, mode)
	
	// Validate mode value
	validModes := map[string]bool{
		"off":  true,
		"heat": true,
		"auto": true,
		"eco":  true,
	}
	
	if !validModes[strings.ToLower(mode)] {
		log.Printf("MQTT: Invalid mode '%s' for zone '%s'", mode, zone)
		return
	}
	
	// Create climate command
	command := viessmann.ClimateCommand{
		Type:  "mode",
		Zone:  zone,
		Value: mode,
	}
	
	// Process the command through the climate controller
	if err := c.climateController.ProcessClimateCommand(command); err != nil {
		log.Printf("MQTT: Failed to process mode command for %s: %v", zone, err)
		
		// Publish error status
		errorTopic := fmt.Sprintf("%s/climate/%s/mode/error", c.config.MQTTTopic, zone)
		errorPayload := map[string]interface{}{
			"error":     err.Error(),
			"mode":      mode,
			"zone":      zone,
			"timestamp": time.Now().Unix(),
		}
		c.publishError(errorTopic, errorPayload)
		return
	}
	
	// Success - publish confirmation
	confirmTopic := fmt.Sprintf("%s/climate/%s/mode/status", c.config.MQTTTopic, zone)
	confirmPayload := map[string]interface{}{
		"success":   true,
		"mode":      mode,
		"zone":      zone,
		"timestamp": time.Now().Unix(),
	}
	c.publishStatus(confirmTopic, confirmPayload)
	
	log.Printf("MQTT: Successfully set %s mode to %s", zone, mode)
}

// handleReadCommand processes read commands
func (c *Client) handleReadCommand(command Command) {
	log.Printf("MQTT: Processing read command for parameter: %s", command.Parameter)
	
	// TODO: Implement parameter reading by name/address
	// Find the parameter in dataPoints and read it
	dataPoints := c.reader.GetDataPoints()
	for i, dp := range dataPoints {
		if dp.Name == command.Parameter || fmt.Sprintf("0x%04X", dp.Address) == command.Parameter {
			reading := c.reader.ReadParameter(dp, i)
			c.PublishReading(reading)
			return
		}
	}
	
	log.Printf("MQTT: Parameter not found: %s", command.Parameter)
}

// handleSetCommand processes set commands (for writable parameters)
func (c *Client) handleSetCommand(command Command) {
	log.Printf("MQTT: Processing set command for parameter: %s = %v", command.Parameter, command.Value)
	
	// TODO: Implement parameter writing (if supported by the device)
	// This would require implementing the write protocol for V200KW2
	log.Printf("MQTT: Set commands not yet implemented")
}

// handleScheduleCommand processes scheduling commands
func (c *Client) handleScheduleCommand(command Command) {
	log.Printf("MQTT: Processing schedule command: %v", command)
	
	// TODO: Implement scheduled reading functionality
	log.Printf("MQTT: Schedule commands not yet implemented")
}

// sanitizeForTopic sanitizes a string for use in MQTT topics
func (c *Client) sanitizeForTopic(input string) string {
	// Replace spaces and special characters with underscores
	result := ""
	for _, char := range input {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result += string(char)
		} else {
			result += "_"
		}
	}
	return result
}

// determineDeviceClass determines Home Assistant device class based on parameter name
func (c *Client) determineDeviceClass(paramName string) string {
	nameLower := string(paramName)
	for i, char := range nameLower {
		if char >= 'A' && char <= 'Z' {
			nameLower = nameLower[:i] + string(char+32) + nameLower[i+1:]
		}
	}
	
	if contains(nameLower, "temperatur") || contains(nameLower, "temp") {
		return "temperature"
	}
	if contains(nameLower, "druck") || contains(nameLower, "pressure") {
		return "pressure"
	}
	if contains(nameLower, "prozent") || contains(nameLower, "%") {
		return "humidity" // Generic percentage
	}
	if contains(nameLower, "stunden") || contains(nameLower, "hours") {
		return "duration"
	}
	if contains(nameLower, "power") || contains(nameLower, "leistung") {
		return "power"
	}
	if contains(nameLower, "energy") || contains(nameLower, "energie") {
		return "energy"
	}
	
	return "" // No specific device class
}

// contains checks if a string contains a substring (helper function)
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// publishError publishes error messages to MQTT (placeholder implementation)
func (c *Client) publishError(topic string, payload map[string]interface{}) {
	if !c.connected {
		return
	}
	
	// TODO: Implement actual MQTT error publishing
	log.Printf("MQTT: Would publish error to %s: %v", topic, payload)
	
	/*
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("MQTT: Failed to marshal error payload: %v", err)
		return
	}
	
	token := c.client.Publish(topic, 0, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		log.Printf("MQTT: Failed to publish error: %v", token.Error())
	}
	*/
}

// publishStatus publishes status messages to MQTT (placeholder implementation)
func (c *Client) publishStatus(topic string, payload map[string]interface{}) {
	if !c.connected {
		return
	}
	
	// TODO: Implement actual MQTT status publishing
	log.Printf("MQTT: Would publish status to %s: %v", topic, payload)
	
	/*
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("MQTT: Failed to marshal status payload: %v", err)
		return
	}
	
	token := c.client.Publish(topic, 0, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		log.Printf("MQTT: Failed to publish status: %v", token.Error())
	}
	*/
}