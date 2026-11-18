// Package mqtt handles MQTT publishing with intelligent filtering
package mqtt

import (
	"fmt"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
)

// notInXMLAnnotation matches the marker addXMLStatusAnnotation appends.
var notInXMLAnnotation = regexp.MustCompile(`\s+\(not in XML\)$`)

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

	// Weekly schedules, gathered per circuit.
	schedules       *scheduleState
	scheduleConfigs []config.MQTTScheduleConfig
	scheduleIndex   map[uint16]scheduleSlot

	// Refuses counter readings that cannot be true.
	guard *counterGuard

	// Per-address settings, indexed by address, for the checks that need them.
	addressSettings map[uint16]config.MQTTAddress

	// Last accepted reading per address, republished on every connection so
	// Home Assistant is current at once rather than at the next change.
	latest map[uint16]viessmann.CacheEvent

	// Readings that may not have reached Home Assistant. Nil disables it.
	ledger *ledger

	// haOnline follows Home Assistant's status topic. While Home Assistant is
	// down the broker still accepts our messages, but nobody records them.
	haOnline atomic.Bool

	// What was announced, kept to announce it again after a reconnection.
	climates         []config.MQTTClimateConfig
	runtimeStatePath string
	announceMutex    sync.Mutex
}

// The broker is retried for ever. Giving up after the first failure is what
// lost two days of readings in November 2026: the target booted before its
// Wi-Fi, the only connection attempt failed, and the daemon went on reading
// the boiler while publishing nothing until someone restarted it.
const (
	connectRetryInterval = 30 * time.Second
	maxReconnectInterval = time.Minute
)

// NewPublisher creates a new MQTT publisher
func NewPublisher(mqttConfig *config.MQTTConfig) *Publisher {
	if mqttConfig == nil {
		return &Publisher{enabled: false}
	}

	p := &Publisher{
		config:        mqttConfig,
		lastPublished: make(map[uint16]*LastPublish),
		addressMap:    make(map[uint16]string),
		latest:        make(map[uint16]viessmann.CacheEvent),
		enabled:       mqttConfig.Enabled,
	}
	// Until Home Assistant says otherwise: its birth message is not retained,
	// so a daemon starting while it runs would never hear it.
	p.haOnline.Store(true)

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

	// The client is built here and connected by Start, once the entities to
	// announce are known.
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
		SetWill(availabilityTopic, availabilityOffline, 0, true).
		SetConnectRetry(true).
		SetConnectRetryInterval(connectRetryInterval).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(maxReconnectInterval)

	// Set credentials if provided
	if p.config.Username != "" {
		opts.SetUsername(p.config.Username)
		if p.config.Password != "" {
			opts.SetPassword(p.config.Password)
		}
	}

	// Set connection handlers
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logging.Warn("MQTT: Connection lost: %v; reconnecting, readings go to the ledger meanwhile", err)
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		logging.Info("MQTT: Connected to broker %s", p.config.Host)

		// Re-asserted on every (re)connection: after a drop it is the
		// "offline" will that sits retained on the broker.
		if token := client.Publish(availabilityTopic, 0, true, availabilityOnline); token.Wait() && token.Error() != nil {
			logging.Error("MQTT: Failed to announce availability: %v", token.Error())
		}

		if token := client.Subscribe(haStatusTopic, 0, p.handleHAStatus); token.Wait() && token.Error() != nil {
			logging.Warn("MQTT: Cannot follow %s: %v", haStatusTopic, token.Error())
		}

		// Off the client's goroutine: announcing waits on dozens of tokens.
		go p.announce()
	})

	p.client = mqtt.NewClient(opts)
}

// Start connects to the broker in the background and returns at once. The
// boiler is read whether or not the broker answers; until it does, readings go
// to the ledger.
func (p *Publisher) Start() {
	if !p.enabled || p.client == nil {
		return
	}
	logging.Info("MQTT: Connecting to broker %s, retrying every %s until it answers", p.config.Host, connectRetryInterval)
	token := p.client.Connect()
	go func() {
		// With ConnectRetry the token completes only once connected, or on an
		// error retrying cannot fix, such as invalid options.
		if token.Wait() && token.Error() != nil {
			logging.Error("MQTT: Gave up connecting to broker %s: %v", p.config.Host, token.Error())
		}
	}()
}

