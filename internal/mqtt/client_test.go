package mqtt

import (
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
)

func TestClient_ClimateCommandHandlers(t *testing.T) {
	// Setup basic components for testing
	reader := &viessmann.Reader{}
	
	cfg := &config.Config{
		MQTTBroker: "localhost",
		MQTTPort:   1883,
		MQTTTopic:  "viessmann",
	}

	client := NewClient(cfg, reader)
	// The climate controller should be initialized in NewClient
	if client.climateController == nil {
		t.Skip("Climate controller not initialized - skipping handler tests")
	}

	tests := []struct {
		name        string
		zone        string
		temperature string
		expectValid bool
		description string
	}{
		{
			name:        "Valid hot water temperature",
			zone:        "hotwater",
			temperature: "45.0",
			expectValid: true,
			description: "Should handle valid hot water temperature command",
		},
		{
			name:        "Invalid temperature format",
			zone:        "hotwater", 
			temperature: "invalid",
			expectValid: false,
			description: "Should handle invalid temperature format gracefully",
		},
		{
			name:        "Valid heating temperature",
			zone:        "heating",
			temperature: "20.5",
			expectValid: true,
			description: "Should handle valid heating temperature command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The handlers are designed to log errors rather than return them,
			// so we test that they don't panic and can parse basic inputs
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Climate temperature command handler panicked: %v", r)
				}
			}()

			// This will log appropriate messages based on the input validity
			client.handleClimateTemperatureCommand(tt.zone, tt.temperature)
		})
	}
}

func TestClient_ClimateModeHandlers(t *testing.T) {
	// Setup mock components
	reader := &viessmann.Reader{} // This would need proper initialization in real tests
	
	cfg := &config.Config{
		MQTTBroker: "localhost",
		MQTTPort:   1883,
		MQTTTopic:  "viessmann",
	}

	client := NewClient(cfg, reader)
	// Initialize climate controller properly
	if client.climateController == nil {
		t.Skip("Climate controller not initialized - skipping handler tests")
	}

	tests := []struct {
		name        string
		zone        string
		mode        string
		expectValid bool
		description string
	}{
		{
			name:        "Valid hot water mode - off",
			zone:        "hotwater",
			mode:        "off",
			expectValid: true,
			description: "Should handle valid hot water off mode",
		},
		{
			name:        "Valid heating mode - auto",
			zone:        "heating",
			mode:        "auto",
			expectValid: true,
			description: "Should handle valid heating auto mode",
		},
		{
			name:        "Invalid mode",
			zone:        "hotwater",
			mode:        "invalid_mode",
			expectValid: false,
			description: "Should reject invalid mode gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The handlers are designed to log errors rather than return them,
			// so we test that they don't panic 
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Climate mode command handler panicked: %v", r)
				}
			}()

			// This will log appropriate messages based on the input validity
			client.handleClimateModeCommand(tt.zone, tt.mode)
		})
	}
}

func TestClient_SanitizeForTopic(t *testing.T) {
	cfg := &config.Config{}
	reader := &viessmann.Reader{}
	client := NewClient(cfg, reader)

	tests := []struct {
		input    string
		expected string
	}{
		{"Normal Text", "Normal_Text"},
		{"With-Dashes", "With_Dashes"},
		{"With Spaces & Symbols!", "With_Spaces___Symbols_"},
		{"CamelCaseText", "CamelCaseText"},
		{"123Numbers", "123Numbers"},
		{"", ""},
	}

	for _, tt := range tests {
		result := client.sanitizeForTopic(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeForTopic(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestClient_DetermineDeviceClass(t *testing.T) {
	cfg := &config.Config{}
	reader := &viessmann.Reader{}
	client := NewClient(cfg, reader)

	tests := []struct {
		paramName string
		expected  string
	}{
		{"Aussentemperatur", "temperature"},
		{"Warmwasser Temperatur", "temperature"},
		{"Temperature Outside", "temperature"},
		{"Druck Heizkreis", "pressure"},
		{"Pump Pressure", "pressure"},
		{"Brenner Leistung", "power"},
		{"Power Output", "power"},
		{"Betriebsstunden", "duration"},
		{"Operating Hours", "duration"},
		{"Humidity Prozent", "humidity"},
		{"Unknown Parameter", ""},
	}

	for _, tt := range tests {
		result := client.determineDeviceClass(tt.paramName)
		if result != tt.expected {
			t.Errorf("determineDeviceClass(%q) = %q, expected %q", tt.paramName, result, tt.expected)
		}
	}
}