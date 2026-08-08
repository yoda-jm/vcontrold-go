package viessmann

import (
	"bytes"
	"testing"
	"time"

	"go.bug.st/serial"
)

// fakePort is an in-memory serial port: it records what is written and replays
// a scripted response. It lets the frames be checked byte by byte against the
// community XML macros, which no end-to-end test does.
type fakePort struct {
	written    bytes.Buffer
	toRead     []byte
	readPos    int
	timeout    time.Duration
	timeoutSet bool
}

func (f *fakePort) Write(p []byte) (int, error) { return f.written.Write(p) }

func (f *fakePort) Read(p []byte) (int, error) {
	if f.readPos >= len(f.toRead) {
		return 0, nil // Timeout: neither byte nor error, like the real driver.
	}
	n := copy(p, f.toRead[f.readPos:])
	f.readPos += n
	return n, nil
}

func (f *fakePort) SetReadTimeout(t time.Duration) error {
	f.timeout = t
	f.timeoutSet = true
	return nil
}

func (f *fakePort) SetMode(*serial.Mode) error { return nil }
func (f *fakePort) Drain() error               { return nil }
func (f *fakePort) ResetInputBuffer() error    { return nil }
func (f *fakePort) ResetOutputBuffer() error   { return nil }
func (f *fakePort) SetDTR(bool) error          { return nil }
func (f *fakePort) SetRTS(bool) error          { return nil }
func (f *fakePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (f *fakePort) Break(time.Duration) error { return nil }
func (f *fakePort) Close() error              { return nil }

// The read frame must reproduce SYNC;GETADDR $addr $hexlen:
//
//	SEND 04 ; WAIT 05 ; SEND 01 F7 <addr_hi> <addr_lo> <len>
func TestReadFromDevice_FrameMatchesGETADDR(t *testing.T) {
	port := &fakePort{
		// 05 acknowledges the SYNC, then the counter's 4 bytes.
		toRead: []byte{0x05, 0x92, 0xE2, 0x5F, 0x02},
	}
	sc := &SerialCommunicator{port: port, device: "fake"}

	data, err := sc.ReadFromDevice(0x08A7, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFrame := []byte{
		0x04,       // SYNC
		0x01, 0xF7, // GETADDR
		0x08, 0xA7, // address
		0x04, // length
	}
	if got := port.written.Bytes(); !bytes.Equal(got, wantFrame) {
		t.Errorf("read frame = % X, want % X", got, wantFrame)
	}

	wantData := []byte{0x92, 0xE2, 0x5F, 0x02}
	if !bytes.Equal(data, wantData) {
		t.Errorf("data = % X, want % X", data, wantData)
	}
}

// The write frame must reproduce SYNC;SETADDR $addr $hexlen;SEND BYTES:
//
//	SEND 04 ; WAIT 05 ; SEND 01 F4 <addr_hi> <addr_lo> <len> <data>
//
// Three regressions are locked down here: opcode 0xF4 (not 0x01), the complete
// absence of a checksum (that belongs to P300, not KW), and the expected 0x00
// status (RECV 1 SR) rather than the 0x05 acknowledgement.
func TestWriteToDevice_FrameMatchesSETADDR(t *testing.T) {
	port := &fakePort{
		toRead: []byte{0x05, 0x00}, // SYNC ack, then OK status
	}
	sc := &SerialCommunicator{port: port, device: "fake"}

	if err := sc.WriteToDevice(0x6300, []byte{0x30}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFrame := []byte{
		0x04,       // SYNC
		0x01, 0xF4, // SETADDR
		0x63, 0x00, // address
		0x01, // length: setTempWWsoll is 1 byte, unit ST
		0x30, // data: 48 degrees, unscaled
	}
	got := port.written.Bytes()
	if !bytes.Equal(got, wantFrame) {
		t.Fatalf("write frame = % X, want % X", got, wantFrame)
	}

	if len(got) != len(wantFrame) {
		t.Errorf("frame length = %d, want %d (KW carries no checksum)", len(got), len(wantFrame))
	}
}

// A non-zero status must surface as an error: unit SR says 00 = OK,
// 05 = SYNC (NOT OK). The old code accepted precisely 05.
func TestWriteToDevice_RejectsNonZeroStatus(t *testing.T) {
	port := &fakePort{
		toRead: []byte{0x05, 0x05}, // SYNC ack, then SYNC (NOT OK)
	}
	sc := &SerialCommunicator{port: port, device: "fake"}

	err := sc.WriteToDevice(0x6300, []byte{0x30})
	if err == nil {
		t.Fatal("expected an error when the device answers status 0x05 (NOT OK)")
	}
}

func TestWriteToDevice_RejectsEmptyPayload(t *testing.T) {
	port := &fakePort{toRead: []byte{0x05, 0x00}}
	sc := &SerialCommunicator{port: port, device: "fake"}

	if err := sc.WriteToDevice(0x6300, nil); err == nil {
		t.Fatal("expected an error for an empty write payload")
	}
}

// A silent boiler must not freeze the daemon: port.Read returns 0 without an
// error, and readNBytes used to hold the mutex forever.
func TestReadNBytes_TimesOutInsteadOfSpinning(t *testing.T) {
	port := &fakePort{toRead: nil} // never answers
	sc := &SerialCommunicator{port: port, device: "fake"}

	done := make(chan error, 1)
	go func() {
		_, err := sc.readNBytes(2)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error from a silent port")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readNBytes did not terminate: the infinite loop is back")
	}

	if !port.timeoutSet {
		t.Error("readNBytes must apply a read timeout before reading")
	}
}
