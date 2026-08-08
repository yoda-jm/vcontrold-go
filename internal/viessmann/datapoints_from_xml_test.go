package viessmann

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// vitoXMLPath returns the community vito.xml fetched by `make deps`. Tests
// depending on it are skipped when the bootstrap has not been run.
func vitoXMLPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err != nil {
		t.Fatalf("failed to resolve vito.xml path: %v", err)
	}
	if err := xml.LoadAddressMappings(path); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}
	return path
}

func TestDataTypeForSize(t *testing.T) {
	tests := []struct {
		size int
		want string
	}{
		{1, "Byte"},
		{2, "Int"},
		{3, "Int"},
		{4, "Int4"},
		{8, "Int4"},
		{0, "Byte"},
	}

	for _, tt := range tests {
		if got := DataTypeForSize(tt.size); got != tt.want {
			t.Errorf("DataTypeForSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

// Lengths come from vito.xml, which is the whole point of no longer guessing:
// getBrennerStunden1 is 4 bytes, and a guessed 2 would truncate the counter.
func TestBuildDataPointsFromXML_UsesXMLLength(t *testing.T) {
	vitoXMLPath(t)

	dataPoints, err := BuildDataPointsFromXML([]AddressSpec{
		{Address: 0x08A7}, // getBrennerStunden1, len 4, unit CS
		{Address: 0x0802}, // getTempKist,        len 2, unit UT
		{Address: 0x2301}, // getBetriebArtM1,    len 1, unit BA
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[uint16]struct {
		size     int
		dataType string
	}{
		0x08A7: {4, "Int4"},
		0x0802: {2, "Int"},
		0x2301: {1, "Byte"},
	}

	if len(dataPoints) != len(want) {
		t.Fatalf("expected %d data points, got %d", len(want), len(dataPoints))
	}

	for _, dp := range dataPoints {
		expected, ok := want[dp.Address]
		if !ok {
			t.Errorf("unexpected address 0x%04X", dp.Address)
			continue
		}
		if dp.Size != expected.size {
			t.Errorf("0x%04X: size = %d, want %d", dp.Address, dp.Size, expected.size)
		}
		if dp.DataType != expected.dataType {
			t.Errorf("0x%04X: dataType = %q, want %q", dp.Address, dp.DataType, expected.dataType)
		}
	}
}

// An explicit size beats the XML: this is the mechanism that corrects
// getBrennerStarts, declared as 2 bytes while the counter is really 4.
func TestBuildDataPointsFromXML_ExplicitSizeWins(t *testing.T) {
	vitoXMLPath(t)

	dataPoints, err := BuildDataPointsFromXML([]AddressSpec{{Address: 0x088A, Size: 4}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dataPoints) != 1 {
		t.Fatalf("expected 1 data point, got %d", len(dataPoints))
	}
	if dataPoints[0].Size != 4 {
		t.Errorf("size = %d, want 4 (explicit spec must override vito.xml)", dataPoints[0].Size)
	}
	if dataPoints[0].DataType != "Int4" {
		t.Errorf("dataType = %q, want Int4", dataPoints[0].DataType)
	}
}

// An address unknown to the XML and without an explicit size must fail loudly:
// guessing would publish a wrong value without saying so.
func TestBuildDataPointsFromXML_UnknownAddressIsAnError(t *testing.T) {
	vitoXMLPath(t)

	_, err := BuildDataPointsFromXML([]AddressSpec{{Address: 0xDEAD}})
	if err == nil {
		t.Fatal("expected an error for an address absent from vito.xml with no size")
	}
}

// The same address named in both a refresh group and the MQTT list must yield
// a single data point, keeping the forced size.
func TestBuildDataPointsFromXML_Deduplicates(t *testing.T) {
	vitoXMLPath(t)

	dataPoints, err := BuildDataPointsFromXML([]AddressSpec{
		{Address: 0x088A}, // via refresh_groups
		{Address: 0x088A, Size: 4, Name: "getBrennerStarts"}, // via mqtt.addresses
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dataPoints) != 1 {
		t.Fatalf("expected 1 deduplicated data point, got %d", len(dataPoints))
	}
	if dataPoints[0].Size != 4 {
		t.Errorf("size = %d, want 4 (the more specific spec must win)", dataPoints[0].Size)
	}
	if dataPoints[0].Name != "getBrennerStarts" {
		t.Errorf("name = %q, want getBrennerStarts", dataPoints[0].Name)
	}
}

func TestAddressSpecsFromYaml(t *testing.T) {
	yamlConfig := &config.YamlConfig{
		RefreshGroups: []config.RefreshGroup{
			{Name: "actif", Enabled: true, Addresses: []string{"0x0802"}},
			{Name: "inactif", Enabled: false, Addresses: []string{"0x0BAD"}},
		},
		MQTT: config.MQTTConfig{
			Addresses: []config.MQTTAddress{{Address: "0x088A", Alias: "getBrennerStarts"}},
		},
	}

	specs, err := AddressSpecsFromYaml(yamlConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (disabled group excluded), got %d", len(specs))
	}

	for _, spec := range specs {
		if spec.Address == 0x0BAD {
			t.Error("a disabled refresh group must not contribute addresses")
		}
	}
}

func TestAddressSpecsFromYaml_RejectsInvalidHex(t *testing.T) {
	yamlConfig := &config.YamlConfig{
		MQTT: config.MQTTConfig{
			Addresses: []config.MQTTAddress{{Address: "pas-un-hexa", Alias: "x"}},
		},
	}

	if _, err := AddressSpecsFromYaml(yamlConfig); err == nil {
		t.Fatal("expected an error for a non-hex address")
	}
}

// Moved from cmd/vcontrold-go/main_test.go: deduplicateDataPoints is not
// exported, so the benchmark could not compile from package main.
func BenchmarkDeduplicateDataPoints(b *testing.B) {
	reader := &Reader{logger: &MockLogger{}}

	dataPoints := make([]config.DataPoint, 100)
	for i := 0; i < 100; i++ {
		dataPoints[i] = config.DataPoint{
			Name:     fmt.Sprintf("Param%d", i%20),
			Address:  uint16(0x1000 + (i % 20)),
			Size:     2,
			DataType: "Int",
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		result := reader.deduplicateDataPoints(dataPoints)
		if len(result) == 0 {
			b.Fatalf("Expected deduplicated results, got empty slice")
		}
	}
}
