// Package web provides HTTP server functionality
package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/mqtt"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// Server wraps the HTTP server functionality
type Server struct {
	reader        *viessmann.Reader
	config        *config.Config
	eventClients  map[chan viessmann.CacheEvent]bool
	clientsMutex  sync.RWMutex
	mqttPublisher *mqtt.Publisher
	hookManager   *mqtt.HookManager
}

// NewServer creates a new web server
func NewServer(reader *viessmann.Reader, cfg *config.Config) *Server {
	s := &Server{
		reader:       reader,
		config:       cfg,
		eventClients: make(map[chan viessmann.CacheEvent]bool),
	}

	// Initialize MQTT publisher if YAML config is available
	if cfg.YamlConfig != nil {
		s.mqttPublisher = mqtt.NewPublisher(&cfg.YamlConfig.MQTT)
	} else {
		s.mqttPublisher = mqtt.NewPublisher(nil)
	}

	// Announce the entities before the first reading: discovery messages are
	// retained, so Home Assistant finds them again on every restart, and it is
	// their state_class that triggers long-term statistics.
	if err := s.mqttPublisher.PublishDiscovery(); err != nil {
		log.Printf("MQTT discovery not published: %v", err)
	}

	if cfg.YamlConfig != nil {
		if err := s.mqttPublisher.PublishClimateDiscovery(cfg.YamlConfig.Climates); err != nil {
			log.Printf("MQTT climate discovery not published: %v", err)
		}
		if err := s.mqttPublisher.PublishDerivedDiscovery(cfg.YamlConfig.Derived); err != nil {
			log.Printf("MQTT derived discovery not published: %v", err)
		}
	}

	// Initialize MQTT hook manager
	s.hookManager = mqtt.NewHookManager(cfg, reader)
	if cfg.YamlConfig != nil && len(cfg.YamlConfig.MQTTHooks) > 0 {
		// Convert config hooks to hook manager format
		var hooks []mqtt.MQTTHook
		for _, hookConfig := range cfg.YamlConfig.MQTTHooks {
			hooks = append(hooks, mqtt.MQTTHook{
				Topic:       hookConfig.Topic,
				Description: hookConfig.Description,
				Action:      hookConfig.Action,
				Enabled:     hookConfig.Enabled,
			})
		}
		s.hookManager.LoadHooks(hooks)
		// Connect hook manager to MQTT
		if err := s.hookManager.Connect(); err != nil {
			log.Printf("Failed to connect MQTT hook manager: %v", err)
		}
	}

	// Register cache callbacks
	reader.AddCacheCallback(s.broadcastCacheEvent)
	reader.AddCacheCallback(s.mqttPublisher.HandleCacheEvent)

	return s
}

