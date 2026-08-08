// Package viessmann - Configuration validation functionality
package viessmann

import (
	"fmt"
	"os"
	"strings"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
)

// ConfigValidator provides configuration validation functionality
type ConfigValidator struct {
	logger LoggerInterface
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(logger LoggerInterface) *ConfigValidator {
	return &ConfigValidator{
		logger: logger,
	}
}

// ValidateReaderConfig validates a complete reader configuration
func (cv *ConfigValidator) ValidateReaderConfig(config *ReaderConfig) error {
	if config == nil {
		return errors.New(errors.ErrValidation, "reader config cannot be nil")
	}
	
	// Validate device
	if err := cv.ValidateDevice(config.Device); err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid device configuration", err)
	}
	
	// Validate required dependencies
	if config.SerialComm == nil {
		return errors.New(errors.ErrValidation, "serial communicator is required")
	}
	
	if config.Formatter == nil {
		return errors.New(errors.ErrValidation, "value formatter is required")
	}
	
	if config.Decoder == nil {
		return errors.New(errors.ErrValidation, "value decoder is required")
	}
	
	if config.Cache == nil {
		return errors.New(errors.ErrValidation, "parameter cache is required")
	}
	
	if config.Logger == nil {
		return errors.New(errors.ErrValidation, "logger is required")
	}
	
	if config.Metrics == nil {
		return errors.New(errors.ErrValidation, "metrics is required")
	}
	
	cv.logger.Debug("Reader configuration validation passed")
	return nil
}

// ValidateDevice validates device configuration
func (cv *ConfigValidator) ValidateDevice(device string) error {
	if device == "" {
		return errors.New(errors.ErrValidation, "device path cannot be empty")
	}
	
	// Check if device path looks valid (basic validation)
	if !strings.HasPrefix(device, "/dev/") && !strings.Contains(device, "mock") && !strings.Contains(device, "test") {
		return errors.New(errors.ErrValidation, "device path should start with /dev/ or be a test device")
	}
	
	// For real devices, check if they exist (skip for mock/test devices)
	if strings.HasPrefix(device, "/dev/") {
		if _, err := os.Stat(device); os.IsNotExist(err) {
			cv.logger.Warn("Device %s does not exist - proceeding anyway for potential creation", device)
		}
	}
	
	return nil
}

// ValidateDataPoints validates a slice of data points
func (cv *ConfigValidator) ValidateDataPoints(dataPoints []config.DataPoint) error {
	if len(dataPoints) == 0 {
		return errors.New(errors.ErrValidation, "at least one data point is required")
	}
	
	// Track addresses to detect duplicates
	addresses := make(map[uint16]string)
	
	for i, dp := range dataPoints {
		if err := cv.ValidateDataPoint(dp, i); err != nil {
			return errors.Wrap(errors.ErrValidation, fmt.Sprintf("data point %d validation failed", i), err)
		}
		
		// Check for duplicate addresses (warn but don't fail - multiple parameters can share addresses)
		if existingName, exists := addresses[dp.Address]; exists {
			cv.logger.Warn("Duplicate address 0x%04X found in '%s' and '%s' - this may be intentional for different interpretations", 
				dp.Address, existingName, dp.Name)
		} else {
			addresses[dp.Address] = dp.Name
		}
	}
	
	cv.logger.Info("Validated %d data points successfully", len(dataPoints))
	return nil
}

