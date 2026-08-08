package viessmann

import (
	"strings"
	"testing"
)

// TestValueFormatter_BytesToNumeric tests byte to numeric conversion
func TestValueFormatter_BytesToNumeric(t *testing.T) {
	vf := NewValueFormatter()
	
	tests := []struct {
		name     string
		data     []byte
		dataType string
		expected *float64
	}{
		{"Byte type", []byte{42}, "Byte", floatPtr(42)},
		{"Empty byte", []byte{}, "Byte", nil},
		{"SInt little-endian", []byte{0x10, 0x80}, "SInt", floatPtr(-32752)}, // 0x8010 as signed = -32752
		{"Int little-endian", []byte{0x10, 0x80}, "Int", floatPtr(32784)},   // 0x8010 as unsigned = 32784
		{"Int4 little-endian", []byte{0x10, 0x20, 0x30, 0x40}, "Int4", floatPtr(1076895760)}, // 0x40302010
		{"Insufficient data SInt", []byte{0x10}, "SInt", nil},
		{"Unknown type", []byte{0x42}, "Unknown", nil},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vf.bytesToNumeric(tt.data, tt.dataType)
			
			if tt.expected == nil && result != nil {
				t.Errorf("bytesToNumeric() = %v, expected nil", result)
			} else if tt.expected != nil && result == nil {
				t.Errorf("bytesToNumeric() = nil, expected %v", *tt.expected)
			} else if tt.expected != nil && result != nil && *result != *tt.expected {
				t.Errorf("bytesToNumeric() = %v, expected %v", *result, *tt.expected)
			}
		})
	}
}

// TestValueFormatter_FormatConvertedValue tests value formatting
func TestValueFormatter_FormatConvertedValue(t *testing.T) {
	vf := NewValueFormatter()
	
	tests := []struct {
		name      string
		value     float64
		entity    string
		unitType  string
		expected  string
	}{
		{"Temperature with entity", 23.5, "°C", "Temperatur", "23.5 °C"},
		{"Brenner Stunden", 1234.5, "h", "Brenner Stunden", "1234.5 h"},
		{"Prozent", 85.2, "%", "Prozent", "85.2 %"},
		{"Regular with entity", 42.7, "bar", "Pressure", "42.7 bar"},
		{"ReturnStatus no entity", 1.0, "", "ReturnStatus", "1"},
		{"Counter no entity", 123.0, "", "Counter", "123"},
		{"Other no entity", 45.6, "", "Other", "45.6"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vf.formatConvertedValue(tt.value, tt.entity, tt.unitType)
			if result != tt.expected {
				t.Errorf("formatConvertedValue() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestValueFormatter_AddXMLStatusAnnotation tests XML status annotation
func TestValueFormatter_AddXMLStatusAnnotation(t *testing.T) {
	vf := NewValueFormatter()
	
	tests := []struct {
		name         string
		value        string
		foundInXML   bool
		expected     string
	}{
		{"Found in XML", "23.5 °C", true, "23.5 °C"},
		{"Not found in XML", "23.5 °C", false, "23.5 °C (not in XML)"},
		{"Empty value not in XML", "", false, " (not in XML)"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vf.addXMLStatusAnnotation(tt.value, tt.foundInXML)
			if result != tt.expected {
				t.Errorf("addXMLStatusAnnotation() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// TestValueFormatter_ParseValueOriginal tests fallback parsing
func TestValueFormatter_ParseValueOriginal(t *testing.T) {
	vf := NewValueFormatter()
	
	result := vf.decoder.ParseValueOriginal([]byte{1, 2, 3}, "Test")
	if !strings.Contains(result, "RAW:") {
		t.Errorf("ParseValueOriginal() = %q, expected to contain 'RAW:'", result)
	}
}

// Helper function to create float64 pointer
func floatPtr(f float64) *float64 {
	return &f
}