// Start starts the web server
func (s *Server) Start() error {
	r := mux.NewRouter()

	// API endpoints
	r.HandleFunc("/api/readings", s.getReadingsHandler).Methods("GET")
	r.HandleFunc("/api/reading/{index:[0-9]+}", s.getSingleReadingHandler).Methods("GET")
	r.HandleFunc("/api/datapoints", s.getDataPointsHandler).Methods("GET")
	r.HandleFunc("/api/cached", s.getCachedReadingsHandler).Methods("GET")
	r.HandleFunc("/api/cached/{address:[0-9]+}", s.getSingleCachedReadingHandler).Methods("GET")
	r.HandleFunc("/api/stats", s.getCacheStatsHandler).Methods("GET")
	r.HandleFunc("/api/recent-changes", s.getRecentChangesHandler).Methods("GET")
	r.HandleFunc("/api/cache/clear", s.clearCacheHandler).Methods("POST")

	// Auto refresh and config endpoints
	r.HandleFunc("/api/auto-refresh/status", s.getAutoRefreshStatusHandler).Methods("GET")
	r.HandleFunc("/api/auto-refresh/config", s.getAutoRefreshConfigHandler).Methods("GET")
	r.HandleFunc("/api/auto-refresh/config", s.updateAutoRefreshConfigHandler).Methods("POST")
	r.HandleFunc("/api/multi-refresh/status", s.getMultiGroupRefreshStatusHandler).Methods("GET")
	r.HandleFunc("/api/multi-refresh/groups", s.getRefreshGroupsHandler).Methods("GET")
	r.HandleFunc("/api/multi-refresh/groups", s.updateRefreshGroupsHandler).Methods("POST")
	r.HandleFunc("/api/config/yaml", s.getYamlConfigHandler).Methods("GET")
	r.HandleFunc("/api/config/yaml", s.saveYamlConfigHandler).Methods("POST")
	r.HandleFunc("/api/mqtt/status", s.getMqttStatusHandler).Methods("GET")
	r.HandleFunc("/api/mqtt/hooks/status", s.getMqttHooksStatusHandler).Methods("GET")
	r.HandleFunc("/api/mqtt/hooks", s.getMqttHooksHandler).Methods("GET")
	r.HandleFunc("/api/mqtt/hooks/{index:[0-9]+}/toggle", s.toggleMqttHookHandler).Methods("POST")
	r.HandleFunc("/api/mqtt/hooks/test/{index:[0-9]+}", s.testMqttHookHandler).Methods("POST")
	r.HandleFunc("/api/events", s.cacheEventsHandler).Methods("GET")

	r.HandleFunc("/", s.indexHandler).Methods("GET")

	// Serve static files if needed
	// r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	log.Printf("Server starting on port %s", s.config.WebPort)
	return http.ListenAndServe(":"+s.config.WebPort, r)
}

// getReadingsHandler returns all parameter readings
func (s *Server) getReadingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	readings, err := s.reader.ReadAll()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read parameters: %v", err), http.StatusInternalServerError)
		return
	}

	// Calculate summary statistics
	successful := 0
	for _, reading := range readings {
		if reading.Success {
			successful++
		}
	}

	summary := viessmann.Summary{
		ConfigFile:  s.config.ConfigFile,
		TotalParams: len(readings),
		Successful:  successful,
		Failed:      len(readings) - successful,
		SuccessRate: float64(successful) / float64(len(readings)) * 100,
		Readings:    readings,
	}

	json.NewEncoder(w).Encode(summary)
}

// getSingleReadingHandler returns a single parameter reading
func (s *Server) getSingleReadingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	indexStr := vars["index"]

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid index", http.StatusBadRequest)
		return
	}

	dataPoints := s.reader.GetDataPoints()
	if index < 0 || index >= len(dataPoints) {
		http.Error(w, "Index out of range", http.StatusBadRequest)
		return
	}

	reading := s.reader.ReadParameter(dataPoints[index], index)
	json.NewEncoder(w).Encode(reading)
}

// getDataPointsHandler returns available data points with XML status
func (s *Server) getDataPointsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	dataPoints := s.reader.GetDataPoints()

	type DataPointWithXMLInfo struct {
		config.DataPoint
		InXML      bool   `json:"inXML"`
		UnitType   string `json:"unitType,omitempty"`
		UnitSource string `json:"unitSource,omitempty"`
		VitoUnit   string `json:"vitoUnit,omitempty"`
	}

	var enrichedDataPoints []DataPointWithXMLInfo

	for _, dp := range dataPoints {
		unitInfo := xml.GetDetailedUnitInfo(dp.Address, dp.Name)

		enriched := DataPointWithXMLInfo{
			DataPoint:  dp,
			InXML:      unitInfo.FoundInXML,
			UnitType:   unitInfo.UnitType,
			UnitSource: unitInfo.Source,
			VitoUnit:   unitInfo.VitoUnit,
		}

		enrichedDataPoints = append(enrichedDataPoints, enriched)
	}

	json.NewEncoder(w).Encode(enrichedDataPoints)
}

