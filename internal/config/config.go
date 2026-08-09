// Package config handles application configuration
package config

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Config holds application configuration
type Config struct {
	Device           string
	ConfigFile       string
	VControlXML      string
	VitoXML          string
	VitoOverridesXML string
	WebPort          string
	WebMode          bool
	Verbose          bool
	LogLevel         string
	MQTTBroker       string
	MQTTPort         int
	MQTTUsername     string
	MQTTPassword     string
	MQTTTopic        string
	YamlConfigFile   string      // Path to YAML config file
	YamlConfig       *YamlConfig // Loaded YAML configuration
}

// New creates a new configuration with defaults
func New() *Config {
	return &Config{
		Device:    "/dev/ttyUSB0",
		WebPort:   "8080",
		WebMode:   false,
		Verbose:   false,
		MQTTPort:  1883,
		MQTTTopic: "viessmann",
	}
}

// DataPoint represents a parameter to read from the heating system
type DataPoint struct {
	Name       string   `json:"name"`
	Address    uint16   `json:"address"`
	Size       int      `json:"size"`
	DataType   string   `json:"dataType"`
	Unit       string   `json:"unit"`
	AliasNames []string `json:"aliasNames,omitempty"`
}

// openConfigFile opens a file that can be either a regular file or inside a zip archive
// Format: "../optolink-splitter/DP_Listen_2.zip:DP_V200KW2_6.txt" or just "path/to/file.txt"
func openConfigFile(filename string) (io.ReadCloser, error) {
	// Check if filename contains zip syntax (zipfile.zip:internal/file.txt)
	if strings.Contains(filename, ".zip:") {
		parts := strings.SplitN(filename, ".zip:", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid zip file format, expected 'zipfile.zip:internal/file.txt'")
		}

		zipPath := parts[0] + ".zip"
		internalPath := parts[1]

		// Open the zip file
		zipReader, err := zip.OpenReader(zipPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open zip file %s: %v", zipPath, err)
		}

		// Find the internal file
		for _, f := range zipReader.File {
			if f.Name == internalPath {
				rc, err := f.Open()
				if err != nil {
					zipReader.Close()
					return nil, fmt.Errorf("failed to open file %s in zip %s: %v", internalPath, zipPath, err)
				}

				// Return a custom ReadCloser that closes both the file and zip reader
				return &zipFileReader{rc: rc, zipReader: zipReader}, nil
			}
		}

		zipReader.Close()
		return nil, fmt.Errorf("file %s not found in zip %s", internalPath, zipPath)
	}

	// Regular file
	return os.Open(filename)
}

// zipFileReader wraps a zip file entry and ensures both the entry and zip reader are closed
type zipFileReader struct {
	rc        io.ReadCloser
	zipReader *zip.ReadCloser
}

func (z *zipFileReader) Read(p []byte) (n int, err error) {
	return z.rc.Read(p)
}

func (z *zipFileReader) Close() error {
	z.rc.Close()
	return z.zipReader.Close()
}

// ParseConfig reads a V200KW2 configuration file (supports both simple and structured formats)
// Supports both regular files and files within zip archives using syntax: "zipfile.zip:internal/file.txt"
func ParseConfig(filename string) ([]DataPoint, error) {
	file, err := openConfigFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer file.Close()

	// Try to detect format by reading first few lines
	scanner := bufio.NewScanner(file)
	isV200KW2Format := false

	// Read first few lines to detect format
	for i := 0; i < 10 && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "V200KW2") || strings.Contains(line, "~0x") {
			isV200KW2Format = true
			break
		}
	}

	// Reopen file for actual parsing
	file.Close()
	file, err = openConfigFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen config file: %v", err)
	}
	defer file.Close()

	if isV200KW2Format {
		return parseV200KW2Config(file)
	} else {
		return parseSimpleConfig(file)
	}
}

// parseSimpleConfig parses simple format config files
func parseSimpleConfig(file io.Reader) ([]DataPoint, error) {
	var dataPoints []DataPoint
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		dp, err := parseSimpleConfigLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("error parsing line %d: %v", lineNum, err)
		}

		if dp != nil {
			dataPoints = append(dataPoints, *dp)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	return dataPoints, nil
}

