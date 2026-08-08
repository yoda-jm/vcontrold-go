package viessmann

import (
	"fmt"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
)

// TestConfigValidator tests the configuration validation functionality
func TestConfigValidator(t *testing.T) {
	logger := NewNoOpLogger()
	validator := NewConfigValidator(logger)

	t.Run("ValidateReaderConfig", func(t *testing.T) {
		tests := []struct {
			name        string
			config      *ReaderConfig
			expectError bool
			errorType   *errors.ErrorType
		}{
			{
				name:        "nil config",
				config:      nil,
				expectError: true,
				errorType:   errors.ErrValidation,
			},
			{
				name: "valid config",
				config: &ReaderConfig{
					Device:     "mock-device",
					SerialComm: NewMockSerialCommunicator(),
					Formatter:  NewValueFormatter(),
					Decoder:    NewValueDecoder(),
					Cache:      NewParameterCache(),
					Logger:     NewNoOpLogger(),
					Metrics:    NewMetrics(),
				},
				expectError: false,
			},
			{
				name: "missing serial communicator",
				config: &ReaderConfig{
					Device:     "mock-device",
					SerialComm: nil,
					Formatter:  NewValueFormatter(),
					Decoder:    NewValueDecoder(),
					Cache:      NewParameterCache(),
					Logger:     NewNoOpLogger(),
					Metrics:    NewMetrics(),
				},
				expectError: true,
				errorType:   errors.ErrValidation,
			},
			{
				name: "missing formatter",
				config: &ReaderConfig{
					Device:     "mock-device",
					SerialComm: NewMockSerialCommunicator(),
					Formatter:  nil,
					Decoder:    NewValueDecoder(),
					Cache:      NewParameterCache(),
					Logger:     NewNoOpLogger(),
					Metrics:    NewMetrics(),
				},
				expectError: true,
				errorType:   errors.ErrValidation,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := validator.ValidateReaderConfig(tt.config)

				if tt.expectError {
					if err == nil {
						t.Errorf("Expected error, got none")
						return
					}
					if tt.errorType != nil && !errors.IsType(err, tt.errorType) {
						t.Errorf("Expected error type %v, got %v", tt.errorType, err)
					}
				} else {
					if err != nil {
						t.Errorf("Unexpected error: %v", err)
					}
				}
			})
		}
	})

	t.Run("ValidateDevice", func(t *testing.T) {
		tests := []struct {
			name        string
			device      string
			expectError bool
		}{
			{
				name:        "valid device path",
				device:      "/dev/ttyUSB0",
				expectError: false,
			},
			{
				name:        "mock device",
				device:      "mock-device",
				expectError: false,
			},
			{
				name:        "test device",
				device:      "test-device",
				expectError: false,
			},
			{
				name:        "empty device",
				device:      "",
				expectError: true,
			},
			{
				name:        "invalid device path",
				device:      "invalid-path",
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := validator.ValidateDevice(tt.device)

				if tt.expectError && err == nil {
					t.Errorf("Expected error for device %s, got none", tt.device)
				}

				if !tt.expectError && err != nil {
					t.Errorf("Unexpected error for device %s: %v", tt.device, err)
				}
			})
		}
	})

	t.Run("ValidateDataPoints", func(t *testing.T) {
		tests := []struct {
			name        string
			dataPoints  []config.DataPoint
			expectError bool
		}{
			{
				name:        "empty data points",
				dataPoints:  []config.DataPoint{},
				expectError: true,
			},
			{
				name: "valid data points",
				dataPoints: []config.DataPoint{
					{Name: "Param1", Address: 0x1000, Size: 2, DataType: "Int"},
					{Name: "Param2", Address: 0x2000, Size: 4, DataType: "Int4"},
					{Name: "Param3", Address: 0x3000, Size: 1, DataType: "Byte"},
				},
				expectError: false,
			},
			{
				name: "invalid data type",
				dataPoints: []config.DataPoint{
					{Name: "Param1", Address: 0x1000, Size: 2, DataType: "InvalidType"},
				},
				expectError: true,
			},
			{
				name: "empty parameter name",
				dataPoints: []config.DataPoint{
					{Name: "", Address: 0x1000, Size: 2, DataType: "Int"},
				},
				expectError: true,
			},
			{
				name: "zero address",
				dataPoints: []config.DataPoint{
					{Name: "Param1", Address: 0x0000, Size: 2, DataType: "Int"},
				},
				expectError: true,
			},
			{
				name: "zero size",
				dataPoints: []config.DataPoint{
					{Name: "Param1", Address: 0x1000, Size: 0, DataType: "Int"},
				},
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := validator.ValidateDataPoints(tt.dataPoints)

				if tt.expectError && err == nil {
					t.Errorf("Expected error, got none")
				}

				if !tt.expectError && err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("ValidateDataPoint", func(t *testing.T) {
		tests := []struct {
			name        string
			dataPoint   config.DataPoint
			expectError bool
		}{
			{
				name: "valid byte parameter",
				dataPoint: config.DataPoint{
					Name: "ByteParam", Address: 0x1000, Size: 1, DataType: "Byte",
				},
				expectError: false,
			},
			{
				name: "valid signed byte parameter",
				dataPoint: config.DataPoint{
					Name: "SByteParam", Address: 0x1000, Size: 1, DataType: "SByte",
				},
				expectError: false,
			},
			{
				name: "valid int parameter",
				dataPoint: config.DataPoint{
					Name: "IntParam", Address: 0x1000, Size: 2, DataType: "Int",
				},
				expectError: false,
			},
			{
				name: "valid sint parameter",
				dataPoint: config.DataPoint{
					Name: "SIntParam", Address: 0x1000, Size: 2, DataType: "SInt",
				},
				expectError: false,
			},
			{
				name: "valid int4 parameter",
				dataPoint: config.DataPoint{
					Name: "Int4Param", Address: 0x1000, Size: 4, DataType: "Int4",
				},
				expectError: false,
			},
			{
				name: "valid array parameter",
				dataPoint: config.DataPoint{
					Name: "ArrayParam", Address: 0x1000, Size: 8, DataType: "Array",
				},
				expectError: false,
			},
			{
				name: "valid string parameter",
				dataPoint: config.DataPoint{
					Name: "StringParam", Address: 0x1000, Size: 10, DataType: "String",
				},
				expectError: false,
			},
			{
				name: "valid float parameter",
				dataPoint: config.DataPoint{
					Name: "FloatParam", Address: 0x1000, Size: 4, DataType: "Float",
				},
				expectError: false,
			},
			{
				name: "size mismatch warning",
				dataPoint: config.DataPoint{
					Name: "MismatchParam", Address: 0x1000, Size: 3, DataType: "Int", // Int should be 2 bytes
				},
				expectError: false, // Should warn but not error
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := validator.ValidateDataPoint(tt.dataPoint, 0)

				if tt.expectError && err == nil {
					t.Errorf("Expected error, got none")
				}

				if !tt.expectError && err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			})
		}
	})

	t.Run("ValidateAutoRefreshConfig", func(t *testing.T) {
		tests := []struct {
			name        string
			config      *config.AutoRefreshConfig
			expectError bool
		}{
			{
				name:        "nil config",
				config:      nil,
				expectError: true,
			},
			{
				name: "disabled config",
				config: &config.AutoRefreshConfig{
					Enabled: false,
				},
				expectError: false,
			},
			{
				name: "valid enabled config",
				config: &config.AutoRefreshConfig{
					Enabled:   true,
					Interval:  "30s",
					Addresses: []string{"0x1000", "0x2000"},
				},
				expectError: false,
			},
			{
				name: "invalid interval",
				config: &config.AutoRefreshConfig{
					Enabled:   true,
					Interval:  "invalid",
					Addresses: []string{"0x1000"},
				},
				expectError: true,
			},
			{
				name: "no addresses",
				config: &config.AutoRefreshConfig{
					Enabled:   true,
					Interval:  "30s",
					Addresses: []string{},
				},
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := validator.ValidateAutoRefreshConfig(tt.config)

				if tt.expectError && err == nil {
					t.Errorf("Expected error, got none")
				}

				if !tt.expectError && err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			})
		}
	})
}

// TestConfigValidatorWithRealData tests validator with realistic configuration data
func TestConfigValidatorWithRealData(t *testing.T) {
	logger := NewNoOpLogger()
	validator := NewConfigValidator(logger)

	t.Run("realistic viessmann config", func(t *testing.T) {
		dataPoints := []config.DataPoint{
			// Temperature parameters
			{Name: "Außentemperatur", Address: 0x5525, Size: 2, DataType: "SInt", Unit: "°C"},
			{Name: "Kesseltemperatur", Address: 0x0810, Size: 2, DataType: "Int", Unit: "°C"},
			{Name: "Vorlauftemperatur A1M1", Address: 0x3900, Size: 2, DataType: "Int", Unit: "°C"},

			// Status parameters
			{Name: "Betriebsart", Address: 0x2323, Size: 1, DataType: "Byte"},
			{Name: "Brennerstatus", Address: 0x55D3, Size: 1, DataType: "Byte"},

			// Error parameters
			{Name: "Fehlerstatus", Address: 0x7507, Size: 9, DataType: "Array"},
			{Name: "Brennerstunden1", Address: 0x08A7, Size: 4, DataType: "Int4", Unit: "h"},

			// Floating point values
			{Name: "Volumenstrom", Address: 0x7663, Size: 4, DataType: "Float", Unit: "l/h"},

			// String parameters
			{Name: "Anlagentyp", Address: 0x00F8, Size: 2, DataType: "String"},
		}

		err := validator.ValidateDataPoints(dataPoints)
		if err != nil {
			t.Errorf("Unexpected error validating realistic config: %v", err)
		}
	})

	t.Run("duplicate addresses are warned but allowed", func(t *testing.T) {
		dataPoints := []config.DataPoint{
			{Name: "Kesseltemperatur", Address: 0x0810, Size: 2, DataType: "Int"},
			{Name: "Vorlauftemperatur A1M1", Address: 0x0810, Size: 2, DataType: "Int"}, // Same address, different interpretation
		}

		err := validator.ValidateDataPoints(dataPoints)
		if err != nil {
			t.Errorf("Should warn but not error on duplicate addresses: %v", err)
		}
	})

	t.Run("edge case addresses", func(t *testing.T) {
		dataPoints := []config.DataPoint{
			{Name: "Max Address", Address: 0xFFFF, Size: 2, DataType: "Int"},
			{Name: "Min Address", Address: 0x0001, Size: 1, DataType: "Byte"},
		}

		err := validator.ValidateDataPoints(dataPoints)
		if err != nil {
			t.Errorf("Unexpected error with edge case addresses: %v", err)
		}
	})
}

// TestConfigValidatorPerformance tests validation performance
func TestConfigValidatorPerformance(t *testing.T) {
	logger := NewNoOpLogger()
	validator := NewConfigValidator(logger)

	// Create large dataset
	dataPoints := make([]config.DataPoint, 1000)
	for i := 0; i < 1000; i++ {
		dataPoints[i] = config.DataPoint{
			Name:     fmt.Sprintf("Param%d", i),
			Address:  uint16(0x1000 + i),
			Size:     2,
			DataType: "Int",
		}
	}

	// Performance test
	err := validator.ValidateDataPoints(dataPoints)
	if err != nil {
		t.Errorf("Validation failed for large dataset: %v", err)
	}
}