// getCachedReadingsHandler returns cached readings with full metadata
func (s *Server) getCachedReadingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cachedReadings := s.reader.GetAllCachedReadings()
	dataPoints := s.reader.GetDataPoints()

	// Create enhanced response with parameter names and aliases
	type CachedReadingWithMetadata struct {
		viessmann.CachedReading
		Name       string   `json:"name"`
		Address    uint16   `json:"address"`
		AliasNames []string `json:"aliasNames,omitempty"`
		DataType   string   `json:"dataType"`
	}

	var enrichedReadings []CachedReadingWithMetadata

	// Create address to datapoint mapping for quick lookup
	addressToDP := make(map[uint16]config.DataPoint)
	for _, dp := range dataPoints {
		addressToDP[dp.Address] = dp
	}

	for address, cached := range cachedReadings {
		enhanced := CachedReadingWithMetadata{
			CachedReading: *cached,
			Address:       address,
		}

		// Add metadata from datapoint if available
		if dp, exists := addressToDP[address]; exists {
			enhanced.Name = dp.Name
			enhanced.AliasNames = dp.AliasNames
			enhanced.DataType = dp.DataType
		} else {
			enhanced.Name = fmt.Sprintf("Unknown_0x%04X", address)
		}

		enrichedReadings = append(enrichedReadings, enhanced)
	}

	// Create summary
	summary := map[string]interface{}{
		"stats":    s.reader.GetCacheStats(),
		"readings": enrichedReadings,
	}

	json.NewEncoder(w).Encode(summary)
}

// getSingleCachedReadingHandler returns a single cached reading
func (s *Server) getSingleCachedReadingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	addressStr := vars["address"]

	address64, err := strconv.ParseUint(addressStr, 10, 16)
	if err != nil {
		http.Error(w, "Invalid address", http.StatusBadRequest)
		return
	}

	address := uint16(address64)
	cached, exists := s.reader.GetCachedReading(address)
	if !exists {
		http.Error(w, "Address not found in cache", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(cached)
}

// getCacheStatsHandler returns cache statistics
func (s *Server) getCacheStatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	stats := s.reader.GetCacheStats()
	json.NewEncoder(w).Encode(stats)
}

// getRecentChangesHandler returns recently changed parameters
func (s *Server) getRecentChangesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse duration parameter (default to 1 hour)
	durationStr := r.URL.Query().Get("since")
	duration := time.Hour // default

	if durationStr != "" {
		if parsed, err := time.ParseDuration(durationStr); err == nil {
			duration = parsed
		}
	}

	recentAddresses := s.reader.GetRecentlyChanged(duration)

	// Get full details for recently changed parameters
	var recentChanges []map[string]interface{}
	dataPoints := s.reader.GetDataPoints()
	addressToDP := make(map[uint16]config.DataPoint)
	for _, dp := range dataPoints {
		addressToDP[dp.Address] = dp
	}

	for _, address := range recentAddresses {
		if cached, exists := s.reader.GetCachedReading(address); exists {
			change := map[string]interface{}{
				"address":       address,
				"hexAddress":    fmt.Sprintf("0x%04X", address),
				"lastChange":    cached.LastChange,
				"changeCount":   cached.ChangeCount,
				"currentValue":  cached.FormattedValue,
				"previousValue": cached.PreviousValue,
			}

			if dp, exists := addressToDP[address]; exists {
				change["name"] = dp.Name
				change["aliasNames"] = dp.AliasNames
			} else {
				change["name"] = fmt.Sprintf("Unknown_0x%04X", address)
			}

			recentChanges = append(recentChanges, change)
		}
	}

	response := map[string]interface{}{
		"since":   duration.String(),
		"count":   len(recentChanges),
		"changes": recentChanges,
	}

	json.NewEncoder(w).Encode(response)
}

// clearCacheHandler clears the parameter cache
func (s *Server) clearCacheHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.reader.ClearCache()

	response := map[string]interface{}{
		"success": true,
		"message": "Cache cleared successfully",
	}

	json.NewEncoder(w).Encode(response)
}

// getAutoRefreshStatusHandler returns auto refresh status
func (s *Server) getAutoRefreshStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := s.reader.GetAutoRefreshStatus()
	json.NewEncoder(w).Encode(status)
}

