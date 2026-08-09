// Package config handles YAML configuration file management
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/errors"
	"gopkg.in/yaml.v2"
)

// ParseHexAddress parses a hex address string (with or without 0x prefix) to uint16
func ParseHexAddress(addressStr string) (uint16, error) {
	// Remove 0x prefix if present
	addressStr = strings.TrimPrefix(strings.ToLower(addressStr), "0x")

	// Parse hex string
	addr, err := strconv.ParseUint(addressStr, 16, 16)
	if err != nil {
		return 0, errors.Wrap(errors.ErrValidation,
			fmt.Sprintf("invalid hex address '%s'", addressStr), err)
	}

	return uint16(addr), nil
}

// YamlConfig represents the YAML configuration file structure
type YamlConfig struct {
	// Command line equivalent settings
	Device      string `yaml:"device" json:"device"`
	ConfigFile  string `yaml:"config_file" json:"config_file"`
	VControlXML string `yaml:"vcontrol_xml" json:"vcontrol_xml"`
	VitoXML     string `yaml:"vito_xml" json:"vito_xml"`

	// Machine-specific address overrides, applied on top of vito.xml without
	// modifying it. See overrides/vitola200-kw2.xml.
	VitoOverridesXML string `yaml:"vito_overrides_xml" json:"vito_overrides_xml"`
	WebPort          string `yaml:"web_port" json:"web_port"`
	WebMode          bool   `yaml:"web" json:"web"`

	// Verbose is shorthand for log_level: debug.
	Verbose bool `yaml:"verbose" json:"verbose"`

	// LogLevel is one of debug, info, warn, error. Empty means info.
	// debug prints every serial transaction and every skipped publish, which
	// is what you want when a reading looks wrong and you need the bytes.
	LogLevel string `yaml:"log_level" json:"log_level"`

	// Auto-refresh configuration
	AutoRefresh AutoRefreshConfig `yaml:"auto_refresh" json:"auto_refresh"`

	// Multiple refresh groups
	RefreshGroups []RefreshGroup `yaml:"refresh_groups" json:"refresh_groups"`

	// MQTT configuration
	MQTT MQTTConfig `yaml:"mqtt" json:"mqtt"`

	// MQTT hooks for listening to topics and executing actions
	MQTTHooks []MQTTHookConfig `yaml:"mqtt_hooks" json:"mqtt_hooks"`

	// Climate entities exposed to Home Assistant (hot water, heating).
	Climates []MQTTClimateConfig `yaml:"mqtt_climates" json:"mqtt_climates"`

	// Values computed from two published readings.
	Derived []MQTTDerivedConfig `yaml:"mqtt_derived" json:"mqtt_derived"`

	// Cumulative run-time counters built from on/off datapoints.
	Runtimes []MQTTRuntimeConfig `yaml:"mqtt_runtime" json:"mqtt_runtime"`

	// Where the run-time totals are persisted. Empty places the file beside
	// the configuration.
	RuntimeStateFile string `yaml:"runtime_state_file,omitempty" json:"runtime_state_file,omitempty"`
}

// MQTTRuntimeConfig turns an on/off datapoint into a cumulative run-time
// counter, in hours.
//
// Home Assistant keeps long-term statistics only for sensors with a
// state_class. A binary_sensor is purged with the states table, so its run time
// would never accumulate — which is precisely the history needed to decide
// whether a recirculation pump is running around the clock.
type MQTTRuntimeConfig struct {
	// SourceAlias is the 0/1 datapoint to integrate, e.g. getPumpeStatusZirku.
	SourceAlias string `yaml:"source_alias" json:"source_alias"`

	// Alias is the topic the accumulated hours are published on.
	Alias        string `yaml:"alias" json:"alias"`
	FriendlyName string `yaml:"friendly_name" json:"friendly_name"`
	Icon         string `yaml:"icon,omitempty" json:"icon,omitempty"`
}

// MQTTDerivedConfig describes a ratio between two already-published readings.
//
// Deliberately limited to a ratio: the only useful derived value here is the
// average burner cycle length, and a full expression engine would be a debt
// taken on for a single use case.
type MQTTDerivedConfig struct {
	Alias        string  `yaml:"alias" json:"alias"`
	FriendlyName string  `yaml:"friendly_name" json:"friendly_name"`
	Numerator    string  `yaml:"numerator_alias" json:"numerator_alias"`
	Denominator  string  `yaml:"denominator_alias" json:"denominator_alias"`
	Factor       float64 `yaml:"factor" json:"factor"`
	Unit         string  `yaml:"unit,omitempty" json:"unit,omitempty"`
	StateClass   string  `yaml:"state_class,omitempty" json:"state_class,omitempty"`
	DeviceClass  string  `yaml:"device_class,omitempty" json:"device_class,omitempty"`
	Icon         string  `yaml:"icon,omitempty" json:"icon,omitempty"`
	Precision    int     `yaml:"precision,omitempty" json:"precision,omitempty"`
}

