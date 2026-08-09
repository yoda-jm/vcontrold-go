package viessmann

import (
	"testing"
)

// stubComm answers one address with fixed bytes.
type stubComm struct {
	*MockSerialCommunicator
	response []byte
	err      error
}

func (s *stubComm) ReadFromDevice(address uint16, size int) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func newStub(response []byte) *stubComm {
	return &stubComm{MockSerialCommunicator: NewMockSerialCommunicator(), response: response}
}

// 20 98 is what the boiler in this house actually answers. The bytes are read
// big-endian here, unlike the little-endian measurements, because the DT unit
// matches them in wire order.
func TestIdentifyDevice_VerifiedHardware(t *testing.T) {
	info, err := IdentifyDevice(newStub([]byte{0x20, 0x98}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.ID != 0x2098 {
		t.Errorf("ID = 0x%04X, want 0x2098", info.ID)
	}
	if info.Model != "V200KW2" {
		t.Errorf("model = %q, want V200KW2", info.Model)
	}
	if info.Support != SupportVerified {
		t.Errorf("support = %v, want verified — this is the one device actually tested", info.Support)
	}
	if info.XMLProfile != "kw" {
		t.Errorf("XML profile = %q, want kw", info.XMLProfile)
	}
}

// Everything other than the KW2 is transcribed from the community XML. Claiming
// otherwise would mislead someone with a different boiler.
func TestIdentifyDevice_OtherModelsAreOnlyInferred(t *testing.T) {
	for _, id := range []uint16{0x2094, 0x2053, 0x20CB} {
		info := knownDevices[id]
		if info.Support == SupportVerified {
			t.Errorf("0x%04X is marked verified, but only 0x2098 has been run "+
				"against real hardware", id)
		}
		if info.Notes == "" {
			t.Errorf("0x%04X carries no note saying what is uncertain", id)
		}
	}
}

func TestIdentifyDevice_UnknownIdentifier(t *testing.T) {
	info, err := IdentifyDevice(newStub([]byte{0x99, 0x99}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Support != SupportUnknown {
		t.Errorf("support = %v, want unknown", info.Support)
	}
	if info.ID != 0x9999 {
		t.Errorf("ID = 0x%04X, want 0x9999", info.ID)
	}
	// An unknown device must still be reported rather than silently accepted.
	if info.Notes == "" {
		t.Error("an unknown identifier must come with a caveat")
	}
}

// A controller answering FF FF is telling us it has no device type, which is
// not an identifier and must not be treated as one.
func TestIdentifyDevice_RejectsUnavailable(t *testing.T) {
	if _, err := IdentifyDevice(newStub([]byte{0xFF, 0xFF})); err == nil {
		t.Error("expected an error when the device type reads as unavailable")
	}
}

func TestIdentifyDevice_RejectsShortResponse(t *testing.T) {
	if _, err := IdentifyDevice(newStub([]byte{0x20})); err == nil {
		t.Error("expected an error for a one-byte device type")
	}
}

func TestSupportLevel_String(t *testing.T) {
	cases := map[SupportLevel]string{
		SupportVerified: "verified",
		SupportInferred: "inferred",
		SupportUnknown:  "unknown",
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("SupportLevel(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestDeviceInfo_Describe(t *testing.T) {
	known := knownDevices[0x2098].Describe()
	if known != "V200KW2 ID=0x2098 (support: verified)" {
		t.Errorf("Describe() = %q", known)
	}

	unknown := DeviceInfo{ID: 0x1234}.Describe()
	if unknown != "unrecognised controller ID=0x1234 (support: unknown)" {
		t.Errorf("Describe() = %q", unknown)
	}
}
