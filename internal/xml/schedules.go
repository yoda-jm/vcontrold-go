// Package xml - decoding the composite unit types: schedules and fault records.
//
// These two are not scaled numbers like UT or CS. They are packed structures,
// and their layout is defined by the reference implementation in
// vcontrold/src/unit.c rather than by anything in the XML itself. The XML only
// names the type: <type>cycletime</type> and <type>errstate</type>.
package xml

import (
	"fmt"
	"strings"
)

// unusedSlot marks a schedule slot that carries no period.
const unusedSlot = 0xFF

// DecodeCycleTime renders a weekday schedule.
//
// Eight bytes hold four on/off pairs. Each byte packs an hour in its top five
// bits and a count of ten-minute steps in its bottom three, so 0x2B is
// (0x2B & 0xF8) >> 3 = 5 hours and (0x2B & 7) * 10 = 30 minutes: 05:30.
//
// A pair whose first byte is 0xFF is unused, which is how a day with fewer than
// four periods is expressed.
func DecodeCycleTime(data []byte) string {
	if len(data) == 0 || len(data)%2 != 0 {
		return fmt.Sprintf("invalid schedule length: %d bytes", len(data))
	}

	var periods []string
	for i := 0; i < len(data); i += 2 {
		if data[i] == unusedSlot {
			continue
		}
		periods = append(periods, fmt.Sprintf("%02d:%02d-%02d:%02d",
			(data[i]&0xF8)>>3, (data[i]&7)*10,
			(data[i+1]&0xF8)>>3, (data[i+1]&7)*10))
	}

	if len(periods) == 0 {
		return "off all day"
	}
	return strings.Join(periods, ", ")
}

// CycleTimeHours returns how long a schedule keeps the circuit on, in hours.
//
// This is what makes a schedule comparable with a run-time counter: a loop
// programmed 05:30 to 22:00 is on for 16.5 hours a day, which is the figure
// that decides whether it is worth reprogramming.
func CycleTimeHours(data []byte) float64 {
	if len(data) == 0 || len(data)%2 != 0 {
		return 0
	}

	var minutes int
	for i := 0; i < len(data); i += 2 {
		if data[i] == unusedSlot {
			continue
		}
		start := int((data[i]&0xF8)>>3)*60 + int(data[i]&7)*10
		end := int((data[i+1]&0xF8)>>3)*60 + int(data[i+1]&7)*10
		if end > start {
			minutes += end - start
		}
	}
	return float64(minutes) / 60
}

// EncodeCycleTime packs periods back into the eight-byte form.
//
// Accepts what DecodeCycleTime produces, so a schedule can be read, edited and
// written back without a second notation. Minutes must be a multiple of ten:
// the format has three bits for them and cannot express anything finer, and
// silently rounding would write a schedule the operator did not ask for.
func EncodeCycleTime(text string, slots int) ([]byte, error) {
	if slots <= 0 || slots%2 != 0 {
		return nil, fmt.Errorf("a schedule needs an even number of bytes, got %d", slots)
	}

	data := make([]byte, slots)
	for i := range data {
		data[i] = unusedSlot
	}

	text = strings.TrimSpace(text)
	if text == "" || strings.EqualFold(text, "off all day") {
		return data, nil
	}

	periods := strings.Split(text, ",")
	if len(periods)*2 > slots {
		return nil, fmt.Errorf("%d periods do not fit in %d bytes", len(periods), slots)
	}

	for index, period := range periods {
		bounds := strings.Split(strings.TrimSpace(period), "-")
		if len(bounds) != 2 {
			return nil, fmt.Errorf("period %q is not of the form HH:MM-HH:MM", period)
		}
		for side, bound := range bounds {
			var hour, minute int
			if _, err := fmt.Sscanf(strings.TrimSpace(bound), "%d:%d", &hour, &minute); err != nil {
				return nil, fmt.Errorf("time %q is not of the form HH:MM", bound)
			}
			if hour < 0 || hour > 23 {
				return nil, fmt.Errorf("hour %d is out of range in %q", hour, bound)
			}
			if minute%10 != 0 || minute < 0 || minute > 50 {
				return nil, fmt.Errorf("minute %d in %q must be a multiple of ten: the "+
					"appliance stores minutes in ten-minute steps", minute, bound)
			}
			data[index*2+side] = byte(hour<<3) | byte(minute/10)
		}
	}

	return data, nil
}

// DecodeErrorState renders one or more fault records.
//
// Nine bytes each: a fault code, then eight bytes of BCD timestamp laid out as
// century, year, month, day, weekday, hour, minute, second. The code is looked
// up in the ES enum of the community XML.
func DecodeErrorState(data []byte) string {
	if len(data) == 0 || len(data)%9 != 0 {
		return fmt.Sprintf("invalid fault record length: %d bytes", len(data))
	}

	var records []string
	for i := 0; i < len(data); i += 9 {
		code := data[i]
		label := fmt.Sprintf("0x%02X", code)
		if converter := GetGlobalUnitConverter(); converter != nil {
			if text, ok := converter.LookupEnumText(data[i:i+1], "ES"); ok {
				label = text
			}
		}
		records = append(records, fmt.Sprintf("%s %s", decodeBCDTimestamp(data[i+1:i+9]), label))
	}

	return strings.Join(records, " | ")
}

// decodeBCDTimestamp renders the eight-byte BCD stamp that follows a fault code.
func decodeBCDTimestamp(data []byte) string {
	if len(data) != 8 {
		return "?"
	}

	bcd := func(b byte) int { return int(b>>4)*10 + int(b&0x0F) }

	// An empty slot reads as zeroes rather than as a reserved value.
	year := bcd(data[0])*100 + bcd(data[1])
	if year == 0 {
		return "(empty)"
	}

	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d",
		year, bcd(data[2]), bcd(data[3]), bcd(data[5]), bcd(data[6]), bcd(data[7]))
}
