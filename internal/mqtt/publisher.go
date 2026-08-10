// Package mqtt handles MQTT publishing with intelligent filtering
package mqtt

import (
	"fmt"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
)

// LastPublish tracks when each address was last published to MQTT
type LastPublish struct {
	Timestamp time.Time
	Value     string
}

// Publisher manages MQTT publishing with intelligent filtering
type Publisher struct {
	config        *config.MQTTConfig
	lastPublished map[uint16]*LastPublish // Track last publish per address
	addressMap    map[uint16]string       // Map address to topic alias
	mutex         sync.RWMutex
	enabled       bool
	client        mqtt.Client // MQTT client instance

	// Values computed from the published readings.
	derived        *derivedState
	derivedConfigs []config.MQTTDerivedConfig

	// Cumulative run-time counters built from on/off datapoints.
	runtime        *runtimeTracker
	runtimeConfigs []config.MQTTRuntimeConfig

	// Refuses counter readings that cannot be true.
	guard *counterGuard

	// Per-address settings, indexed by address, for the checks that need them.
	addressSettings map[uint16]config.MQTTAddress
}

// NewPublisher creates a new MQTT publisher
func NewPublisher(mqttConfig *config.MQTTConfig) *Publisher {
	if mqttConfig == nil {
		return &Publisher{enabled: false}
	}

	p := &Publisher{
		config:        mqttConfig,
		lastPublished: make(map[uint16]*LastPublish),
		addressMap:    make(map[uint16]string),
		enabled:       mqttConfig.Enabled,
	}

	// Build address to alias mapping
	p.addressSettings = make(map[uint16]config.MQTTAddress)
	for _, addr := range mqttConfig.Addresses {
		if hexAddr, err := config.ParseHexAddress(addr.Address); err == nil {
			p.addressMap[hexAddr] = addr.Alias
			p.addressSettings[hexAddr] = addr
		} else {
			logging.Warn("MQTT: Invalid address '%s' ignored: %v", addr.Address, err)
		}
	}
	p.guard = newCounterGuard()

	// Initialize MQTT client if enabled
	if mqttConfig.Enabled {
		p.initMQTTClient()
	}

	logging.Info("MQTT Publisher initialized: %d addresses monitored, enabled: %t", len(p.addressMap), p.enabled)
	return p
}

// initMQTTClient initializes the MQTT client connection
func (p *Publisher) initMQTTClient() {
	clientID := fmt.Sprintf("vcontrold-go-%d", time.Now().Unix())

	// Last will: if the daemon dies or loses the network, the broker publishes
	// "offline" on its behalf. Without it Home Assistant would keep the last
	// retained value forever and show a stopped boiler as a fresh reading.
	availabilityTopic := p.availabilityTopic()

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s", p.config.Host)).
		SetClientID(clientID).
		SetCleanSession(true).
		SetWill(availabilityTopic, availabilityOffline, 0, true)

	// Set credentials if provided
	if p.config.Username != "" {
		opts.SetUsername(p.config.Username)
		if p.config.Password != "" {
			opts.SetPassword(p.config.Password)
		}
	}

	// Set connection handlers
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logging.Info("MQTT: Connection lost: %v", err)
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		logging.Info("MQTT: Connected to broker %s", p.config.Host)

		// Re-asserted on every (re)connection: after a drop it is the
		// "offline" will that sits retained on the broker.
		if token := client.Publish(availabilityTopic, 0, true, availabilityOnline); token.Wait() && token.Error() != nil {
			logging.Error("MQTT: Failed to announce availability: %v", token.Error())
		}
	})

	// Create and connect client
	p.client = mqtt.NewClient(opts)
	if token := p.client.Connect(); token.Wait() && token.Error() != nil {
		logging.Error("MQTT: Failed to connect to broker %s: %v", p.config.Host, token.Error())
		p.enabled = false
	}
}

