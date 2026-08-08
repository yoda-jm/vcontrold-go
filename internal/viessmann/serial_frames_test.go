package viessmann

import (
	"bytes"
	"testing"
	"time"

	"go.bug.st/serial"
)

// fakePort est un port serie en memoire : il enregistre ce qui est ecrit et
// rejoue une reponse programmee. Il permet de verifier les trames octet par
// octet contre les macros du XML communautaire, ce qu'aucun test de bout en
// bout ne fait.
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
		return 0, nil // Timeout : ni octet ni erreur, comme le vrai pilote.
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

// La trame de lecture doit reproduire SYNC;GETADDR $addr $hexlen :
//
//	SEND 04 ; WAIT 05 ; SEND 01 F7 <adr_hi> <adr_lo> <len>
func TestReadFromDevice_FrameMatchesGETADDR(t *testing.T) {
	port := &fakePort{
		// 05 acquitte le SYNC, puis les 4 octets du compteur.
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
		0x08, 0xA7, // adresse
		0x04, // longueur
	}
	if got := port.written.Bytes(); !bytes.Equal(got, wantFrame) {
		t.Errorf("read frame = % X, want % X", got, wantFrame)
	}

	wantData := []byte{0x92, 0xE2, 0x5F, 0x02}
	if !bytes.Equal(data, wantData) {
		t.Errorf("data = % X, want % X", data, wantData)
	}
}

// La trame d'ecriture doit reproduire SYNC;SETADDR $addr $hexlen;SEND BYTES :
//
//	SEND 04 ; WAIT 05 ; SEND 01 F4 <adr_hi> <adr_lo> <len> <donnees>
//
// Trois regressions sont verrouillees ici : l'opcode 0xF4 (et non 0x01),
// l'absence totale de checksum (c'est du P300, pas du KW), et le statut
// attendu 0x00 (RECV 1 SR) plutot que l'acquittement 0x05.
func TestWriteToDevice_FrameMatchesSETADDR(t *testing.T) {
	port := &fakePort{
		toRead: []byte{0x05, 0x00}, // ACK du SYNC, puis statut OK
	}
	sc := &SerialCommunicator{port: port, device: "fake"}

	if err := sc.WriteToDevice(0x6300, []byte{0x30}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFrame := []byte{
		0x04,       // SYNC
		0x01, 0xF4, // SETADDR
		0x63, 0x00, // adresse
		0x01, // longueur : setTempWWsoll fait 1 octet, unite ST
		0x30, // donnee : 48 degres, sans facteur
	}
	got := port.written.Bytes()
	if !bytes.Equal(got, wantFrame) {
		t.Fatalf("write frame = % X, want % X", got, wantFrame)
	}

	if len(got) != len(wantFrame) {
		t.Errorf("frame length = %d, want %d (KW carries no checksum)", len(got), len(wantFrame))
	}
}

// Un statut non nul doit remonter une erreur : l'unite SR dit 00 = OK,
// 05 = SYNC (NOT OK). L'ancien code acceptait justement 05.
func TestWriteToDevice_RejectsNonZeroStatus(t *testing.T) {
	port := &fakePort{
		toRead: []byte{0x05, 0x05}, // ACK du SYNC, puis SYNC (NOT OK)
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

// Une chaudiere muette ne doit pas figer le daemon : port.Read rend 0 sans
// erreur, et readNBytes tenait le mutex indefiniment.
func TestReadNBytes_TimesOutInsteadOfSpinning(t *testing.T) {
	port := &fakePort{toRead: nil} // ne repond jamais
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
