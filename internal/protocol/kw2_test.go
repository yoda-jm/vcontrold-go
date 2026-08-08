package protocol

import (
	"testing"
)

// TestProtocolConstants ensures all protocol constants have expected values
func TestProtocolConstants(t *testing.T) {
	tests := []struct {
		name     string
		got      int
		expected int
	}{
		{"KW2_RESET_CMD", KW2_RESET_CMD, 0x04},
		{"KW2_ACK_RESPONSE", KW2_ACK_RESPONSE, 0x05},
		{"KW2_START_BYTE", KW2_START_BYTE, 0x01},
		{"KW2_CMD_READ", KW2_CMD_READ, 0xF7},
		{"KW2_CMD_WRITE", KW2_CMD_WRITE, 0xF4},
		{"KW2_DEFAULT_BAUDRATE", KW2_DEFAULT_BAUDRATE, 4800},
		{"KW2_DATA_BITS", KW2_DATA_BITS, 8},
		{"KW2_STOP_BITS", KW2_STOP_BITS, 2},
		{"KW2_READ_TIMEOUT_MS", KW2_READ_TIMEOUT_MS, 500},
		{"KW2_RETRY_DELAY_MS", KW2_RETRY_DELAY_MS, 200},
		{"KW2_READ_DELAY_MS", KW2_READ_DELAY_MS, 100},
		{"KW2_MAX_RETRIES", KW2_MAX_RETRIES, 3},
		{"KW2_MAX_DATA_SIZE", KW2_MAX_DATA_SIZE, 8},
		{"KW2_DATETIME_SIZE", KW2_DATETIME_SIZE, 8},
		{"KW2_TEMP_DATA_SIZE", KW2_TEMP_DATA_SIZE, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, expected %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

// TestStatusCodes ensures status code constants are properly defined
func TestStatusCodes(t *testing.T) {
	if KW2_STATUS_SUCCESS != 0x00 {
		t.Errorf("KW2_STATUS_SUCCESS should be 0x00, got 0x%02X", KW2_STATUS_SUCCESS)
	}

	if KW2_STATUS_COMM_ERROR != 0xFF {
		t.Errorf("KW2_STATUS_COMM_ERROR should be 0xFF, got 0x%02X", KW2_STATUS_COMM_ERROR)
	}

	if KW2_STATUS_INVALID_ADDR != 0xFE {
		t.Errorf("KW2_STATUS_INVALID_ADDR should be 0xFE, got 0x%02X", KW2_STATUS_INVALID_ADDR)
	}

	if KW2_STATUS_TIMEOUT != 0xFD {
		t.Errorf("KW2_STATUS_TIMEOUT should be 0xFD, got 0x%02X", KW2_STATUS_TIMEOUT)
	}
}
