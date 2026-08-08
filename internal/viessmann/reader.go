// Package viessmann handles communication with Viessmann V200KW2 heating systems
package viessmann

import (
	"fmt"
	"time"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/protocol"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// Reader handles communication with the Viessmann heating system
type Reader struct {
	device         string
	serialComm     SerialCommunicatorInterface
	formatter      ValueFormatterInterface
	decoder        ValueDecoderInterface
	cache          ParameterCacheInterface
	autoRefresher  AutoRefresherInterface
	multiRefresher MultiGroupRefresherInterface
	logger         LoggerInterface
	metrics        MetricsInterface
	dataPoints     []config.DataPoint
}

// Reading represents a parameter reading with metadata
type Reading struct {
	Name           string      `json:"name"`
	Address        uint16      `json:"address"`
	RawValue       interface{} `json:"rawValue"`
	FormattedValue string      `json:"formattedValue"`
	DataType       string      `json:"dataType"`
	Unit           string      `json:"unit,omitempty"`
	InXML          bool        `json:"inXML"`
	UnitType       string      `json:"unitType,omitempty"`
	UnitSource     string      `json:"unitSource,omitempty"`
	VitoUnit       string      `json:"vitoUnit,omitempty"`
	Timestamp      time.Time   `json:"timestamp"`
	Success        bool        `json:"success"`
	Error          string      `json:"error,omitempty"`
}

// Summary represents a summary of all readings
type Summary struct {
	ConfigFile  string    `json:"configFile,omitempty"`
	TotalParams int       `json:"totalParams"`
	Successful  int       `json:"successful"`
	Failed      int       `json:"failed"`
	SuccessRate float64   `json:"successRate"`
	Readings    []Reading `json:"readings"`
}

// NewReader creates a new Viessmann reader
func NewReader(device string) (*Reader, error) {
	config, err := NewDefaultReaderConfig(device)
	if err != nil {
		return nil, errors.Wrap(errors.ErrConfig, "failed to create reader config", err)
	}

	reader := NewReaderWithDependencies(config)
	reader.logger.Info("Reader created with device: %s", device)

	return reader, nil
}

// LoadConfig loads data points from a configuration file
func (r *Reader) LoadConfig(configFile string) error {
	r.logger.Info("Loading configuration from: %s", configFile)

	dataPoints, err := r.parseAndValidateConfig(configFile)
	if err != nil {
		return err
	}

	r.dataPoints = dataPoints

	// Add XML-only parameters if available
	if err := r.addXMLOnlyParameters(); err != nil {
		return errors.Wrap(errors.ErrConfig, "failed to add XML-only parameters", err)
	}

	// Final deduplication pass
	r.dataPoints = r.deduplicateDataPoints(r.dataPoints)

	r.logger.Info("Loaded %d data points from configuration", len(r.dataPoints))
	return nil
}

// GetDataPoints returns the configured data points
func (r *Reader) GetDataPoints() []config.DataPoint {
	return r.dataPoints
}

// GetSerialComm returns the serial communicator interface
func (r *Reader) GetSerialComm() SerialCommunicatorInterface {
	return r.serialComm
}

// ReadAll reads all configured parameters
func (r *Reader) ReadAll() ([]Reading, error) {
	if len(r.dataPoints) == 0 {
		return nil, errors.New(errors.ErrConfig, "no data points configured")
	}

	var readings []Reading

	for i, dp := range r.dataPoints {
		reading := r.ReadParameter(dp, i)
		readings = append(readings, reading)
	}

	return readings, nil
}

// ReadParameter reads a single parameter with retry logic like original
func (r *Reader) ReadParameter(dp config.DataPoint, index int) Reading {
	startTime := time.Now()
	r.logger.Debug("Reading parameter %s at address 0x%04X", dp.Name, dp.Address)

	reading := Reading{
		Name:      dp.Name,
		Address:   dp.Address,
		DataType:  dp.DataType,
		Unit:      dp.Unit,
		Timestamp: time.Now(),
		Success:   false,
	}

	// Get detailed unit info
	unitInfo := xml.GetDetailedUnitInfo(dp.Address, dp.Name)
	reading.InXML = unitInfo.FoundInXML
	reading.UnitType = unitInfo.UnitType
	reading.UnitSource = unitInfo.Source
	reading.VitoUnit = unitInfo.VitoUnit

	// Try up to 2 attempts for each datapoint like original
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		rawValue, err := r.serialComm.ReadFromDevice(dp.Address, dp.Size)
		if err == nil {
			// Check if this is an error value from XML before processing
			if xml.IsErrorValue(dp.Address, rawValue) {
				// This is an error value - treat as read failure
				lastErr = fmt.Errorf("device returned error pattern for address 0x%04X", dp.Address)
				r.logger.Warn("Read attempt %d returned error pattern for %s", attempt, dp.Name)
				continue // Try again or fail
			}

			// Valeur reservee « pas de donnee » : inutile de reessayer, le
			// point de mesure n'existe pas sur cet appareil. La publier
			// donnerait une mesure absurde -- 3276,7 °C pour un capteur
			// solaire absent.
			if IsUnavailableValue(rawValue) {
				reading.RawValue = rawValue
				reading.Error = fmt.Sprintf("datapoint not available on this appliance (raw % X)", rawValue)
				reading.FormattedValue = "unavailable"
				r.metrics.IncrementErrorCount()
				r.cache.Set(dp.Address, reading)
				r.logger.Warn("%s (0x%04X) is not available on this appliance (raw % X)", dp.Name, dp.Address, rawValue)
				return reading
			}

			// Success!
			reading.RawValue = rawValue
			reading.Success = true
			reading.FormattedValue = r.formatter.FormatValueWithAllSources(rawValue, dp.DataType, dp.Name, dp.Address)

			// Record metrics
			r.metrics.IncrementReadCount()
			r.metrics.RecordReadLatency(time.Since(startTime))

			// Store in cache
			r.cache.Set(dp.Address, reading)

			r.logger.Debug("Successfully read %s: %s", dp.Name, reading.FormattedValue)
			return reading
		}

		lastErr = err
		r.logger.Warn("Read attempt %d failed for %s: %v", attempt, dp.Name, err)

		// Small delay before retry (like original)
		if attempt < 2 {
			time.Sleep(protocol.KW2_RETRY_DELAY_MS * time.Millisecond)
		}
	}

	// Both attempts failed
	reading.Error = fmt.Sprintf("Read error: %v", lastErr)
	reading.FormattedValue = fmt.Sprintf("ERROR: %v", lastErr)

	// Record error metrics
	r.metrics.IncrementErrorCount()
	r.metrics.RecordReadLatency(time.Since(startTime))

	// Store failed reading in cache too
	r.cache.Set(dp.Address, reading)

	r.logger.Error("Failed to read parameter %s after 2 attempts: %v", dp.Name, lastErr)
	return reading
}