// MQTTClimateConfig describes a Home Assistant climate entity.
//
// A climate entity ties together an adjustable setpoint, a measured
// temperature and a mode, which is what turns a bare slider into a
// thermostat.
type MQTTClimateConfig struct {
	Name string `yaml:"name" json:"name"`

	// ObjectID identifies the entity on the wire: it is the node in the
	// discovery topic and the suffix of unique_id, which is what Home
	// Assistant keys the entity off. It does not decide the entity_id — that
	// is derived from Name, as it is for every other entity here.
	ObjectID string `yaml:"object_id" json:"object_id"`
	Icon     string `yaml:"icon,omitempty" json:"icon,omitempty"`

	// Alias of the topic carrying the current setpoint, and the address to write.
	TargetAlias   string `yaml:"target_alias" json:"target_alias"`
	TargetAddress string `yaml:"target_address" json:"target_address"`

	// Alias of the topic carrying the measured temperature. Optional: the KW2
	// controller exposes no room sensor, and an entity without a current
	// temperature stays honest.
	CurrentAlias string `yaml:"current_alias,omitempty" json:"current_alias,omitempty"`

	MinTemp  float64 `yaml:"min_temp" json:"min_temp"`
	MaxTemp  float64 `yaml:"max_temp" json:"max_temp"`
	TempStep float64 `yaml:"temp_step,omitempty" json:"temp_step,omitempty"`

	// Offered modes. The controller has a single global mode (unit BA), so
	// leaving this empty exposes a setpoint-only entity rather than promising
	// per-zone control the appliance cannot deliver.
	Modes       []string `yaml:"modes,omitempty" json:"modes,omitempty"`
	ModeAlias   string   `yaml:"mode_alias,omitempty" json:"mode_alias,omitempty"`
	ModeAddress string   `yaml:"mode_address,omitempty" json:"mode_address,omitempty"`

	// ModeTemplate maps the raw mode topic value onto an HVAC mode. Without
	// it Home Assistant parks the entity on "off" and hides the setpoint.
	ModeTemplate string `yaml:"mode_template,omitempty" json:"mode_template,omitempty"`

	// ActionAlias carries the burner state: it feeds hvac_action, hence the
	// "heating / idle" indication in Home Assistant.
	ActionAlias string `yaml:"action_alias,omitempty" json:"action_alias,omitempty"`
}

// AutoRefreshConfig defines parameters to automatically refresh
type AutoRefreshConfig struct {
	Enabled   bool     `yaml:"enabled" json:"enabled"`
	Interval  string   `yaml:"interval" json:"interval"`   // e.g., "30s", "1m", "5m"
	Addresses []string `yaml:"addresses" json:"addresses"` // Hex addresses like ["0x810", "0x811", "0x812"]
}

// RefreshGroup defines a group of addresses with their own refresh interval
type RefreshGroup struct {
	Name      string   `yaml:"name" json:"name"`           // Display name for the group
	Interval  string   `yaml:"interval" json:"interval"`   // e.g., "30s", "5m", "1h", "24h"
	Addresses []string `yaml:"addresses" json:"addresses"` // Hex addresses like ["0x810", "0x811"]
	Enabled   bool     `yaml:"enabled" json:"enabled"`     // Whether this group is active
}

// MQTTConfig defines MQTT connection and publishing configuration
type MQTTConfig struct {
	Enabled          bool          `yaml:"enabled" json:"enabled"`                     // Whether MQTT is enabled
	Host             string        `yaml:"host" json:"host"`                           // MQTT broker host (e.g., "localhost:1883")
	Username         string        `yaml:"username" json:"username"`                   // MQTT username
	Password         string        `yaml:"password" json:"password"`                   // MQTT password
	TopicRoot        string        `yaml:"topic_root" json:"topic_root"`               // Root topic (e.g., "viessmann")
	RefreshThreshold string        `yaml:"refresh_threshold" json:"refresh_threshold"` // Time threshold for republishing (e.g., "1h")
	Addresses        []MQTTAddress `yaml:"addresses" json:"addresses"`                 // Addresses to watch and publish
}

