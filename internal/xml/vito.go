// Package xml handles vito.xml address mappings and unit detection
package xml

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// VitoCommand represents a command from vito.xml
type VitoCommand struct {
	Name        string `xml:"name,attr"`
	ProtCmd     string `xml:"protocmd,attr"`
	Addr        string `xml:"addr"`
	Len         string `xml:"len"`
	Unit        string `xml:"unit"`
	Error       string `xml:"error"`
	Description string `xml:"description"`
}

// VitoXML represents the full vito.xml structure
type VitoXML struct {
	Commands []VitoCommand `xml:"commands>command"`
}

// AddressMappingEntry represents a mapping from address to unit
type AddressMappingEntry struct {
	Address      uint16
	Length       int
	Unit         string
	Description  string
	ErrorPattern []byte // Parsed error bytes from XML
}

// UnitInfo represents unit detection information
type UnitInfo struct {
	UnitType   string
	FoundInXML bool
	Source     string // "vito.xml", "name", "pattern"
	VitoUnit   string // Original vito.xml abbreviation (if any)
}

var globalAddressMappings map[uint16]AddressMappingEntry

// LoadAddressMappings parses vito.xml and creates address-to-unit mappings
func LoadAddressMappings(xmlPath string) error {
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to read vito.xml: %v", err)
	}

	var vito VitoXML
	if err := xml.Unmarshal(data, &vito); err != nil {
		return fmt.Errorf("failed to parse vito.xml: %v", err)
	}

	globalAddressMappings = make(map[uint16]AddressMappingEntry)

	for _, cmd := range vito.Commands {
		if cmd.Addr == "" || cmd.Unit == "" {
			continue
		}

		// Parse address (hex string like "0800" or "2301")
		addr, err := strconv.ParseUint(cmd.Addr, 16, 16)
		if err != nil {
			continue
		}

		// Parse length
		length := 2 // Default
		if cmd.Len != "" {
			if l, err := strconv.Atoi(cmd.Len); err == nil {
				length = l
			}
		}

		// Parse error pattern if present
		var errorPattern []byte
		if cmd.Error != "" {
			errorPattern = parseErrorPattern(cmd.Error)
		}

		mapping := AddressMappingEntry{
			Address:      uint16(addr),
			Length:       length,
			Unit:         cmd.Unit,
			Description:  cmd.Description,
			ErrorPattern: errorPattern,
		}

		globalAddressMappings[uint16(addr)] = mapping
	}

	fmt.Printf("Loaded %d address mappings from vito.xml\n", len(globalAddressMappings))
	return nil
}

// LoadAddressMappingOverrides applies an override file on top of the vito.xml
// mappings, without ever modifying vito.xml itself.
//
// The community XML is authoritative, but it describes a family of appliances
// rather than one specimen: some lengths are wrong for a given machine. On the
// Vitotronic 200 KW2, getBrennerStarts (0x088A) is declared as 2 bytes while
// the counter is really 4 — read short it caps at 65535 and wraps to zero.
//
// The file follows exactly the vito.xml grammar. Only the fields present are
// overridden; a <command> whose address is unknown is added. That leaves
// `make deps` free to refresh vito.xml without losing anything.
func LoadAddressMappingOverrides(xmlPath string) error {
	if xmlPath == "" {
		return nil
	}

	data, err := os.ReadFile(xmlPath)
	if err != nil {
		return fmt.Errorf("failed to read overrides %s: %v", xmlPath, err)
	}

	var vito VitoXML
	if err := xml.Unmarshal(data, &vito); err != nil {
		return fmt.Errorf("failed to parse overrides %s: %v", xmlPath, err)
	}

	if globalAddressMappings == nil {
		globalAddressMappings = make(map[uint16]AddressMappingEntry)
	}

	applied := 0
	for _, cmd := range vito.Commands {
		if cmd.Addr == "" {
			continue
		}

		addr, err := strconv.ParseUint(cmd.Addr, 16, 16)
		if err != nil {
			return fmt.Errorf("overrides %s: invalid address %q", xmlPath, cmd.Addr)
		}

		// Start from the upstream entry when there is one, so only what the
		// file mentions explicitly gets overridden.
		mapping, exists := globalAddressMappings[uint16(addr)]
		mapping.Address = uint16(addr)

		if cmd.Len != "" {
			length, err := strconv.Atoi(cmd.Len)
			if err != nil || length <= 0 {
				return fmt.Errorf("overrides %s: invalid length %q for address %s", xmlPath, cmd.Len, cmd.Addr)
			}
			mapping.Length = length
		} else if !exists {
			return fmt.Errorf("overrides %s: address %s is absent from vito.xml and declares no <len>", xmlPath, cmd.Addr)
		}

		if cmd.Unit != "" {
			mapping.Unit = cmd.Unit
		}
		if cmd.Description != "" {
			mapping.Description = cmd.Description
		}
		if cmd.Error != "" {
			mapping.ErrorPattern = parseErrorPattern(cmd.Error)
		}

		globalAddressMappings[uint16(addr)] = mapping
		applied++

		action := "overrode"
		if !exists {
			action = "added"
		}
		fmt.Printf("  overrides: %s 0x%04X (%s) -> %d bytes, unit %s\n",
			action, mapping.Address, cmd.Name, mapping.Length, mapping.Unit)
	}

	fmt.Printf("Applied %d address overrides from %s\n", applied, xmlPath)
	return nil
}

