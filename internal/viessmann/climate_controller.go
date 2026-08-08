// Package viessmann - Climate control functionality for Home Assistant integration
package viessmann

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
)

// ClimateController handles climate control operations for Home Assistant integration
type ClimateController struct {
	reader *Reader
	logger LoggerInterface
}

// ClimateCommand represents a climate control command from Home Assistant
type ClimateCommand struct {
	Type        string  // "temperature", "mode"
	Zone        string  // "heating", "hotwater"
	Value       string  // temperature value or mode string
	Parameter   string  // specific parameter name if known
	Address     uint16  // specific address if known
}

// NewClimateController creates a new climate controller
func NewClimateController(reader *Reader) *ClimateController {
	return &ClimateController{
		reader: reader,
		logger: reader.logger,
	}
}

// ProcessClimateCommand processes a climate control command
func (cc *ClimateController) ProcessClimateCommand(command ClimateCommand) error {
	cc.logger.Info("Processing climate command: Type=%s, Zone=%s, Value=%s", 
		command.Type, command.Zone, command.Value)

	switch command.Type {
	case "temperature":
		return cc.setTemperature(command)
	case "mode":
		return cc.setMode(command)
	default:
		return errors.New(errors.ErrValidation, fmt.Sprintf("unknown climate command type: %s", command.Type))
	}
}

// setTemperature handles temperature setting commands
func (cc *ClimateController) setTemperature(command ClimateCommand) error {
	// Parse temperature value
	temperature, err := strconv.ParseFloat(command.Value, 64)
	if err != nil {
		return errors.Wrap(errors.ErrValidation, "invalid temperature value", err)
	}

	switch command.Zone {
	case "hotwater":
		return cc.setHotWaterTemperature(temperature)
	case "heating":
		return cc.setHeatingTemperature(temperature)
	default:
		return errors.New(errors.ErrValidation, fmt.Sprintf("unknown temperature zone: %s", command.Zone))
	}
}

// setHotWaterTemperature sets the hot water target temperature
func (cc *ClimateController) setHotWaterTemperature(temperature float64) error {
	// Validate temperature range for hot water
	if temperature < 35.0 || temperature > 65.0 {
		return errors.New(errors.ErrValidation, 
			fmt.Sprintf("hot water temperature %.1f°C out of range (35-65°C)", temperature))
	}

	// Find the hot water temperature setpoint parameter
	// Common Viessmann addresses for hot water temperature:
	// 0x6300 - Warmwasser Solltemperatur (most common)
	// 0x6301 - Alternative hot water setpoint
	hotWaterAddresses := []uint16{0x6300, 0x6301}

	var targetAddress uint16
	var targetDataPoint *config.DataPoint

	// Find the hot water setpoint parameter in loaded configuration
	dataPoints := cc.reader.GetDataPoints()
	for _, dp := range dataPoints {
		for _, addr := range hotWaterAddresses {
			if dp.Address == addr {
				// Check if this looks like a hot water temperature parameter
				nameLower := strings.ToLower(dp.Name)
				if strings.Contains(nameLower, "warmwasser") && 
				   (strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) {
					targetAddress = addr
					targetDataPoint = &dp
					break
				}
			}
		}
		if targetAddress != 0 {
			break
		}
	}

	// If not found in config, try to find by name pattern
	if targetAddress == 0 {
		for _, dp := range dataPoints {
			nameLower := strings.ToLower(dp.Name)
			if (strings.Contains(nameLower, "warmwasser") || strings.Contains(nameLower, "hot") || strings.Contains(nameLower, "ww")) &&
			   (strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target") || strings.Contains(nameLower, "setpoint")) {
				targetAddress = dp.Address
				targetDataPoint = &dp
				break
			}
		}
	}

	if targetAddress == 0 {
		return errors.New(errors.ErrNotFound, 
			"hot water temperature setpoint parameter not found in configuration")
	}

	cc.logger.Info("Setting hot water temperature to %.1f°C using address 0x%04X (%s)", 
		temperature, targetAddress, targetDataPoint.Name)

	// Convert temperature to the format expected by the Viessmann system
	// Most Viessmann systems expect temperature * 10 as integer (e.g., 45.5°C = 455)
	tempValue := int(temperature * 10)

	// Create the data to write (little-endian 16-bit integer)
	writeData := []byte{
		byte(tempValue & 0xFF),         // Low byte
		byte((tempValue >> 8) & 0xFF),  // High byte
	}

	// Write the temperature setpoint
	err := cc.writeParameter(targetAddress, writeData)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, 
			fmt.Sprintf("failed to write hot water temperature %.1f°C", temperature), err)
	}

	cc.logger.Info("Successfully set hot water temperature to %.1f°C", temperature)
	return nil
}

