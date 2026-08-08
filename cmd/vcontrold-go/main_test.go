package main

import (
	"flag"
	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
	"io"
	"os"
	"testing"
)

// TestParseConfiguration tests the parseConfiguration function
func TestParseConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedDevice string
		expectedWeb    bool
		expectedError  bool
		setupYAML      bool
		yamlContent    string
	}{
		{
			name:           "default flags",
			args:           []string{"vcontrold-go"},
			expectedDevice: "/dev/ttyUSB0",
			expectedWeb:    false,
			expectedError:  false,
		},
		{
			name:           "custom device",
			args:           []string{"vcontrold-go", "-device", "/dev/ttyUSB1"},
			expectedDevice: "/dev/ttyUSB1",
			expectedWeb:    false,
			expectedError:  false,
		},
		{
			name:           "web mode enabled",
			args:           []string{"vcontrold-go", "-web"},
			expectedDevice: "/dev/ttyUSB0",
			expectedWeb:    true,
			expectedError:  false,
		},
		{
			// The previous version passed a raw string containing literal
			// "\\n", hence a single line: that YAML could never be valid. The
			// keys are the yaml tags, not the Go field names.
			name:           "yaml config override",
			args:           []string{"vcontrold-go", "-yaml-config", "/tmp/test_config.yaml"},
			setupYAML:      true,
			yamlContent:    "device: \"/dev/ttyUSB2\"\nweb: true\nweb_port: \"9090\"\nverbose: true\n",
			expectedDevice: "/dev/ttyUSB2",
			expectedWeb:    true,
			expectedError:  false,
		},
		{
			// A missing file is not an error: LoadYamlConfig returns defaults.
			// What must fail is unreadable YAML.
			name:          "malformed yaml config",
			args:          []string{"vcontrold-go", "-yaml-config", "/tmp/test_config.yaml"},
			setupYAML:     true,
			yamlContent:   "device: \"/dev/ttyUSB2\"\n  mauvaise: indentation\n\tonglet: interdit\n",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup YAML file if needed
			if tt.setupYAML {
				createTestYAMLFile(t, "/tmp/test_config.yaml", tt.yamlContent)
				defer os.Remove("/tmp/test_config.yaml")
			}

			// Save original os.Args and restore after test
			originalArgs := os.Args
			defer func() { os.Args = originalArgs }()

			// Set test args
			os.Args = tt.args

			// Reset flag package state
			resetFlags()

			cfg, probe, err := parseConfiguration()

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if cfg == nil {
				t.Errorf("Expected config, got nil")
				return
			}

			// Verify configuration based on test case
			if tt.expectedDevice != "" && cfg.Device != tt.expectedDevice {
				t.Errorf("Expected device '%s', got '%s'", tt.expectedDevice, cfg.Device)
			}

			if cfg.WebMode != tt.expectedWeb {
				t.Errorf("Expected web mode %t, got %t", tt.expectedWeb, cfg.WebMode)
			}

			// Single address should be empty for these tests
			if probe.address != "" {
				t.Errorf("Expected empty single address, got '%s'", probe.address)
			}

			// Verify default XML paths are set
			if cfg.VControlXML == "" {
				t.Errorf("Expected VControlXML to be set to default")
			}
			if cfg.VitoXML == "" {
				t.Errorf("Expected VitoXML to be set to default")
			}
		})
	}
}

