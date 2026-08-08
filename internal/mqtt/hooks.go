// Package mqtt - Generic MQTT hook system for listening to topics and executing actions
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
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// MQTTHook defines a topic to listen to and what action to execute
type MQTTHook struct {
	Topic       string                `yaml:"topic" json:"topic"`             // MQTT topic to listen to
	Description string                `yaml:"description" json:"description"` // Human readable description
	Action      config.MQTTHookAction `yaml:"action" json:"action"`           // Action to execute
	Enabled     bool                  `yaml:"enabled" json:"enabled"`         // Whether this hook is active
}

// HookManager manages MQTT topic subscriptions and actions
type HookManager struct {
	config    *config.Config
	reader    *viessmann.Reader
	client    mqtt.Client
	hooks     []MQTTHook
	connected bool
}

// NewHookManager creates a new MQTT hook manager
func NewHookManager(cfg *config.Config, reader *viessmann.Reader) *HookManager {
	return &HookManager{
		config: cfg,
		reader: reader,
		hooks:  []MQTTHook{},
	}
}

// AddHook registers a new MQTT hook
func (hm *HookManager) AddHook(hook MQTTHook) {
	hm.hooks = append(hm.hooks, hook)
	log.Printf("MQTT Hook registered: %s -> %s", hook.Topic, hook.Description)

	// Subscribe if already connected
	if hm.connected && hm.client != nil && hook.Enabled {
		hm.subscribeToHook(hook)
	}
}

// LoadHooks loads hooks from configuration
func (hm *HookManager) LoadHooks(hooks []MQTTHook) {
	hm.hooks = hooks
	log.Printf("MQTT: Loaded %d hooks from configuration", len(hooks))

	// Subscribe to all enabled hooks if connected
	if hm.connected && hm.client != nil {
		for _, hook := range hm.hooks {
			if hook.Enabled {
				hm.subscribeToHook(hook)
			}
		}
	}
}

// Connect establishes MQTT connection and subscribes to all hooks
func (hm *HookManager) Connect() error {
	// Use YAML config for MQTT connection
	if hm.config.YamlConfig == nil || hm.config.YamlConfig.MQTT.Host == "" {
		return fmt.Errorf("MQTT broker not configured in YAML config")
	}

	mqttConfig := hm.config.YamlConfig.MQTT
	if !mqttConfig.Enabled {
		return fmt.Errorf("MQTT is disabled in configuration")
	}

	// Create MQTT client
	clientID := fmt.Sprintf("vcontrold-hooks-%d", time.Now().Unix())
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s", mqttConfig.Host)).
		SetClientID(clientID).
		SetCleanSession(true)

	// Set credentials if provided
	if mqttConfig.Username != "" {
		opts.SetUsername(mqttConfig.Username)
		if mqttConfig.Password != "" {
			opts.SetPassword(mqttConfig.Password)
		}
	}

	// Connection handlers
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT Hooks: Connection lost: %v", err)
		hm.connected = false
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Printf("MQTT Hooks: Connected to broker %s", mqttConfig.Host)
		hm.connected = true

		// Subscribe to all enabled hooks
		for _, hook := range hm.hooks {
			if hook.Enabled {
				hm.subscribeToHook(hook)
			}
		}
	})

	// Connect
	hm.client = mqtt.NewClient(opts)
	if token := hm.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT hooks connection failed: %v", token.Error())
	}

	return nil
}

// subscribeToHook subscribes to a single hook topic
func (hm *HookManager) subscribeToHook(hook MQTTHook) {
	if hm.client == nil {
		return
	}

	log.Printf("MQTT Hook: Subscribing to %s (%s)", hook.Topic, hook.Description)

	token := hm.client.Subscribe(hook.Topic, 0, func(client mqtt.Client, msg mqtt.Message) {
		hm.handleHookMessage(hook, msg)
	})

	if token.Wait() && token.Error() != nil {
		log.Printf("MQTT Hook: Failed to subscribe to %s: %v", hook.Topic, token.Error())
	}
}