// setHeatingTemperature sets the heating target temperature
func (cc *ClimateController) setHeatingTemperature(temperature float64) error {
	// Validate temperature range for heating
	if temperature < 10.0 || temperature > 30.0 {
		return errors.New(errors.ErrValidation, 
			fmt.Sprintf("heating temperature %.1f°C out of range (10-30°C)", temperature))
	}

	// Common addresses for heating temperature setpoints:
	// 0x2306 - Raumtemperatur Normal Sollwert A1M1
	// 0x2307 - Raumtemperatur Reduziert Sollwert A1M1
	// 0x2544 - Vorlauftemperatur Sollwert A1M1
	heatingAddresses := []uint16{0x2306, 0x2544, 0x2307}

	var targetAddress uint16
	var targetDataPoint *config.DataPoint

	// Find the heating setpoint parameter
	dataPoints := cc.reader.GetDataPoints()
	for _, dp := range dataPoints {
		for _, addr := range heatingAddresses {
			if dp.Address == addr {
				nameLower := strings.ToLower(dp.Name)
				if (strings.Contains(nameLower, "raum") || strings.Contains(nameLower, "vorlauf")) &&
				   (strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) {
					targetAddress = addr
					targetDataPoint = &dp
					break
				}
			}
		}
		if targetAddress != 0 {
			break
		}
	}

	if targetAddress == 0 {
		return errors.New(errors.ErrNotFound, 
			"heating temperature setpoint parameter not found in configuration")
	}

	cc.logger.Info("Setting heating temperature to %.1f°C using address 0x%04X (%s)", 
		temperature, targetAddress, targetDataPoint.Name)

	// Convert temperature (similar format as hot water)
	tempValue := int(temperature * 10)
	writeData := []byte{
		byte(tempValue & 0xFF),
		byte((tempValue >> 8) & 0xFF),
	}

	err := cc.writeParameter(targetAddress, writeData)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, 
			fmt.Sprintf("failed to write heating temperature %.1f°C", temperature), err)
	}

	cc.logger.Info("Successfully set heating temperature to %.1f°C", temperature)
	return nil
}

// setMode handles mode setting commands
func (cc *ClimateController) setMode(command ClimateCommand) error {
	cc.logger.Info("Setting mode for zone %s to %s", command.Zone, command.Value)
	
	// Mode setting is more complex and system-specific
	// This is a placeholder implementation
	switch command.Zone {
	case "hotwater":
		return cc.setHotWaterMode(command.Value)
	case "heating":
		return cc.setHeatingMode(command.Value)
	default:
		return errors.New(errors.ErrValidation, fmt.Sprintf("unknown mode zone: %s", command.Zone))
	}
}

