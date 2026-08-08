// Package viessmann - multi-group auto-refresh functionality
package viessmann

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/protocol"
)

// MultiGroupRefresher handles multiple refresh groups with different intervals
type MultiGroupRefresher struct {
	reader      *Reader
	groups      []config.RefreshGroup
	groupTickers map[string]*time.Ticker
	stopChannels map[string]chan bool
	running     bool
	mutex       sync.RWMutex
}

// NewMultiGroupRefresher creates a new multi-group refresher
func NewMultiGroupRefresher(reader *Reader, groups []config.RefreshGroup) (*MultiGroupRefresher, error) {
	// Validate all groups
	for _, group := range groups {
		if err := group.ValidateAddresses(); err != nil {
			return nil, errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid refresh group '%s'", group.Name), err)
		}
	}
	
	return &MultiGroupRefresher{
		reader:       reader,
		groups:       groups,
		groupTickers: make(map[string]*time.Ticker),
		stopChannels: make(map[string]chan bool),
		running:      false,
	}, nil
}

// Start begins the multi-group refresh process
func (mgr *MultiGroupRefresher) Start() error {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	
	if mgr.running {
		return errors.New(errors.ErrValidation, "multi-group refresher is already running")
	}
	
	enabledGroups := 0
	for _, group := range mgr.groups {
		if group.Enabled && len(group.Addresses) > 0 {
			duration, err := group.GetRefreshDuration()
			if err != nil {
				return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid refresh interval for group '%s'", group.Name), err)
			}
			
			log.Printf("Starting refresh group '%s' with %d addresses every %v", 
				group.Name, len(group.Addresses), duration)
			
			ticker := time.NewTicker(duration)
			stopCh := make(chan bool)
			
			mgr.groupTickers[group.Name] = ticker
			mgr.stopChannels[group.Name] = stopCh
			
			// Do initial refresh immediately for this group
			log.Printf("Performing initial refresh for group '%s'", group.Name)
			go func(g config.RefreshGroup) {
				mgr.refreshGroup(g)
			}(group)
			
			// Start goroutine for this group
			go mgr.refreshGroupLoop(group, ticker, stopCh)
			enabledGroups++
		}
	}
	
	if enabledGroups == 0 {
		log.Printf("No enabled refresh groups found")
		return nil
	}
	
	mgr.running = true
	log.Printf("Multi-group refresher started with %d active groups", enabledGroups)
	return nil
}

// Stop stops all refresh groups
func (mgr *MultiGroupRefresher) Stop() {
	mgr.mutex.Lock()
	defer mgr.mutex.Unlock()
	
	if !mgr.running {
		return
	}
	
	log.Printf("Stopping multi-group refresher")
	
	// Stop all groups
	for groupName, stopCh := range mgr.stopChannels {
		stopCh <- true
		
		if ticker, exists := mgr.groupTickers[groupName]; exists {
			ticker.Stop()
		}
	}
	
	// Clear maps
	mgr.groupTickers = make(map[string]*time.Ticker)
	mgr.stopChannels = make(map[string]chan bool)
	mgr.running = false
}

// IsRunning returns whether the multi-group refresher is currently running
func (mgr *MultiGroupRefresher) IsRunning() bool {
	mgr.mutex.RLock()
	defer mgr.mutex.RUnlock()
	return mgr.running
}

// GetStatus returns the current status of all refresh groups
func (mgr *MultiGroupRefresher) GetStatus() map[string]interface{} {
	mgr.mutex.RLock()
	defer mgr.mutex.RUnlock()
	
	var activeGroups []map[string]interface{}
	totalAddresses := 0
	
	for _, group := range mgr.groups {
		if group.Enabled {
			duration, _ := group.GetRefreshDuration()
			activeGroups = append(activeGroups, map[string]interface{}{
				"name":          group.Name,
				"interval":      group.Interval,
				"duration_sec":  duration.Seconds(),
				"address_count": len(group.Addresses),
				"addresses":     group.Addresses,
				"running":       mgr.running,
			})
			totalAddresses += len(group.Addresses)
		}
	}
	
	return map[string]interface{}{
		"running":         mgr.running,
		"total_groups":    len(mgr.groups),
		"active_groups":   len(activeGroups),
		"total_addresses": totalAddresses,
		"groups":          activeGroups,
	}
}

// UpdateGroups updates the refresh groups configuration
func (mgr *MultiGroupRefresher) UpdateGroups(newGroups []config.RefreshGroup) error {
	// Validate new groups
	for _, group := range newGroups {
		if err := group.ValidateAddresses(); err != nil {
			return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid refresh group '%s'", group.Name), err)
		}
	}
	
	// Stop current refresher if running
	wasRunning := mgr.IsRunning()
	if wasRunning {
		mgr.Stop()
	}
	
	// Update groups
	mgr.mutex.Lock()
	mgr.groups = newGroups
	mgr.mutex.Unlock()
	
	// Restart if it was running
	if wasRunning {
		return mgr.Start()
	}
	
	return nil
}

// refreshGroupLoop is the main refresh loop for a specific group
func (mgr *MultiGroupRefresher) refreshGroupLoop(group config.RefreshGroup, ticker *time.Ticker, stopCh chan bool) {
	defer func() {
		log.Printf("Refresh group '%s' loop stopped", group.Name)
	}()
	
	for {
		select {
		case <-ticker.C:
			mgr.refreshGroup(group)
		case <-stopCh:
			return
		}
	}
}

// refreshGroup refreshes all addresses in a specific group
func (mgr *MultiGroupRefresher) refreshGroup(group config.RefreshGroup) {
	log.Printf("Group '%s': refreshing %d addresses", group.Name, len(group.Addresses))
	
	dataPoints := mgr.reader.GetDataPoints()
	addressToDP := make(map[uint16]config.DataPoint)
	addressToIndex := make(map[uint16]int)
	
	// Create lookup maps
	for i, dp := range dataPoints {
		addressToDP[dp.Address] = dp
		addressToIndex[dp.Address] = i
	}
	
	successCount := 0
	
	// Parse addresses to uint16
	addresses, err := group.GetAddressesAsUint16()
	if err != nil {
		log.Printf("Group '%s': failed to parse addresses: %v", group.Name, err)
		return
	}
	
	for _, address := range addresses {
		if dp, exists := addressToDP[address]; exists {
			if index, indexExists := addressToIndex[address]; indexExists {
				// Read the parameter
				reading := mgr.reader.ReadParameter(dp, index)
				if reading.Success {
					successCount++
					log.Printf("Group '%s': 0x%04X (%s) = %s", group.Name, address, dp.Name, reading.FormattedValue)
				} else {
					log.Printf("Group '%s': 0x%04X (%s) failed: %s", group.Name, address, dp.Name, reading.Error)
				}
			}
		} else {
			log.Printf("Group '%s': 0x%04X not found in data points", group.Name, address)
		}
		
		// Small delay between reads to avoid overwhelming the device
		time.Sleep(protocol.KW2_READ_DELAY_MS * time.Millisecond)
	}
	
	log.Printf("Group '%s' completed: %d/%d successful", group.Name, successCount, len(addresses))
}