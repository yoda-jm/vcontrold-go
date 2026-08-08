package mqtt

import (
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// The hot water thermostat: a real cylinder probe, so a complete entity.
func TestClimatePayload_HotWater(t *testing.T) {
	p := newTestPublisher(nil)

	payload := climatePayload{
		entityBase:              newEntityBase(p, "vitola_ecs", "Eau chaude sanitaire", "mdi:water-thermometer"),
		TemperatureCommandTopic: p.commandTopic("getTempWWsoll"),
		TemperatureStateTopic:   p.stateTopic("getTempWWsoll"),
		CurrentTemperatureTopic: p.stateTopic("getTempWWist"),
	}

	if payload.TemperatureCommandTopic != "openv/setTempWWsoll" {
		t.Errorf("command topic = %q, want openv/setTempWWsoll — the topic the hook listens on",
			payload.TemperatureCommandTopic)
	}
	if payload.TemperatureStateTopic != "openv/getTempWWsoll" {
		t.Errorf("state topic = %q", payload.TemperatureStateTopic)
	}
	if payload.CurrentTemperatureTopic != "openv/getTempWWist" {
		t.Errorf("current temperature topic = %q", payload.CurrentTemperatureTopic)
	}
	if payload.ObjectID != "vitola_eau_chaude_sanitaire" {
		t.Errorf("object_id = %q", payload.ObjectID)
	}
}

// The step must stay whole-degree: the setpoints are single bytes in whole
// degrees, so a half-degree step would promise precision the appliance lacks.
func TestDefaultTempStepIsWholeDegree(t *testing.T) {
	if defaultTempStep != 1 {
		t.Errorf("defaultTempStep = %v, want 1", defaultTempStep)
	}
}

// A climate without a declared mode address must not advertise a mode command
// topic: the mode is global to the appliance, so a per-zone command would move
// the other zone too.
func TestClimateDiscovery_ModeIsReadOnlyWithoutAddress(t *testing.T) {
	p := newTestPublisher(nil)

	readOnly := config.MQTTClimateConfig{
		Name: "Heating", ObjectID: "vitola_chauffage",
		TargetAlias: "getTempRaumNorSollM1",
		Modes:       []string{"off", "heat"},
		ModeAlias:   "getBetriebArtM1",
		// no ModeAddress
	}

	payload := climatePayload{
		entityBase:     newEntityBase(p, readOnly.ObjectID, readOnly.Name, ""),
		ModeStateTopic: p.stateTopic(readOnly.ModeAlias),
	}
	if readOnly.ModeAddress != "" {
		payload.ModeCommandTopic = p.commandTopic(readOnly.ModeAlias)
	}

	if payload.ModeCommandTopic != "" {
		t.Errorf("mode command topic = %q, want empty while the mode is read-only",
			payload.ModeCommandTopic)
	}
	if payload.ModeStateTopic != "openv/getBetriebArtM1" {
		t.Errorf("mode state topic = %q", payload.ModeStateTopic)
	}
}

// The derived value is a plain ratio; the guards are what stop it publishing
// nonsense before both terms have been read.
func TestComputeDerived(t *testing.T) {
	spec := config.MQTTDerivedConfig{
		Alias:       "cycleMoyen",
		Numerator:   "getBrennerStunden1",
		Denominator: "getBrennerStarts",
		Factor:      60,
		Precision:   2,
	}

	t.Run("both terms known", func(t *testing.T) {
		state := newDerivedState()
		// The real readings: 11066.2 h over 206315 starts.
		state.record("getBrennerStunden1", "11066.2")
		state.record("getBrennerStarts", "206315")

		got, ok := computeDerived(spec, state)
		if !ok {
			t.Fatal("expected a value once both terms are known")
		}
		if got != "3.22" {
			t.Errorf("cycle length = %q, want 3.22 minutes", got)
		}
	})

	t.Run("a term still missing", func(t *testing.T) {
		state := newDerivedState()
		state.record("getBrennerStunden1", "11066.2")
		if _, ok := computeDerived(spec, state); ok {
			t.Error("expected no value while a term is unknown")
		}
	})

	t.Run("zero denominator", func(t *testing.T) {
		state := newDerivedState()
		state.record("getBrennerStunden1", "11066.2")
		state.record("getBrennerStarts", "0")
		if _, ok := computeDerived(spec, state); ok {
			t.Error("expected no value for an undefined ratio")
		}
	})

	t.Run("non-numeric readings are ignored", func(t *testing.T) {
		state := newDerivedState()
		// The operating mode publishes "WW", which must never become a term.
		state.record("getBetriebArtM1", "WW")
		if _, ok := state.get("getBetriebArtM1"); ok {
			t.Error("a non-numeric reading must not be recorded")
		}
	})
}

// A derived value changes far less often than the readings behind it.
// Republishing it on every read would flood the log and the broker.
func TestDerivedState_PublishesOnlyOnChange(t *testing.T) {
	state := newDerivedState()

	if !state.changed("cycleMoyen", "3.22") {
		t.Error("the first value must be published")
	}
	if state.changed("cycleMoyen", "3.22") {
		t.Error("an unchanged value must not be republished")
	}
	if !state.changed("cycleMoyen", "3.23") {
		t.Error("a changed value must be published")
	}
}