// HandleCacheEvent processes cache events and decides whether to publish to MQTT
func (p *Publisher) HandleCacheEvent(event viessmann.CacheEvent) {
	if !p.enabled || !p.config.Enabled {
		return
	}

	p.mutex.RLock()
	alias, exists := p.addressMap[event.Address]
	p.mutex.RUnlock()

	if !exists {
		// Address not monitored for MQTT
		return
	}

	// Only publish if successful reading
	if !event.Reading.Success {
		return
	}

	cleanedForRuntime := p.cleanValueForHomeAssistant(event.Reading.FormattedValue)

	// Integrate on/off datapoints on every observation, before any publish
	// suppression. Republishing is throttled to once an hour for an unchanged
	// value, and a tracker fed only by those would see a one-hour gap between
	// samples, credit nothing, and silently report zero run time forever.
	p.handleRuntime(alias, cleanedForRuntime, time.Now())

	shouldPublish, reason := p.shouldPublish(event)
	if !shouldPublish {
		logging.Warn("MQTT: Skipping publish for 0x%04X (%s): %s", event.Address, alias, reason)
		return
	}

	// A counter that cannot be true must not leave the daemon: filtering it in
	// Home Assistant would be too late, the row is already written.
	settings := p.settingsFor(event.Address)
	if p.guard != nil && !p.guard.accept(alias, cleanedForRuntime, settings.MaxIncreasePerHour, time.Now()) {
		return
	}

	// Publish to MQTT
	topic := fmt.Sprintf("%s/%s", p.config.TopicRoot, alias)
	p.publishValue(topic, event.Reading.FormattedValue, event.Address, alias)

	// A text value has no long-term history in Home Assistant. Where the
	// configuration asks for it, mirror the raw protocol value as a number.
	if settings.RawAlias != "" {
		p.publishRawMirror(settings.RawAlias, event.Reading.RawValue)
	}

	// Update last published timestamp with cleaned value
	cleanedValue := p.cleanValueForHomeAssistant(event.Reading.FormattedValue)
	p.mutex.Lock()
	p.lastPublished[event.Address] = &LastPublish{
		Timestamp: time.Now(),
		Value:     cleanedValue,
	}
	p.mutex.Unlock()
}

// shouldPublish determines if a value should be published based on change and time rules
func (p *Publisher) shouldPublish(event viessmann.CacheEvent) (bool, string) {
	p.mutex.RLock()
	lastPub, hasLastPub := p.lastPublished[event.Address]
	p.mutex.RUnlock()

	// Always publish if it's the first time we see this address
	if !hasLastPub {
		return true, "first publication"
	}

	// Always publish if value changed
	if event.Type == viessmann.EventValueChanged {
		return true, fmt.Sprintf("value changed from '%s' to '%s'", event.PreviousValue, event.Reading.FormattedValue)
	}

	// Check if enough time has passed since last publication
	threshold, err := p.config.GetRefreshThresholdDuration()
	if err != nil {
		logging.Warn("MQTT: Invalid refresh threshold, using default 1h: %v", err)
		threshold = time.Hour
	}

	timeSinceLastPub := time.Since(lastPub.Timestamp)
	if timeSinceLastPub >= threshold {
		return true, fmt.Sprintf("time threshold reached (%s >= %s)", timeSinceLastPub.Truncate(time.Second), threshold)
	}

	return false, fmt.Sprintf("recently published (%s ago, threshold: %s)", timeSinceLastPub.Truncate(time.Second), threshold)
}

