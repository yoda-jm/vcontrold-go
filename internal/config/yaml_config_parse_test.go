package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeYaml(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

// The shape of the real configuration, in miniature. If any of these keys stops
// binding, the daemon starts up looking healthy and publishes nothing.
func TestLoadYamlConfig_BindsEverySection(t *testing.T) {
	path := writeYaml(t, `
device: /dev/ttyUSB9
vito_overrides_xml: overrides/machine.xml
web: true
web_port: "9090"

refresh_groups:
  - name: Fast
    interval: 60s
    enabled: true
    addresses: ["0x0800", "0x0802"]

mqtt:
  enabled: true
  host: broker:1883
  topic_root: openv
  addresses:
    - address: "0x08A7"
      alias: getBrennerStunden1
      friendly_name: Burner hours
    - address: "0x551E"
      alias: getBrennerStatus
      component: binary_sensor
      device_class: running
      icon: mdi:fire

mqtt_hooks:
  - topic: openv/setTempWWsoll
    enabled: true
    action:
      type: write_address
      address: "0x6300"
      min_value: 35.0
      max_value: 60.0

mqtt_climates:
  - name: Hot water
    object_id: vitola_ecs
    target_alias: getTempWWsoll
    target_address: "0x6300"
    current_alias: getTempWWist
    modes: ["off", "heat"]
    mode_alias: getBetriebArtM1
    mode_template: "{{ 'heat' }}"
    min_temp: 10
    max_temp: 60

mqtt_derived:
  - alias: cycleMoyen
    friendly_name: Average cycle
    numerator_alias: getBrennerStunden1
    denominator_alias: getBrennerStarts
    factor: 60
    unit: min
    state_class: measurement
`)

	cfg, err := LoadYamlConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Device != "/dev/ttyUSB9" {
		t.Errorf("device = %q", cfg.Device)
	}
	if cfg.VitoOverridesXML != "overrides/machine.xml" {
		t.Errorf("vito_overrides_xml = %q — the override path must bind, or "+
			"wrong read lengths come back", cfg.VitoOverridesXML)
	}
	if !cfg.WebMode {
		t.Error("web = true must bind: MQTT publishing only runs in web mode")
	}

	if len(cfg.RefreshGroups) != 1 || len(cfg.RefreshGroups[0].Addresses) != 2 {
		t.Fatalf("refresh groups = %+v", cfg.RefreshGroups)
	}

	if len(cfg.MQTT.Addresses) != 2 {
		t.Fatalf("mqtt addresses = %d, want 2", len(cfg.MQTT.Addresses))
	}
	binary := cfg.MQTT.Addresses[1]
	if binary.Component != "binary_sensor" || binary.DeviceClass != "running" || binary.Icon != "mdi:fire" {
		t.Errorf("binary sensor fields did not bind: %+v", binary)
	}

	if len(cfg.MQTTHooks) != 1 || cfg.MQTTHooks[0].Action.Address != "0x6300" {
		t.Fatalf("hooks = %+v", cfg.MQTTHooks)
	}
	if cfg.MQTTHooks[0].Action.MinValue == nil || *cfg.MQTTHooks[0].Action.MinValue != 35 {
		t.Error("min_value must bind: it is what keeps an absurd setpoint off the boiler")
	}

	if len(cfg.Climates) != 1 {
		t.Fatalf("climates = %d, want 1", len(cfg.Climates))
	}
	climate := cfg.Climates[0]
	if climate.CurrentAlias != "getTempWWist" || climate.ModeTemplate == "" || climate.MaxTemp != 60 {
		t.Errorf("climate fields did not bind: %+v", climate)
	}
	if climate.ModeAddress != "" {
		t.Error("no mode_address was declared, so the mode must stay read-only")
	}

	if len(cfg.Derived) != 1 || cfg.Derived[0].Factor != 60 {
		t.Fatalf("derived = %+v", cfg.Derived)
	}
}

// A missing file yields defaults; that is deliberate, and different from a file
// that exists but cannot be parsed.
func TestLoadYamlConfig_MissingFileYieldsDefaults(t *testing.T) {
	cfg, err := LoadYamlConfig("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Device != "/dev/ttyUSB0" || cfg.MQTT.Enabled {
		t.Errorf("expected defaults, got %+v", cfg)
	}
}

func TestLoadYamlConfig_MalformedFileIsAnError(t *testing.T) {
	path := writeYaml(t, "device: /dev/ttyUSB0\n  bad: indent\n\tworse: tab\n")
	if _, err := LoadYamlConfig(path); err == nil {
		t.Error("expected an error for unreadable YAML")
	}
}

func TestRefreshGroup_Duration(t *testing.T) {
	group := RefreshGroup{Name: "counters", Interval: "5m"}
	d, err := group.GetRefreshDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Minutes() != 5 {
		t.Errorf("duration = %v, want 5m", d)
	}

	if _, err := (&RefreshGroup{Interval: "soon"}).GetRefreshDuration(); err == nil {
		t.Error("expected an error for an unparsable interval")
	}
}

func TestRefreshGroup_ValidateAddresses(t *testing.T) {
	valid := RefreshGroup{Addresses: []string{"0x0800", "6300"}}
	if err := valid.ValidateAddresses(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	invalid := RefreshGroup{Name: "g", Addresses: []string{"0x0800", "nope"}}
	if err := invalid.ValidateAddresses(); err == nil {
		t.Error("expected an error for a non-hex address")
	}

	empty := RefreshGroup{Name: "g", Addresses: []string{""}}
	if err := empty.ValidateAddresses(); err == nil {
		t.Error("expected an error for an empty address")
	}
}

func TestMQTTConfig_RefreshThreshold(t *testing.T) {
	cfg := MQTTConfig{RefreshThreshold: "1h"}
	d, err := cfg.GetRefreshThresholdDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Hours() != 1 {
		t.Errorf("threshold = %v, want 1h", d)
	}

	// An unset threshold must not mean "republish constantly".
	defaulted, err := (&MQTTConfig{}).GetRefreshThresholdDuration()
	if err != nil || defaulted.Hours() != 1 {
		t.Errorf("default threshold = %v (err %v), want 1h", defaulted, err)
	}
}