func parseSimpleConfigLine(line string, lineNum int) (*DataPoint, error) {
	// Split by whitespace but handle quoted strings
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid line format, expected at least 4 fields")
	}

	// Parse address (hex format like 0x0800)
	addrStr := parts[1]
	if !strings.HasPrefix(addrStr, "0x") && !strings.HasPrefix(addrStr, "0X") {
		return nil, fmt.Errorf("address must be in hex format (0x....)")
	}

	addr, err := strconv.ParseUint(addrStr[2:], 16, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid address format: %v", err)
	}

	// Parse size
	size, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid size: %v", err)
	}

	// Extract name and handle potential quotes
	name := parts[0]
	if strings.HasPrefix(name, "\"") && strings.HasSuffix(name, "\"") {
		name = strings.Trim(name, "\"")
	}

	dataPoint := &DataPoint{
		Name:     name,
		Address:  uint16(addr),
		Size:     size,
		DataType: parts[3],
		Unit:     "",
	}

	// Handle additional fields (aliases, units, etc.)
	if len(parts) > 4 {
		for i := 4; i < len(parts); i++ {
			field := parts[i]
			if strings.Contains(field, "=") {
				// Handle key=value pairs
				kv := strings.SplitN(field, "=", 2)
				if len(kv) == 2 {
					switch strings.ToLower(kv[0]) {
					case "unit":
						dataPoint.Unit = kv[1]
					case "alias":
						dataPoint.AliasNames = append(dataPoint.AliasNames, kv[1])
					}
				}
			} else {
				// Treat as alias
				dataPoint.AliasNames = append(dataPoint.AliasNames, field)
			}
		}
	}

	return dataPoint, nil
}

// parseV200KW2Config parses the structured V200KW2 configuration format
func parseV200KW2Config(file io.Reader) ([]DataPoint, error) {
	var dataPoints []DataPoint
	scanner := bufio.NewScanner(file)

	// Regex to match parameter lines: [InternalName~0xADDR (Type)]
	paramRegex := regexp.MustCompile(`\[([^~]+)~(0x[0-9A-Fa-f]{4})\s+\(([^)]+)\)\]`)
	hiddenRegex := regexp.MustCompile(`HIDDEN:\(([^)]+)\)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines, comments, and section headers
		if len(line) == 0 || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "V200KW2") || strings.HasPrefix(line, "===") {
			continue
		}

		// Look for parameter definitions
		if strings.Contains(line, "[") && strings.Contains(line, "~0x") {
			matches := paramRegex.FindStringSubmatch(line)
			if len(matches) >= 4 {
				// Extract address
				addrStr := strings.TrimPrefix(matches[2], "0x")
				addr, err := strconv.ParseUint(addrStr, 16, 16)
				if err != nil {
					continue
				}

				// Extract name from the line (before the bracket)
				namePart := strings.Split(line, "[")[0]
				name := strings.TrimSpace(strings.TrimPrefix(namePart, "- "))

				// Clean up name by removing ID numbers in parentheses
				if idx := strings.Index(name, " ("); idx != -1 {
					name = strings.TrimSpace(name[:idx])
				}

				// Determine data type and size
				dataType := strings.TrimSpace(matches[3])
				size := getDataTypeSize(dataType)
				unit := inferUnit(name, dataType)

				// Check for HIDDEN condition
				hiddenCondition := ""
				hiddenMatches := hiddenRegex.FindStringSubmatch(line)
				if len(hiddenMatches) >= 2 {
					hiddenCondition = hiddenMatches[1]
				}

				// For now, we'll include all parameters (ignore HIDDEN conditions)
				// In a full implementation, you'd evaluate the hidden conditions
				isAvailable := hiddenCondition == "" // Simple heuristic

				if isAvailable || len(dataPoints) < 100 { // Limit to prevent too many params
					dataPoint := DataPoint{
						Name:       name,
						Address:    uint16(addr),
						Size:       size,
						DataType:   dataType,
						Unit:       unit,
						AliasNames: []string{matches[1]}, // Add internal name as alias
					}

					dataPoints = append(dataPoints, dataPoint)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %v", err)
	}

	return dataPoints, nil
}

// getDataTypeSize returns the size in bytes for each data type
func getDataTypeSize(dataType string) int {
	switch strings.ToLower(dataType) {
	case "byte", "sbyte":
		return 1
	case "int", "sint":
		return 2
	case "int4":
		return 4
	case "array":
		return 8 // Default for arrays like date/time
	default:
		return 2 // Default to 2 bytes
	}
}

// inferUnit tries to determine the unit based on parameter name and type
func inferUnit(name, dataType string) string {
	nameLower := strings.ToLower(name)

	if strings.Contains(nameLower, "temperatur") || strings.Contains(nameLower, "temp") {
		return "°C"
	}
	if strings.Contains(nameLower, "druck") || strings.Contains(nameLower, "pressure") {
		return "bar"
	}
	if strings.Contains(nameLower, "prozent") || strings.Contains(nameLower, "%") {
		return "%"
	}
	if strings.Contains(nameLower, "stunden") || strings.Contains(nameLower, "hours") {
		return "h"
	}

	return "" // No unit
}
