// Package protocol defines constants and types for the KW2 protocol
// used to communicate with Viessmann V200KW2 heating systems
package protocol

// KW2 Protocol Commands
const (
	// Reset command sent to initialize the protocol connection
	KW2_RESET_CMD = 0x04
	
	// Expected acknowledgment response after reset
	KW2_ACK_RESPONSE = 0x05
	
	// Start byte for KW2 protocol messages
	KW2_START_BYTE = 0x01
	
	// Initialization value for checksum calculations
	KW2_CHECKSUM_INIT = 0xF7
)

// Serial Communication Parameters for V200KW2
const (
	// Default baud rate for V200KW2 communication
	KW2_DEFAULT_BAUDRATE = 4800
	
	// Data bits setting
	KW2_DATA_BITS = 8
	
	// Stop bits setting  
	KW2_STOP_BITS = 2
	
	// Parity setting (Even parity required)
	KW2_PARITY_EVEN = true
)

// Protocol Timing Constants
const (
	// Default read timeout in milliseconds
	KW2_READ_TIMEOUT_MS = 500
	
	// Delay between retry attempts in milliseconds
	KW2_RETRY_DELAY_MS = 200
	
	// Delay between consecutive reads in milliseconds
	KW2_READ_DELAY_MS = 100
	
	// Maximum number of retry attempts for failed operations
	KW2_MAX_RETRIES = 3
)

// Data Processing Constants
const (
	// Maximum expected data size for a single parameter read
	KW2_MAX_DATA_SIZE = 8
	
	// Expected size for BCD date/time data
	KW2_DATETIME_SIZE = 8
	
	// Standard temperature data size in bytes
	KW2_TEMP_DATA_SIZE = 2
)

// Protocol Status Codes
const (
	// Successful operation
	KW2_STATUS_SUCCESS = 0x00
	
	// Communication error
	KW2_STATUS_COMM_ERROR = 0xFF
	
	// Invalid address error
	KW2_STATUS_INVALID_ADDR = 0xFE
	
	// Timeout error
	KW2_STATUS_TIMEOUT = 0xFD
)