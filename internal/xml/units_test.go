package xml

import (
	"path/filepath"
	"testing"
)

func loadUnits(t *testing.T) *UnitConverter {
	t.Helper()
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vcontrold.xml")
	if err != nil {
		t.Fatalf("failed to resolve vcontrold.xml path: %v", err)
	}
	if err := LoadUnits(path); err != nil {
		t.Skipf("vcontrold.xml unavailable (run `make deps`): %v", err)
	}
	uc := GetGlobalUnitConverter()
	if uc == nil {
		t.Fatal("LoadUnits succeeded but no converter was published")
	}
	return uc
}

// This is where a raw reading becomes a number a human believes. Every
// expectation below is the <calc> expression from the community XML, and the
// conversions are the ones this project actually depends on.
func TestUnitConverter_ConvertValue(t *testing.T) {
	uc := loadUnits(t)

	tests := []struct {
		name   string
		unit   string
		raw    float64
		want   float64
		entity string
	}{
		{
			// UT is <calc get="V/10">. 275 is what the outside sensor reported
			// when it was 27.5 degrees out.
			name: "temperature is tenths of a degree",
			unit: "UT", raw: 275, want: 27.5, entity: "Grad Celsius",
		},
		{
			// CS is <calc get="V/3600"> with entity Stunden: the burner hours
			// counter is stored in seconds.
			name: "burner seconds become hours",
			unit: "CS", raw: 39838866, want: 11066.351666666667, entity: "Stunden",
		},
		{
			// CO is <calc get="V">: a plain count, no scaling. Scaling this
			// would corrupt the burner starts.
			name: "a counter is unscaled",
			unit: "CO", raw: 206315, want: 206315,
		},
		{
			// ST is <calc get="V" set="V">. This is the one that matters for
			// writes: the hot water setpoint is a whole number of degrees.
			name: "status value is unscaled",
			unit: "ST", raw: 50, want: 50,
		},
		{
			name: "percentage is halved",
			unit: "PR", raw: 100, want: 50, entity: "%",
		},
		{
			name: "one-byte temperature is halved",
			unit: "UT1", raw: 45, want: 22.5, entity: "Grad Celsius",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, entity, err := uc.ConvertValue(tt.raw, tt.unit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ConvertValue(%v, %q) = %v, want %v", tt.raw, tt.unit, got, tt.want)
			}
			if tt.entity != "" && entity != tt.entity {
				t.Errorf("entity = %q, want %q", entity, tt.entity)
			}
		})
	}
}

func TestUnitConverter_ConvertValue_UnknownUnit(t *testing.T) {
	uc := loadUnits(t)

	// An unknown unit must not silently pass the raw value off as converted.
	got, _, err := uc.ConvertValue(275, "NOPE")
	if err == nil {
		t.Error("expected an error for an unknown unit")
	}
	if got != 275 {
		t.Errorf("value = %v, want the input returned unchanged alongside the error", got)
	}
}

// The enum units are how 0/1 and the operating mode become words.
func TestUnitConverter_ConvertEnumValue(t *testing.T) {
	uc := loadUnits(t)

	tests := []struct {
		name string
		unit string
		raw  []byte
		want string
	}{
		{"burner off", "RT", []byte{0x00}, "0"},
		{"burner on", "RT", []byte{0x01}, "1"},
		{"hot water only", "BA", []byte{0x00}, "WW"},
		{"reduced heating", "BA", []byte{0x01}, "RED"},
		{"normal heating", "BA", []byte{0x02}, "NORM"},
		{"heating and hot water", "BA", []byte{0x04}, "H+WW"},
		{"shutdown", "BA", []byte{0x05}, "ABSCHALT"},
		// SR is the write status: 00 is OK, and 05 is NOT OK — not the 05 that
		// acknowledges a SYNC. Confusing the two is what made writes look
		// successful when they were refused.
		{"write accepted", "SR", []byte{0x00}, "OK"},
		{"write refused", "SR", []byte{0x05}, "SYNC (NOT OK)"},
		{"the appliance itself", "DT", []byte{0x20, 0x98}, "V200KW2 ID=2098 Protokoll:KW"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := uc.ConvertEnumValue(tt.raw, tt.unit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ConvertEnumValue(% X, %q) = %q, want %q", tt.raw, tt.unit, got, tt.want)
			}
		})
	}
}

