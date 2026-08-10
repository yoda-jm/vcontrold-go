// Package xml handles XML parsing for vcontrold.xml and vito.xml
package xml

import (
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Unit represents a unit definition from vcontrold.xml
type Unit struct {
	Name   string `xml:"name,attr"`
	Abbrev string `xml:"abbrev"`
	Calc   struct {
		Get string `xml:"get,attr"`
		Set string `xml:"set,attr"`
	} `xml:"calc"`
	Type   string      `xml:"type"`
	Entity string      `xml:"entity"`
	Enums  []EnumEntry `xml:"enum"`
}

// EnumEntry represents an enum value mapping
type EnumEntry struct {
	Bytes string `xml:"bytes,attr"`
	Text  string `xml:"text,attr"`
}

// Units represents the units section from vcontrold.xml
type Units struct {
	Units []Unit `xml:"unit"`
}

// VControlXML represents the full vcontrold.xml structure
type VControlXML struct {
	Units Units `xml:"units"`
}

// UnitConverter holds unit conversion logic
type UnitConverter struct {
	units map[string]Unit
}

var globalUnitConverter *UnitConverter

// LoadUnits parses the vcontrold.xml file and loads unit definitions
func LoadUnits(xmlPath string) error {
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to read XML file: %v", err)
	}

	var vcontrol VControlXML
	if err := xml.Unmarshal(data, &vcontrol); err != nil {
		return fmt.Errorf("failed to parse XML: %v", err)
	}

	converter := &UnitConverter{
		units: make(map[string]Unit),
	}

	for _, unit := range vcontrol.Units.Units {
		converter.units[unit.Name] = unit
		if unit.Abbrev != "" {
			converter.units[unit.Abbrev] = unit
		}
	}

	globalUnitConverter = converter
	fmt.Printf("Loaded %d unit definitions from XML\n", len(vcontrol.Units.Units))
	return nil
}

// GetUnitByName returns a unit definition by name or abbreviation
func (uc *UnitConverter) GetUnitByName(name string) (Unit, bool) {
	unit, exists := uc.units[name]
	return unit, exists
}

// ConvertValue applies unit conversion to a raw value
func (uc *UnitConverter) ConvertValue(rawValue float64, unitName string) (float64, string, error) {
	unit, exists := uc.GetUnitByName(unitName)
	if !exists {
		return rawValue, "", fmt.Errorf("unit not found: %s", unitName)
	}

	// Apply the "get" calculation
	convertedValue, err := uc.evaluateCalc(unit.Calc.Get, rawValue)
	if err != nil {
		return rawValue, unit.Entity, fmt.Errorf("calculation error: %v", err)
	}

	return convertedValue, unit.Entity, nil
}

// ConvertEnumValue converts raw bytes to enum text
func (uc *UnitConverter) ConvertEnumValue(rawBytes []byte, unitName string) (string, error) {
	unit, exists := uc.GetUnitByName(unitName)
	if !exists {
		return "", fmt.Errorf("unit not found: %s", unitName)
	}

	if unit.Type != "enum" {
		return "", fmt.Errorf("unit %s is not an enum type", unitName)
	}

	return uc.lookupEnum(unit, rawBytes), nil
}

// LookupEnumText resolves bytes against a unit's enum table without requiring
// the unit to be declared as an enum.
//
// The ES unit is declared "errstate" yet carries an enum for its fault code,
// which is how the reference implementation reads it too.
func (uc *UnitConverter) LookupEnumText(rawBytes []byte, unitName string) (string, bool) {
	unit, exists := uc.GetUnitByName(unitName)
	if !exists || len(unit.Enums) == 0 {
		return "", false
	}
	text := uc.lookupEnum(unit, rawBytes)
	return text, text != ""
}

// lookupEnum matches bytes against a unit's enum entries.
func (uc *UnitConverter) lookupEnum(unit Unit, rawBytes []byte) string {
	// Convert bytes to hex string for comparison
	hexString := ""
	for _, b := range rawBytes {
		if hexString != "" {
			hexString += " "
		}
		hexString += fmt.Sprintf("%02X", b)
	}

	// Look for matching enum entry
	for _, enumEntry := range unit.Enums {
		if enumEntry.Bytes == "" {
			continue
		}

		// Compare hex strings (case insensitive)
		if strings.EqualFold(enumEntry.Bytes, hexString) {
			return enumEntry.Text
		}
	}

	// No match found, look for UNKNOWN default
	for _, enumEntry := range unit.Enums {
		if enumEntry.Bytes == "" && (enumEntry.Text == "UNKNOWN" || enumEntry.Text == "NOT OK") {
			return fmt.Sprintf("%s (0x%s)", enumEntry.Text, hexString)
		}
	}

	// No default found, return hex value
	return fmt.Sprintf("UNKNOWN (0x%s)", hexString)
}

// evaluateCalc evaluates a simple calculation expression like "V/10" or "V*2"
func (uc *UnitConverter) evaluateCalc(calc string, value float64) (float64, error) {
	if calc == "" || calc == "V" {
		return value, nil
	}

	// Handle common patterns like "V/10", "V*2", "V/2"
	re := regexp.MustCompile(`V\s*([+\-*/])\s*(\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(calc)

	if len(matches) != 3 {
		// Try to handle more complex expressions, but for now just return original value
		return value, fmt.Errorf("unsupported calculation: %s", calc)
	}

	operator := matches[1]
	operand, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return value, fmt.Errorf("invalid operand in calc: %s", matches[2])
	}

	switch operator {
	case "/":
		if operand == 0 {
			return value, fmt.Errorf("division by zero")
		}
		return value / operand, nil
	case "*":
		return value * operand, nil
	case "+":
		return value + operand, nil
	case "-":
		return value - operand, nil
	default:
		return value, fmt.Errorf("unsupported operator: %s", operator)
	}
}

// GetGlobalUnitConverter returns the global unit converter instance
func GetGlobalUnitConverter() *UnitConverter {
	return globalUnitConverter
}