// MQTTAddress defines an address to watch and its MQTT topic alias
type MQTTAddress struct {
	Address string `yaml:"address" json:"address"` // Hex address like "0x810"
	Alias   string `yaml:"alias" json:"alias"`     // Topic alias (e.g., "boiler_temp")

	// DeviceClass and StateClass feed Home Assistant discovery. Without
	// state_class, HA keeps the entity out of long-term statistics and the
	// history disappears after ten days.
	DeviceClass  string `yaml:"device_class,omitempty" json:"device_class,omitempty"`
	StateClass   string `yaml:"state_class,omitempty" json:"state_class,omitempty"`
	Unit         string `yaml:"unit,omitempty" json:"unit,omitempty"`
	FriendlyName string `yaml:"friendly_name,omitempty" json:"friendly_name,omitempty"`

	// Component picks the Home Assistant entity type: "sensor" (default) or
	// "binary_sensor". A 0/1 state — burner, pump — reads far better as a
	// binary sensor: "Running / Not running" rather than a bare "0".
	Icon      string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Component string `yaml:"component,omitempty" json:"component,omitempty"`

	// MaxIncreasePerHour bounds how fast a monotonic counter may rise. The KW
	// protocol has no checksum, so a disturbed frame can yield a well-formed
	// but wrong number; published to a total_increasing entity that is
	// permanent damage. Zero disables the check.
	//
	// The bounds are physical, not guessed: burner hours cannot rise faster
	// than one hour per hour of wall time.
	MaxIncreasePerHour float64 `yaml:"max_increase_per_hour,omitempty" json:"max_increase_per_hour,omitempty"`

	// RawAlias publishes the raw protocol value as a number alongside the
	// decoded text. A text sensor has no memory in Home Assistant beyond
	// purge_keep_days; if the value matters historically it must also exist as
	// a number.
	RawAlias        string `yaml:"raw_alias,omitempty" json:"raw_alias,omitempty"`
	RawFriendlyName string `yaml:"raw_friendly_name,omitempty" json:"raw_friendly_name,omitempty"`

	// Options lists the possible values of an enum sensor. Cosmetic — it makes
	// the UI and the logbook readable — and requires device_class: enum.
	Options    []string `yaml:"options,omitempty" json:"options,omitempty"`
	PayloadOn  string   `yaml:"payload_on,omitempty" json:"payload_on,omitempty"`
	PayloadOff string   `yaml:"payload_off,omitempty" json:"payload_off,omitempty"`
}

// MQTTHookConfig defines a hook configuration in YAML
type MQTTHookConfig struct {
	Topic       string         `yaml:"topic" json:"topic"`             // MQTT topic to listen to
	Description string         `yaml:"description" json:"description"` // Human readable description
	Action      MQTTHookAction `yaml:"action" json:"action"`           // Action to execute
	Enabled     bool           `yaml:"enabled" json:"enabled"`         // Whether this hook is active
}

// MQTTHookAction defines the action configuration
type MQTTHookAction struct {
	Type      string                 `yaml:"type" json:"type"`                               // "set_temperature", "set_mode", "write_address", "write_parameter"
	Address   string                 `yaml:"address,omitempty" json:"address,omitempty"`     // Target address (hex format)
	Parameter string                 `yaml:"parameter,omitempty" json:"parameter,omitempty"` // Parameter name to find
	Zone      string                 `yaml:"zone,omitempty" json:"zone,omitempty"`           // For temperature/mode: "heating", "hotwater"
	MinValue  *float64               `yaml:"min_value,omitempty" json:"min_value,omitempty"` // Value validation
	MaxValue  *float64               `yaml:"max_value,omitempty" json:"max_value,omitempty"` // Value validation
	Transform string                 `yaml:"transform,omitempty" json:"transform,omitempty"` // Value transformation: "multiply_10"
	Options   map[string]interface{} `yaml:"options,omitempty" json:"options,omitempty"`     // Additional options
}

// LoadYamlConfig loads configuration from YAML file
func LoadYamlConfig(filename string) (*YamlConfig, error) {
	// Set defaults
	config := &YamlConfig{
		Device:      "/dev/ttyUSB0",
		WebPort:     "8080",
		WebMode:     false,
		VControlXML: "xml/vcontrold.xml",
		VitoXML:     "xml/vito.xml",
		Verbose:     false,
		AutoRefresh: AutoRefreshConfig{
			Enabled:   false,
			Interval:  "30s",
			Addresses: []string{},
		},
		MQTT: MQTTConfig{
			Enabled:          false,
			Host:             "localhost:1883",
			Username:         "",
			Password:         "",
			TopicRoot:        "viessmann",
			RefreshThreshold: "1h",
			Addresses:        []MQTTAddress{},
		},
	}

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return config, nil // Return defaults if file doesn't exist
	}

	// Read YAML file
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, errors.Wrap(errors.ErrConfig,
			fmt.Sprintf("failed to read YAML config file %s", filename), err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, errors.Wrap(errors.ErrConfig,
			fmt.Sprintf("failed to parse YAML config file %s", filename), err)
	}

	return config, nil
}