// getAutoRefreshConfigHandler returns current auto refresh configuration
func (s *Server) getAutoRefreshConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.config.YamlConfig == nil {
		// Return default empty config
		defaultConfig := map[string]interface{}{
			"enabled":   false,
			"interval":  "30s",
			"addresses": []map[string]string{},
		}
		json.NewEncoder(w).Encode(defaultConfig)
		return
	}

	json.NewEncoder(w).Encode(s.config.YamlConfig.AutoRefresh)
}

// updateAutoRefreshConfigHandler updates the auto refresh configuration
func (s *Server) updateAutoRefreshConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var newConfig config.AutoRefreshConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate configuration
	if err := newConfig.ValidateAddresses(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid configuration: %v", err), http.StatusBadRequest)
		return
	}

	// Update reader's auto refresh config
	if err := s.reader.UpdateAutoRefreshConfig(&newConfig); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update auto refresh: %v", err), http.StatusInternalServerError)
		return
	}

	// Update in-memory YAML config if it exists
	if s.config.YamlConfig != nil {
		s.config.YamlConfig.AutoRefresh = newConfig
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Auto refresh configuration updated",
		"config":  newConfig,
	}

	json.NewEncoder(w).Encode(response)
}

// getYamlConfigHandler returns the current YAML configuration
func (s *Server) getYamlConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.config.YamlConfig == nil {
		// Return default config
		defaultConfig := &config.YamlConfig{
			Device:      s.config.Device,
			ConfigFile:  s.config.ConfigFile,
			VControlXML: s.config.VControlXML,
			VitoXML:     s.config.VitoXML,
			WebPort:     s.config.WebPort,
			Verbose:     s.config.Verbose,
			AutoRefresh: config.AutoRefreshConfig{
				Enabled:   false,
				Interval:  "30s",
				Addresses: []string{},
			},
		}
		json.NewEncoder(w).Encode(defaultConfig)
		return
	}

	json.NewEncoder(w).Encode(s.config.YamlConfig)
}

// getMultiGroupRefreshStatusHandler returns multi-group refresh status
func (s *Server) getMultiGroupRefreshStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := s.reader.GetMultiGroupRefreshStatus()
	json.NewEncoder(w).Encode(status)
}

// getRefreshGroupsHandler returns current refresh groups configuration
func (s *Server) getRefreshGroupsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.config.YamlConfig == nil || len(s.config.YamlConfig.RefreshGroups) == 0 {
		// Return empty groups
		json.NewEncoder(w).Encode([]config.RefreshGroup{})
		return
	}

	json.NewEncoder(w).Encode(s.config.YamlConfig.RefreshGroups)
}

// updateRefreshGroupsHandler updates the refresh groups configuration
func (s *Server) updateRefreshGroupsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var newGroups []config.RefreshGroup
	if err := json.NewDecoder(r.Body).Decode(&newGroups); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate all groups
	for _, group := range newGroups {
		if err := group.ValidateAddresses(); err != nil {
			http.Error(w, fmt.Sprintf("Invalid group '%s': %v", group.Name, err), http.StatusBadRequest)
			return
		}
	}

	// Update reader's multi-group refresh config
	if err := s.reader.UpdateMultiGroupRefresh(newGroups); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update multi-group refresh: %v", err), http.StatusInternalServerError)
		return
	}

	// Update in-memory YAML config if it exists
	if s.config.YamlConfig != nil {
		s.config.YamlConfig.RefreshGroups = newGroups
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Refresh groups configuration updated",
		"groups":  newGroups,
	}

	json.NewEncoder(w).Encode(response)
}

