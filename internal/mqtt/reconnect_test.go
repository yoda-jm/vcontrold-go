package mqtt

import (
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
)

// doneToken is a token that has already completed.
type doneToken struct{}

func (doneToken) Wait() bool                     { return true }
func (doneToken) WaitTimeout(time.Duration) bool { return true }
func (doneToken) Done() <-chan struct{}          { c := make(chan struct{}); close(c); return c }
func (doneToken) Error() error                   { return nil }

// fakeClient stands in for the broker connection. Only what the publisher
// calls is implemented; the embedded interface panics on anything else, which
// is what a test wants to hear about.
type fakeClient struct {
	mqtt.Client
	mutex     sync.Mutex
	open      bool
	published map[string]string
}

func newFakeClient() *fakeClient {
	return &fakeClient{published: make(map[string]string)}
}

// IsConnected answers true even when the connection is down, as paho does
// while it reconnects. That is the trap: a publisher trusting it loses data.
func (f *fakeClient) IsConnected() bool { return true }

func (f *fakeClient) IsConnectionOpen() bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.open
}

func (f *fakeClient) setOpen(open bool) {
	f.mutex.Lock()
	f.open = open
	f.mutex.Unlock()
}

func (f *fakeClient) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if !f.open {
		// What paho does with QoS 0 while reconnecting: report success,
		// send nothing.
		return doneToken{}
	}
	switch p := payload.(type) {
	case string:
		f.published[topic] = p
	case []byte:
		f.published[topic] = string(p)
	}
	return doneToken{}
}

func (f *fakeClient) get(topic string) (string, bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	v, ok := f.published[topic]
	return v, ok
}

func newReconnectPublisher(t *testing.T, client *fakeClient) *Publisher {
	t.Helper()
	p := newTestPublisher([]config.MQTTAddress{{Address: "0x0802", Alias: "getTempKist"}})
	p.addressMap = map[uint16]string{0x0802: "getTempKist"}
	p.lastPublished = make(map[uint16]*LastPublish)
	p.latest = make(map[uint16]viessmann.CacheEvent)
	p.config.RefreshThreshold = "1h"
	p.haOnline.Store(true)
	p.client = client
	p.ledger = newLedger(t.TempDir())
	return p
}

func reading(value string) viessmann.CacheEvent {
	return viessmann.CacheEvent{
		Type:    viessmann.EventValueChanged,
		Address: 0x0802,
		Reading: &viessmann.CachedReading{FormattedValue: value + " Grad Celsius", Success: true},
	}
}

// The November 2026 outage: paho reports a publish during reconnection as a
// success and sends nothing. The reading must not count as published, or it
// would wait an hour to go out again; and it must be in the ledger.
func TestPublisher_ReadingWhileReconnectingIsNotCountedAsSent(t *testing.T) {
	client := newFakeClient()
	p := newReconnectPublisher(t, client)

	p.HandleCacheEvent(reading("55.0"))

	if _, sent := client.get("openv/getTempKist"); sent {
		t.Error("a reading was handed to a closed connection")
	}
	if len(p.lastPublished) != 0 {
		t.Error("a reading that never left was recorded as published")
	}
	if n := len(p.ledger.recent); n != 1 {
		t.Errorf("ledger holds %d readings, want 1", n)
	}
	if p.ledger.undeliveredSince.IsZero() {
		t.Error("the ledger was told the reading was delivered")
	}
}

// On reconnection the latest values go out at once, not at the next change:
// a counter does not change at night.
func TestPublisher_ReconnectionRepublishesTheLatestValues(t *testing.T) {
	client := newFakeClient()
	p := newReconnectPublisher(t, client)

	p.HandleCacheEvent(reading("55.0"))
	p.HandleCacheEvent(reading("56.5"))

	client.setOpen(true)
	p.republish()

	if got, _ := client.get("openv/getTempKist"); got != "56.5" {
		t.Errorf("after reconnection the broker holds %q, want the latest reading 56.5", got)
	}
	if _, ok := p.lastPublished[0x0802]; !ok {
		t.Error("the republished reading was not recorded as published")
	}
}

// Home Assistant down: the broker takes the message, but nobody records it.
func TestPublisher_HomeAssistantOfflineMeansUndelivered(t *testing.T) {
	client := newFakeClient()
	client.setOpen(true)
	p := newReconnectPublisher(t, client)

	p.handleHAStatus(nil, fakeMessage("offline"))
	p.HandleCacheEvent(reading("55.0"))

	if _, sent := client.get("openv/getTempKist"); !sent {
		t.Error("the reading should still be published, for when Home Assistant returns")
	}
	if p.ledger.undeliveredSince.IsZero() {
		t.Error("a reading published while Home Assistant is offline was counted as delivered")
	}
}

// Everything that keeps state must be configured even when the broker is
// unreachable at start-up. Before, the run-time tracker was only created once
// discovery had been published, so a daemon that started offline never counted
// a minute of pump run time.
func TestPublisher_ConfiguredWhileOffline(t *testing.T) {
	p := newReconnectPublisher(t, newFakeClient())

	runtimes := []config.MQTTRuntimeConfig{testRuntimeSpec()}
	if err := p.PublishRuntimeDiscovery(runtimes, ""); err != nil {
		t.Errorf("run-time discovery while offline: %v, want it deferred silently", err)
	}
	if p.runtime == nil || len(p.runtimeConfigs) != 1 {
		t.Error("the run-time tracker was not set up while offline")
	}

	derived := []config.MQTTDerivedConfig{{Alias: "ratio"}}
	if err := p.PublishDerivedDiscovery(derived); err != nil {
		t.Errorf("derived discovery while offline: %v", err)
	}
	if p.derived == nil || len(p.derivedConfigs) != 1 {
		t.Error("the derived values were not set up while offline")
	}

	schedules := []config.MQTTScheduleConfig{{Alias: "scheduleM2", Addresses: []string{"0x3000"}}}
	if err := p.PublishScheduleDiscovery(schedules); err != nil {
		t.Errorf("schedule discovery while offline: %v", err)
	}
	if _, ok := p.scheduleIndex[0x3000]; !ok {
		t.Error("the schedule addresses were not indexed while offline")
	}

	climates := []config.MQTTClimateConfig{{Name: "Eau chaude", TargetAlias: "getTempWWsoll", ObjectID: "ecs"}}
	if err := p.PublishClimateDiscovery(climates); err != nil {
		t.Errorf("climate discovery while offline: %v", err)
	}
	if len(p.climates) != 1 {
		t.Error("the thermostats were not kept for the announcement on connection")
	}
}

type fakeMessage string

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return haStatusTopic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return []byte(m) }
func (m fakeMessage) Ack()              {}
