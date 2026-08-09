package viessmann

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yoda-jm/vcontrold-go/internal/xml"
)

// loadFormatterXML makes the community units and address mappings available.
func loadFormatterXML(t *testing.T) {
	t.Helper()
	units, _ := filepath.Abs("../../third_party/vcontrold/xml/kw/vcontrold.xml")
	addresses, _ := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err := xml.LoadUnits(units); err != nil {
		t.Skipf("vcontrold.xml unavailable (run `make deps`): %v", err)
	}
	if err := xml.LoadAddressMappings(addresses); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}
}

// The burner-hours counter is stored in seconds and converted by the XML
// (unit CS is V/3600). Rounding the result to 0.1 h is a six-minute step, about
// the length of one hot-water cycle, so a single cycle could not be measured at
// all. The underlying resolution is one second and the published value must
// keep it.
func TestFormatValue_BurnerHoursKeepSecondResolution(t *testing.T) {
	loadFormatterXML(t)
	formatter := NewValueFormatter()

	// 0x025FE292 = 39 838 354 seconds, a reading taken from the appliance.
	raw := []byte{0x92, 0xE2, 0x5F, 0x02}

	got := formatter.FormatValueWithAllSources(raw, "Int4", "getBrennerStunden1", 0x08A7)

	if !strings.HasSuffix(got, " h") {
		t.Fatalf("burner hours = %q, want a value in hours", got)
	}
	// 39838354 / 3600 = 11066.2094
	if !strings.HasPrefix(got, "11066.209") {
		t.Errorf("burner hours = %q, want about 11066.2094 h at full resolution", got)
	}
	// One decimal would make a whole hot-water cycle invisible.
	decimals := len(strings.TrimSuffix(got, " h")) - strings.Index(got, ".") - 1
	if decimals < 4 {
		t.Errorf("burner hours = %q carries %d decimals; a cycle of six minutes is 0.1 h "+
			"and would be lost below four", got, decimals)
	}
}

// Stage 2 must be formatted by exactly the same rules. It used to be handled by
// a hardcoded special case keyed on address 0x08A7, so any second counter fell
// through to a different path.
func TestFormatValue_BothBurnerStagesFormatAlike(t *testing.T) {
	loadFormatterXML(t)
	formatter := NewValueFormatter()

	raw := []byte{0x10, 0x27, 0x00, 0x00} // 10000 s = 2.7778 h

	stage1 := formatter.FormatValueWithAllSources(raw, "Int4", "getBrennerStunden1", 0x08A7)
	stage2 := formatter.FormatValueWithAllSources(raw, "Int4", "getBrennerStunden2", 0x08AB)

	if stage1 != stage2 {
		t.Errorf("the same reading formats as %q for stage 1 and %q for stage 2; "+
			"both are unit CS and must share one path", stage1, stage2)
	}
	if !strings.HasPrefix(stage1, "2.777") {
		t.Errorf("= %q, want about 2.7778 h", stage1)
	}
}
