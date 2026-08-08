// Package config handles YAML configuration file management
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yourusername/vcontrold-go/internal/errors"
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

	// Surcharges d'adresses propres a la machine, appliquees par-dessus
	// vito.xml sans le modifier. Voir overrides/vitola200-kw2.xml.
	VitoOverridesXML string `yaml:"vito_overrides_xml" json:"vito_overrides_xml"`
	WebPort          string `yaml:"web_port" json:"web_port"`
	WebMode          bool   `yaml:"web" json:"web"`
	Verbose          bool   `yaml:"verbose" json:"verbose"`

	// Auto-refresh configuration
	AutoRefresh AutoRefreshConfig `yaml:"auto_refresh" json:"auto_refresh"`

	// Multiple refresh groups
	RefreshGroups []RefreshGroup `yaml:"refresh_groups" json:"refresh_groups"`

	// MQTT configuration
	MQTT MQTTConfig `yaml:"mqtt" json:"mqtt"`

	// MQTT hooks for listening to topics and executing actions
	MQTTHooks []MQTTHookConfig `yaml:"mqtt_hooks" json:"mqtt_hooks"`
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

	// DeviceClass et StateClass alimentent la decouverte Home Assistant.
	// Sans state_class, HA n'archive pas l'entite en statistiques long terme
	// et l'historique disparait au bout de dix jours.
	DeviceClass  string `yaml:"device_class,omitempty" json:"device_class,omitempty"`
	StateClass   string `yaml:"state_class,omitempty" json:"state_class,omitempty"`
	Unit         string `yaml:"unit,omitempty" json:"unit,omitempty"`
	FriendlyName string `yaml:"friendly_name,omitempty" json:"friendly_name,omitempty"`
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