// EnableLedger keeps a record of the readings that may not have reached Home
// Assistant, in dir.
func (p *Publisher) EnableLedger(dir string) {
	// Absolute, so the log says where to look rather than "ledger".
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	p.ledger = newLedger(dir)
	logging.Info("Ledger: undelivered readings will be written to %s", dir)
}

// online reports whether a publish would actually leave now. IsConnected is not
// enough: with automatic reconnection it also answers true while reconnecting,
// and paho then completes QoS 0 publishes without sending them.
func (p *Publisher) online() bool {
	return p.client != nil && p.client.IsConnectionOpen()
}

// delivering reports whether a reading published now would be recorded by Home
// Assistant, as far as the daemon can know.
func (p *Publisher) delivering() bool {
	return p.online() && p.haOnline.Load()
}

// handleHAStatus follows Home Assistant's birth and last-will messages.
func (p *Publisher) handleHAStatus(_ mqtt.Client, msg mqtt.Message) {
	switch string(msg.Payload()) {
	case availabilityOnline:
		if !p.haOnline.Swap(true) {
			logging.Info("MQTT: Home Assistant is back online")
		}
		// Home Assistant asks for this: a restarted instance relies on the
		// integrations announcing themselves again.
		go p.announce()
	case availabilityOffline:
		if p.haOnline.Swap(false) {
			logging.Warn("MQTT: Home Assistant went offline; readings go to the ledger until it returns")
		}
	}
}

// announce publishes every discovery message and the current values. It runs on
// every connection, so entities configured while the broker was unreachable
// are not left unannounced.
func (p *Publisher) announce() {
	p.announceMutex.Lock()
	defer p.announceMutex.Unlock()

	if err := p.PublishDiscovery(); err != nil {
		logging.Warn("MQTT discovery not published: %v", err)
		return
	}
	for _, step := range []struct {
		name    string
		publish func() error
	}{
		{"climate", func() error { return p.PublishClimateDiscovery(p.climates) }},
		{"derived", func() error { return p.PublishDerivedDiscovery(p.derivedConfigs) }},
		{"raw mirror", p.PublishRawMirrorDiscovery},
		{"run-time", func() error { return p.PublishRuntimeDiscovery(p.runtimeConfigs, p.runtimeStatePath) }},
		{"schedule", func() error { return p.PublishScheduleDiscovery(p.scheduleConfigs) }},
	} {
		if err := step.publish(); err != nil {
			logging.Warn("MQTT %s discovery not published: %v", step.name, err)
		}
	}
	p.republish()
}

// republish sends the latest known values. Without it an unchanged reading, a
// counter at night for instance, would stay missing until the hourly refresh.
func (p *Publisher) republish() {
	p.mutex.RLock()
	events := make([]viessmann.CacheEvent, 0, len(p.latest))
	for _, e := range p.latest {
		events = append(events, e)
	}
	aliases := make(map[uint16]string, len(p.addressMap))
	for k, v := range p.addressMap {
		aliases[k] = v
	}
	p.mutex.RUnlock()

	sent := 0
	for _, e := range events {
		if alias, ok := aliases[e.Address]; ok && p.publishReading(e, alias) {
			sent++
		}
	}

	if p.derived != nil {
		p.derived.forget()
		p.publishDerived()
	}
	p.republishRuntime()
	p.republishSchedules()

	if sent > 0 {
		logging.Info("MQTT: Republished %d current value(s)", sent)
	}
}

