// Package viessmann - auto-refresh functionality
package viessmann

import (
	"fmt"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
	"time"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/errors"
	"github.com/yoda-jm/vcontrold-go/internal/protocol"
)

// AutoRefresher handles automatic refreshing of specific addresses
type AutoRefresher struct {
	reader    *Reader
	config    *config.AutoRefreshConfig
	ticker    *time.Ticker
	stopCh    chan bool
	running   bool
	addresses []uint16 // Cached parsed addresses
}

// NewAutoRefresher creates a new auto refresher
func NewAutoRefresher(reader *Reader, autoConfig *config.AutoRefreshConfig) (*AutoRefresher, error) {
	if autoConfig == nil {
		return nil, errors.New(errors.ErrValidation, "auto refresh config cannot be nil")
	}

	// Validate and parse addresses
	addresses, err := autoConfig.GetAddressesAsUint16()
	if err != nil {
		return nil, errors.Wrap(errors.ErrValidation, "invalid addresses in auto refresh config", err)
	}

	return &AutoRefresher{
		reader:    reader,
		config:    autoConfig,
		addresses: addresses,
		stopCh:    make(chan bool),
	}, nil
}

// Start begins the auto-refresh process
func (ar *AutoRefresher) Start() error {
	if ar.running {
		return errors.New(errors.ErrValidation, "auto refresher is already running")
	}

	if !ar.config.Enabled || len(ar.addresses) == 0 {
		logging.Info("Auto refresh is disabled or no addresses configured")
		return nil
	}

	duration, err := ar.config.GetRefreshDuration()
	if err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid refresh interval", err)
	}

	logging.Info("Starting auto refresh for %d addresses every %v", len(ar.addresses), duration)

	ar.ticker = time.NewTicker(duration)
	ar.running = true

	// Start the refresh goroutine
	go ar.refreshLoop()

	return nil
}

// Stop stops the auto-refresh process
func (ar *AutoRefresher) Stop() {
	if !ar.running {
		return
	}

	logging.Info("Stopping auto refresh")
	ar.stopCh <- true

	if ar.ticker != nil {
		ar.ticker.Stop()
	}

	ar.running = false
}

// IsRunning returns whether the auto refresher is currently running
func (ar *AutoRefresher) IsRunning() bool {
	return ar.running
}

// GetStatus returns the current status of auto refresh
func (ar *AutoRefresher) GetStatus() map[string]interface{} {
	duration, _ := ar.config.GetRefreshDuration()

	return map[string]interface{}{
		"enabled":       ar.config.Enabled,
		"running":       ar.running,
		"interval":      ar.config.Interval,
		"duration_sec":  duration.Seconds(),
		"address_count": len(ar.addresses),
		"addresses":     ar.getAddressStrings(),
	}
}

// UpdateConfig updates the auto refresh configuration
func (ar *AutoRefresher) UpdateConfig(newConfig *config.AutoRefreshConfig) error {
	// Validate new config
	if err := newConfig.ValidateAddresses(); err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid configuration", err)
	}

	// Parse addresses
	addresses, err := newConfig.GetAddressesAsUint16()
	if err != nil {
		return errors.Wrap(errors.ErrValidation, "failed to parse addresses", err)
	}

	// Stop current refresh if running
	wasRunning := ar.running
	if ar.running {
		ar.Stop()
	}

	// Update configuration
	ar.config = newConfig
	ar.addresses = addresses

	// Restart if it was running and still enabled
	if wasRunning && newConfig.Enabled {
		return ar.Start()
	}

	return nil
}

// refreshLoop is the main refresh loop
func (ar *AutoRefresher) refreshLoop() {
	defer func() {
		ar.running = false
	}()

	for {
		select {
		case <-ar.ticker.C:
			ar.refreshAddresses()
		case <-ar.stopCh:
			logging.Info("Auto refresh loop stopped")
			return
		}
	}
}

// refreshAddresses refreshes all configured addresses
func (ar *AutoRefresher) refreshAddresses() {
	logging.Info("Auto refresh: reading %d addresses", len(ar.addresses))

	dataPoints := ar.reader.GetDataPoints()
	addressToDP := make(map[uint16]config.DataPoint)
	addressToIndex := make(map[uint16]int)

	// Create lookup maps
	for i, dp := range dataPoints {
		addressToDP[dp.Address] = dp
		addressToIndex[dp.Address] = i
	}

	successCount := 0

	for _, address := range ar.addresses {
		if dp, exists := addressToDP[address]; exists {
			if index, indexExists := addressToIndex[address]; indexExists {
				// Read the parameter
				reading := ar.reader.ReadParameter(dp, index)
				if reading.Success {
					successCount++
					logging.Info("Auto refresh: 0x%04X (%s) = %s", address, dp.Name, reading.FormattedValue)
				} else {
					logging.Error("Auto refresh: 0x%04X (%s) failed: %s", address, dp.Name, reading.Error)
				}
			}
		} else {
			logging.Info("Auto refresh: 0x%04X not found in data points", address)
		}

		// Small delay between reads to avoid overwhelming the device
		time.Sleep(protocol.KW2_READ_DELAY_MS * time.Millisecond)
	}

	logging.Info("Auto refresh completed: %d/%d successful", successCount, len(ar.addresses))
}

// getAddressStrings returns addresses as hex strings for status display
func (ar *AutoRefresher) getAddressStrings() []string {
	var addressStrings []string
	for _, addr := range ar.addresses {
		addressStrings = append(addressStrings, fmt.Sprintf("0x%04X", addr))
	}
	return addressStrings
}

// GetNextRefreshTime returns when the next refresh will occur
func (ar *AutoRefresher) GetNextRefreshTime() *time.Time {
	if !ar.running || ar.ticker == nil {
		return nil
	}

	duration, err := ar.config.GetRefreshDuration()
	if err != nil {
		return nil
	}

	nextTime := time.Now().Add(duration)
	return &nextTime
}