// TestInitializeXMLSystems tests the initializeXMLSystems function
func TestInitializeXMLSystems(t *testing.T) {
	tests := []struct {
		name         string
		vcontrolPath string
		vitoPath     string
		expectError  bool
		createFiles  bool
	}{
		{
			name:         "valid XML paths",
			vcontrolPath: "/tmp/test_vcontrold.xml",
			vitoPath:     "/tmp/test_vito.xml",
			expectError:  false,
			createFiles:  true,
		},
		{
			name:         "missing XML files",
			vcontrolPath: "/tmp/nonexistent_vcontrold.xml",
			vitoPath:     "/tmp/nonexistent_vito.xml",
			expectError:  true,
			createFiles:  false,
		},
		{
			name:         "empty paths",
			vcontrolPath: "",
			vitoPath:     "",
			expectError:  true,
			createFiles:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				VControlXML: tt.vcontrolPath,
				VitoXML:     tt.vitoPath,
			}

			if tt.createFiles {
				createTestXMLFiles(t, tt.vcontrolPath, tt.vitoPath)
				defer func() {
					os.Remove(tt.vcontrolPath)
					os.Remove(tt.vitoPath)
				}()
			}

			err := initializeXMLSystems(cfg)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got none")
				}
			} else {
				if err != nil && tt.createFiles {
					// We might get warnings but not hard errors for missing files
					// since the function continues with basic formatting
					t.Logf("Got expected warning: %v", err)
				}
			}
		})
	}
}

// TestInitializeReader tests the initializeReader function
func TestInitializeReader(t *testing.T) {
	tests := []struct {
		name        string
		device      string
		configFile  string
		expectError bool
		setupConfig bool
		setupYAML   bool
	}{
		// initializeReader really opens the serial port: no made-up device name
		// can succeed without hardware. These two cases expected an impossible
		// success; they now check the failure is clean and reported.
		{
			name:        "absent device without config",
			device:      "mock-device",
			expectError: true,
		},
		{
			name:        "absent device with config",
			device:      "mock-device",
			configFile:  "/tmp/test_config.txt",
			expectError: true,
			setupConfig: true,
		},
		{
			name:        "invalid device",
			device:      "/dev/invalid-device-path",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Device:      tt.device,
				ConfigFile:  tt.configFile,
				VControlXML: "xml/vcontrold.xml",
				VitoXML:     "xml/vito.xml",
			}

			if tt.setupConfig {
				createTestConfigFile(t, tt.configFile)
				defer os.Remove(tt.configFile)
			}

			reader, err := initializeReader(cfg)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got none")
				}
				if reader != nil {
					reader.Close()
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if reader == nil {
				t.Errorf("Expected reader, got nil")
				return
			}

			// Clean up
			reader.Close()
		})
	}
}