// HandleCacheEvent processes cache events and decides whether to publish to MQTT
func (p *Publisher) HandleCacheEvent(event viessmann.CacheEvent) {
	if !p.enabled || !p.config.Enabled {
		return
	}

	// Only publish if successful reading
	if !event.Reading.Success {
		return
	}

	// Schedule days are read but not published individually, so they never
	// reach the alias lookup below. Feed the aggregator first.
	p.handleSchedule(event.Address, p.cleanValueForHomeAssistant(event.Reading.FormattedValue))

	p.mutex.RLock()
	alias, exists := p.addressMap[event.Address]
	p.mutex.RUnlock()

	if !exists {
		// Address not monitored for MQTT
		return
	}

	cleaned := p.cleanValueForHomeAssistant(event.Reading.FormattedValue)
	now := time.Now()

	// Integrate on/off datapoints on every observation, before any publish
	// suppression. Republishing is throttled to once an hour for an unchanged
	// value, and a tracker fed only by those would see a one-hour gap between
	// samples, credit nothing, and silently report zero run time forever.
	p.handleRuntime(alias, cleaned, now)

	entry := ledgerEntry{Time: now, Address: event.Address, Alias: alias, Value: cleaned}

	shouldPublish, reason := p.shouldPublish(event)
	if !shouldPublish {
		// Debug, not warn: suppressing an unchanged value is the normal case,
		// several times a minute. Logging it as a warning buries the real ones
		// and makes the journal unreadable.
		logging.Debug("MQTT: Skipping publish for 0x%04X (%s): %s", event.Address, alias, reason)
		p.remember(event)
		p.ledger.record(entry, p.delivering())
		return
	}

	// A counter that cannot be true must not leave the daemon: filtering it in
	// Home Assistant would be too late, the row is already written. Nor may it
	// reach the ledger, from which it would be imported later.
	settings := p.settingsFor(event.Address)
	if p.guard != nil && !p.guard.accept(alias, cleaned, settings.MaxIncreasePerHour, now) {
		return
	}

	p.remember(event)
	delivered := p.publishReading(event, alias)
	p.ledger.record(entry, delivered && p.haOnline.Load())
}

// remember keeps a reading for the republication after a reconnection.
func (p *Publisher) remember(event viessmann.CacheEvent) {
	p.mutex.Lock()
	p.latest[event.Address] = event
	p.mutex.Unlock()
}

// publishReading sends one reading, with its raw mirror, and reports whether it
// left. Only a reading that left counts as published: otherwise an unchanged
// value would wait an hour to be sent again after a reconnection.
func (p *Publisher) publishReading(event viessmann.CacheEvent, alias string) bool {
	if !p.publishValue(p.stateTopic(alias), event.Reading.FormattedValue, event.Address, alias) {
		return false
	}

	// A text value has no long-term history in Home Assistant. Where the
	// configuration asks for it, mirror the raw protocol value as a number.
	if settings := p.settingsFor(event.Address); settings.RawAlias != "" {
		p.publishRawMirror(settings.RawAlias, event.Reading.RawValue)
	}

	p.mutex.Lock()
	p.lastPublished[event.Address] = &LastPublish{
		Timestamp: time.Now(),
		Value:     p.cleanValueForHomeAssistant(event.Reading.FormattedValue),
	}
	p.mutex.Unlock()
	return true
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

	// Strip only the annotation the formatter adds. Removing any trailing
	// parenthesis would eat legitimate text: a fault label reads
	// "Regelbetrieb (kein Fehler)", and losing its tail changes the meaning.
	value = notInXMLAnnotation.ReplaceAllString(value, "")

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

// publishValue publishes a value to MQTT and reports whether it left.
func (p *Publisher) publishValue(topic, value string, address uint16, alias string) bool {
	if !p.online() {
		// Debug: during an outage this would fire on every reading. The
		// outage itself is logged once, by the connection handler and the
		// ledger.
		logging.Debug("MQTT: Not connected, %s = '%s' not published", topic, value)
		return false
	}

	// Clean the value for Home Assistant
	cleanedValue := p.cleanValueForHomeAssistant(value)

	// Publish with QoS 0 (at most once delivery) and retain flag set to true
	token := p.client.Publish(topic, 0, true, cleanedValue)
	if token.Wait() && token.Error() != nil {
		logging.Error("MQTT: Failed to publish %s = '%s': %v", topic, cleanedValue, token.Error())
		return false
	}

	logging.Debug("MQTT PUBLISHED: %s = '%s' (0x%04X -> %s) [cleaned from: '%s']", topic, cleanedValue, address, alias, value)

	// A reading just changed, so anything derived from it may have too.
	if p.derived != nil {
		p.derived.record(alias, cleanedValue)
		p.publishDerived()
	}
	return true
}

// UpdateConfig updates the MQTT configuration
func (p *Publisher) UpdateConfig(newConfig *config.MQTTConfig) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Disconnect existing client if any, which also stops its retries
	if p.client != nil {
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
			p.Start()
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

	return map[string]interface{}{
		"enabled":          p.enabled,
		"connected":        p.online(),
		"haOnline":         p.haOnline.Load(),
		"ledger":           p.ledger.status(),
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

	if !p.online() {
		return
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
		if err == errDiscoveryDisabled || err == errNotConnected {
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

	p.ledger.close()

	if !p.online() {
		if p.client != nil {
			// Stops the connection retries.
			p.client.Disconnect(0)
		}
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
