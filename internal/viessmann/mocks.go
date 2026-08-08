// Package viessmann - Mock implementations for testing
package viessmann

import (
	"fmt"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// MockSerialCommunicator is a mock implementation for testing
type MockSerialCommunicator struct {
	device        string
	responses     map[uint16][]byte
	errors        map[uint16]error
	callCount     int
	errorAddress  uint16
	errorResponse error
}

// NewMockSerialCommunicator creates a new mock serial communicator
func NewMockSerialCommunicator() *MockSerialCommunicator {
	return &MockSerialCommunicator{
		device:    "/dev/mock",
		responses: make(map[uint16][]byte),
		errors:    make(map[uint16]error),
	}
}

// SetResponse sets a mock response for a given address
func (m *MockSerialCommunicator) SetResponse(address uint16, data []byte) {
	m.responses[address] = data
}

// SetError sets a mock error for a given address
func (m *MockSerialCommunicator) SetError(address uint16, err error) {
	m.errors[address] = err
}

// SetWriteError sets a mock error for write operations to a specific address
func (m *MockSerialCommunicator) SetWriteError(address uint16, err error) {
	m.errorAddress = address
	m.errorResponse = err
}

// ReadFromDevice returns the mock response or error
func (m *MockSerialCommunicator) ReadFromDevice(address uint16, size int) ([]byte, error) {
	m.callCount++
	
	if err, exists := m.errors[address]; exists {
		return nil, err
	}
	
	if data, exists := m.responses[address]; exists {
		if len(data) >= size {
			return data[:size], nil
		}
		return data, nil
	}
	
	// Default response for unknown addresses
	return make([]byte, size), nil
}

// Close does nothing in mock
func (m *MockSerialCommunicator) Close() error {
	return nil
}

// GetDevice returns the mock device name
func (m *MockSerialCommunicator) GetDevice() string {
	return m.device
}

// GetCallCount returns the number of times ReadFromDevice was called
func (m *MockSerialCommunicator) GetCallCount() int {
	return m.callCount
}

// WriteToDevice mock implementation for writing to device
func (m *MockSerialCommunicator) WriteToDevice(address uint16, data []byte) error {
	m.callCount++
	
	if m.errorAddress != 0 && m.errorAddress == address {
		return m.errorResponse
	}
	
	// Mock successful write
	return nil
}

// MockAutoRefresher is a mock implementation for testing
type MockAutoRefresher struct {
	running bool
	status  map[string]interface{}
}

// NewMockAutoRefresher creates a new mock auto refresher
func NewMockAutoRefresher() *MockAutoRefresher {
	return &MockAutoRefresher{
		status: make(map[string]interface{}),
	}
}

// Start starts the mock refresher
func (m *MockAutoRefresher) Start() error {
	m.running = true
	return nil
}

// Stop stops the mock refresher
func (m *MockAutoRefresher) Stop() {
	m.running = false
}

// IsRunning returns the running state
func (m *MockAutoRefresher) IsRunning() bool {
	return m.running
}

// GetStatus returns the mock status
func (m *MockAutoRefresher) GetStatus() map[string]interface{} {
	m.status["running"] = m.running
	m.status["mock"] = true
	return m.status
}

// UpdateConfig updates the mock config
func (m *MockAutoRefresher) UpdateConfig(newConfig *config.AutoRefreshConfig) error {
	if newConfig == nil {
		return fmt.Errorf("config cannot be nil")
	}
	return nil
}

// MockMultiGroupRefresher is a mock implementation for testing
type MockMultiGroupRefresher struct {
	running bool
	status  map[string]interface{}
}

// NewMockMultiGroupRefresher creates a new mock multi-group refresher
func NewMockMultiGroupRefresher() *MockMultiGroupRefresher {
	return &MockMultiGroupRefresher{
		status: make(map[string]interface{}),
	}
}

// Start starts the mock refresher
func (m *MockMultiGroupRefresher) Start() error {
	m.running = true
	return nil
}

// Stop stops the mock refresher
func (m *MockMultiGroupRefresher) Stop() {
	m.running = false
}

// IsRunning returns the running state
func (m *MockMultiGroupRefresher) IsRunning() bool {
	return m.running
}

// GetStatus returns the mock status
func (m *MockMultiGroupRefresher) GetStatus() map[string]interface{} {
	m.status["running"] = m.running
	m.status["mock"] = true
	return m.status
}

// UpdateGroups updates the mock groups
func (m *MockMultiGroupRefresher) UpdateGroups(newGroups []config.RefreshGroup) error {
	return nil
}