// setHotWaterMode sets hot water operating mode
func (cc *ClimateController) setHotWaterMode(mode string) error {
	// Hot water mode addresses (system-specific):
	// 0x6301 - Hot water operating mode
	modeAddress := uint16(0x6301)
	
	var modeValue byte
	switch strings.ToLower(mode) {
	case "off":
		modeValue = 0
	case "heat", "on":
		modeValue = 1
	case "auto":
		modeValue = 2
	default:
		return errors.New(errors.ErrValidation, fmt.Sprintf("unknown hot water mode: %s", mode))
	}

	writeData := []byte{modeValue}
	
	err := cc.writeParameter(modeAddress, writeData)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, 
			fmt.Sprintf("failed to set hot water mode to %s", mode), err)
	}

	cc.logger.Info("Successfully set hot water mode to %s", mode)
	return nil
}

// setHeatingMode sets heating operating mode
func (cc *ClimateController) setHeatingMode(mode string) error {
	// Heating mode addresses (system-specific):
	// 0x2323 - Betriebsart A1M1
	modeAddress := uint16(0x2323)
	
	var modeValue byte
	switch strings.ToLower(mode) {
	case "off":
		modeValue = 0
	case "heat", "normal":
		modeValue = 1
	case "auto":
		modeValue = 2
	case "eco", "reduced":
		modeValue = 3
	default:
		return errors.New(errors.ErrValidation, fmt.Sprintf("unknown heating mode: %s", mode))
	}

	writeData := []byte{modeValue}
	
	err := cc.writeParameter(modeAddress, writeData)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, 
			fmt.Sprintf("failed to set heating mode to %s", mode), err)
	}

	cc.logger.Info("Successfully set heating mode to %s", mode)
	return nil
}

// writeParameter writes data to a specific address on the Viessmann system
func (cc *ClimateController) writeParameter(address uint16, data []byte) error {
	// This requires extending the SerialCommunicator interface to support writing
	// For now, we'll use the reader's serial communicator interface
	
	// Check if the serial communicator supports writing
	if writer, ok := cc.reader.serialComm.(WriterInterface); ok {
		return writer.WriteToDevice(address, data)
	}

	// If direct writing is not supported, return an error
	return errors.New(errors.ErrCommunication, 
		"writing to device not supported by current serial communicator implementation")
}

// WriterInterface defines the interface for writing to the Viessmann device
type WriterInterface interface {
	WriteToDevice(address uint16, data []byte) error
}

// GetAvailableSetpoints returns information about available temperature setpoints
func (cc *ClimateController) GetAvailableSetpoints() map[string]SetpointInfo {
	setpoints := make(map[string]SetpointInfo)
	
	dataPoints := cc.reader.GetDataPoints()
	for _, dp := range dataPoints {
		nameLower := strings.ToLower(dp.Name)
		
		// Identify hot water setpoints
		if (strings.Contains(nameLower, "warmwasser") || strings.Contains(nameLower, "ww")) &&
		   (strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) {
			setpoints["hotwater"] = SetpointInfo{
				Name:        dp.Name,
				Address:     dp.Address,
				Zone:        "hotwater",
				MinValue:    35.0,
				MaxValue:    65.0,
				Unit:        "°C",
				Available:   true,
			}
		}
		
		// Identify heating setpoints
		if (strings.Contains(nameLower, "raum") || strings.Contains(nameLower, "vorlauf")) &&
		   (strings.Contains(nameLower, "soll") || strings.Contains(nameLower, "target")) &&
		   !strings.Contains(nameLower, "warmwasser") {
			setpoints["heating"] = SetpointInfo{
				Name:        dp.Name,
				Address:     dp.Address,
				Zone:        "heating",
				MinValue:    10.0,
				MaxValue:    30.0,
				Unit:        "°C",
				Available:   true,
			}
		}
	}
	
	return setpoints
}

// SetpointInfo contains information about a temperature setpoint
type SetpointInfo struct {
	Name      string  `json:"name"`
	Address   uint16  `json:"address"`
	Zone      string  `json:"zone"`
	MinValue  float64 `json:"min_value"`
	MaxValue  float64 `json:"max_value"`
	Unit      string  `json:"unit"`
	Available bool    `json:"available"`
}