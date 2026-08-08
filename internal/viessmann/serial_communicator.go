// Package viessmann - Serial communication handling for KW2 protocol
package viessmann

import (
	"fmt"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/protocol"
)

// SerialCommunicator handles all KW2 protocol serial communication
type SerialCommunicator struct {
	port   serial.Port
	mutex  sync.Mutex
	device string

	// lastTransaction records when the last transaction ended, so a rest can
	// be enforced before the next one. Guarded by mutex.
	lastTransaction time.Time
}

// settle waits until the controller is ready for another SYNC sequence.
//
// The mutex makes each transaction atomic, but not spaced out: several refresh
// groups run in parallel and hand the port back and forth with no rest. The
// controller then loses sync and stops emitting its 05, which fails the next
// transaction — always the same one, whichever group arrives second.
//
// Call with the mutex held.
func (sc *SerialCommunicator) settle() {
	if sc.lastTransaction.IsZero() {
		return
	}

	elapsed := time.Since(sc.lastTransaction)
	if wait := protocol.KW2_SETTLE_DELAY_MS*time.Millisecond - elapsed; wait > 0 {
		time.Sleep(wait)
	}
}

// NewSerialCommunicator creates a new serial communicator for the given device
func NewSerialCommunicator(device string) (*SerialCommunicator, error) {
	comm := &SerialCommunicator{
		device: device,
	}

	if err := comm.initializePort(); err != nil {
		return nil, errors.Wrap(errors.ErrCommunication, "failed to initialize serial port", err)
	}

	return comm, nil
}

// initializePort opens and configures the serial port for KW2 communication
func (sc *SerialCommunicator) initializePort() error {
	mode := &serial.Mode{
		BaudRate: protocol.KW2_DEFAULT_BAUDRATE,
		Parity:   serial.EvenParity,
		DataBits: protocol.KW2_DATA_BITS,
		StopBits: protocol.KW2_STOP_BITS,
	}

	port, err := serial.Open(sc.device, mode)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, fmt.Sprintf("failed to open serial port %s", sc.device), err)
	}

	sc.port = port
	return nil
}

// ReadFromDevice executes a complete KW2 protocol read transaction
func (sc *SerialCommunicator) ReadFromDevice(address uint16, size int) ([]byte, error) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.settle()
	defer func() { sc.lastTransaction = time.Now() }()

	// Prepare serial communication
	if err := sc.clearSerialBuffers(); err != nil {
		return nil, err
	}

	// Send reset and wait for acknowledgment
	if err := sc.sendResetAndWaitForAck(); err != nil {
		return nil, err
	}

	// Send read command and get response
	return sc.sendReadCommandAndGetResponse(address, size)
}

// WriteToDevice executes a complete KW2 protocol write transaction
func (sc *SerialCommunicator) WriteToDevice(address uint16, data []byte) error {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	sc.settle()
	defer func() { sc.lastTransaction = time.Now() }()

	// Prepare serial communication
	if err := sc.clearSerialBuffers(); err != nil {
		return err
	}

	// Send reset and wait for acknowledgment
	if err := sc.sendResetAndWaitForAck(); err != nil {
		return err
	}

	// Send write command
	return sc.sendWriteCommand(address, data)
}

// clearSerialBuffers clears both input and output buffers
func (sc *SerialCommunicator) clearSerialBuffers() error {
	if clearer, ok := sc.port.(interface{ ResetInputBuffer() error }); ok {
		if err := clearer.ResetInputBuffer(); err != nil {
			return errors.Wrap(errors.ErrCommunication, "failed to clear input buffer", err)
		}
	}
	if clearer, ok := sc.port.(interface{ ResetOutputBuffer() error }); ok {
		if err := clearer.ResetOutputBuffer(); err != nil {
			return errors.Wrap(errors.ErrCommunication, "failed to clear output buffer", err)
		}
	}
	return nil
}

// sendResetAndWaitForAck sends KW2 reset command and waits for acknowledgment
func (sc *SerialCommunicator) sendResetAndWaitForAck() error {
	// Send reset command
	if err := sc.writeByte(protocol.KW2_RESET_CMD); err != nil {
		return err
	}

	// Wait for acknowledgment
	resetResponse, err := sc.readNBytes(1)
	if err != nil {
		return err
	}

	if resetResponse[0] != protocol.KW2_ACK_RESPONSE {
		return errors.New(errors.ErrProtocol, fmt.Sprintf("invalid reset response received, expected 0x%02X, got 0x%02X",
			protocol.KW2_ACK_RESPONSE, resetResponse[0]))
	}

	return nil
}

// sendReadCommandAndGetResponse sends KW2 read command and returns the response data
func (sc *SerialCommunicator) sendReadCommandAndGetResponse(address uint16, size int) ([]byte, error) {
	// GETADDR macro: SEND 01 F7, then $addr $hexlen.
	command := []byte{
		protocol.KW2_START_BYTE,
		protocol.KW2_CMD_READ,
		byte(address >> 8),
		byte(address & 0xFF),
		byte(size),
	}

	if err := sc.writeBytes(command); err != nil {
		return nil, err
	}

	// Read response
	return sc.readNBytes(uint64(size))
}