// GetCachedReading returns a cached reading for the given address
func (r *Reader) GetCachedReading(address uint16) (*CachedReading, bool) {
	return r.cache.Get(address)
}

// GetAllCachedReadings returns all cached readings
func (r *Reader) GetAllCachedReadings() map[uint16]*CachedReading {
	return r.cache.GetAll()
}

// GetCacheStats returns cache statistics
func (r *Reader) GetCacheStats() map[string]interface{} {
	return r.cache.GetStats()
}

// GetRecentlyChanged returns parameters that changed recently
func (r *Reader) GetRecentlyChanged(since time.Duration) []uint16 {
	return r.cache.GetRecentlyChanged(since)
}

// ClearCache clears all cached readings
func (r *Reader) ClearCache() {
	r.cache.Clear()
}

// SetupAutoRefresh configures the auto refresh functionality
func (r *Reader) SetupAutoRefresh(autoConfig *config.AutoRefreshConfig) error {
	if autoConfig == nil {
		return errors.New(errors.ErrValidation, "auto refresh config cannot be nil")
	}

	// Stop existing auto refresher if running
	if r.autoRefresher != nil {
		r.autoRefresher.Stop()
	}

	// Create new auto refresher
	var err error
	r.autoRefresher, err = NewAutoRefresher(r, autoConfig)
	if err != nil {
		return errors.Wrap(errors.ErrConfig, "failed to create auto refresher", err)
	}

	// Start if enabled
	if autoConfig.Enabled {
		return r.autoRefresher.Start()
	}

	return nil
}

// GetAutoRefreshStatus returns the status of auto refresh
func (r *Reader) GetAutoRefreshStatus() map[string]interface{} {
	if r.autoRefresher == nil {
		return map[string]interface{}{
			"enabled": false,
			"running": false,
		}
	}

	return r.autoRefresher.GetStatus()
}

