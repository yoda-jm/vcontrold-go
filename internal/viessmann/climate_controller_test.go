package viessmann

import (
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
)

func TestClimateController_ProcessClimateCommand(t *testing.T) {
	// Setup mock communicator and reader
	mockComm := NewMockSerialCommunicator()
	reader := &Reader{
		serialComm: mockComm,
		logger:     &MockLogger{},
		dataPoints: []config.DataPoint{
			{Name: "Warmwasser Solltemperatur", Address: 0x6300, DataType: "short", Unit: "°C"},
			{Name: "Raumtemperatur Normal Sollwert A1M1", Address: 0x2306, DataType: "short", Unit: "°C"},
		},
	}

	controller := NewClimateController(reader)

	tests := []struct {
		name        string
		command     ClimateCommand
		expectError bool
		description string
	}{
		{
			name: "Set hot water temperature - valid",
			command: ClimateCommand{
				Type:  "temperature",
				Zone:  "hotwater",
				Value: "45.0",
			},
			expectError: false,
			description: "Should successfully set hot water temperature to 45°C",
		},
		{
			name: "Set hot water temperature - out of range",
			command: ClimateCommand{
				Type:  "temperature",
				Zone:  "hotwater",
				Value: "70.0",
			},
			expectError: true,
			description: "Should fail when temperature is above 65°C",
		},
		{
			name: "Set heating temperature - valid",
			command: ClimateCommand{
				Type:  "temperature",
				Zone:  "heating",
				Value: "20.5",
			},
			expectError: false,
			description: "Should successfully set heating temperature to 20.5°C",
		},
		{
			name: "Set hot water mode - off",
			command: ClimateCommand{
				Type:  "mode",
				Zone:  "hotwater",
				Value: "off",
			},
			expectError: false,
			description: "Should successfully set hot water mode to off",
		},
		{
			name: "Invalid command type",
			command: ClimateCommand{
				Type:  "invalid",
				Zone:  "hotwater",
				Value: "test",
			},
			expectError: true,
			description: "Should fail with unknown command type",
		},
		{
			name: "Invalid temperature format",
			command: ClimateCommand{
				Type:  "temperature",
				Zone:  "hotwater",
				Value: "not_a_number",
			},
			expectError: true,
			description: "Should fail with invalid temperature format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := controller.ProcessClimateCommand(tt.command)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none for test: %s", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v for test: %s", err, tt.description)
			}
		})
	}
}

func TestClimateController_GetAvailableSetpoints(t *testing.T) {
	// Setup reader with test data points
	reader := &Reader{
		logger: &MockLogger{},
		dataPoints: []config.DataPoint{
			{Name: "Warmwasser Solltemperatur", Address: 0x6300, DataType: "short", Unit: "°C"},
			{Name: "Raumtemperatur Normal Sollwert A1M1", Address: 0x2306, DataType: "short", Unit: "°C"},
			{Name: "Aussentemperatur", Address: 0x5525, DataType: "short", Unit: "°C"}, // Should be ignored
		},
	}

	controller := NewClimateController(reader)
	setpoints := controller.GetAvailableSetpoints()

	// Check that we found the expected setpoints
	if len(setpoints) != 2 {
		t.Errorf("Expected 2 setpoints, got %d", len(setpoints))
	}

	// Check hot water setpoint
	if hotwater, exists := setpoints["hotwater"]; exists {
		if hotwater.Address != 0x6300 {
			t.Errorf("Hot water setpoint address: expected 0x6300, got 0x%04X", hotwater.Address)
		}
		if hotwater.MinValue != 35.0 || hotwater.MaxValue != 65.0 {
			t.Errorf("Hot water setpoint range: expected 35-65°C, got %.1f-%.1f°C",
				hotwater.MinValue, hotwater.MaxValue)
		}
	} else {
		t.Error("Hot water setpoint not found")
	}

	// Check heating setpoint
	if heating, exists := setpoints["heating"]; exists {
		if heating.Address != 0x2306 {
			t.Errorf("Heating setpoint address: expected 0x2306, got 0x%04X", heating.Address)
		}
		if heating.MinValue != 10.0 || heating.MaxValue != 30.0 {
			t.Errorf("Heating setpoint range: expected 10-30°C, got %.1f-%.1f°C",
				heating.MinValue, heating.MaxValue)
		}
	} else {
		t.Error("Heating setpoint not found")
	}
}

func TestClimateController_WriteParameterError(t *testing.T) {
	// Setup mock communicator that returns write errors
	mockComm := NewMockSerialCommunicator()
	mockComm.SetWriteError(0x6300, errors.New(errors.ErrCommunication, "mock write error"))

	reader := &Reader{
		serialComm: mockComm,
		logger:     &MockLogger{},
		dataPoints: []config.DataPoint{
			{Name: "Warmwasser Solltemperatur", Address: 0x6300, DataType: "short", Unit: "°C"},
		},
	}

	controller := NewClimateController(reader)

	// This should fail due to the mock write error
	command := ClimateCommand{
		Type:  "temperature",
		Zone:  "hotwater",
		Value: "45.0",
	}

	err := controller.ProcessClimateCommand(command)
	if err == nil {
		t.Error("Expected write error but command succeeded")
	}

	if !errors.IsType(err, errors.ErrCommunication) {
		t.Errorf("Expected communication error, got: %v", err)
	}
}

// MockLogger for testing
type MockLogger struct{}

func (m *MockLogger) Debug(msg string, args ...interface{}) {}
func (m *MockLogger) Info(msg string, args ...interface{})  {}
func (m *MockLogger) Warn(msg string, args ...interface{})  {}
func (m *MockLogger) Error(msg string, args ...interface{}) {}
