// Package viessmann - Value formatting and conversion handling
package viessmann

import (
	"fmt"
	"strings"

	"github.com/yoda-jm/vcontrold-go/internal/xml"
)

// ValueFormatter handles conversion of raw data to formatted values using XML units
type ValueFormatter struct {
	decoder *ValueDecoder
}

// NewValueFormatter creates a new value formatter
func NewValueFormatter() *ValueFormatter {
	return &ValueFormatter{
		decoder: NewValueDecoder(),
	}
}

// FormatValueWithAllSources formats a value using all available unit sources
func (vf *ValueFormatter) FormatValueWithAllSources(rawValue interface{}, dataType, paramName string, address uint16) string {
	converter := xml.GetGlobalUnitConverter()
	if converter == nil {
		return vf.decoder.ParseValueOriginal(rawValue, dataType)
	}

	data, ok := rawValue.([]byte)
	if !ok {
		return vf.decoder.ParseValueOriginal(rawValue, dataType)
	}

	// Check for Viessmann error values from XML first
	if xml.IsErrorValue(address, data) {
		return "ERROR"
	}

	// Get the best unit type using all sources
	unitType, foundInXML := xml.GetBestUnitForParameter(address, paramName)
	if unitType == "" {
		return vf.decoder.ParseValueOriginal(rawValue, dataType)
	}

	// Try specialized formatters first
	if result := vf.trySpecializedFormatting(converter, data, dataType, unitType, foundInXML); result != "" {
		return result
	}

	// Fall back to numeric conversion and formatting
	return vf.formatNumericValue(converter, data, dataType, unitType, foundInXML)
}

// trySpecializedFormatting handles special unit types that need custom processing
func (vf *ValueFormatter) trySpecializedFormatting(converter *xml.UnitConverter, data []byte, dataType, unitType string, foundInXML bool) string {
	// Check if this is an enum type first
	if unit, exists := converter.GetUnitByName(unitType); exists && unit.Type == "enum" {
		if enumText, err := converter.ConvertEnumValue(data, unitType); err == nil {
			if !foundInXML {
				return enumText + " (not in XML)"
			}
			return enumText
		}
	}

	// Handle SystemTime
	if unitType == "SystemTime" {
		return vf.formatSystemTime(data, dataType, foundInXML)
	}

	// Handle CycleTime
	if unitType == "CycleTime" {
		return vf.formatCycleTime(data, dataType, foundInXML)
	}

	// Handle ErrorState
	if unitType == "ErrorState" {
		return vf.formatErrorState(data, unitType, foundInXML)
	}

	return "" // No specialized formatting applied
}

// formatSystemTime handles system time formatting
func (vf *ValueFormatter) formatSystemTime(data []byte, dataType string, foundInXML bool) string {
	if dataType == "Array" && len(data) == 8 {
		if dateTime := vf.decoder.ParseBCDDateTime(data); dateTime != "" {
			if !foundInXML {
				return dateTime + " (not in XML)"
			}
			return dateTime
		}
	}
	return "" // Fall back to original parsing
}

// formatCycleTime handles cycle time formatting
func (vf *ValueFormatter) formatCycleTime(data []byte, dataType string, foundInXML bool) string {
	cycleTime := vf.decoder.DecodeCycleTime(data, dataType)
	if cycleTime != "" {
		if !foundInXML {
			return cycleTime + " (not in XML)"
		}
		return cycleTime
	}
	return "" // Fall back to original parsing
}

// formatErrorState handles error state formatting
func (vf *ValueFormatter) formatErrorState(data []byte, unitType string, foundInXML bool) string {
	errorState := vf.decoder.DecodeErrorState(data, unitType)
	if errorState != "" {
		if !foundInXML {
			return errorState + " (not in XML)"
		}
		return errorState
	}
	return "" // Fall through to enum processing
}

// formatNumericValue converts raw data to numeric value and applies unit conversion
func (vf *ValueFormatter) formatNumericValue(converter *xml.UnitConverter, data []byte, dataType, unitType string, foundInXML bool) string {
	// Convert raw bytes to numeric value
	numericValue := vf.bytesToNumeric(data, dataType)
	if numericValue == nil {
		return "" // Unable to convert, fall back to original
	}

	// Apply unit conversion
	convertedValue, entity, err := converter.ConvertValue(*numericValue, unitType)
	if err != nil {
		return "" // Conversion failed, fall back to original
	}

	// Format the converted value
	formattedValue := vf.formatConvertedValue(convertedValue, entity, unitType)

	// Add XML status annotation if needed
	return vf.addXMLStatusAnnotation(formattedValue, foundInXML)
}

// bytesToNumeric converts byte data to numeric value based on data type
func (vf *ValueFormatter) bytesToNumeric(data []byte, dataType string) *float64 {
	var numericValue float64

	switch dataType {
	case "Byte":
		if len(data) > 0 {
			numericValue = float64(data[0])
		} else {
			return nil
		}
	case "SInt": // Signed 16-bit
		if len(data) >= 2 {
			raw := uint16(data[0]) | uint16(data[1])<<8 // Little-endian
			signed := int16(raw)
			numericValue = float64(signed)
		} else {
			return nil
		}
	case "Int": // Unsigned 16-bit
		if len(data) >= 2 {
			raw := uint16(data[0]) | uint16(data[1])<<8 // Little-endian
			numericValue = float64(raw)
		} else {
			return nil
		}
	case "Int4": // 32-bit integer
		if len(data) >= 4 {
			raw := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
			numericValue = float64(raw)
		} else {
			return nil
		}
	default:
		return nil // Unsupported data type
	}

	return &numericValue
}

// formatConvertedValue formats the converted numeric value with appropriate units
func (vf *ValueFormatter) formatConvertedValue(convertedValue float64, entity, unitType string) string {
	if entity != "" {
		if strings.Contains(unitType, "Temperatur") {
			return fmt.Sprintf("%.1f %s", convertedValue, entity)
		} else if unitType == "CounterS" {
			// Seconds converted to hours by the XML (CS is V/3600), so the
			// underlying resolution is one second. Rounding to 0.1 h would be a
			// six-minute step -- about the length of a whole hot-water cycle,
			// which would make every single-cycle measurement meaningless.
			// Four decimals keeps roughly one third of a second.
			return fmt.Sprintf("%.4f h", convertedValue)
		} else if unitType == "Brenner Stunden" {
			return fmt.Sprintf("%.1f h", convertedValue)
		} else if unitType == "Prozent" {
			return fmt.Sprintf("%.1f %%", convertedValue)
		} else {
			return fmt.Sprintf("%.1f %s", convertedValue, entity)
		}
	}

	// No entity, format based on type
	if unitType == "ReturnStatus" || unitType == "Counter" {
		return fmt.Sprintf("%.0f", convertedValue)
	}
	return fmt.Sprintf("%.1f", convertedValue)
}

// addXMLStatusAnnotation adds "(not in XML)" annotation if the unit wasn't found in XML
func (vf *ValueFormatter) addXMLStatusAnnotation(formattedValue string, foundInXML bool) string {
	if !foundInXML {
		return formattedValue + " (not in XML)"
	}
	return formattedValue
}