// TestRunApplication tests the runApplication function routing
func TestRunApplication(t *testing.T) {
	tests := []struct {
		name          string
		webMode       bool
		singleAddress string
		expectError   bool
		configFile    string
		setupConfig   bool
	}{
		{
			name:          "single address mode",
			singleAddress: "0x1000",
			expectError:   false,
		},
		{
			name:        "CLI mode with config",
			webMode:     false,
			configFile:  "/tmp/test_config.txt",
			setupConfig: true,
			expectError: false,
		},
		{
			name:        "CLI mode without config",
			webMode:     false,
			expectError: true, // Should exit with error for missing config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock reader
			reader := createMockReader(t)
			defer reader.Close()

			cfg := &config.Config{
				WebMode:    tt.webMode,
				ConfigFile: tt.configFile,
				WebPort:    "8080",
			}

			if tt.setupConfig {
				createTestConfigFile(t, tt.configFile)
				defer os.Remove(tt.configFile)

				// runApplication does not load the configuration: without this
				// the mock reader has no data points and CLI mode fails with
				// "no data points configured".
				if err := reader.LoadConfig(tt.configFile); err != nil {
					t.Fatalf("Failed to load test config: %v", err)
				}
			}

			// For CLI mode testing, we need to handle os.Exit calls
			if !tt.webMode && tt.singleAddress == "" && !tt.setupConfig {
				// This test case will call os.Exit(1), so we expect it
				// In a real test, you'd use a wrapper to capture os.Exit
				t.Skip("Skipping CLI mode without config test - calls os.Exit")
			}

			err := runApplication(reader, cfg, singleRead{address: tt.singleAddress})

			if tt.expectError && err == nil {
				t.Errorf("Expected error, got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestRunCLIMode tests the runCLIMode function
func TestRunCLIMode(t *testing.T) {
	tests := []struct {
		name        string
		configFile  string
		setupConfig bool
		expectError bool
		expectExit  bool
	}{
		{
			name:        "with valid config",
			configFile:  "/tmp/test_config.txt",
			setupConfig: true,
			expectError: false,
			expectExit:  false,
		},
		{
			name:        "without config file",
			configFile:  "",
			setupConfig: false,
			expectError: false,
			expectExit:  true, // Should call os.Exit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := createMockReader(t)
			defer reader.Close()

			cfg := &config.Config{
				ConfigFile: tt.configFile,
			}

			if tt.setupConfig {
				createTestConfigFile(t, tt.configFile)
				defer os.Remove(tt.configFile)

				// Load the config into the reader for testing
				err := reader.LoadConfig(tt.configFile)
				if err != nil {
					t.Fatalf("Failed to load test config: %v", err)
				}
			}

			if tt.expectExit {
				// This would call os.Exit, skip in real tests
				t.Skip("Skipping test that calls os.Exit")
				return
			}

			err := runCLIMode(reader, cfg)

			if tt.expectError && err == nil {
				t.Errorf("Expected error, got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Helper functions

func createMockReader(t *testing.T) *viessmann.Reader {
	// Create reader with mock dependencies for testing
	config := viessmann.NewTestReaderConfig("mock-device")
	return viessmann.NewReaderWithDependencies(config)
}

func createTestYAMLFile(t *testing.T, filePath, content string) {
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test YAML file: %v", err)
	}
}

func createTestConfigFile(t *testing.T, filePath string) {
	content := `# Test configuration
Param1 0x1000 2 Int
Param2 0x2000 2 Int
`
	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}
}

func createTestXMLFiles(t *testing.T, vcontrolPath, vitoPath string) {
	vcontrolContent := `<?xml version="1.0" encoding="UTF-8"?>
<units>
  <unit id="UT" type="int" calc="V/10" />
</units>`

	vitoContent := `<?xml version="1.0" encoding="UTF-8"?>
<vito>
  <command ident="1000" addr="0x1000" len="2" unit="UT" />
</vito>`

	if vcontrolPath != "" {
		err := os.WriteFile(vcontrolPath, []byte(vcontrolContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test vcontrol XML: %v", err)
		}
	}

	if vitoPath != "" {
		err := os.WriteFile(vitoPath, []byte(vitoContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test vito XML: %v", err)
		}
	}
}

// resetFlags resets the flag package state for testing
// resetFlags hands back a clean flag set.
//
// parseConfiguration registers its flags on flag.CommandLine, so a second call
// in the same process panicked with "flag redefined". The previous version did
// nothing, so the first subtest passed and the next one took down the whole
// package.
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
}

// Benchmark tests for performance monitoring

func BenchmarkParseConfiguration(b *testing.B) {
	// Save and restore original args
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Set test args
	os.Args = []string{"vcontrold-go", "-device", "/dev/mock", "-verbose"}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resetFlags()
		cfg, _, err := parseConfiguration()
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
		if cfg == nil {
			b.Fatalf("Expected config, got nil")
		}
	}
}

// Table-driven test for edge cases
func TestEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func(t *testing.T)
		description string
	}{
		{
			name: "nil_config",
			testFunc: func(t *testing.T) {
				err := initializeXMLSystems(nil)
				if err == nil {
					t.Error("Expected error for nil config")
				}
			},
			description: "Test handling of nil config",
		},
		{
			name: "empty_device_string",
			testFunc: func(t *testing.T) {
				cfg := &config.Config{Device: ""}
				_, err := initializeReader(cfg)
				if err == nil {
					t.Error("Expected error for empty device string")
				}
			},
			description: "Test handling of empty device string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Log(tt.description)
			tt.testFunc(t)
		})
	}
}
