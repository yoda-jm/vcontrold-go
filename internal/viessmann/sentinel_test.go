package viessmann

import "testing"

// The patterns tested here come from the V200KW2 reference profile
// (doc/examples/sim-2098.ini) and from readings taken on the machine.
func TestIsUnavailableValue(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		// Absent: documented as such in the reference profile.
		{"mixer absent (FF)", []byte{0xFF}, true},
		{"flow setpoint absent (FF FF)", []byte{0xFF, 0xFF}, true},
		{"four FF bytes", []byte{0xFF, 0xFF, 0xFF, 0xFF}, true},

		// Solar sensor not connected: read as FF 7F on the machine, i.e.
		// 0x7FFF, which decoding turned into 3276.7 °C.
		{"sensor not connected (FF 7F)", []byte{0xFF, 0x7F}, true},

		// Real readings taken from the boiler: none may be mistaken for an
		// absence.
		{"outside temperature 27.6", []byte{0x14, 0x01}, false},
		{"hot water setpoint 50", []byte{0x32}, false},
		{"burner stopped", []byte{0x00}, false},
		{"burner hours", []byte{0x92, 0xE2, 0x5F, 0x02}, false},
		{"burner starts", []byte{0xEB, 0x25, 0x03, 0x00}, false},

		// 255 in one byte IS an absence, but 0x00FF across two bytes is a
		// legitimate value (25.5 °C).
		{"25.5 degrees (FF 00)", []byte{0xFF, 0x00}, false},

		// Across four bytes, a leading 0x7FFF is a plausible counter.
		{"counter 32767", []byte{0xFF, 0x7F, 0x00, 0x00}, false},

		{"empty", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUnavailableValue(tt.data); got != tt.want {
				t.Errorf("IsUnavailableValue(% X) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
