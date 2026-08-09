package mqtt

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/yoda-jm/vcontrold-go/internal/xml"
)

func TestEncodeLittleEndian(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		length  int
		want    []byte
		wantErr bool
	}{
		// 48 degrees in one byte: exactly setTempWWsoll, unit ST.
		{name: "hot water setpoint 48 C", value: 48, length: 1, want: []byte{0x30}},
		{name: "two bytes", value: 480, length: 2, want: []byte{0xE0, 0x01}},
		{name: "four bytes", value: 206315, length: 4, want: []byte{0xEB, 0x25, 0x03, 0x00}},
		{name: "one byte upper bound", value: 255, length: 1, want: []byte{0xFF}},

		// The case that motivated the fix: 48 x 10 does not fit in a byte.
		// Truncating would give 224, an absurd setpoint on a boiler in
		// service. Failing is the only acceptable behaviour.
		{name: "one byte overflow", value: 480, length: 1, wantErr: true},
		{name: "negative value", value: -1, length: 1, wantErr: true},
		{name: "zero length", value: 1, length: 0, wantErr: true},
		{name: "excessive length", value: 1, length: 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeLittleEndian(tt.value, tt.length)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("encodeLittleEndian(%d, %d) = % X, want an error", tt.value, tt.length, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("encodeLittleEndian(%d, %d) = % X, want % X", tt.value, tt.length, got, tt.want)
			}
		})
	}
}

// The write length must come from vito.xml, not from a two-byte assumption:
// writing two bytes to 0x6300 would overrun the next address.
func TestWriteLengthFor(t *testing.T) {
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err != nil {
		t.Fatalf("failed to resolve vito.xml path: %v", err)
	}
	if err := xml.LoadAddressMappings(path); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}

	tests := []struct {
		address uint16
		want    int
		comment string
	}{
		{0x6300, 1, "setTempWWsoll, unit ST"},
		{0x2301, 1, "setBetriebArtM1, unit BA"},
		{0x0802, 2, "getTempKist, unit UT"},
		{0x08A7, 4, "getBrennerStunden1, unit CS"},
		{0xDEAD, 1, "unknown address: one byte, the least destructive default"},
	}

	for _, tt := range tests {
		if got := writeLengthFor(tt.address); got != tt.want {
			t.Errorf("writeLengthFor(0x%04X) = %d, want %d (%s)", tt.address, got, tt.want, tt.comment)
		}
	}
}
