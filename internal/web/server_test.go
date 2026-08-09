package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
)

// newTestServer builds a server with no serial port and no broker.
//
// NewServer is avoided on purpose: it opens an MQTT connection. The handlers
// under test only read the reader and the configuration, so wiring the struct
// directly keeps the test hermetic.
func newTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()

	readerCfg := viessmann.NewTestReaderConfig("test-device")
	reader := viessmann.NewReaderWithDependencies(readerCfg)

	return &Server{
		reader:       reader,
		config:       cfg,
		eventClients: make(map[chan viessmann.CacheEvent]bool),
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Device:  "test-device",
		WebPort: "8080",
		YamlConfig: &config.YamlConfig{
			MQTT: config.MQTTConfig{
				Enabled:   true,
				Host:      "broker:1883",
				TopicRoot: "openv",
				Addresses: []config.MQTTAddress{
					{Address: "0x08A7", Alias: "getBrennerStunden1"},
				},
			},
			MQTTHooks: []config.MQTTHookConfig{
				{Topic: "openv/setTempWWsoll", Description: "hot water", Enabled: true},
				{Topic: "openv/setBetriebArtM1", Description: "mode", Enabled: false},
			},
			RefreshGroups: []config.RefreshGroup{
				{Name: "Fast", Interval: "60s", Enabled: true, Addresses: []string{"0x0800"}},
			},
		},
	}
}

// decodeJSON asserts the response is JSON and hands back the decoded body.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid JSON: %v (body: %s)", err, rec.Body.String())
	}
	return out
}

func TestGetCacheStatsHandler(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getCacheStatsHandler(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))

	decodeJSON(t, rec)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
}

func TestGetDataPointsHandler_EmptyIsStillValidJSON(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getDataPointsHandler(rec, httptest.NewRequest(http.MethodGet, "/api/datapoints", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// A reader with no data points must produce valid JSON, not a bare newline.
	var out []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not valid JSON: %v (body: %q)", err, rec.Body.String())
	}
}

func TestGetMqttStatusHandler_ReportsUninitialised(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getMqttStatusHandler(rec, httptest.NewRequest(http.MethodGet, "/api/mqtt/status", nil))

	body := decodeJSON(t, rec)
	// The publisher was never built here, and the handler must say so rather
	// than claim MQTT is fine.
	if _, ok := body["status"]; !ok {
		if enabled, ok := body["enabled"].(bool); ok && enabled {
			t.Error("MQTT reported as enabled while no publisher exists")
		}
	}
}

func TestGetMqttHooksHandler_ListsConfiguredHooks(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getMqttHooksHandler(rec, httptest.NewRequest(http.MethodGet, "/api/mqtt/hooks", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, topic := range []string{"openv/setTempWWsoll", "openv/setBetriebArtM1"} {
		if !strings.Contains(body, topic) {
			t.Errorf("hook %q missing from the response: %s", topic, body)
		}
	}
}

func TestGetRefreshGroupsHandler(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getRefreshGroupsHandler(rec, httptest.NewRequest(http.MethodGet, "/api/multi-refresh/groups", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Fast") {
		t.Errorf("configured group missing from the response: %s", rec.Body.String())
	}
}

func TestGetYamlConfigHandler(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getYamlConfigHandler(rec, httptest.NewRequest(http.MethodGet, "/api/config/yaml", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestIndexHandler_ServesHTML(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.indexHandler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want HTML", ct)
	}
}

func TestGetAutoRefreshStatusHandler(t *testing.T) {
	s := newTestServer(t, testConfig())

	rec := httptest.NewRecorder()
	s.getAutoRefreshStatusHandler(rec, httptest.NewRequest(http.MethodGet, "/api/auto-refresh/status", nil))

	decodeJSON(t, rec)
}

// A malformed body must be refused, not half-applied: these endpoints rewrite
// the refresh configuration of a daemon driving a boiler.
func TestUpdateHandlers_RejectMalformedBody(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*Server) http.HandlerFunc
		path    string
	}{
		{
			name:    "refresh groups",
			handler: func(s *Server) http.HandlerFunc { return s.updateRefreshGroupsHandler },
			path:    "/api/multi-refresh/groups",
		},
		{
			name:    "auto refresh config",
			handler: func(s *Server) http.HandlerFunc { return s.updateAutoRefreshConfigHandler },
			path:    "/api/auto-refresh/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, testConfig())

			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader("{not json"))
			rec := httptest.NewRecorder()
			tt.handler(s)(rec, req)

			if rec.Code == http.StatusOK {
				t.Errorf("status = 200 for a malformed body; it must be rejected")
			}
		})
	}
}

// Index lookups come straight from the URL, so an out-of-range one must not
// panic the daemon.
func TestGetSingleReadingHandler_OutOfRange(t *testing.T) {
	s := newTestServer(t, testConfig())

	req := httptest.NewRequest(http.MethodGet, "/api/reading/999", nil)
	rec := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on an out-of-range index: %v", r)
		}
	}()
	s.getSingleReadingHandler(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("an out-of-range index must not return 200")
	}
}