// writeByte writes a single byte to the serial port
func (sc *SerialCommunicator) writeByte(b byte) error {
	if _, err := sc.port.Write([]byte{b}); err != nil {
		return errors.Wrap(errors.ErrCommunication, fmt.Sprintf("failed to write byte 0x%02X", b), err)
	}
	return nil
}

// writeBytes writes multiple bytes to the serial port
func (sc *SerialCommunicator) writeBytes(bytes []byte) error {
	for _, b := range bytes {
		if err := sc.writeByte(b); err != nil {
			return err
		}
	}
	return nil
}

// readNBytes reads exactly n bytes from the serial port.
//
// A silent boiler yields neither a byte nor an error: port.Read simply returns
// 0. Unbounded, the loop would spin forever holding the communicator's mutex,
// and the whole daemon would freeze.
func (sc *SerialCommunicator) readNBytes(n uint64) ([]byte, error) {
	response := make([]byte, n)
	var offset uint64

	// With no timeout applied, Read blocks forever. That is an error, not
	// something to swallow quietly.
	setter, ok := sc.port.(interface{ SetReadTimeout(time.Duration) error })
	if !ok {
		return nil, errors.New(errors.ErrCommunication,
			"serial port does not support read timeouts; refusing to read without a deadline")
	}
	if err := setter.SetReadTimeout(protocol.KW2_READ_TIMEOUT_MS * time.Millisecond); err != nil {
		return nil, errors.Wrap(errors.ErrCommunication, "failed to set read timeout", err)
	}

	// A deadline, and nothing else. Counting empty reads is harsher than it
	// looks: the controller emits its sync byte only intermittently, and two
	// or three empty reads in a row are normal before it answers. The bound
	// belongs on elapsed time, not on the number of attempts.
	deadline := time.Now().Add(protocol.KW2_READ_DEADLINE_MS * time.Millisecond)

	for offset < n {
		readBytes, err := sc.port.Read(response[offset:])
		if err != nil {
			return nil, errors.Wrap(errors.ErrCommunication, "failed to read", err)
		}

		if readBytes == 0 {
			if time.Now().After(deadline) {
				return nil, errors.New(errors.ErrCommunication,
					fmt.Sprintf("read timed out after %s: got %d of %d bytes",
						protocol.KW2_READ_DEADLINE_MS*time.Millisecond, offset, n))
			}
			continue
		}

		offset += uint64(readBytes)
		if offset > n {
			return nil, errors.New(errors.ErrProtocol, fmt.Sprintf("read overrun: expected %d bytes, got %d total", n, offset))
		}
	}

	return response, nil
}

// Close closes the serial port connection
func (sc *SerialCommunicator) Close() error {
	if sc.port != nil {
		return sc.port.Close()
	}
	return nil
}

// GetDevice returns the device name
func (sc *SerialCommunicator) GetDevice() string {
	return sc.device
}

// sendWriteCommand sends a KW2 protocol write command.
//
// The frame follows the setaddr command of the community XML:
//
//	<command name="setaddr">
//	  <send>SYNC;SETADDR $addr $hexlen;SEND BYTES $unit;RECV 1 SR</send>
//	</command>
//
// that is: 01 F4 <addr_hi> <addr_lo> <len> <data...>, then one status byte.
// KW carries NO checksum — that belongs to P300. The reply is a
// SetReturnStatus (unit SR): 00 = OK, 05 = SYNC (NOT OK).
func (sc *SerialCommunicator) sendWriteCommand(address uint16, data []byte) error {
	dataLength := len(data)
	if dataLength == 0 {
		return errors.New(errors.ErrValidation, "write data cannot be empty")
	}
	if dataLength > protocol.KW2_MAX_DATA_SIZE {
		return errors.New(errors.ErrValidation,
			fmt.Sprintf("write data too large: %d bytes (max %d)", dataLength, protocol.KW2_MAX_DATA_SIZE))
	}

	command := []byte{
		protocol.KW2_START_BYTE,     // 01
		protocol.KW2_CMD_WRITE,      // F4 — SETADDR macro
		byte((address >> 8) & 0xFF), // address, high byte
		byte(address & 0xFF),        // address, low byte
		byte(dataLength),            // $hexlen
	}
	command = append(command, data...) // SEND BYTES

	if err := sc.writeBytes(command); err != nil {
		return errors.Wrap(errors.ErrCommunication, "failed to send write command", err)
	}

	// RECV 1 SR
	statusResponse, err := sc.readNBytes(1)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "failed to read write status", err)
	}

	if statusResponse[0] != protocol.KW2_STATUS_SUCCESS {
		return errors.New(errors.ErrProtocol,
			fmt.Sprintf("write rejected at 0x%04X: status 0x%02X (expected 0x%02X = OK)",
				address, statusResponse[0], protocol.KW2_STATUS_SUCCESS))
	}

	return nil
}
