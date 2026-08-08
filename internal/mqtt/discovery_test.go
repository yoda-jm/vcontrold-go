package mqtt

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// newTestPublisher builds a publisher that can render payloads without a
// broker. Discovery is pure data: everything asserted here is decided before a
// single byte reaches the network.
func newTestPublisher(addresses []config.MQTTAddress) *Publisher {
	return &Publisher{
		config: &config.MQTTConfig{
			Enabled:   true,
			TopicRoot: "openv",
			Addresses: addresses,
		},
		enabled: true,
	}
}

// loadVitoXML makes the community address mappings available, since the unit
// they carry drives device_class and state_class.
func loadVitoXML(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err != nil {
		t.Fatalf("failed to resolve vito.xml path: %v", err)
	}
	if err := xml.LoadAddressMappings(path); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}
}

// The whole point of discovery is state_class: without it Home Assistant drops
// the entity from long-term statistics and the history disappears with the
// recorder purge. These expectations are taken from the units in vito.xml.
func TestBuildSensorPayload_StateClassFromUnit(t *testing.T) {
	loadVitoXML(t)

	tests := []struct {
		name            string
		addr            config.MQTTAddress
		wantComponent   string
		wantObjectID    string
		wantEntityID    string
		wantStateTopic  string
		wantUnit        string
		wantDeviceClass string
		wantStateClass  string
	}{
		{
			name:            "burner hours are a climbing counter",
			addr:            config.MQTTAddress{Address: "0x08A7", Alias: "getBrennerStunden1", FriendlyName: "Heures brûleur"},
			wantComponent:   "sensor",
			wantObjectID:    "getbrennerstunden1",
			wantEntityID:    "vitola_heures_bruleur",
			wantStateTopic:  "openv/getBrennerStunden1",
			wantUnit:        "h",
			wantDeviceClass: "duration",
			wantStateClass:  "total_increasing",
		},
		{
			name:           "burner starts are a climbing counter without a unit",
			addr:           config.MQTTAddress{Address: "0x088A", Alias: "getBrennerStarts", FriendlyName: "Démarrages brûleur"},
			wantComponent:  "sensor",
			wantObjectID:   "getbrennerstarts",
			wantEntityID:   "vitola_demarrages_bruleur",
			wantStateTopic: "openv/getBrennerStarts",
			wantStateClass: "total_increasing",
		},
		{
			name:            "a temperature is an instantaneous measurement",
			addr:            config.MQTTAddress{Address: "0x0802", Alias: "getTempKist", FriendlyName: "Température chaudière"},
			wantComponent:   "sensor",
			wantObjectID:    "gettempkist",
			wantEntityID:    "vitola_temperature_chaudiere",
			wantStateTopic:  "openv/getTempKist",
			wantUnit:        "°C",
			wantDeviceClass: "temperature",
			wantStateClass:  "measurement",
		},
		{
			name:           "the operating mode is textual, so it carries no state_class",
			addr:           config.MQTTAddress{Address: "0x2301", Alias: "getBetriebArtM1", FriendlyName: "Mode de fonctionnement"},
			wantComponent:  "sensor",
			wantObjectID:   "getbetriebartm1",
			wantEntityID:   "vitola_mode_de_fonctionnement",
			wantStateTopic: "openv/getBetriebArtM1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPublisher(nil)
			component, objectID, payload, err := p.buildSensorPayload(tt.addr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if component != tt.wantComponent {
				t.Errorf("component = %q, want %q", component, tt.wantComponent)
			}
			if objectID != tt.wantObjectID {
				t.Errorf("objectID = %q, want %q", objectID, tt.wantObjectID)
			}
			if payload.ObjectID != tt.wantEntityID {
				t.Errorf("object_id = %q, want %q", payload.ObjectID, tt.wantEntityID)
			}
			if payload.StateTopic != tt.wantStateTopic {
				t.Errorf("state_topic = %q, want %q", payload.StateTopic, tt.wantStateTopic)
			}
			if payload.UnitOfMeasurement != tt.wantUnit {
				t.Errorf("unit = %q, want %q", payload.UnitOfMeasurement, tt.wantUnit)
			}
			if payload.DeviceClass != tt.wantDeviceClass {
				t.Errorf("device_class = %q, want %q", payload.DeviceClass, tt.wantDeviceClass)
			}
			if payload.StateClass != tt.wantStateClass {
				t.Errorf("state_class = %q, want %q", payload.StateClass, tt.wantStateClass)
			}
		})
	}
}

