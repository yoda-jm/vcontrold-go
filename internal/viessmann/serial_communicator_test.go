package viessmann

import (
	"testing"

	"github.com/yourusername/vcontrold-go/internal/protocol"
)

// TestSerialCommunicator_GetDevice tests device name retrieval
func TestSerialCommunicator_GetDevice(t *testing.T) {
	device := "/dev/ttyUSB0"

	// We can't actually initialize a real serial port in tests,
	// but we can test the construction logic
	sc := &SerialCommunicator{
		device: device,
	}

	if sc.GetDevice() != device {
		t.Errorf("GetDevice() = %s, expected %s", sc.GetDevice(), device)
	}
}

// TestSerialCommunicator_Constants verifies protocol constants are used correctly
func TestSerialCommunicator_Constants(t *testing.T) {
	// Test that our constants match expected KW2 protocol values
	if protocol.KW2_RESET_CMD != 0x04 {
		t.Errorf("KW2_RESET_CMD = 0x%02X, expected 0x04", protocol.KW2_RESET_CMD)
	}

	if protocol.KW2_ACK_RESPONSE != 0x05 {
		t.Errorf("KW2_ACK_RESPONSE = 0x%02X, expected 0x05", protocol.KW2_ACK_RESPONSE)
	}

	if protocol.KW2_START_BYTE != 0x01 {
		t.Errorf("KW2_START_BYTE = 0x%02X, expected 0x01", protocol.KW2_START_BYTE)
	}

	// Opcodes of the GETADDR (SEND 01 F7) and SETADDR (SEND 01 F4) macros.
	if protocol.KW2_CMD_READ != 0xF7 {
		t.Errorf("KW2_CMD_READ = 0x%02X, expected 0xF7", protocol.KW2_CMD_READ)
	}

	if protocol.KW2_CMD_WRITE != 0xF4 {
		t.Errorf("KW2_CMD_WRITE = 0x%02X, expected 0xF4", protocol.KW2_CMD_WRITE)
	}

	if protocol.KW2_DEFAULT_BAUDRATE != 4800 {
		t.Errorf("KW2_DEFAULT_BAUDRATE = %d, expected 4800", protocol.KW2_DEFAULT_BAUDRATE)
	}
}
