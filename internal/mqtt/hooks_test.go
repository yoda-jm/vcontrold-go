package mqtt

import (
	"testing"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
)

func TestHookManager_Creation(t *testing.T) {
	cfg := &config.Config{
		MQTTBroker: "localhost",
		MQTTPort:   1883,
	}

	reader := &viessmann.Reader{}

	hm := NewHookManager(cfg, reader)
	if hm == nil {
		// t.Fatal, not t.Error: the assertions below dereference hm, so
		// carrying on after a nil would panic and hide the real failure.
		t.Fatal("Expected hook manager to be created")
	}

	if len(hm.hooks) != 0 {
		t.Errorf("Expected 0 hooks, got %d", len(hm.hooks))
	}
}

func TestHookManager_AddHook(t *testing.T) {
	cfg := &config.Config{}
	reader := &viessmann.Reader{}
	hm := NewHookManager(cfg, reader)

	hook := MQTTHook{
		Topic:       "test/topic",
		Description: "Test hook",
		Action: config.MQTTHookAction{
			Type: "set_temperature",
			Zone: "hotwater",
		},
		Enabled: true,
	}

	hm.AddHook(hook)

	if len(hm.hooks) != 1 {
		t.Errorf("Expected 1 hook, got %d", len(hm.hooks))
	}

	if hm.hooks[0].Topic != "test/topic" {
		t.Errorf("Expected topic 'test/topic', got '%s'", hm.hooks[0].Topic)
	}
}

func TestHookManager_GetStatus(t *testing.T) {
	cfg := &config.Config{}
	reader := &viessmann.Reader{}
	hm := NewHookManager(cfg, reader)

	// Add some test hooks
	hm.AddHook(MQTTHook{
		Topic:       "test/enabled",
		Description: "Enabled test hook",
		Enabled:     true,
		Action:      config.MQTTHookAction{Type: "set_temperature"},
	})

	hm.AddHook(MQTTHook{
		Topic:       "test/disabled",
		Description: "Disabled test hook",
		Enabled:     false,
		Action:      config.MQTTHookAction{Type: "set_mode"},
	})

	status := hm.GetStatus()

	if status["total_hooks"] != 2 {
		t.Errorf("Expected total_hooks=2, got %v", status["total_hooks"])
	}

	if status["enabled_hooks"] != 1 {
		t.Errorf("Expected enabled_hooks=1, got %v", status["enabled_hooks"])
	}

	if status["connected"] != false {
		t.Errorf("Expected connected=false, got %v", status["connected"])
	}
}