// UpdateAutoRefreshConfig updates the auto refresh configuration
func (r *Reader) UpdateAutoRefreshConfig(newConfig *config.AutoRefreshConfig) error {
	if r.autoRefresher == nil {
		return r.SetupAutoRefresh(newConfig)
	}

	return r.autoRefresher.UpdateConfig(newConfig)
}

// SetupMultiGroupRefresh configures the multi-group refresh functionality
func (r *Reader) SetupMultiGroupRefresh(groups []config.RefreshGroup) error {
	// Stop existing multi refresher if running
	if r.multiRefresher != nil {
		r.multiRefresher.Stop()
	}

	if len(groups) == 0 {
		r.multiRefresher = nil
		return nil
	}

	// Create new multi refresher
	var err error
	r.multiRefresher, err = NewMultiGroupRefresher(r, groups)
	if err != nil {
		return errors.Wrap(errors.ErrConfig, "failed to create multi-group refresher", err)
	}

	// Start refresher
	return r.multiRefresher.Start()
}

// GetMultiGroupRefreshStatus returns the status of multi-group refresh
func (r *Reader) GetMultiGroupRefreshStatus() map[string]interface{} {
	if r.multiRefresher == nil {
		return map[string]interface{}{
			"running":         false,
			"total_groups":    0,
			"active_groups":   0,
			"total_addresses": 0,
			"groups":          []interface{}{},
		}
	}

	return r.multiRefresher.GetStatus()
}

// UpdateMultiGroupRefresh updates the multi-group refresh configuration
func (r *Reader) UpdateMultiGroupRefresh(newGroups []config.RefreshGroup) error {
	if r.multiRefresher == nil {
		return r.SetupMultiGroupRefresh(newGroups)
	}

	return r.multiRefresher.UpdateGroups(newGroups)
}

// AddCacheCallback registers a callback for cache events
func (r *Reader) AddCacheCallback(callback CacheCallback) {
	if r.cache != nil {
		r.cache.AddCallback(callback)
	}
}

// RemoveAllCacheCallbacks removes all cache callbacks
func (r *Reader) RemoveAllCacheCallbacks() {
	if r.cache != nil {
		r.cache.RemoveAllCallbacks()
	}
}

// Close closes the serial connection and stops auto refresh
func (r *Reader) Close() error {
	// Stop auto refresh first
	if r.autoRefresher != nil {
		r.autoRefresher.Stop()
	}

	// Stop multi-group refresh
	if r.multiRefresher != nil {
		r.multiRefresher.Stop()
	}

	// Close serial communicator
	if r.serialComm != nil {
		return r.serialComm.Close()
	}
	return nil
}

// parseAndValidateConfig parses and validates the configuration file
func (r *Reader) parseAndValidateConfig(configFile string) ([]config.DataPoint, error) {
	dataPoints, err := config.ParseConfig(configFile)
	if err != nil {
		return nil, errors.Wrap(errors.ErrParsing, "failed to parse config", err)
	}

	// Validate the loaded configuration
	validator := NewConfigValidator(r.logger)
	if err := validator.ValidateDataPoints(dataPoints); err != nil {
		return nil, errors.Wrap(errors.ErrValidation, "configuration validation failed", err)
	}

	return dataPoints, nil
}

// addXMLOnlyParameters adds XML-only parameters to the data points
func (r *Reader) addXMLOnlyParameters() error {
	if len(r.dataPoints) == 0 {
		return nil
	}

	existingAddresses := make(map[uint16]bool)
	for _, dp := range r.dataPoints {
		existingAddresses[dp.Address] = true
	}

	xmlOnlyParams := xml.GetXMLOnlyParameters(existingAddresses)
	if len(xmlOnlyParams) == 0 {
		return nil
	}

	r.logger.Info("Adding %d XML-only parameters (ErrorState, CycleTime, etc.)", len(xmlOnlyParams))

	for _, xmlParam := range xmlOnlyParams {
		// Double-check address doesn't already exist (safety net)
		if existingAddresses[xmlParam.Address] {
			continue
		}

		name, dataType := r.createXMLParameterInfo(xmlParam)
		if name == "" {
			continue // Skip unsupported parameter types
		}

		aliases := r.createXMLParameterAliases(xmlParam)

		xmlOnlyDP := config.DataPoint{
			Name:       name,
			Address:    xmlParam.Address,
			Size:       xmlParam.Length,
			DataType:   dataType,
			Unit:       "", // Will be determined by XML system
			AliasNames: aliases,
		}

		r.dataPoints = append(r.dataPoints, xmlOnlyDP)
		existingAddresses[xmlParam.Address] = true
	}

	return nil
}

