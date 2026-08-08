// Package protocol defines constants and types for the KW2 protocol
// used to communicate with Viessmann V200KW2 heating systems
package protocol

// KW2 Protocol Commands
//
// Ces valeurs proviennent du bloc <protocol name="KW2"> de vcontrold.xml :
//
//	<macro name="SYNC">    <command>SEND 04;WAIT 05</command></macro>
//	<macro name="GETADDR"> <command>SEND 01 F7</command></macro>
//	<macro name="SETADDR"> <command>SEND 01 F4</command></macro>
const (
	// Reset command sent to initialize the protocol connection (macro SYNC)
	KW2_RESET_CMD = 0x04

	// Expected acknowledgment response after reset (macro SYNC : WAIT 05)
	KW2_ACK_RESPONSE = 0x05

	// Start byte for KW2 protocol messages
	KW2_START_BYTE = 0x01

	// Opcode de lecture, second octet de la macro GETADDR (SEND 01 F7).
	// Anciennement nomme KW2_CHECKSUM_INIT : ce n'a jamais ete un checksum,
	// et cette confusion est a l'origine du chemin d'ecriture defectueux.
	KW2_CMD_READ = 0xF7

	// Opcode d'ecriture, second octet de la macro SETADDR (SEND 01 F4).
	KW2_CMD_WRITE = 0xF4
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

	// Echeance globale d'une lecture de trame, toutes tentatives comprises.
	// Sans elle, un port qui ne repond jamais fige le daemon : readNBytes
	// boucle en tenant le mutex du communicateur serie.
	//
	// Large a dessein. La regulation n'emet son 05 de synchronisation que par
	// intermittence : une borne trop courte fait echouer les lectures alors que
	// la liaison est saine. A 4800 bauds, 8N2, meme huit octets ne prennent que
	// ~19 ms -- ce delai ne borne donc pas la transmission, mais l'attente.
	KW2_READ_DEADLINE_MS = 5000

	// Repos impose entre deux transactions completes. Enchainer immediatement
	// une nouvelle sequence SYNC alors que la precedente vient de s'achever
	// desynchronise la regulation.
	KW2_SETTLE_DELAY_MS = 100
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