// ValidateDataPoint validates a single data point
func (cv *ConfigValidator) ValidateDataPoint(dp config.DataPoint, index int) error {
	// Name validation
	if strings.TrimSpace(dp.Name) == "" {
		return errors.New(errors.ErrValidation, "data point name cannot be empty")
	}
	
	// Address validation (basic range check)
	if dp.Address == 0 {
		return errors.New(errors.ErrValidation, "data point address cannot be zero")
	}
	
	// DataType validation
	validDataTypes := map[string]bool{
		"Byte":   true,
		"SByte":  true,  // Signed byte
		"SInt":   true,
		"Int":    true,
		"Int4":   true,
		"Array":  true,
		"String": true,  // String data type
		"Float":  true,  // Float data type
	}
	
	if !validDataTypes[dp.DataType] {
		return errors.New(errors.ErrValidation, 
			fmt.Sprintf("invalid data type '%s', must be one of: Byte, SByte, SInt, Int, Int4, Array, String, Float", dp.DataType))
	}
	
	// Size validation
	if dp.Size <= 0 {
		return errors.New(errors.ErrValidation, "data point size must be greater than zero")
	}
	
	// Size consistency validation
	expectedSizes := map[string]int{
		"Byte":   1,
		"SByte":  1,
		"SInt":   2,
		"Int":    2,
		"Int4":   4,
		"Float":  4,
		"Array":  0, // Variable size
		"String": 0, // Variable size
	}
	
	if expectedSize, exists := expectedSizes[dp.DataType]; exists && expectedSize > 0 {
		if dp.Size != expectedSize {
			cv.logger.Warn("Data point '%s' has size %d but type '%s' typically uses size %d", 
				dp.Name, dp.Size, dp.DataType, expectedSize)
		}
	}
	
	return nil
}

// ValidateAutoRefreshConfig validates auto refresh configuration
func (cv *ConfigValidator) ValidateAutoRefreshConfig(config *config.AutoRefreshConfig) error {
	if config == nil {
		return errors.New(errors.ErrValidation, "auto refresh config cannot be nil")
	}
	
	if !config.Enabled {
		return nil // No further validation needed for disabled config
	}
	
	// Validate interval
	if _, err := config.GetRefreshDuration(); err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid refresh interval", err)
	}
	
	// Validate addresses
	if err := config.ValidateAddresses(); err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid addresses in auto refresh config", err)
	}
	
	if len(config.Addresses) == 0 {
		return errors.New(errors.ErrValidation, "at least one address is required when auto refresh is enabled")
	}
	
	cv.logger.Debug("Auto refresh configuration validation passed")
	return nil
}

// ValidateRefreshGroups validates refresh group configuration
func (cv *ConfigValidator) ValidateRefreshGroups(groups []config.RefreshGroup) error {
	if len(groups) == 0 {
		return nil // Empty groups are valid
	}
	
	// Track group names to detect duplicates
	groupNames := make(map[string]bool)
	
	for i, group := range groups {
		if err := cv.ValidateRefreshGroup(group, i); err != nil {
			return errors.Wrap(errors.ErrValidation, fmt.Sprintf("refresh group %d validation failed", i), err)
		}
		
		// Check for duplicate group names
		if groupNames[group.Name] {
			return errors.New(errors.ErrValidation, fmt.Sprintf("duplicate group name '%s'", group.Name))
		}
		groupNames[group.Name] = true
	}
	
	cv.logger.Debug("Validated %d refresh groups successfully", len(groups))
	return nil
}

// ValidateRefreshGroup validates a single refresh group
func (cv *ConfigValidator) ValidateRefreshGroup(group config.RefreshGroup, index int) error {
	// Name validation
	if strings.TrimSpace(group.Name) == "" {
		return errors.New(errors.ErrValidation, "refresh group name cannot be empty")
	}
	
	if !group.Enabled {
		return nil // No further validation needed for disabled groups
	}
	
	// Validate interval
	if _, err := group.GetRefreshDuration(); err != nil {
		return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid interval for group '%s'", group.Name), err)
	}
	
	// Validate addresses
	if err := group.ValidateAddresses(); err != nil {
		return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid addresses in group '%s'", group.Name), err)
	}
	
	if len(group.Addresses) == 0 {
		return errors.New(errors.ErrValidation, fmt.Sprintf("group '%s' must have at least one address when enabled", group.Name))
	}
	
	return nil
}