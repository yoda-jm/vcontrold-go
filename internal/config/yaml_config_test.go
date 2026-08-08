package config

import (
	"testing"
)

// TestParseHexAddress tests the ParseHexAddress function with various inputs
func TestParseHexAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint16
		wantErr  bool
	}{
		{"Valid hex with 0x prefix", "0x810", 0x810, false},
		{"Valid hex without prefix", "810", 0x810, false},
		{"Valid hex uppercase", "0x8A0", 0x8A0, false},
		{"Valid hex lowercase", "0x8a0", 0x8A0, false},
		{"Maximum value", "0xFFFF", 0xFFFF, false},
		{"Minimum value", "0x0000", 0x0000, false},
		{"Short hex", "0x1", 0x1, false},
		{"Invalid hex characters", "0xGHI", 0, true},
		{"Empty string", "", 0, true},
		{"Too large value", "0x10000", 0, true},
		{"Non-hex string", "hello", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHexAddress(tt.input)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseHexAddress(%q) expected error, got nil", tt.input)
				}
				return
			}
			
			if err != nil {
				t.Errorf("ParseHexAddress(%q) unexpected error: %v", tt.input, err)
				return
			}
			
			if got != tt.expected {
				t.Errorf("ParseHexAddress(%q) = 0x%X, expected 0x%X", tt.input, got, tt.expected)
			}
		})
	}
}

// TestAutoRefreshConfig_ValidateAddresses tests address validation
func TestAutoRefreshConfig_ValidateAddresses(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		wantErr   bool
	}{
		{"Valid addresses", []string{"0x810", "0x811", "812"}, false},
		{"Empty addresses", []string{}, false},
		{"Invalid hex", []string{"0x810", "invalid", "0x812"}, true},
		{"Empty address in list", []string{"0x810", "", "0x812"}, true},
		{"Mixed valid formats", []string{"0x810", "811", "0x812"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AutoRefreshConfig{
				Addresses: tt.addresses,
			}
			
			err := config.ValidateAddresses()
			
			if tt.wantErr && err == nil {
				t.Errorf("ValidateAddresses() expected error, got nil")
			}
			
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateAddresses() unexpected error: %v", err)
			}
		})
	}
}

// TestAutoRefreshConfig_GetAddressesAsUint16 tests conversion to uint16
func TestAutoRefreshConfig_GetAddressesAsUint16(t *testing.T) {
	config := &AutoRefreshConfig{
		Addresses: []string{"0x810", "811", "0x812"},
	}
	
	addresses, err := config.GetAddressesAsUint16()
	if err != nil {
		t.Errorf("GetAddressesAsUint16() unexpected error: %v", err)
		return
	}
	
	expected := []uint16{0x810, 0x811, 0x812}
	if len(addresses) != len(expected) {
		t.Errorf("GetAddressesAsUint16() length = %d, expected %d", len(addresses), len(expected))
		return
	}
	
	for i, addr := range addresses {
		if addr != expected[i] {
			t.Errorf("GetAddressesAsUint16()[%d] = 0x%X, expected 0x%X", i, addr, expected[i])
		}
	}
}

// TestAutoRefreshConfig_GetRefreshDuration tests duration parsing
func TestAutoRefreshConfig_GetRefreshDuration(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		wantErr  bool
	}{
		{"Valid seconds", "30s", false},
		{"Valid minutes", "5m", false},
		{"Valid hours", "1h", false},
		{"Empty interval", "", false}, // Should use default
		{"Invalid format", "invalid", true},
		{"Negative duration", "-5s", false}, // Go allows negative durations
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AutoRefreshConfig{
				Interval: tt.interval,
			}
			
			duration, err := config.GetRefreshDuration()
			
			if tt.wantErr && err == nil {
				t.Errorf("GetRefreshDuration() expected error, got nil")
				return
			}
			
			if !tt.wantErr && err != nil {
				t.Errorf("GetRefreshDuration() unexpected error: %v", err)
				return
			}
			
			if !tt.wantErr && tt.interval != "" && duration <= 0 && tt.interval != "-5s" {
				t.Errorf("GetRefreshDuration() = %v, expected positive duration", duration)
			}
		})
	}
}