// cleanValueForHomeAssistant cleans values to be Home Assistant friendly
func (p *Publisher) cleanValueForHomeAssistant(value string) string {
	// Remove common suffixes that aren't needed
	value = strings.TrimSpace(value)

	// Remove unit annotations like " Grad Celsius (not in XML)"
	re := regexp.MustCompile(`\s+\([^)]*\)$`)
	value = re.ReplaceAllString(value, "")

	// Remove units for temperature values - extract just the number
	tempPattern := regexp.MustCompile(`^(-?\d+(?:\.\d+)?)\s+Grad\s+Celsius$`)
	if matches := tempPattern.FindStringSubmatch(value); len(matches) > 1 {
		return matches[1]
	}

	// For other units, try to extract just the numeric part if it starts with a number
	numPattern := regexp.MustCompile(`^(-?\d+(?:\.\d+)?)`)
	if matches := numPattern.FindStringSubmatch(value); len(matches) > 1 {
		// Check if it's a pure number by trying to parse it
		if _, err := strconv.ParseFloat(matches[1], 64); err == nil {
			// If the whole string is just the number (plus maybe whitespace), return the number
			if strings.TrimSpace(value) == matches[1] || strings.HasPrefix(strings.TrimSpace(value), matches[1]+" ") {
				return matches[1]
			}
		}
	}

	// For enums and text values, return as-is (already cleaned of annotations above)
	return value
}

// publishValue publishes a value to MQTT
func (p *Publisher) publishValue(topic, value string, address uint16, alias string) {
	if p.client == nil || !p.client.IsConnected() {
		logging.Error("MQTT: Cannot publish %s = '%s' - client not connected", topic, value)
		return
	}

	// Clean the value for Home Assistant
	cleanedValue := p.cleanValueForHomeAssistant(value)

	// Publish with QoS 0 (at most once delivery) and retain flag set to true
	token := p.client.Publish(topic, 0, true, cleanedValue)
	if token.Wait() && token.Error() != nil {
		logging.Error("MQTT: Failed to publish %s = '%s': %v", topic, cleanedValue, token.Error())
		return
	}

	logging.Info("MQTT PUBLISHED: %s = '%s' (0x%04X -> %s) [cleaned from: '%s']", topic, cleanedValue, address, alias, value)

	// A reading just changed, so anything derived from it may have too.
	if p.derived != nil {
		p.derived.record(alias, cleanedValue)
		p.publishDerived()
	}
}

// UpdateConfig updates the MQTT configuration
func (p *Publisher) UpdateConfig(newConfig *config.MQTTConfig) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Disconnect existing client if any
	if p.client != nil && p.client.IsConnected() {
		logging.Info("MQTT: Disconnecting existing client for config update")
		p.client.Disconnect(250)
	}

	p.config = newConfig
	p.enabled = newConfig != nil && newConfig.Enabled

	// Rebuild address mapping
	p.addressMap = make(map[uint16]string)
	if newConfig != nil {
		for _, addr := range newConfig.Addresses {
			if hexAddr, err := config.ParseHexAddress(addr.Address); err == nil {
				p.addressMap[hexAddr] = addr.Alias
			} else {
				logging.Warn("MQTT: Invalid address '%s' ignored: %v", addr.Address, err)
			}
		}

		// Reconnect with new config if enabled
		if newConfig.Enabled {
			p.initMQTTClient()
		}
	}

	logging.Info("MQTT Publisher updated: enabled=%t, %d addresses monitored", p.enabled, len(p.addressMap))
}

// GetStatus returns the current status of the MQTT publisher
func (p *Publisher) GetStatus() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	var threshold string
	if p.config != nil {
		if duration, err := p.config.GetRefreshThresholdDuration(); err == nil {
			threshold = duration.String()
		} else {
			threshold = "invalid"
		}
	}

	published := make(map[string]interface{})
	for addr, lastPub := range p.lastPublished {
		if alias, exists := p.addressMap[addr]; exists {
			published[fmt.Sprintf("0x%04X", addr)] = map[string]interface{}{
				"alias":       alias,
				"lastPublish": lastPub.Timestamp.Format(time.RFC3339),
				"lastValue":   lastPub.Value,
				"timeSince":   time.Since(lastPub.Timestamp).String(),
			}
		}
	}

	connected := false
	if p.client != nil {
		connected = p.client.IsConnected()
	}

	return map[string]interface{}{
		"enabled":          p.enabled,
		"connected":        connected,
		"broker":           p.config.Host,
		"refreshThreshold": threshold,
		"monitoredCount":   len(p.addressMap),
		"publishedCount":   len(p.lastPublished),
		"publishedValues":  published,
	}
}