// createXMLParameterInfo creates name and data type for XML parameter
func (r *Reader) createXMLParameterInfo(xmlParam xml.AddressMappingEntry) (string, string) {
	var name, dataType string

	switch xmlParam.Unit {
	case "CT":
		name = fmt.Sprintf("CycleTime 0x%04X", xmlParam.Address)
		dataType = "Array"
	case "ES":
		if xmlParam.Length == 9 {
			dataType = "Array"
			// Map specific error history addresses to descriptive names
			switch xmlParam.Address {
			case 0x7507:
				name = "Error History Entry 1"
			case 0x7510:
				name = "Error History Entry 2"
			case 0x7519:
				name = "Error History Entry 3"
			case 0x7522:
				name = "Error History Entry 4"
			case 0x752B:
				name = "Error History Entry 5"
			case 0x7534:
				name = "Error History Entry 6"
			case 0x753D:
				name = "Error History Entry 7"
			case 0x7546:
				name = "Error History Entry 8"
			case 0x754F:
				name = "Error History Entry 9"
			case 0x7558:
				name = "Error History Entry 10"
			default:
				name = fmt.Sprintf("Error History 0x%04X", xmlParam.Address)
			}
		} else {
			dataType = "Byte"
			name = fmt.Sprintf("ErrorState 0x%04X", xmlParam.Address)
		}
	case "BST", "ESG":
		name = fmt.Sprintf("Status 0x%04X", xmlParam.Address)
		dataType = "Byte"
	case "DT":
		name = fmt.Sprintf("DeviceType 0x%04X", xmlParam.Address)
		dataType = "Byte"
	default:
		return "", "" // Unsupported parameter type
	}

	// Use description from vito.xml if available
	if xmlParam.Description != "" {
		name = fmt.Sprintf("%s (0x%04X)", xmlParam.Description, xmlParam.Address)
	}

	return name, dataType
}

// createXMLParameterAliases creates aliases for XML parameters
func (r *Reader) createXMLParameterAliases(xmlParam xml.AddressMappingEntry) []string {
	var aliases []string

	if xmlParam.Description != "" {
		aliases = append(aliases, xmlParam.Description)
	}

	// Add unit type as alias if it's meaningful
	switch xmlParam.Unit {
	case "CT":
		aliases = append(aliases, "CycleTime", "Zykluszeit")
	case "ES":
		if xmlParam.Length == 9 {
			aliases = append(aliases, "ErrorHistory", "Fehlerhistorie")
		} else {
			aliases = append(aliases, "ErrorState", "Fehlerstatus")
		}
	case "BST":
		aliases = append(aliases, "BetriebsStatus", "OperatingStatus")
	case "DT":
		aliases = append(aliases, "DeviceType", "Gerätetyp")
	case "ESG":
		aliases = append(aliases, "StatusGroup", "Statusgruppe")
	}

	return aliases
}

// deduplicateDataPoints removes duplicate addresses, keeping the first occurrence
func (r *Reader) deduplicateDataPoints(dataPoints []config.DataPoint) []config.DataPoint {
	seen := make(map[uint16]int)
	var deduplicatedDataPoints []config.DataPoint

	for i, dp := range dataPoints {
		if firstIndex, exists := seen[dp.Address]; exists {
			r.logger.Warn("Duplicate address 0x%04X found - keeping '%s' (index %d) and removing '%s' (index %d)",
				dp.Address, dataPoints[firstIndex].Name, firstIndex, dp.Name, i)
			continue
		}
		seen[dp.Address] = len(deduplicatedDataPoints)
		deduplicatedDataPoints = append(deduplicatedDataPoints, dp)
	}

	if len(deduplicatedDataPoints) != len(dataPoints) {
		r.logger.Info("Removed %d duplicate parameters, %d unique parameters remain",
			len(dataPoints)-len(deduplicatedDataPoints), len(deduplicatedDataPoints))
	}

	return deduplicatedDataPoints
}
