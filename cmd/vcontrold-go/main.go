package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
	"github.com/yourusername/vcontrold-go/internal/web"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

func main() {
	cfg, singleAddress, err := parseConfiguration()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	

	if err := initializeXMLSystems(cfg); err != nil {
		fmt.Printf("Warning: XML initialization issues: %v\n", err)
	}

	reader, err := initializeReader(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize reader: %v", err)
	}
	defer reader.Close()

	if err := runApplication(reader, cfg, singleAddress); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}

// readSingleAddress reads a single parameter by hex address
func readSingleAddress(reader *viessmann.Reader, addressStr string, cfg *config.Config) error {
	// Parse the address
	addressStr = strings.TrimPrefix(strings.ToLower(addressStr), "0x")
	address, err := strconv.ParseUint(addressStr, 16, 16)
	if err != nil {
		return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid hex address '%s'", addressStr), err)
	}
	
	fmt.Printf("Reading parameter at address 0x%04X...\n", address)
	
	// Try to find this address in loaded configuration
	dataPoints := reader.GetDataPoints()
	var foundDataPoint *config.DataPoint
	var foundIndex int
	
	for i, dp := range dataPoints {
		if dp.Address == uint16(address) {
			foundDataPoint = &dp
			foundIndex = i
			break
		}
	}
	
	if foundDataPoint != nil {
		// Found in configuration - use existing parameter
		fmt.Printf("Found parameter: %s\n", foundDataPoint.Name)
		fmt.Printf("Type: %s, Size: %d bytes\n", foundDataPoint.DataType, foundDataPoint.Size)
		
		reading := reader.ReadParameter(*foundDataPoint, foundIndex)
		
		if reading.Success {
			fmt.Printf("Value: %s\n", reading.FormattedValue)
			if cfg.Verbose {
				fmt.Printf("Raw value: %v\n", reading.RawValue)
				fmt.Printf("Unit type: %s (source: %s)\n", reading.UnitType, reading.UnitSource)
				fmt.Printf("In XML: %t\n", reading.InXML)
			}
		} else {
			fmt.Printf("Error: %s\n", reading.Error)
			return errors.New(errors.ErrCommunication, "parameter reading failed")
		}
	} else {
		// Not found in configuration - try to read with defaults
		fmt.Printf("Address not found in configuration, attempting to read with defaults...\n")
		
		// Get unit info from XML mappings if available
		unitInfo := xml.GetDetailedUnitInfo(uint16(address), "Unknown Parameter")
		
		// Determine size and data type based on unit info or use defaults
		size := 2        // Default size
		dataType := "Int" // Default type
		
		if unitInfo.VitoUnit != "" {
			// Use size from vito.xml if available
			if _, exists := xml.GetUnitForAddress(uint16(address)); exists {
				// Could parse length from vito.xml mapping here if needed
			}
		}
		
		// Create a temporary data point
		tempDP := config.DataPoint{
			Name:     fmt.Sprintf("Unknown_0x%04X", address),
			Address:  uint16(address),
			Size:     size,
			DataType: dataType,
		}
		
		fmt.Printf("Using defaults: Type=%s, Size=%d bytes\n", dataType, size)
		
		reading := reader.ReadParameter(tempDP, -1)
		
		if reading.Success {
			fmt.Printf("Value: %s\n", reading.FormattedValue)
			if cfg.Verbose {
				fmt.Printf("Raw value: %v\n", reading.RawValue)
				fmt.Printf("Unit type: %s (source: %s)\n", reading.UnitType, reading.UnitSource)
				fmt.Printf("In XML: %t\n", reading.InXML)
			}
		} else {
			fmt.Printf("Error: %s\n", reading.Error)
			return errors.New(errors.ErrCommunication, "parameter reading failed")
		}
	}
	
	return nil
}

// parseConfiguration handles command line flag parsing and YAML config loading
func parseConfiguration() (*config.Config, string, error) {
	cfg := config.New()
	
	// Command line flags
	var yamlConfigFile string
	flag.StringVar(&yamlConfigFile, "yaml-config", "", "YAML configuration file (overrides other flags)")
	flag.StringVar(&cfg.Device, "device", "/dev/ttyUSB0", "Serial device to use")
	flag.StringVar(&cfg.ConfigFile, "config", "", "V200KW2 configuration file to parse (supports zip: zipfile.zip:internal/file.txt)")
	flag.StringVar(&cfg.VControlXML, "vcontrol-xml", "", "Path to vcontrold.xml unit definitions")
	flag.StringVar(&cfg.VitoXML, "vito-xml", "", "Path to vito.xml address mappings")
	flag.StringVar(&cfg.WebPort, "port", "8080", "HTTP server port")
	flag.BoolVar(&cfg.WebMode, "web", false, "Start web server instead of CLI")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Show verbose output")
	
	// Single parameter reading mode
	var singleAddress string
	flag.StringVar(&singleAddress, "address", "", "Read single parameter by hex address (e.g., 0x5525)")
	
	flag.Parse()
	
	// Load YAML config if provided (overrides command line flags)
	if yamlConfigFile != "" {
		yamlConfig, err := config.LoadYamlConfig(yamlConfigFile)
		if err != nil {
			return nil, "", errors.Wrap(errors.ErrConfig, "failed to load YAML config", err)
		}
		
		// Override config with YAML values
		cfg.Device = yamlConfig.Device
		cfg.ConfigFile = yamlConfig.ConfigFile
		cfg.VControlXML = yamlConfig.VControlXML
		cfg.VitoXML = yamlConfig.VitoXML
		cfg.WebPort = yamlConfig.WebPort
		cfg.WebMode = yamlConfig.WebMode
		cfg.Verbose = yamlConfig.Verbose
		cfg.YamlConfigFile = yamlConfigFile
		cfg.YamlConfig = yamlConfig
	}

	// Set default XML paths if not provided
	if cfg.VControlXML == "" {
		cfg.VControlXML = "xml/vcontrold.xml"
	}
	if cfg.VitoXML == "" {
		cfg.VitoXML = "xml/vito.xml"
	}
	
	// Show configuration if verbose
	if cfg.Verbose {
		fmt.Printf("Configuration:\n")
		fmt.Printf("  Device: %s\n", cfg.Device)
		fmt.Printf("  Config File: %s\n", cfg.ConfigFile)
		fmt.Printf("  VControl XML: %s\n", cfg.VControlXML)
		fmt.Printf("  Vito XML: %s\n", cfg.VitoXML)
		fmt.Printf("  Web Port: %s\n", cfg.WebPort)
		fmt.Printf("  Web Mode: %t\n", cfg.WebMode)
	}

	return cfg, singleAddress, nil
}