// parseErrorPattern converts error string like "05 05" to byte slice
func parseErrorPattern(errorStr string) []byte {
	if errorStr == "" {
		return nil
	}

	// Split by spaces and parse hex bytes
	parts := strings.Fields(errorStr)
	var pattern []byte

	for _, part := range parts {
		if b, err := strconv.ParseUint(part, 16, 8); err == nil {
			pattern = append(pattern, byte(b))
		}
	}

	return pattern
}

// IsErrorValue checks if the raw data matches known error patterns for the address
func IsErrorValue(address uint16, rawData []byte) bool {
	if globalAddressMappings == nil {
		return false
	}

	mapping, exists := globalAddressMappings[address]
	if !exists || len(mapping.ErrorPattern) == 0 {
		return false
	}

	// Check if raw data starts with the error pattern
	if len(rawData) >= len(mapping.ErrorPattern) {
		for i, expectedByte := range mapping.ErrorPattern {
			if rawData[i] != expectedByte {
				return false
			}
		}
		return true
	}

	return false
}

// GetUnitForAddress returns the unit type for a given address from vito.xml
func GetUnitForAddress(address uint16) (string, bool) {
	if globalAddressMappings == nil {
		return "", false
	}

	mapping, exists := globalAddressMappings[address]
	if exists {
		return mapping.Unit, true
	}

	return "", false
}

// GetAddressMapping returns the full vito.xml entry for an address.
//
// The length the XML carries is authoritative: guessing it (two bytes by
// default) silently truncates four-byte counters such as getBrennerStunden1.
func GetAddressMapping(address uint16) (AddressMappingEntry, bool) {
	if globalAddressMappings == nil {
		return AddressMappingEntry{}, false
	}

	mapping, exists := globalAddressMappings[address]
	if !exists {
		return AddressMappingEntry{}, false
	}

	return mapping, true
}

// GetBestUnitForParameter determines the best unit using all three sources
func GetBestUnitForParameter(address uint16, paramName string) (string, bool) {
	info := GetDetailedUnitInfo(address, paramName)
	return info.UnitType, info.FoundInXML
}

// GetDetailedUnitInfo returns detailed unit detection information
func GetDetailedUnitInfo(address uint16, paramName string) UnitInfo {
	// Priority 1: Check vito.xml for exact address match (MUST take precedence!)
	if vitoUnit, exists := GetUnitForAddress(address); exists {
		if xmlUnit := ConvertVitoUnitToXMLUnit(vitoUnit); xmlUnit != "" {
			return UnitInfo{
				UnitType:   xmlUnit,
				FoundInXML: true,
				Source:     "vito.xml",
				VitoUnit:   vitoUnit,
			}
		}
	}

	// Priority 2: Use parameter name inference ONLY if not found in vito.xml
	if inferredUnit := InferUnitFromName(paramName); inferredUnit != "" {
		return UnitInfo{
			UnitType:   inferredUnit,
			FoundInXML: false,
			Source:     "name",
			VitoUnit:   "",
		}
	}

	// Priority 3: Fallback based on common address patterns
	unit := inferUnitFromAddressPattern(address, paramName)
	return UnitInfo{
		UnitType:   unit,
		FoundInXML: false,
		Source:     "pattern",
		VitoUnit:   "",
	}
}

// ConvertVitoUnitToXMLUnit maps vito.xml unit abbreviations to vcontrold.xml unit names
func ConvertVitoUnitToXMLUnit(vitoUnit string) string {
	switch vitoUnit {
	case "UT":
		return "Temperatur" // V/10 temperature
	case "UT1", "UT1U":
		return "Temperatur 1Byte unsigned" // V/2 temperature
	case "UTI":
		return "Temperatur 1Byte ganzzahlig" // V temperature
	case "UN":
		return "Neigung" // V/10 heating curve slope
	case "ST":
		return "Status" // May be enum or simple status
	case "BA":
		return "BetriebsArt" // Operating mode enum
	case "RT":
		return "ReturnStatus" // Status enum
	case "ES":
		return "ErrorState" // Error state enum
	case "BH":
		return "Brenner Stunden" // Hours calculation
	case "CT":
		return "CycleTime" // Cycle time
	case "TI":
		return "SystemTime" // System time
	case "P":
		return "Prozent" // Percentage
	case "BST", "ESG":
		return "ReturnStatus" // Various status types
	case "CS":
		return "CounterS" // Counter in seconds
	case "CO":
		return "Counter" // Simple counter
	case "DT", "HKT":
		return "DeviceType" // Device/heating circuit types
	case "PR1":
		return "Prozent 1 Byte ganzzahlig" // Single byte percentage
	default:
		return ""
	}
}