// SaveYamlConfig saves configuration to YAML file
func SaveYamlConfig(filename string, config *YamlConfig) error {
	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return errors.Wrap(errors.ErrConfig, "failed to marshal config to YAML", err)
	}

	// Write to file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return errors.Wrap(errors.ErrConfig, fmt.Sprintf("failed to write YAML config file %s", filename), err)
	}

	return nil
}

// GetRefreshDuration parses the interval string to time.Duration
func (c *AutoRefreshConfig) GetRefreshDuration() (time.Duration, error) {
	if c.Interval == "" {
		return 30 * time.Second, nil // Default
	}

	duration, err := time.ParseDuration(c.Interval)
	if err != nil {
		return 0, errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid refresh interval '%s'", c.Interval), err)
	}

	return duration, nil
}

// ValidateAddresses checks if addresses are valid hex format
func (c *AutoRefreshConfig) ValidateAddresses() error {
	for i, addr := range c.Addresses {
		if addr == "" {
			return errors.New(errors.ErrValidation, fmt.Sprintf("address %d: address cannot be empty", i))
		}

		// Try to parse hex address
		if _, err := ParseHexAddress(addr); err != nil {
			return errors.Wrap(errors.ErrValidation, fmt.Sprintf("address %d: invalid hex address '%s'", i, addr), err)
		}
	}

	return nil
}

// GetAddressesAsUint16 returns the addresses as uint16 values
func (c *AutoRefreshConfig) GetAddressesAsUint16() ([]uint16, error) {
	var addresses []uint16

	for _, addr := range c.Addresses {
		hexAddr, err := ParseHexAddress(addr)
		if err != nil {
			return nil, errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid address '%s'", addr), err)
		}
		addresses = append(addresses, hexAddr)
	}

	return addresses, nil
}

// GetRefreshDuration parses the interval string to time.Duration for RefreshGroup
func (rg *RefreshGroup) GetRefreshDuration() (time.Duration, error) {
	if rg.Interval == "" {
		return 30 * time.Second, nil // Default
	}

	duration, err := time.ParseDuration(rg.Interval)
	if err != nil {
		return 0, errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid refresh interval '%s' for group '%s'", rg.Interval, rg.Name), err)
	}

	return duration, nil
}

// ValidateAddresses checks if addresses are valid hex format for RefreshGroup
func (rg *RefreshGroup) ValidateAddresses() error {
	for i, addr := range rg.Addresses {
		if addr == "" {
			return errors.New(errors.ErrValidation, fmt.Sprintf("group '%s' address %d: address cannot be empty", rg.Name, i))
		}

		// Try to parse hex address
		if _, err := ParseHexAddress(addr); err != nil {
			return errors.Wrap(errors.ErrValidation, fmt.Sprintf("group '%s' address %d: invalid hex address '%s'", rg.Name, i, addr), err)
		}
	}

	return nil
}

// GetAddressesAsUint16 returns the addresses as uint16 values for RefreshGroup
func (rg *RefreshGroup) GetAddressesAsUint16() ([]uint16, error) {
	var addresses []uint16

	for _, addr := range rg.Addresses {
		hexAddr, err := ParseHexAddress(addr)
		if err != nil {
			return nil, errors.Wrap(errors.ErrValidation, fmt.Sprintf("group '%s' invalid address '%s'", rg.Name, addr), err)
		}
		addresses = append(addresses, hexAddr)
	}

	return addresses, nil
}

// GetRefreshThresholdDuration parses the refresh threshold to time.Duration
func (m *MQTTConfig) GetRefreshThresholdDuration() (time.Duration, error) {
	if m.RefreshThreshold == "" {
		return time.Hour, nil // Default to 1 hour
	}

	duration, err := time.ParseDuration(m.RefreshThreshold)
	if err != nil {
		return 0, fmt.Errorf("invalid MQTT refresh threshold '%s': %v", m.RefreshThreshold, err)
	}

	return duration, nil
}
