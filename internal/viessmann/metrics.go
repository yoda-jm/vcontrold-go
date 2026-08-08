// Package viessmann - Performance metrics and monitoring
package viessmann

import (
	"sync"
	"time"
)

// Metrics implements performance monitoring for the viessmann package
type Metrics struct {
	mu               sync.RWMutex
	readCount        int64
	errorCount       int64
	totalReadLatency time.Duration
	cacheHitRate     float64
	lastResetTime    time.Time
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		lastResetTime: time.Now(),
	}
}

// RecordReadLatency records the time taken for a read operation
func (m *Metrics) RecordReadLatency(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalReadLatency += duration
}

// IncrementReadCount increments the total number of read operations
func (m *Metrics) IncrementReadCount() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readCount++
}

// IncrementErrorCount increments the total number of errors
func (m *Metrics) IncrementErrorCount() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorCount++
}

// RecordCacheHitRate records the current cache hit rate
func (m *Metrics) RecordCacheHitRate(rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cacheHitRate = rate
}

// GetMetrics returns a snapshot of all metrics
func (m *Metrics) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	avgLatency := time.Duration(0)
	if m.readCount > 0 {
		avgLatency = time.Duration(int64(m.totalReadLatency) / m.readCount)
	}

	successRate := float64(0)
	if m.readCount > 0 {
		successRate = float64(m.readCount-m.errorCount) / float64(m.readCount) * 100
	}

	return map[string]interface{}{
		"read_count":         m.readCount,
		"error_count":        m.errorCount,
		"total_read_latency": m.totalReadLatency.String(),
		"avg_read_latency":   avgLatency.String(),
		"cache_hit_rate":     m.cacheHitRate,
		"success_rate":       successRate,
		"uptime":             time.Since(m.lastResetTime).String(),
	}
}

// Reset resets all metrics to zero
func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readCount = 0
	m.errorCount = 0
	m.totalReadLatency = 0
	m.cacheHitRate = 0
	m.lastResetTime = time.Now()
}

// GetReadCount returns the current read count (thread-safe)
func (m *Metrics) GetReadCount() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.readCount
}

// GetErrorCount returns the current error count (thread-safe)
func (m *Metrics) GetErrorCount() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errorCount
}

// GetAverageLatency returns the average read latency (thread-safe)
func (m *Metrics) GetAverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.readCount == 0 {
		return 0
	}
	return time.Duration(int64(m.totalReadLatency) / m.readCount)
}

// NoOpMetrics is a metrics implementation that does nothing - useful for testing
type NoOpMetrics struct{}

// NewNoOpMetrics creates a metrics instance that discards all data
func NewNoOpMetrics() *NoOpMetrics {
	return &NoOpMetrics{}
}

// RecordReadLatency does nothing
func (n *NoOpMetrics) RecordReadLatency(duration time.Duration) {}

// IncrementReadCount does nothing
func (n *NoOpMetrics) IncrementReadCount() {}

// IncrementErrorCount does nothing
func (n *NoOpMetrics) IncrementErrorCount() {}

// RecordCacheHitRate does nothing
func (n *NoOpMetrics) RecordCacheHitRate(rate float64) {}

// GetMetrics returns empty metrics
func (n *NoOpMetrics) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"read_count":         0,
		"error_count":        0,
		"total_read_latency": "0s",
		"avg_read_latency":   "0s",
		"cache_hit_rate":     0.0,
		"success_rate":       0.0,
		"uptime":             "0s",
	}
}
