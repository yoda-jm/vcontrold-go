package xml

import (
	"os"
	"path/filepath"
	"testing"
)

// writeOverrides puts an override file in a temporary directory.
func writeOverrides(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "overrides.xml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write overrides: %v", err)
	}
	return path
}

// loadBaseMappings loads the community vito.xml the overrides sit on top of.
func loadBaseMappings(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err != nil {
		t.Fatalf("failed to resolve vito.xml path: %v", err)
	}
	if err := LoadAddressMappings(path); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}
}

// The case this mechanism exists for: vito.xml declares getBrennerStarts as two
// bytes, while the counter on this appliance is four. Read short it caps at
// 65535 and wraps.
func TestLoadAddressMappingOverrides_CorrectsLength(t *testing.T) {
	loadBaseMappings(t)

	before, ok := GetAddressMapping(0x088A)
	if !ok {
		t.Fatal("0x088A should be present in vito.xml")
	}
	if before.Length != 2 {
		t.Fatalf("upstream length = %d, expected the wrong value 2 this test corrects", before.Length)
	}

	path := writeOverrides(t, `<?xml version="1.0" encoding="utf-8"?>
<vito>
  <commands>
    <command name="getBrennerStarts" protocmd="getaddr">
      <addr>088A</addr>
      <len>4</len>
      <unit>CO</unit>
    </command>
  </commands>
</vito>`)

	if err := LoadAddressMappingOverrides(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after, _ := GetAddressMapping(0x088A)
	if after.Length != 4 {
		t.Errorf("length = %d, want 4", after.Length)
	}
	if after.Unit != "CO" {
		t.Errorf("unit = %q, want CO", after.Unit)
	}
}

// Only the fields present are overridden, so an override may correct a length
// without discarding the upstream description.
func TestLoadAddressMappingOverrides_KeepsUnmentionedFields(t *testing.T) {
	loadBaseMappings(t)

	before, _ := GetAddressMapping(0x0802)
	if before.Description == "" {
		t.Skip("vito.xml carries no description for 0x0802")
	}

	path := writeOverrides(t, `<?xml version="1.0" encoding="utf-8"?>
<vito>
  <commands>
    <command name="getTempKist"><addr>0802</addr><len>4</len></command>
  </commands>
</vito>`)

	if err := LoadAddressMappingOverrides(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after, _ := GetAddressMapping(0x0802)
	if after.Length != 4 {
		t.Errorf("length = %d, want 4", after.Length)
	}
	if after.Description != before.Description {
		t.Errorf("description = %q, want it kept as %q", after.Description, before.Description)
	}
	if after.Unit != before.Unit {
		t.Errorf("unit = %q, want it kept as %q", after.Unit, before.Unit)
	}
}

// An address absent from vito.xml may be added, provided it declares a length —
// otherwise there is nothing to read it with.
func TestLoadAddressMappingOverrides_AddsUnknownAddress(t *testing.T) {
	loadBaseMappings(t)

	path := writeOverrides(t, `<?xml version="1.0" encoding="utf-8"?>
<vito>
  <commands>
    <command name="getSomethingNew"><addr>7F01</addr><len>2</len><unit>UT</unit></command>
  </commands>
</vito>`)

	if err := LoadAddressMappingOverrides(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	added, ok := GetAddressMapping(0x7F01)
	if !ok {
		t.Fatal("0x7F01 should have been added")
	}
	if added.Length != 2 || added.Unit != "UT" {
		t.Errorf("added mapping = %+v, want length 2 unit UT", added)
	}
}

// A malformed override must fail loudly. It carries read lengths, and silently
// skipping one republishes truncated counters.
func TestLoadAddressMappingOverrides_RejectsBadInput(t *testing.T) {
	loadBaseMappings(t)

	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown address with no length",
			body: `<vito><commands><command name="x"><addr>7F02</addr></command></commands></vito>`,
		},
		{
			name: "non-numeric length",
			body: `<vito><commands><command name="x"><addr>088A</addr><len>four</len></command></commands></vito>`,
		},
		{
			name: "zero length",
			body: `<vito><commands><command name="x"><addr>088A</addr><len>0</len></command></commands></vito>`,
		},
		{
			name: "address that is not hex",
			body: `<vito><commands><command name="x"><addr>ZZZZ</addr><len>2</len></command></commands></vito>`,
		},
		{
			name: "malformed XML",
			body: `<vito><commands><command`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := LoadAddressMappingOverrides(writeOverrides(t, tt.body)); err == nil {
				t.Error("expected the override to be rejected")
			}
		})
	}
}

// No override file configured is not an error: the community XML alone is a
// perfectly valid setup.
func TestLoadAddressMappingOverrides_EmptyPathIsNotAnError(t *testing.T) {
	if err := LoadAddressMappingOverrides(""); err != nil {
		t.Errorf("unexpected error for an empty path: %v", err)
	}
}

func TestLoadAddressMappingOverrides_MissingFileIsAnError(t *testing.T) {
	if err := LoadAddressMappingOverrides("/nonexistent/overrides.xml"); err == nil {
		t.Error("expected an error for a missing override file")
	}
}
