package mqtt

import "testing"

// The cleaner exists to strip the "(not in XML)" annotation. Schedules and
// fault records are text, and must survive it intact: a fault label ends in
// parentheses, and a schedule starts with digits.
func TestCleanValueForHomeAssistant_LeavesCompositeValuesAlone(t *testing.T) {
	p := &Publisher{}
	tests := map[string]string{
		"05:30-22:00":                         "05:30-22:00",
		"05:00-05:20, 11:00-11:50":            "05:00-05:20, 11:00-11:50",
		"off all day":                         "off all day",
		"2021-06-07 11:54:40 Brennerstoerung": "2021-06-07 11:54:40 Brennerstoerung",
		"(empty) Regelbetrieb (kein Fehler)":  "(empty) Regelbetrieb (kein Fehler)",
		"27.6 Grad Celsius":                   "27.6",
		"11066.3400 h":                        "11066.3400",
		"50.0 (not in XML)":                   "50.0",
	}
	for in, want := range tests {
		if got := p.cleanValueForHomeAssistant(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}