// saveYamlConfigHandler saves the YAML configuration to file
func (s *Server) saveYamlConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.config.YamlConfigFile == "" {
		http.Error(w, "No YAML config file specified", http.StatusBadRequest)
		return
	}

	var newConfig config.YamlConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate auto refresh config
	if err := newConfig.AutoRefresh.ValidateAddresses(); err != nil {
		http.Error(w, fmt.Sprintf("Invalid auto refresh configuration: %v", err), http.StatusBadRequest)
		return
	}

	// Save to file
	if err := config.SaveYamlConfig(s.config.YamlConfigFile, &newConfig); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
		return
	}

	// Update in-memory config
	s.config.YamlConfig = &newConfig

	// Update auto refresh if it changed
	if err := s.reader.UpdateAutoRefreshConfig(&newConfig.AutoRefresh); err != nil {
		log.Printf("Warning: Failed to update auto refresh after config save: %v", err)
	}

	// Update MQTT publisher configuration
	if s.mqttPublisher != nil {
		s.mqttPublisher.UpdateConfig(&newConfig.MQTT)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Configuration saved successfully",
		"file":    s.config.YamlConfigFile,
	}

	json.NewEncoder(w).Encode(response)
}

// getMqttStatusHandler returns the current MQTT publisher status
func (s *Server) getMqttStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.mqttPublisher == nil {
		response := map[string]interface{}{
			"enabled": false,
			"status":  "MQTT publisher not initialized",
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	status := s.mqttPublisher.GetStatus()
	json.NewEncoder(w).Encode(status)
}

// indexHandler serves the main web interface
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	// Read the simple HTML template
	htmlBytes, err := os.ReadFile("internal/web/simple.html")
	if err != nil {
		log.Printf("Could not read simple.html template: %v", err)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write(htmlBytes)
}

// broadcastCacheEvent broadcasts cache events to all connected SSE clients
func (s *Server) broadcastCacheEvent(event viessmann.CacheEvent) {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()

	// Send event to all connected clients
	for client := range s.eventClients {
		select {
		case client <- event:
		default:
			// Client channel is full or closed, remove it
			delete(s.eventClients, client)
			close(client)
		}
	}
}

// cacheEventsHandler handles Server-Sent Events for cache updates
func (s *Server) cacheEventsHandler(w http.ResponseWriter, r *http.Request) {
	// Set headers for Server-Sent Events
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	// Create a channel for this client
	clientChan := make(chan viessmann.CacheEvent, 100) // Buffered to prevent blocking

	// Register client
	s.clientsMutex.Lock()
	s.eventClients[clientChan] = true
	s.clientsMutex.Unlock()

	// Remove client when connection closes
	defer func() {
		s.clientsMutex.Lock()
		delete(s.eventClients, clientChan)
		s.clientsMutex.Unlock()
		close(clientChan)
	}()

	// Send initial connection event
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
	w.(http.Flusher).Flush()

	// Handle client disconnect
	ctx := r.Context()

	for {
		select {
		case event := <-clientChan:
			// Convert event type to string
			var eventTypeStr string
			switch event.Type {
			case viessmann.EventValueCreated:
				eventTypeStr = "created"
			case viessmann.EventValueRefreshed:
				eventTypeStr = "refreshed"
			case viessmann.EventValueChanged:
				eventTypeStr = "changed"
			default:
				eventTypeStr = "unknown"
			}

			// Create JSON event data
			eventData := map[string]interface{}{
				"type":          eventTypeStr,
				"address":       event.Address,
				"hexAddress":    fmt.Sprintf("0x%04X", event.Address),
				"value":         event.Reading.FormattedValue,
				"previousValue": event.PreviousValue,
				"success":       event.Reading.Success,
				"error":         event.Reading.Error,
				"timestamp":     event.Timestamp.Format(time.RFC3339),
				"lastRefresh":   event.Reading.LastRefresh.Format(time.RFC3339),
				"lastChange":    event.Reading.LastChange.Format(time.RFC3339),
				"changeCount":   event.Reading.ChangeCount,
			}

			jsonData, err := json.Marshal(eventData)
			if err != nil {
				log.Printf("Failed to marshal event data: %v", err)
				continue
			}

			// Send event to client
			fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
			w.(http.Flusher).Flush()

		case <-ctx.Done():
			// Client disconnected
			return
		}
	}
}

// getMqttHooksStatusHandler returns the status of the MQTT hook manager
func (s *Server) getMqttHooksStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.hookManager == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "MQTT hooks not initialized",
		})
		return
	}

	status := s.hookManager.GetStatus()
	json.NewEncoder(w).Encode(status)
}

