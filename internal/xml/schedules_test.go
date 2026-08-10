package xml

import (
	"path/filepath"
	"testing"
)

// The bytes below were read from the appliance. Decoding them wrongly would
// misreport a recirculation loop that runs 16.5 hours a day as one that never
// stops, or vice versa.
func TestDecodeCycleTime(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		want  string
		hours float64
	}{
		{
			// Read from 0x2200: the hot-water recirculation loop, Monday.
			name:  "recirculation loop as programmed",
			data:  []byte{0x2B, 0xB0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want:  "05:30-22:00",
			hours: 16.5,
		},
		{
			// Read from 0x2000: the heating circuit, Monday.
			name:  "heating circuit",
			data:  []byte{0x30, 0xB0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want:  "06:00-22:00",
			hours: 16,
		},
		{
			// The reference profile's example, with three periods.
			name: "three periods in one day",
			data: []byte{0x28, 0x2A, 0x58, 0x5D, 0x95, 0xA2, 0xFF, 0xFF},
			want: "05:00-05:20, 11:00-11:50, 18:50-20:20",
			// 20 + 50 + 90 minutes.
			hours: 160.0 / 60,
		},
		{
			name:  "no period at all",
			data:  []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want:  "off all day",
			hours: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeCycleTime(tt.data); got != tt.want {
				t.Errorf("DecodeCycleTime = %q, want %q", got, tt.want)
			}
			if got := CycleTimeHours(tt.data); got != tt.hours {
				t.Errorf("CycleTimeHours = %v, want %v", got, tt.hours)
			}
		})
	}
}

// A schedule has to survive a read, an edit and a write without changing
// notation, or an operator cannot correct one safely.
func TestEncodeCycleTime_RoundTrip(t *testing.T) {
	for _, original := range []string{
		"05:30-22:00",
		"06:00-22:00",
		"05:00-05:20, 11:00-11:50, 18:50-20:20",
		"off all day",
	} {
		data, err := EncodeCycleTime(original, 8)
		if err != nil {
			t.Fatalf("EncodeCycleTime(%q) failed: %v", original, err)
		}
		if got := DecodeCycleTime(data); got != original {
			t.Errorf("round trip of %q produced %q", original, got)
		}
	}
}

// The format stores minutes in ten-minute steps. Rounding silently would write
// a schedule the operator did not ask for, on an appliance in service.
func TestEncodeCycleTime_RejectsUnrepresentable(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"minutes not a multiple of ten", "05:35-22:00"},
		{"hour out of range", "25:00-26:00"},
		{"missing bound", "05:30"},
		{"not a time", "morning-evening"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := EncodeCycleTime(tt.text, 8); err == nil {
				t.Errorf("EncodeCycleTime(%q) was accepted; it must be refused", tt.text)
			}
		})
	}

	// More periods than the appliance can store must fail rather than truncate.
	if _, err := EncodeCycleTime("01:00-02:00, 03:00-04:00, 05:00-06:00, 07:00-08:00, 09:00-10:00", 8); err == nil {
		t.Error("five periods were accepted into four slots")
	}
}

// The fault record read from slot 0 of the appliance.
func TestDecodeErrorState(t *testing.T) {
	units, _ := filepath.Abs("../../third_party/vcontrold/xml/kw/vcontrold.xml")
	if err := LoadUnits(units); err != nil {
		t.Skipf("vcontrold.xml unavailable (run `make deps`): %v", err)
	}

	// D1 then BCD 2021-06-07, weekday 01, 11:54:40.
	record := []byte{0xD1, 0x20, 0x21, 0x06, 0x07, 0x01, 0x11, 0x54, 0x40}

	got := DecodeErrorState(record)
	want := "2021-06-07 11:54:40 Brennerstoerung"
	if got != want {
		t.Errorf("DecodeErrorState = %q, want %q", got, want)
	}
}

// An unused slot reads as zeroes, not as a reserved value, and must not be
// rendered as a fault dated year zero.
func TestDecodeErrorState_EmptySlot(t *testing.T) {
	empty := make([]byte, 9)
	if got := DecodeErrorState(empty); got == "" || !contains(got, "(empty)") {
		t.Errorf("DecodeErrorState of an empty slot = %q, want it marked empty", got)
	}
}

func TestDecodeErrorState_RejectsBadLength(t *testing.T) {
	if got := DecodeErrorState([]byte{0x01, 0x02}); !contains(got, "invalid") {
		t.Errorf("a short record returned %q, want it refused", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