// availabilityTopic returns the daemon's availability topic.
func (p *Publisher) availabilityTopic() string {
	root := "viessmann"
	if p.config != nil && p.config.TopicRoot != "" {
		root = p.config.TopicRoot
	}
	return root + "/status"
}

// settingsFor returns the per-address configuration, or a zero value when the
// address is not configured.
func (p *Publisher) settingsFor(address uint16) config.MQTTAddress {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.addressSettings[address]
}

// publishRawMirror publishes the raw protocol value as a number.
//
// The decoded text is what a human reads; this is what survives. Home Assistant
// keeps hourly statistics for a numeric sensor forever, so a mode that changes
// a handful of times a year leaves a faithful timeline that never purges.
// Emitting the protocol byte rather than an invented mapping keeps the meaning
// anchored to vcontrold.xml.
func (p *Publisher) publishRawMirror(alias string, raw interface{}) {
	data, ok := raw.([]byte)
	if !ok || len(data) == 0 {
		return
	}

	// Little-endian, as everywhere else in this protocol.
	var value uint64
	for i := len(data) - 1; i >= 0; i-- {
		value = value<<8 | uint64(data[i])
	}

	topic := p.stateTopic(alias)
	payload := strconv.FormatUint(value, 10)
	if token := p.client.Publish(topic, 0, true, payload); token.Wait() && token.Error() != nil {
		logging.Error("Failed to publish the raw mirror %s: %v", topic, token.Error())
		return
	}
	logging.Debug("MIRROR: %s = %s", topic, payload)
}

// PublishRawMirrorDiscovery announces the numeric mirrors to Home Assistant.
func (p *Publisher) PublishRawMirrorDiscovery() error {
	if err := p.readyToDiscover(); err != nil {
		if err == errDiscoveryDisabled {
			return nil
		}
		return err
	}

	for _, addr := range p.config.Addresses {
		if addr.RawAlias == "" {
			continue
		}

		objectID := sanitizeObjectID(addr.RawAlias)
		if objectID == "" {
			continue
		}

		displayName := addr.RawFriendlyName
		if displayName == "" {
			displayName = addr.RawAlias
		}

		payload := sensorPayload{
			entityBase: newEntityBase(p, objectID, displayName, "mdi:numeric"),
			StateTopic: p.stateTopic(addr.RawAlias),
			// measurement, with no unit: the mean is meaningless for a code,
			// but the hourly min and max carry the timeline and never purge.
			StateClass: "measurement",
		}

		p.publishDiscovery("sensor", objectID, payload)
	}

	return nil
}

// Shutdown announces the daemon as offline and closes the connection.
//
// The broker's last will covers an abrupt death, but a clean DISCONNECT
// suppresses it: the broker assumes the client meant to leave and publishes
// nothing. Home Assistant would then keep the last retained values and present
// them as live, indefinitely.
//
// Today the daemon is killed by SIGTERM without disconnecting, so the will
// fires and the behaviour happens to be right. Announcing it explicitly makes
// that a decision rather than a side effect of not shutting down cleanly.
func (p *Publisher) Shutdown() {
	if p.runtime != nil {
		if err := p.runtime.save(); err != nil {
			logging.Warn("Could not persist the run-time counters on shutdown: %v", err)
		}
	}

	if p.client == nil || !p.client.IsConnected() {
		return
	}

	token := p.client.Publish(p.availabilityTopic(), 0, true, availabilityOffline)
	if !token.WaitTimeout(2*time.Second) || token.Error() != nil {
		logging.Warn("Could not announce the daemon as offline: %v", token.Error())
	} else {
		logging.Info("Announced offline on %s", p.availabilityTopic())
	}

	p.client.Disconnect(250)
}
