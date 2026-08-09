// Package viessmann - cache implementation for parameter readings
package viessmann

import (
	"github.com/yourusername/vcontrold-go/internal/logging"
	"sync"
	"time"
)

// CacheEventType represents the type of cache event
type CacheEventType int

const (
	EventValueCreated   CacheEventType = iota // First time a value is cached
	EventValueRefreshed                       // Value refreshed (same value)
	EventValueChanged                         // Value changed to different value
)

// CacheEvent represents an event in the cache
type CacheEvent struct {
	Type          CacheEventType `json:"type"`
	Address       uint16         `json:"address"`
	Reading       *CachedReading `json:"reading"`
	PreviousValue string         `json:"previousValue,omitempty"`
	Timestamp     time.Time      `json:"timestamp"`
}

// CacheCallback is a function that handles cache events
type CacheCallback func(event CacheEvent)

// CachedReading represents a cached parameter reading with timestamps
type CachedReading struct {
	// Core data
	RawValue       interface{} `json:"rawValue"`
	FormattedValue string      `json:"formattedValue"`
	DataType       string      `json:"dataType"`
	Unit           string      `json:"unit,omitempty"`

	// XML metadata
	InXML      bool   `json:"inXML"`
	UnitType   string `json:"unitType,omitempty"`
	UnitSource string `json:"unitSource,omitempty"`
	VitoUnit   string `json:"vitoUnit,omitempty"`

	// Status
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`

	// Timestamps
	LastRefresh time.Time `json:"lastRefresh"`
	LastChange  time.Time `json:"lastChange"`

	// Change detection
	PreviousValue string `json:"previousValue,omitempty"`
	ChangeCount   int    `json:"changeCount"`
}

// ParameterCache manages cached readings by address
type ParameterCache struct {
	mutex     sync.RWMutex
	readings  map[uint16]*CachedReading
	callbacks []CacheCallback
}

// NewParameterCache creates a new parameter cache
func NewParameterCache() *ParameterCache {
	return &ParameterCache{
		readings:  make(map[uint16]*CachedReading),
		callbacks: make([]CacheCallback, 0),
	}
}

// AddCallback registers a callback for cache events
func (c *ParameterCache) AddCallback(callback CacheCallback) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.callbacks = append(c.callbacks, callback)
}

// RemoveCallback removes all callbacks (useful for cleanup)
func (c *ParameterCache) RemoveAllCallbacks() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.callbacks = make([]CacheCallback, 0)
}

// Get returns a cached reading for the given address
func (c *ParameterCache) Get(address uint16) (*CachedReading, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	reading, exists := c.readings[address]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid external modifications
	copy := *reading
	return &copy, true
}

// Set stores or updates a cached reading
func (c *ParameterCache) Set(address uint16, reading Reading) {
	c.mutex.Lock()

	now := time.Now()

	// Get existing cached reading if any
	existing, exists := c.readings[address]
	var eventType CacheEventType
	var previousValue string

	// Create new cached reading
	cached := &CachedReading{
		RawValue:       reading.RawValue,
		FormattedValue: reading.FormattedValue,
		DataType:       reading.DataType,
		Unit:           reading.Unit,
		InXML:          reading.InXML,
		UnitType:       reading.UnitType,
		UnitSource:     reading.UnitSource,
		VitoUnit:       reading.VitoUnit,
		Success:        reading.Success,
		Error:          reading.Error,
		LastRefresh:    now,
	}

	if exists {
		// Check if value changed
		if existing.FormattedValue != reading.FormattedValue {
			cached.LastChange = now
			cached.PreviousValue = existing.FormattedValue
			cached.ChangeCount = existing.ChangeCount + 1
			eventType = EventValueChanged
			previousValue = existing.FormattedValue
			logging.Info("Cache updated: 0x%04X value changed from '%s' to '%s' (change #%d)",
				address, existing.FormattedValue, reading.FormattedValue, cached.ChangeCount)
		} else {
			// Value didn't change, keep existing timestamps
			cached.LastChange = existing.LastChange
			cached.PreviousValue = existing.PreviousValue
			cached.ChangeCount = existing.ChangeCount
			eventType = EventValueRefreshed
			previousValue = existing.FormattedValue
		}
	} else {
		// First time reading
		cached.LastChange = now
		cached.ChangeCount = 1
		eventType = EventValueCreated
		logging.Info("Cache initialized: 0x%04X first value '%s'", address, reading.FormattedValue)
	}

	c.readings[address] = cached

	// Create and fire the event (copy cached reading to avoid race conditions)
	cachedCopy := *cached
	event := CacheEvent{
		Type:          eventType,
		Address:       address,
		Reading:       &cachedCopy,
		PreviousValue: previousValue,
		Timestamp:     now,
	}

	// Store callbacks to fire outside of mutex
	callbacks := make([]CacheCallback, len(c.callbacks))
	copy(callbacks, c.callbacks)

	c.mutex.Unlock()

	// Fire callbacks outside of mutex to avoid deadlocks
	for _, callback := range callbacks {
		go func(cb CacheCallback) {
			defer func() {
				if r := recover(); r != nil {
					logging.Error("Cache callback panic: %v", r)
				}
			}()
			cb(event)
		}(callback)
	}
}

// GetAll returns all cached readings
func (c *ParameterCache) GetAll() map[uint16]*CachedReading {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	// Return a copy of the map with copied readings
	result := make(map[uint16]*CachedReading)
	for addr, reading := range c.readings {
		copy := *reading
		result[addr] = &copy
	}

	return result
}

// GetStats returns cache statistics
func (c *ParameterCache) GetStats() map[string]interface{} {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	totalParams := len(c.readings)
	successfulParams := 0
	recentlyChanged := 0
	now := time.Now()

	for _, reading := range c.readings {
		if reading.Success {
			successfulParams++
		}
		// Consider "recently changed" as within last hour
		if now.Sub(reading.LastChange) < time.Hour {
			recentlyChanged++
		}
	}

	failedParams := totalParams - successfulParams
	successRate := 0.0
	if totalParams > 0 {
		successRate = float64(successfulParams) / float64(totalParams) * 100
	}

	return map[string]interface{}{
		"totalParams":     totalParams,
		"successful":      successfulParams,
		"failed":          failedParams,
		"successRate":     successRate,
		"recentlyChanged": recentlyChanged,
	}
}

// Clear clears all cached readings
func (c *ParameterCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.readings = make(map[uint16]*CachedReading)
}

// GetOldestRefresh returns the oldest refresh time among all cached readings
func (c *ParameterCache) GetOldestRefresh() *time.Time {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if len(c.readings) == 0 {
		return nil
	}

	var oldest *time.Time
	for _, reading := range c.readings {
		if oldest == nil || reading.LastRefresh.Before(*oldest) {
			oldest = &reading.LastRefresh
		}
	}

	return oldest
}

// GetRecentlyChanged returns addresses that changed within the given duration
func (c *ParameterCache) GetRecentlyChanged(since time.Duration) []uint16 {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var changed []uint16
	cutoff := time.Now().Add(-since)

	for addr, reading := range c.readings {
		if reading.LastChange.After(cutoff) {
			changed = append(changed, addr)
		}
	}

	return changed
}
