package xml

import (
	"testing"
)

// TestConvertVitoUnitToXMLUnit tests vito.xml unit conversion
func TestConvertVitoUnitToXMLUnit(t *testing.T) {
	tests := []struct {
		vitoUnit string
		expected string
	}{
		// Temperature units
		{"UT", "Temperatur"},
		{"UT1", "Temperatur 1Byte unsigned"},
		{"UT1U", "Temperatur 1Byte unsigned"},
		{"UTI", "Temperatur 1Byte ganzzahlig"},

		// Other common units
		{"UN", "Neigung"},
		{"ST", "Status"},
		{"BA", "BetriebsArt"},
		{"RT", "ReturnStatus"},
		{"ES", "ErrorState"},
		{"BH", "Brenner Stunden"},
		{"CT", "CycleTime"},
		{"TI", "SystemTime"},
		{"P", "Prozent"},
		{"BST", "ReturnStatus"},
		{"ESG", "ReturnStatus"},
		{"CS", "CounterS"},
		{"CO", "Counter"},
		{"DT", "DeviceType"},
		{"HKT", "DeviceType"},
		{"PR1", "Prozent 1 Byte ganzzahlig"},

		// Unknown unit should return empty
		{"UNKNOWN", ""},
		{"", ""},
		{"XYZ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.vitoUnit, func(t *testing.T) {
			got := ConvertVitoUnitToXMLUnit(tt.vitoUnit)
			if got != tt.expected {
				t.Errorf("ConvertVitoUnitToXMLUnit(%q) = %q, expected %q", tt.vitoUnit, got, tt.expected)
			}
		})
	}
}

// TestInferUnitFromName tests name-based unit inference
func TestInferUnitFromName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		// Error states (highest priority)
		{"error status", "ErrorState"},
		{"Fehler", "ErrorState"},
		{"störung", "ErrorState"},
		{"System Error", "ErrorState"},

		// Operating modes
		{"betriebsart", "BetriebsArt"},
		{"operating mode", "BetriebsArt"},
		{"aktuelle betriebsart", "BetriebsArt"},

		// Status fields
		{"pump status", "ReturnStatus"},
		{"general status", "ReturnStatus"},

		// Temperature (should not conflict with status)
		{"temperatur", "Temperatur"},
		{"temp sensor", "Temperatur"},
		{"status temperatur", "Temperatur"}, // Temperatur should win

		// Heating curve
		{"neigung", "Neigung"},
		{"heizkennlinie", "Neigung"},

		// Percentage
		{"prozent", "Prozent"},
		{"value %", "Prozent"},

		// Hours/counters
		{"betriebsstunden", "Brenner Stunden"},
		{"stunden", "Brenner Stunden"},
		{"hours", "Brenner Stunden"},
		{"starts", "Counter"},
		{"zähler", "Counter"},

		// Time
		{"datum", "SystemTime"},
		{"zeit", "SystemTime"},
		{"date", "SystemTime"},
		{"time", "SystemTime"},

		// Device type
		{"brennertyp", "DeviceType"},
		{"device", "DeviceType"},
		{"typ", "DeviceType"},

		// Unknown should return empty
		{"unknown parameter", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferUnitFromName(tt.name)
			if got != tt.expected {
				t.Errorf("InferUnitFromName(%q) = %q, expected %q", tt.name, got, tt.expected)
			}
		})
	}
}

// TestInferUnitFromAddressPattern tests address-based unit inference
func TestInferUnitFromAddressPattern(t *testing.T) {
	tests := []struct {
		name      string
		address   uint16
		paramName string
		expected  string
	}{
		// Temperature address ranges
		{"Temp in 0x08xx range", 0x0810, "some temp", "Temperatur"},
		{"Temp in 0x55xx range", 0x5500, "sensor", "Temperatur"},

		// Operating mode ranges
		{"Mode in 0x23xx", 0x2301, "betriebsart", "BetriebsArt"},
		{"Mode in 0x33xx", 0x3300, "mode", "BetriebsArt"},
		{"Mode in 0x25xx", 0x2500, "operating mode", "BetriebsArt"},
		{"Non-mode in 0x23xx", 0x2301, "some value", ""}, // Should not infer mode

		// Hot water temp
		{"Hot water temp", 0x6300, "temperatur", "Temperatur"},
		{"Hot water non-temp", 0x6300, "flow", ""}, // Should not infer temp

		// Pump status ranges - NOTE: 0x084x falls in temp range first
		{"Address in temp range", 0x0840, "pump", "Temperatur"},
		{"M1 pump status", 0x2900, "m1 pump", "ReturnStatus"},
		{"M2 pump status", 0x3900, "m2 pump", "ReturnStatus"},

		// Unknown ranges
		{"Unknown range", 0x1000, "unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferUnitFromAddressPattern(tt.address, tt.paramName)
			if got != tt.expected {
				t.Errorf("inferUnitFromAddressPattern(0x%04X, %q) = %q, expected %q",
					tt.address, tt.paramName, got, tt.expected)
			}
		})
	}
}

// TestGetDetailedUnitInfo tests the complete unit detection pipeline
func TestGetDetailedUnitInfo(t *testing.T) {
	// Note: This test doesn't have globalAddressMappings loaded,
	// so it should fall back to name and pattern inference

	tests := []struct {
		name       string
		address    uint16
		paramName  string
		expectUnit string
		expectSrc  string
	}{
		// Name inference should take priority over pattern
		{"Error from name", 0x0810, "error status", "ErrorState", "name"},

		// Pattern inference when no name match
		{"Temp from pattern", 0x0810, "unknown param", "Temperatur", "pattern"},

		// No match at all
		{"No match", 0x1000, "unknown", "", "pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := GetDetailedUnitInfo(tt.address, tt.paramName)

			if info.UnitType != tt.expectUnit {
				t.Errorf("GetDetailedUnitInfo(0x%04X, %q).UnitType = %q, expected %q",
					tt.address, tt.paramName, info.UnitType, tt.expectUnit)
			}

			if info.Source != tt.expectSrc {
				t.Errorf("GetDetailedUnitInfo(0x%04X, %q).Source = %q, expected %q",
					tt.address, tt.paramName, info.Source, tt.expectSrc)
			}

			if info.FoundInXML != false { // Should be false since no XML loaded
				t.Errorf("GetDetailedUnitInfo(0x%04X, %q).FoundInXML = %v, expected false",
					tt.address, tt.paramName, info.FoundInXML)
			}
		})
	}
}