// handleHookMessage processes a message received on a hook topic
func (hm *HookManager) handleHookMessage(hook MQTTHook, msg mqtt.Message) {
	payload := string(msg.Payload())
	log.Printf("MQTT Hook: Received on %s: %s (Action: %s)", hook.Topic, payload, hook.Action.Type)

	// Execute the configured action
	if err := hm.executeAction(hook.Action, payload); err != nil {
		log.Printf("MQTT Hook: Failed to execute action %s for topic %s: %v", hook.Action.Type, hook.Topic, err)

		// Optionally publish error status
		hm.publishHookStatus(hook.Topic+"/error", map[string]interface{}{
			"error":   err.Error(),
			"payload": payload,
			"topic":   hook.Topic,
			"time":    time.Now().Unix(),
		})
		return
	}

	// Publish success status
	hm.publishHookStatus(hook.Topic+"/status", map[string]interface{}{
		"success": true,
		"payload": payload,
		"topic":   hook.Topic,
		"action":  hook.Action.Type,
		"time":    time.Now().Unix(),
	})
}

// executeAction executes the configured action based on the message payload
func (hm *HookManager) executeAction(action config.MQTTHookAction, payload string) error {
	switch action.Type {
	case "set_temperature":
		return hm.executeSetTemperature(action, payload)
	case "set_mode":
		return hm.executeSetMode(action, payload)
	case "write_address":
		return hm.executeWriteAddress(action, payload)
	case "write_parameter":
		return hm.executeWriteParameter(action, payload)
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// executeSetTemperature handles temperature setting actions
func (hm *HookManager) executeSetTemperature(action config.MQTTHookAction, payload string) error {
	// Parse temperature value
	temp, err := strconv.ParseFloat(strings.TrimSpace(payload), 64)
	if err != nil {
		return fmt.Errorf("invalid temperature format '%s': %v", payload, err)
	}

	// Validate range if specified
	if action.MinValue != nil && temp < *action.MinValue {
		return fmt.Errorf("temperature %.1f°C below minimum %.1f°C", temp, *action.MinValue)
	}
	if action.MaxValue != nil && temp > *action.MaxValue {
		return fmt.Errorf("temperature %.1f°C above maximum %.1f°C", temp, *action.MaxValue)
	}

	// Find target address
	var targetAddress uint16
	var found bool

	if action.Address != "" {
		// Direct address specified
		if addr, err := config.ParseHexAddress(action.Address); err == nil {
			targetAddress = addr
			found = true
		}
	} else if action.Parameter != "" || action.Zone != "" {
		// Find by parameter name or zone
		targetAddress, found = hm.findTemperatureSetpoint(action.Zone, action.Parameter)
	}

	if !found {
		return fmt.Errorf("temperature setpoint not found (zone: %s, parameter: %s, address: %s)",
			action.Zone, action.Parameter, action.Address)
	}

	// Transform value if needed
	writeValue := temp
	if action.Transform == "multiply_10" {
		writeValue = temp * 10
	}

	// The length comes from the XML, not from an assumption. setTempWWsoll
	// (0x6300) is ONE byte, unit ST, unscaled: a two-byte write overruns the
	// next address, and 480 for 48 degrees does not even fit in a byte —
	// which is why the multiply_10 transform is gone from the config.
	writeData, err := encodeLittleEndian(int(writeValue+0.5), writeLengthFor(targetAddress))
	if err != nil {
		return fmt.Errorf("cannot encode temperature %.1f°C for address 0x%04X: %v", temp, targetAddress, err)
	}

	// Write to device
	if writer, ok := hm.reader.GetSerialComm().(interface{ WriteToDevice(uint16, []byte) error }); ok {
		err := writer.WriteToDevice(targetAddress, writeData)
		if err != nil {
			return fmt.Errorf("failed to write temperature %.1f°C to address 0x%04X: %v", temp, targetAddress, err)
		}
		log.Printf("MQTT Hook: Set temperature to %.1f°C at address 0x%04X", temp, targetAddress)
		return nil
	}

	return fmt.Errorf("write operations not supported by current serial communicator")
}

// executeSetMode handles mode setting actions
func (hm *HookManager) executeSetMode(action config.MQTTHookAction, payload string) error {
	mode := strings.ToLower(strings.TrimSpace(payload))

	// Find target address
	var targetAddress uint16
	var found bool

	if action.Address != "" {
		if addr, err := config.ParseHexAddress(action.Address); err == nil {
			targetAddress = addr
			found = true
		}
	} else {
		targetAddress, found = hm.findModeAddress(action.Zone)
	}

	if !found {
		return fmt.Errorf("mode address not found for zone: %s", action.Zone)
	}

	// Map mode to value
	var modeValue byte
	switch mode {
	case "off", "0":
		modeValue = 0
	case "heat", "on", "1":
		modeValue = 1
	case "auto", "2":
		modeValue = 2
	case "eco", "reduced", "3":
		modeValue = 3
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	// Write to device
	writeData := []byte{modeValue}
	if writer, ok := hm.reader.GetSerialComm().(interface{ WriteToDevice(uint16, []byte) error }); ok {
		err := writer.WriteToDevice(targetAddress, writeData)
		if err != nil {
			return fmt.Errorf("failed to write mode %s to address 0x%04X: %v", mode, targetAddress, err)
		}
		log.Printf("MQTT Hook: Set mode to %s at address 0x%04X", mode, targetAddress)
		return nil
	}

	return fmt.Errorf("write operations not supported by current serial communicator")
}

// executeWriteAddress handles direct address writing
func (hm *HookManager) executeWriteAddress(action config.MQTTHookAction, payload string) error {
	if action.Address == "" {
		return fmt.Errorf("address required for write_address action")
	}

	targetAddress, err := config.ParseHexAddress(action.Address)
	if err != nil {
		return fmt.Errorf("invalid address %s: %v", action.Address, err)
	}

	// Parse payload as raw bytes or value
	var writeData []byte

	// Try to parse as numeric value first
	if value, err := strconv.ParseFloat(strings.TrimSpace(payload), 64); err == nil {
		// Transform if needed
		if action.Transform == "multiply_10" {
			value *= 10
		}

		// Length taken from the XML rather than assumed to be two bytes.
		writeData, err = encodeLittleEndian(int(value+0.5), writeLengthFor(targetAddress))
		if err != nil {
			return fmt.Errorf("cannot encode value for address 0x%04X: %v", targetAddress, err)
		}
	} else {
		// Treat as raw string/bytes
		writeData = []byte(payload)
	}

	// Write to device
	if writer, ok := hm.reader.GetSerialComm().(interface{ WriteToDevice(uint16, []byte) error }); ok {
		err := writer.WriteToDevice(targetAddress, writeData)
		if err != nil {
			return fmt.Errorf("failed to write to address 0x%04X: %v", targetAddress, err)
		}
		log.Printf("MQTT Hook: Wrote %v to address 0x%04X", writeData, targetAddress)
		return nil
	}

	return fmt.Errorf("write operations not supported by current serial communicator")
}

// executeWriteParameter handles parameter name-based writing
func (hm *HookManager) executeWriteParameter(action config.MQTTHookAction, payload string) error {
	if action.Parameter == "" {
		return fmt.Errorf("parameter name required for write_parameter action")
	}

	// Find parameter by name
	dataPoints := hm.reader.GetDataPoints()
	var targetAddress uint16
	var found bool

	for _, dp := range dataPoints {
		if dp.Name == action.Parameter {
			targetAddress = dp.Address
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("parameter '%s' not found in configuration", action.Parameter)
	}

	// Parse and write value (similar to write_address)
	value, err := strconv.ParseFloat(strings.TrimSpace(payload), 64)
	if err != nil {
		return fmt.Errorf("invalid numeric value '%s': %v", payload, err)
	}

	// Transform if needed
	if action.Transform == "multiply_10" {
		value *= 10
	}

	// Length taken from the XML rather than assumed to be two bytes.
	writeData, err := encodeLittleEndian(int(value+0.5), writeLengthFor(targetAddress))
	if err != nil {
		return fmt.Errorf("cannot encode value for parameter '%s' (0x%04X): %v", action.Parameter, targetAddress, err)
	}

	// Write to device
	if writer, ok := hm.reader.GetSerialComm().(interface{ WriteToDevice(uint16, []byte) error }); ok {
		err := writer.WriteToDevice(targetAddress, writeData)
		if err != nil {
			return fmt.Errorf("failed to write to parameter '%s' (0x%04X): %v", action.Parameter, targetAddress, err)
		}
		log.Printf("MQTT Hook: Wrote %.1f to parameter '%s' (0x%04X)", value, action.Parameter, targetAddress)
		return nil
	}

	return fmt.Errorf("write operations not supported by current serial communicator")
}

// findTemperatureSetpoint finds the address for temperature setpoints by zone or parameter name
func (hm *HookManager) findTemperatureSetpoint(zone, parameterName string) (uint16, bool) {
	dataPoints := hm.reader.GetDataPoints()

	// First try exact parameter name match
	if parameterName != "" {
		for _, dp := range dataPoints {
			if dp.Name == parameterName {
				return dp.Address, true
			}
		}
	}

	// Then try zone-based matching
	if zone != "" {
		for _, dp := range dataPoints {
			nameLower := strings.ToLower(dp.Name)

			switch zone {
			case "hotwater":
				if (strings.Contains(nameLower, "warmwasser") || strings.Contains(nameLower, "ww")) &&
					(strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) {
					return dp.Address, true
				}
			case "heating":
				if (strings.Contains(nameLower, "raum") || strings.Contains(nameLower, "vorlauf")) &&
					(strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) &&
					!strings.Contains(nameLower, "warmwasser") {
					return dp.Address, true
				}
			}
		}
	}

	return 0, false
}

// findModeAddress finds the address for mode controls by zone
func (hm *HookManager) findModeAddress(zone string) (uint16, bool) {
	// Common mode addresses for Viessmann systems
	switch zone {
	case "hotwater":
		return 0x6301, true // Hot water operating mode
	case "heating":
		return 0x2323, true // Heating operating mode (Betriebsart A1M1)
	default:
		return 0, false
	}
}

// publishHookStatus publishes status messages for hooks
func (hm *HookManager) publishHookStatus(topic string, payload map[string]interface{}) {
	if hm.client == nil || !hm.connected {
		return
	}

	jsonData, err := json.Marshal(map[string]interface{}{"status": payload})
	if err != nil {
		log.Printf("MQTT Hook: Failed to create status JSON: %v", err)
		return
	}

	token := hm.client.Publish(topic, 0, false, string(jsonData))
	if token.Wait() && token.Error() != nil {
		log.Printf("MQTT Hook: Failed to publish status to %s: %v", topic, token.Error())
	}
}

// Disconnect closes the MQTT connection
func (hm *HookManager) Disconnect() {
	if hm.client != nil && hm.connected {
		hm.client.Disconnect(250)
		hm.connected = false
		log.Printf("MQTT Hooks: Disconnected")
	}
}

// GetStatus returns the current status of the hook manager
func (hm *HookManager) GetStatus() map[string]interface{} {
	enabledHooks := 0
	var hookList []map[string]interface{}

	for _, hook := range hm.hooks {
		if hook.Enabled {
			enabledHooks++
		}

		hookList = append(hookList, map[string]interface{}{
			"topic":       hook.Topic,
			"description": hook.Description,
			"action_type": hook.Action.Type,
			"enabled":     hook.Enabled,
		})
	}

	return map[string]interface{}{
		"connected":     hm.connected,
		"total_hooks":   len(hm.hooks),
		"enabled_hooks": enabledHooks,
		"hooks":         hookList,
	}
}

// writeLengthFor returns an address's write length according to vito.xml,
// overrides included. One byte by default: that is the common case for
// setpoints, and it is the least destructive guess — writing too long would
// overrun the neighbouring address.
func writeLengthFor(address uint16) int {
	if mapping, exists := xml.GetAddressMapping(address); exists && mapping.Length > 0 {
		return mapping.Length
	}
	return 1
}

// encodeLittleEndian encodes an integer into exactly length bytes.
//
// Overflow is an error, never a silent truncation: writing 480 into one byte
// would yield 224 — an absurd hot water setpoint on a boiler in service.
func encodeLittleEndian(value int, length int) ([]byte, error) {
	if length <= 0 || length > 4 {
		return nil, fmt.Errorf("unsupported write length %d bytes", length)
	}
	if value < 0 {
		return nil, fmt.Errorf("negative value %d cannot be written", value)
	}
	if max := (1 << (8 * length)) - 1; value > max {
		return nil, fmt.Errorf("value %d does not fit in %d byte(s) (max %d)", value, length, max)
	}

	data := make([]byte, length)
	for i := 0; i < length; i++ {
		data[i] = byte((value >> (8 * i)) & 0xFF)
	}

	return data, nil
}
