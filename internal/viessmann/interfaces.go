// Package viessmann - Interface definitions for dependency injection and testability
package viessmann

import (
	"time"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// SerialCommunicatorInterface defines the contract for serial communication
type SerialCommunicatorInterface interface {
	ReadFromDevice(address uint16, size int) ([]byte, error)
	WriteToDevice(address uint16, data []byte) error
	Close() error
	GetDevice() string
}

// ValueFormatterInterface defines the contract for value formatting
type ValueFormatterInterface interface {
	FormatValueWithAllSources(rawValue interface{}, dataType, paramName string, address uint16) string
}

// ValueDecoderInterface defines the contract for value decoding
type ValueDecoderInterface interface {
	ParseValueOriginal(rawValue interface{}, dataType string) string
	ParseBCDDateTime(data []byte) string
	DecodeCycleTime(data []byte, dataType string) string
	DecodeErrorState(data []byte, unitType string) string
	DecodeBCDTimestamp(data []byte) string
}

// ParameterCacheInterface defines the contract for parameter caching
type ParameterCacheInterface interface {
	Get(address uint16) (*CachedReading, bool)
	Set(address uint16, reading Reading)
	GetAll() map[uint16]*CachedReading
	GetStats() map[string]interface{}
	Clear()
	GetRecentlyChanged(since time.Duration) []uint16
	AddCallback(callback CacheCallback)
	RemoveAllCallbacks()
}

// AutoRefresherInterface defines the contract for auto refresh functionality
type AutoRefresherInterface interface {
	Start() error
	Stop()
	IsRunning() bool
	GetStatus() map[string]interface{}
	UpdateConfig(newConfig *config.AutoRefreshConfig) error
}

// MultiGroupRefresherInterface defines the contract for multi-group refresh
type MultiGroupRefresherInterface interface {
	Start() error
	Stop()
	IsRunning() bool
	GetStatus() map[string]interface{}
	UpdateGroups(newGroups []config.RefreshGroup) error
}

// ReaderInterface defines the contract for the main Reader functionality
type ReaderInterface interface {
	LoadConfig(configFile string) error
	GetDataPoints() []config.DataPoint
	ReadAll() ([]Reading, error)
	ReadParameter(dp config.DataPoint, index int) Reading

	// Cache operations
	GetCachedReading(address uint16) (*CachedReading, bool)
	GetAllCachedReadings() map[uint16]*CachedReading
	GetCacheStats() map[string]interface{}
	GetRecentlyChanged(since time.Duration) []uint16
	ClearCache()

	// Refresh management
	SetupAutoRefresh(autoConfig *config.AutoRefreshConfig) error
	GetAutoRefreshStatus() map[string]interface{}
	UpdateAutoRefreshConfig(newConfig *config.AutoRefreshConfig) error
	SetupMultiGroupRefresh(groups []config.RefreshGroup) error
	GetMultiGroupRefreshStatus() map[string]interface{}
	UpdateMultiGroupRefresh(newGroups []config.RefreshGroup) error

	// Cache callbacks
	AddCacheCallback(callback CacheCallback)
	RemoveAllCacheCallbacks()

	// Lifecycle
	Close() error
}

// LoggerInterface defines the contract for logging functionality
type LoggerInterface interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// MetricsInterface defines the contract for performance monitoring
type MetricsInterface interface {
	RecordReadLatency(duration time.Duration)
	IncrementReadCount()
	IncrementErrorCount()
	RecordCacheHitRate(rate float64)
	GetMetrics() map[string]interface{}
}