// initializeXMLSystems loads XML unit definitions and address mappings
func initializeXMLSystems(cfg *config.Config) error {
	var errorList []string
	
	// Load XML unit definitions
	if err := xml.LoadUnits(cfg.VControlXML); err != nil {
		errorList = append(errorList, fmt.Sprintf("Could not load XML units from %s: %v", cfg.VControlXML, err))
		fmt.Printf("Warning: Could not load XML units from %s: %v\n", cfg.VControlXML, err)
		fmt.Printf("Continuing with basic unit formatting...\n")
	}

	// Load vito.xml address mappings
	if err := xml.LoadAddressMappings(cfg.VitoXML); err != nil {
		errorList = append(errorList, fmt.Sprintf("Could not load vito.xml from %s: %v", cfg.VitoXML, err))
		fmt.Printf("Warning: Could not load vito.xml from %s: %v\n", cfg.VitoXML, err)
		fmt.Printf("Continuing without address-unit mappings...\n")
	}
	
	if len(errorList) > 0 {
		return errors.New(errors.ErrConfig, fmt.Sprintf("XML initialization issues: %s", strings.Join(errorList, "; ")))
	}
	
	return nil
}

// initializeReader creates and configures the Viessmann reader
func initializeReader(cfg *config.Config) (*viessmann.Reader, error) {
	// Initialize Viessmann reader
	reader, err := viessmann.NewReader(cfg.Device)
	if err != nil {
		return nil, errors.Wrap(errors.ErrCommunication, "failed to create Viessmann reader", err)
	}

	// Load configuration if provided
	if cfg.ConfigFile != "" {
		if err := reader.LoadConfig(cfg.ConfigFile); err != nil {
			reader.Close()
			return nil, errors.Wrap(errors.ErrConfig, "failed to load config file", err)
		}
	}
	
	// Setup auto refresh if YAML config is available
	if cfg.YamlConfig != nil && cfg.YamlConfig.AutoRefresh.Enabled {
		if err := reader.SetupAutoRefresh(&cfg.YamlConfig.AutoRefresh); err != nil {
			log.Printf("Warning: Failed to setup auto refresh: %v", err)
		} else {
			log.Printf("Auto refresh configured for %d addresses every %s", 
				len(cfg.YamlConfig.AutoRefresh.Addresses), cfg.YamlConfig.AutoRefresh.Interval)
		}
	}
	
	// Setup multi-group refresh if YAML config has refresh groups
	if cfg.YamlConfig != nil && len(cfg.YamlConfig.RefreshGroups) > 0 {
		if err := reader.SetupMultiGroupRefresh(cfg.YamlConfig.RefreshGroups); err != nil {
			log.Printf("Warning: Failed to setup multi-group refresh: %v", err)
		} else {
			enabledGroups := 0
			totalAddresses := 0
			for _, group := range cfg.YamlConfig.RefreshGroups {
				if group.Enabled {
					enabledGroups++
					totalAddresses += len(group.Addresses)
				}
			}
			log.Printf("Multi-group refresh configured: %d enabled groups with %d total addresses", 
				enabledGroups, totalAddresses)
		}
	}
	
	return reader, nil
}

// runApplication handles the main application logic based on mode
func runApplication(reader *viessmann.Reader, cfg *config.Config, singleAddress string) error {
	// Handle single address reading mode
	if singleAddress != "" {
		return readSingleAddress(reader, singleAddress, cfg)
	}

	if cfg.WebMode {
		// Start web server
		server := web.NewServer(reader, cfg)
		fmt.Printf("Starting web server on port %s...\n", cfg.WebPort)
		return server.Start()
	} else {
		// CLI mode
		return runCLIMode(reader, cfg)
	}
}

// runCLIMode handles command line interface mode
func runCLIMode(reader *viessmann.Reader, cfg *config.Config) error {
	fmt.Println("vcontrold-go - Viessmann V200KW2 Reader")
	fmt.Println("Use -web flag to start web interface")
	
	if cfg.ConfigFile == "" {
		fmt.Println("No config file specified. Use -config flag or -web for web interface.")
		os.Exit(1)
	}
	
	// Read all parameters once in CLI mode
	readings, err := reader.ReadAll()
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "failed to read parameters", err)
	}
	
	fmt.Printf("Successfully read %d parameters:\n", len(readings))
	for _, reading := range readings {
		fmt.Printf("%s (0x%04X): %s\n", reading.Name, reading.Address, reading.FormattedValue)
	}
	
	return nil
}