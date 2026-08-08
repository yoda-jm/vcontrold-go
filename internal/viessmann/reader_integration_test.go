package viessmann

import (
	"fmt"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// TestReaderWithMockDependencies tests the Reader with mocked dependencies
func TestReaderWithMockDependencies(t *testing.T) {
	// Create test configuration with mocks
	testConfig := NewTestReaderConfig("/dev/test")
	
	// Create reader with injected dependencies
	reader := NewReaderWithDependencies(testConfig)
	
	// Verify reader was created correctly
	if reader.device != "/dev/test" {
		t.Errorf("Expected device '/dev/test', got %s", reader.device)
	}
	
	// Test that interfaces are properly injected
	if reader.logger == nil {
		t.Error("Logger was not injected")
	}
	
	if reader.metrics == nil {
		t.Error("Metrics was not injected")
	}
	
	if reader.cache == nil {
		t.Error("Cache was not injected")
	}
}

// TestReaderWithMockReadParameter tests reading parameters with mocked serial communication
func TestReaderWithMockReadParameter(t *testing.T) {
	// Setup
	testConfig := NewTestReaderConfig("/dev/test")
	mockComm := testConfig.SerialComm.(*MockSerialCommunicator)
	reader := NewReaderWithDependencies(testConfig)
	
	// Setup mock response for address 0x810
	mockComm.SetResponse(0x810, []byte{0x01, 0x23}) // Mock temperature data
	
	// Create test data point
	dp := config.DataPoint{
		Name:     "Test Temperature",
		Address:  0x810,
		DataType: "Int",
		Unit:     "°C",
		Size:     2,
	}
	
	// Test reading
	reading := reader.ReadParameter(dp, 0)
	
	// Verify reading was successful
	if !reading.Success {
		t.Errorf("Expected successful reading, got error: %s", reading.Error)
	}
	
	if reading.Name != "Test Temperature" {
		t.Errorf("Expected name 'Test Temperature', got %s", reading.Name)
	}
	
	if reading.Address != 0x810 {
		t.Errorf("Expected address 0x810, got 0x%04X", reading.Address)
	}
	
	// Verify the mock was called
	if mockComm.GetCallCount() != 1 {
		t.Errorf("Expected 1 call to ReadFromDevice, got %d", mockComm.GetCallCount())
	}
}

// TestReaderWithMockError tests error handling with mocked failures
func TestReaderWithMockError(t *testing.T) {
	// Setup
	testConfig := NewTestReaderConfig("/dev/test")
	mockComm := testConfig.SerialComm.(*MockSerialCommunicator)
	reader := NewReaderWithDependencies(testConfig)
	
	// Setup mock error for address 0x820
	mockComm.SetError(0x820, fmt.Errorf("communication timeout"))
	
	// Create test data point
	dp := config.DataPoint{
		Name:     "Test Parameter",
		Address:  0x820,
		DataType: "Byte",
		Unit:     "raw",
		Size:     1,
	}
	
	// Test reading
	reading := reader.ReadParameter(dp, 0)
	
	// Verify reading failed as expected
	if reading.Success {
		t.Error("Expected failed reading, got success")
	}
	
	if reading.Error == "" {
		t.Error("Expected error message, got empty string")
	}
	
	// Verify retry logic - should call twice
	if mockComm.GetCallCount() != 2 {
		t.Errorf("Expected 2 calls to ReadFromDevice (retry), got %d", mockComm.GetCallCount())
	}
}

// TestMetricsIntegration tests that metrics are properly recorded
func TestMetricsIntegration(t *testing.T) {
	// Setup
	testConfig := NewTestReaderConfig("/dev/test")
	mockComm := testConfig.SerialComm.(*MockSerialCommunicator)
	metrics := NewMetrics()
	testConfig.Metrics = metrics
	reader := NewReaderWithDependencies(testConfig)
	
	// Setup successful response
	mockComm.SetResponse(0x800, []byte{0x42})
	
	// Create test data point
	dp := config.DataPoint{
		Name:     "Test Parameter",
		Address:  0x800,
		DataType: "Byte",
		Unit:     "raw",
		Size:     1,
	}
	
	// Test reading
	reading := reader.ReadParameter(dp, 0)
	
	// Verify metrics were recorded
	if reading.Success {
		if metrics.GetReadCount() != 1 {
			t.Errorf("Expected read count 1, got %d", metrics.GetReadCount())
		}
		
		if metrics.GetErrorCount() != 0 {
			t.Errorf("Expected error count 0, got %d", metrics.GetErrorCount())
		}
		
		if metrics.GetAverageLatency() <= 0 {
			t.Error("Expected positive latency, got zero or negative")
		}
	}
}

// TestCacheIntegration tests that cache operations work correctly
func TestCacheIntegration(t *testing.T) {
	// Setup
	testConfig := NewTestReaderConfig("/dev/test")
	mockComm := testConfig.SerialComm.(*MockSerialCommunicator)
	reader := NewReaderWithDependencies(testConfig)
	
	// Setup mock response
	mockComm.SetResponse(0x900, []byte{0xFF, 0xEE})
	
	// Create test data point
	dp := config.DataPoint{
		Name:     "Cached Parameter",
		Address:  0x900,
		DataType: "Int",
		Unit:     "test",
		Size:     2,
	}
	
	// First read - should hit serial communication
	reading1 := reader.ReadParameter(dp, 0)
	if !reading1.Success {
		t.Fatalf("First reading failed: %s", reading1.Error)
	}
	
	// Verify it was cached
	cachedReading, exists := reader.GetCachedReading(0x900)
	if !exists {
		t.Error("Reading was not cached")
	}
	
	if cachedReading == nil {
		t.Fatal("Cached reading is nil")
	}
	
	if cachedReading.FormattedValue == "" {
		t.Error("Cached reading has empty formatted value")
	}
	
	// Verify cache stats
	stats := reader.GetCacheStats()
	if stats == nil {
		t.Error("Cache stats are nil")
	}
	
	// Clear cache and verify
	reader.ClearCache()
	_, exists = reader.GetCachedReading(0x900)
	if exists {
		t.Error("Cache was not cleared")
	}
}

// TestAutoRefresherIntegration tests auto refresher setup
func TestAutoRefresherIntegration(t *testing.T) {
	// Setup
	testConfig := NewTestReaderConfig("/dev/test")
	reader := NewReaderWithDependencies(testConfig)
	
	// Create mock auto refresh config (unused but shows typical config structure)
	_ = &config.AutoRefreshConfig{
		Enabled:   true,
		Interval:  "1s",
		Addresses: []string{"0x800", "0x801"},
	}
	
	// Create mock auto refresher
	mockRefresher := NewMockAutoRefresher()
	reader.autoRefresher = mockRefresher
	
	// Test setup (this will create a new AutoRefresher, but we want to test the mock)
	err := mockRefresher.Start()
	if err != nil {
		t.Errorf("Starting mock refresher failed: %v", err)
	}
	
	// Verify refresher was configured
	if !mockRefresher.IsRunning() {
		t.Error("Auto refresher should be running")
	}
	
	// Test status retrieval
	status := reader.GetAutoRefreshStatus()
	if status == nil {
		t.Error("Auto refresh status is nil")
	}
	
	if !status["running"].(bool) {
		t.Error("Auto refresher should report as running")
	}
}