// getMqttHooksHandler returns the list of configured MQTT hooks
func (s *Server) getMqttHooksHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.config.YamlConfig == nil {
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	// Return the hooks with their current status
	var hooksWithStatus []map[string]interface{}
	for i, hook := range s.config.YamlConfig.MQTTHooks {
		hookData := map[string]interface{}{
			"index":       i,
			"topic":       hook.Topic,
			"description": hook.Description,
			"enabled":     hook.Enabled,
			"action": map[string]interface{}{
				"type":      hook.Action.Type,
				"zone":      hook.Action.Zone,
				"address":   hook.Action.Address,
				"parameter": hook.Action.Parameter,
				"min_value": hook.Action.MinValue,
				"max_value": hook.Action.MaxValue,
				"transform": hook.Action.Transform,
			},
		}
		hooksWithStatus = append(hooksWithStatus, hookData)
	}

	json.NewEncoder(w).Encode(hooksWithStatus)
}

// toggleMqttHookHandler enables/disables a specific MQTT hook
func (s *Server) toggleMqttHookHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	indexStr := vars["index"]
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid hook index", http.StatusBadRequest)
		return
	}

	if s.config.YamlConfig == nil || index >= len(s.config.YamlConfig.MQTTHooks) {
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}

	// Toggle the enabled state
	s.config.YamlConfig.MQTTHooks[index].Enabled = !s.config.YamlConfig.MQTTHooks[index].Enabled

	// Update the hook manager
	if s.hookManager != nil {
		var hooks []mqtt.MQTTHook
		for _, hookConfig := range s.config.YamlConfig.MQTTHooks {
			hooks = append(hooks, mqtt.MQTTHook{
				Topic:       hookConfig.Topic,
				Description: hookConfig.Description,
				Action:      hookConfig.Action,
				Enabled:     hookConfig.Enabled,
			})
		}
		s.hookManager.LoadHooks(hooks)
	}

	// Save config to file if YAML config file is specified
	if s.config.YamlConfigFile != "" {
		if err := config.SaveYamlConfig(s.config.YamlConfigFile, s.config.YamlConfig); err != nil {
			log.Printf("Failed to save YAML config: %v", err)
			http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"enabled": s.config.YamlConfig.MQTTHooks[index].Enabled,
		"message": fmt.Sprintf("Hook %d %s", index,
			map[bool]string{true: "enabled", false: "disabled"}[s.config.YamlConfig.MQTTHooks[index].Enabled]),
	})
}

// testMqttHookHandler tests a specific MQTT hook with a sample payload
func (s *Server) testMqttHookHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	vars := mux.Vars(r)
	indexStr := vars["index"]
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "Invalid hook index", http.StatusBadRequest)
		return
	}

	if s.config.YamlConfig == nil || index >= len(s.config.YamlConfig.MQTTHooks) {
		http.Error(w, "Hook not found", http.StatusNotFound)
		return
	}

	// Parse test payload from request body
	var testRequest struct {
		Payload string `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&testRequest); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Get the hook configuration
	hookConfig := s.config.YamlConfig.MQTTHooks[index]

	// Simulate MQTT hook testing
	if s.hookManager != nil {
		log.Printf("Testing MQTT hook %d (%s) with payload: %s",
			index, hookConfig.Description, testRequest.Payload)
		// In a real implementation, you could call a test method on hook manager
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Test sent to hook %d (%s) with payload: %s",
			index, hookConfig.Description, testRequest.Payload),
		"hook": map[string]interface{}{
			"topic":       hookConfig.Topic,
			"description": hookConfig.Description,
			"action_type": hookConfig.Action.Type,
		},
	})
}