// InferUnitFromName tries to guess the unit type from parameter name
func InferUnitFromName(name string) string {
	nameLower := strings.ToLower(name)

	// HIGHEST PRIORITY: Error states - must come first to avoid conflicts with other patterns
	if strings.Contains(nameLower, "error") || strings.Contains(nameLower, "fehler") ||
		strings.Contains(nameLower, "störung") {
		return "ErrorState"
	}

	// Operating mode/status - these are typically enums
	if strings.Contains(nameLower, "betriebsart") || strings.Contains(nameLower, "operating mode") ||
		strings.Contains(nameLower, "aktuelle betriebsart") {
		return "BetriebsArt" // Enum type
	}

	// General status fields (might be enums)
	if strings.Contains(nameLower, "status") && !strings.Contains(nameLower, "temperatur") {
		return "ReturnStatus" // Try enum first
	}

	// Temperature-related
	if strings.Contains(nameLower, "temperatur") || strings.Contains(nameLower, "temp") {
		return "Temperatur" // Will use V/10 conversion
	}

	// Heating curve slope (Neigung) - needs V/10 conversion
	if strings.Contains(nameLower, "neigung") || strings.Contains(nameLower, "heizkennlinie") {
		return "Neigung" // Will use V/10 conversion
	}

	// Percentage-related
	if strings.Contains(nameLower, "prozent") || strings.Contains(nameLower, "%") {
		return "Prozent"
	}

	// Counter/Hours-related
	if strings.Contains(nameLower, "stunden") || strings.Contains(nameLower, "hours") ||
		strings.Contains(nameLower, "betriebsstunden") {
		return "Brenner Stunden"
	}

	// Starts/Counter
	if strings.Contains(nameLower, "starts") || strings.Contains(nameLower, "zähler") {
		return "Counter"
	}

	// System time
	if strings.Contains(nameLower, "datum") || strings.Contains(nameLower, "zeit") ||
		strings.Contains(nameLower, "date") || strings.Contains(nameLower, "time") {
		return "SystemTime"
	}

	// Device/Burner type might be enum
	if strings.Contains(nameLower, "brennertyp") || strings.Contains(nameLower, "device") ||
		strings.Contains(nameLower, "typ") {
		return "DeviceType"
	}

	// Default to no specific unit
	return ""
}

// inferUnitFromAddressPattern makes educated guesses based on address ranges
func inferUnitFromAddressPattern(address uint16, paramName string) string {
	// Temperature addresses typically in these ranges
	if (address >= 0x0800 && address <= 0x089F) || // 0x08xx range
		(address >= 0x5500 && address <= 0x559F) { // 0x55xx range
		return "Temperatur"
	}

	// Operating modes typically in 0x23xx, 0x33xx, 0x25xx, 0x35xx
	if (address >= 0x2300 && address <= 0x23FF) ||
		(address >= 0x3300 && address <= 0x33FF) ||
		(address >= 0x2500 && address <= 0x25FF) ||
		(address >= 0x3500 && address <= 0x35FF) {

		// Check if it's likely an operating mode
		nameLower := strings.ToLower(paramName)
		if strings.Contains(nameLower, "betriebsart") || strings.Contains(nameLower, "mode") {
			return "BetriebsArt"
		}
	}

	// Hot water addresses often in 0x63xx, 0x65xx
	if address >= 0x6300 && address <= 0x65FF {
		if strings.Contains(strings.ToLower(paramName), "temperatur") {
			return "Temperatur"
		}
	}

	// Pump/Status addresses often in specific ranges
	if (address >= 0x0840 && address <= 0x084F) || // Pump status range
		(address >= 0x2900 && address <= 0x29FF) || // M1 pumps
		(address >= 0x3900 && address <= 0x39FF) { // M2 pumps
		return "ReturnStatus"
	}

	return ""
}

// GetXMLOnlyParameters finds interesting parameters from vito.xml that aren't in config file
func GetXMLOnlyParameters(existingAddresses map[uint16]bool) []AddressMappingEntry {
	if globalAddressMappings == nil {
		return nil
	}

	var xmlOnlyParams []AddressMappingEntry

	// Look for interesting unit types in vito.xml
	interestingUnits := map[string]bool{
		"CT":  true, // CycleTime
		"ES":  true, // ErrorState
		"BST": true, // Various status
		"DT":  true, // DeviceType
		"ESG": true, // Status
	}

	for address, mapping := range globalAddressMappings {
		// Skip if address already exists in config
		if existingAddresses[address] {
			continue
		}

		// Only add parameters with interesting unit types
		if !interestingUnits[mapping.Unit] {
			continue
		}

		xmlOnlyParams = append(xmlOnlyParams, mapping)
	}

	return xmlOnlyParams
}