// A binary sensor must not carry state_class or a unit: Home Assistant refuses
// the configuration outright.
func TestBuildSensorPayload_BinarySensorDropsStateClass(t *testing.T) {
	loadVitoXML(t)

	p := newTestPublisher(nil)
	component, _, payload, err := p.buildSensorPayload(config.MQTTAddress{
		Address:      "0x551E",
		Alias:        "getBrennerStatus",
		FriendlyName: "Brûleur",
		Component:    "binary_sensor",
		DeviceClass:  "running",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if component != "binary_sensor" {
		t.Errorf("component = %q, want binary_sensor", component)
	}
	if payload.StateClass != "" {
		t.Errorf("state_class = %q, want empty on a binary sensor", payload.StateClass)
	}
	if payload.UnitOfMeasurement != "" {
		t.Errorf("unit = %q, want empty on a binary sensor", payload.UnitOfMeasurement)
	}
	if payload.PayloadOn != "1" || payload.PayloadOff != "0" {
		t.Errorf("payload_on/off = %q/%q, want 1/0", payload.PayloadOn, payload.PayloadOff)
	}
	// device_class heat renders as "Hot/Normal" — meaningless for a burner.
	if payload.DeviceClass != "running" {
		t.Errorf("device_class = %q, want running", payload.DeviceClass)
	}
}

// An explicit device_class must drop the icon guessed from the unit: unit RT
// covers the burner and the pumps alike, and gave a flame to both.
func TestResolveMetadata_ExplicitDeviceClassClearsGuessedIcon(t *testing.T) {
	loadVitoXML(t)

	guessed := resolveMetadata(config.MQTTAddress{Address: "0x08A7"}, 0x08A7, false)
	if guessed.Icon == "" {
		t.Fatal("expected an icon guessed from unit CS, so the next assertion means something")
	}

	overridden := resolveMetadata(config.MQTTAddress{Address: "0x08A7", DeviceClass: "duration"}, 0x08A7, false)
	if overridden.Icon != "" {
		t.Errorf("icon = %q, want empty once a device_class is declared", overridden.Icon)
	}
}

// Configuration wins over the XML, which is what lets a wrong upstream unit be
// corrected without touching vito.xml.
func TestResolveMetadata_ConfigurationOverridesXML(t *testing.T) {
	loadVitoXML(t)

	meta := resolveMetadata(config.MQTTAddress{
		Address:    "0x0802",
		Unit:       "K",
		StateClass: "total",
	}, 0x0802, false)

	if meta.Unit != "K" {
		t.Errorf("unit = %q, want K", meta.Unit)
	}
	if meta.StateClass != "total" {
		t.Errorf("state_class = %q, want total", meta.StateClass)
	}
}

// Every entity must belong to the same device, otherwise Home Assistant shows
// two dozen loose entities instead of one appliance.
func TestBuildSensorPayload_GroupsUnderOneDevice(t *testing.T) {
	loadVitoXML(t)

	p := newTestPublisher(nil)
	_, _, payload, err := p.buildSensorPayload(config.MQTTAddress{Address: "0x0800", Alias: "getTempA"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(payload.Device.Identifiers) != 1 || payload.Device.Identifiers[0] != deviceIdentifier {
		t.Errorf("device identifiers = %v, want [%s]", payload.Device.Identifiers, deviceIdentifier)
	}
	if len(payload.Availability) != 1 || payload.Availability[0]["topic"] != "openv/status" {
		t.Errorf("availability = %v, want the openv/status topic", payload.Availability)
	}
}

func TestBuildSensorPayload_RejectsUnusableAddresses(t *testing.T) {
	p := newTestPublisher(nil)

	if _, _, _, err := p.buildSensorPayload(config.MQTTAddress{Address: "not-hex"}); err == nil {
		t.Error("expected an error for a non-hex address")
	}
	if _, _, _, err := p.buildSensorPayload(config.MQTTAddress{Address: "0x0800", Alias: "!!!"}); err == nil {
		t.Error("expected an error for an alias with no usable characters")
	}
}

// The payload must serialise to the keys Home Assistant actually reads. A
// renamed JSON tag would break discovery silently.
func TestSensorPayload_JSONKeys(t *testing.T) {
	loadVitoXML(t)

	p := newTestPublisher(nil)
	_, _, payload, err := p.buildSensorPayload(config.MQTTAddress{
		Address: "0x08A7", Alias: "getBrennerStunden1", FriendlyName: "Heures brûleur",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	for _, key := range []string{
		"name", "unique_id", "object_id", "state_topic",
		"unit_of_measurement", "device_class", "state_class",
		"device", "availability",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("discovery payload is missing key %q", key)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Heures brûleur":         "heures_bruleur",
		"Démarrages brûleur":     "demarrages_bruleur",
		"Température extérieure": "temperature_exterieure",
		"Circulateur M1":         "circulateur_m1",
		"Mode de fonctionnement": "mode_de_fonctionnement",
		"  multiple   spaces  ":  "multiple_spaces",
		"Écart chaudière / ECS":  "ecart_chaudiere_ecs",
	}
	for in, want := range tests {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// Command topics are how a thermostat reaches the hooks: getTempWWsoll must
// become openv/setTempWWsoll, the topic the hook subscribes to.
func TestCommandTopic(t *testing.T) {
	p := newTestPublisher(nil)

	if got := p.commandTopic("getTempWWsoll"); got != "openv/setTempWWsoll" {
		t.Errorf("commandTopic = %q, want openv/setTempWWsoll", got)
	}
	if got := p.stateTopic("getTempWWsoll"); got != "openv/getTempWWsoll" {
		t.Errorf("stateTopic = %q, want openv/getTempWWsoll", got)
	}
}
