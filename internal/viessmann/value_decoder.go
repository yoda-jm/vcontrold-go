// Package viessmann - Shared value decoding functionality
package viessmann

import (
	"fmt"
)

// ValueDecoder handles decoding of various Viessmann data formats
type ValueDecoder struct{}

// NewValueDecoder creates a new value decoder
func NewValueDecoder() *ValueDecoder {
	return &ValueDecoder{}
}

// ParseValueOriginal is the original parsing logic as fallback
func (vd *ValueDecoder) ParseValueOriginal(rawValue interface{}, dataType string) string {
	data, ok := rawValue.([]byte)
	if !ok {
		return fmt.Sprintf("RAW:%v", rawValue)
	}

	switch dataType {
	case "Byte":
		return fmt.Sprintf("%d", data[0])
	case "SInt": // Signed 16-bit, divide by 10 for temperature
		if len(data) >= 2 {
			raw := uint16(data[0]) | uint16(data[1])<<8
			signed := int16(raw)
			return fmt.Sprintf("%.1f", float64(signed)/10.0)
		}
	case "Int": // Unsigned 16-bit, divide by 10 for temperature
		if len(data) >= 2 {
			raw := uint16(data[0]) | uint16(data[1])<<8
			return fmt.Sprintf("%.1f", float64(raw)/10.0)
		}
	case "Int4": // 32-bit integer
		if len(data) >= 4 {
			raw := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
			return fmt.Sprintf("%d", raw)
		}
	case "Array":
		// Special handling for arrays (like date/time)
		if len(data) == 8 {
			return vd.ParseBCDDateTime(data)
		}
		return fmt.Sprintf("Array[%d]:%v", len(data), data)
	}

	return fmt.Sprintf("RAW:%v", rawValue)
}

// ParseBCDDateTime parses BCD-encoded date/time data (8 bytes)
func (vd *ValueDecoder) ParseBCDDateTime(data []byte) string {
	if len(data) < 8 {
		return ""
	}

	// Check for valid BCD data (each byte should be 0-99 in BCD format)
	for i := 0; i < 8; i++ {
		high := (data[i] >> 4) & 0x0F
		low := data[i] & 0x0F
		if high > 9 || low > 9 {
			// Not valid BCD, return empty
			return ""
		}
	}

	// Convert BCD to decimal values
	year := int(data[0]>>4)*10 + int(data[0]&0x0F)
	month := int(data[1]>>4)*10 + int(data[1]&0x0F)
	day := int(data[2]>>4)*10 + int(data[2]&0x0F)
	hour := int(data[3]>>4)*10 + int(data[3]&0x0F)
	minute := int(data[4]>>4)*10 + int(data[4]&0x0F)
	second := int(data[5]>>4)*10 + int(data[5]&0x0F)

	// Additional validation
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 {
		return ""
	}

	// Format as ISO 8601 date/time (assuming 20xx year)
	return fmt.Sprintf("20%02d-%02d-%02d %02d:%02d:%02d", year, month, day, hour, minute, second)
}

// DecodeCycleTime decodes cycle time data into a readable format.
//
// It does NOT convert to hours. That conversion belongs to the XML unit: CS
// carries <calc get="V/3600"/> and the entity "Stunden". A hard-coded
// `if raw > 3600 { raw / 3600 }` here opened a second path for the same
// conversion — two possible results for one reading depending on which path ran
// — and produced a "11066:21 h" format no MQTT consumer can read as a number.
//
// The raw value is returned as-is; the formatter, backed by the XML, is what
// gives it meaning.
func (vd *ValueDecoder) DecodeCycleTime(data []byte, dataType string) string {
	if dataType == "Int4" && len(data) >= 4 {
		raw := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
		return fmt.Sprintf("%d", raw)
	}

	if dataType == "Int" && len(data) >= 2 {
		raw := uint16(data[0]) | uint16(data[1])<<8
		return fmt.Sprintf("%d", raw)
	}

	return ""
}

// DecodeErrorState decodes error state data
func (vd *ValueDecoder) DecodeErrorState(data []byte, unitType string) string {
	if len(data) == 0 {
		return ""
	}

	// Single byte error states
	if len(data) == 1 {
		errorCode := data[0]
		switch errorCode {
		case 0x00:
			return "No Error"
		case 0x01:
			return "Temperature Sensor Error"
		case 0x02:
			return "Communication Error"
		case 0xFF:
			return "System Error"
		default:
			return fmt.Sprintf("Error Code %d", errorCode)
		}
	}

	// Multi-byte error states
	if len(data) >= 2 {
		errorCode := uint16(data[0]) | uint16(data[1])<<8
		return fmt.Sprintf("Error Code %d", errorCode)
	}

	return ""
}

// DecodeBCDTimestamp decodes timestamp data (similar to DateTime but for timestamp fields)
func (vd *ValueDecoder) DecodeBCDTimestamp(data []byte) string {
	// Delegate to ParseBCDDateTime for consistency
	return vd.ParseBCDDateTime(data)
}