func TestUnitConverter_ConvertEnumValue_Rejections(t *testing.T) {
	uc := loadUnits(t)

	if _, err := uc.ConvertEnumValue([]byte{0x00}, "NOPE"); err == nil {
		t.Error("expected an error for an unknown unit")
	}
	// UT is a scaled number, not an enum; treating it as one would be a bug.
	if _, err := uc.ConvertEnumValue([]byte{0x00}, "UT"); err == nil {
		t.Error("expected an error when a non-enum unit is decoded as an enum")
	}

	// An unmapped byte must be reported as unknown rather than guessed at.
	got, err := uc.ConvertEnumValue([]byte{0x7E}, "BA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" || got == "WW" {
		t.Errorf("unmapped enum byte returned %q; it must not be mistaken for a known mode", got)
	}
}

func TestUnitConverter_GetUnitByName(t *testing.T) {
	uc := loadUnits(t)

	// Units are reachable by both their long name and their abbreviation.
	byAbbrev, ok := uc.GetUnitByName("UT")
	if !ok {
		t.Fatal("unit UT should be found by abbreviation")
	}
	byName, ok := uc.GetUnitByName("Temperatur")
	if !ok {
		t.Fatal("unit Temperatur should be found by name")
	}
	if byAbbrev.Abbrev != byName.Abbrev {
		t.Errorf("the two lookups returned different units: %q vs %q", byAbbrev.Abbrev, byName.Abbrev)
	}

	if _, ok := uc.GetUnitByName("does-not-exist"); ok {
		t.Error("an unknown unit must not be found")
	}
}

// evaluateCalc is what interprets the <calc> expressions. It is small, and it
// decides every number this project publishes.
func TestUnitConverter_evaluateCalc(t *testing.T) {
	uc := &UnitConverter{units: map[string]Unit{}}

	tests := []struct {
		calc    string
		value   float64
		want    float64
		wantErr bool
	}{
		{calc: "V/10", value: 275, want: 27.5},
		{calc: "V/3600", value: 7200, want: 2},
		{calc: "V*2", value: 21, want: 42},
		{calc: "V+5", value: 10, want: 15},
		{calc: "V-5", value: 10, want: 5},
		{calc: "V", value: 42, want: 42},
		{calc: "", value: 42, want: 42},
		{calc: "V / 10", value: 275, want: 27.5}, // whitespace tolerated

		// Division by zero must be an error, not an infinity published as a
		// temperature.
		{calc: "V/0", value: 42, want: 42, wantErr: true},

		// An expression this parser cannot handle must say so rather than
		// silently return the raw value as if converted. Unit BH uses
		// (B1*100)+B0, which is byte arithmetic this does not implement.
		{calc: "(B1 * 100)+B0", value: 42, want: 42, wantErr: true},
	}

	for _, tt := range tests {
		got, err := uc.evaluateCalc(tt.calc, tt.value)
		if tt.wantErr {
			if err == nil {
				t.Errorf("evaluateCalc(%q, %v) = %v, want an error", tt.calc, tt.value, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("evaluateCalc(%q, %v) returned %v", tt.calc, tt.value, err)
			continue
		}
		if got != tt.want {
			t.Errorf("evaluateCalc(%q, %v) = %v, want %v", tt.calc, tt.value, got, tt.want)
		}
	}
}

func TestLoadUnits_MissingFile(t *testing.T) {
	if err := LoadUnits("/nonexistent/vcontrold.xml"); err == nil {
		t.Error("expected an error for a missing unit definition file")
	}
}
