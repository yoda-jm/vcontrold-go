package errors

import (
	"errors"
	"testing"
)

// TestNew tests creating new standardized errors
func TestNew(t *testing.T) {
	err := New(ErrValidation, "invalid input")

	if err.Type != ErrValidation {
		t.Errorf("New() Type = %v, expected %v", err.Type, ErrValidation)
	}

	if err.Message != "invalid input" {
		t.Errorf("New() Message = %q, expected %q", err.Message, "invalid input")
	}

	if err.Underlying != nil {
		t.Errorf("New() Underlying = %v, expected nil", err.Underlying)
	}

	expected := "VALIDATION_ERROR: invalid input"
	if err.Error() != expected {
		t.Errorf("Error() = %q, expected %q", err.Error(), expected)
	}
}

// TestWrap tests wrapping existing errors
func TestWrap(t *testing.T) {
	originalErr := errors.New("original error")
	err := Wrap(ErrConfig, "config problem", originalErr)

	if err.Type != ErrConfig {
		t.Errorf("Wrap() Type = %v, expected %v", err.Type, ErrConfig)
	}

	if err.Message != "config problem" {
		t.Errorf("Wrap() Message = %q, expected %q", err.Message, "config problem")
	}

	if err.Underlying != originalErr {
		t.Errorf("Wrap() Underlying = %v, expected %v", err.Underlying, originalErr)
	}

	expected := "CONFIG_ERROR: config problem: original error"
	if err.Error() != expected {
		t.Errorf("Error() = %q, expected %q", err.Error(), expected)
	}
}

// TestUnwrap tests error unwrapping
func TestUnwrap(t *testing.T) {
	originalErr := errors.New("original error")
	err := Wrap(ErrConfig, "config problem", originalErr)

	unwrapped := err.Unwrap()
	if unwrapped != originalErr {
		t.Errorf("Unwrap() = %v, expected %v", unwrapped, originalErr)
	}
}

// TestWithContext tests adding context to errors
func TestWithContext(t *testing.T) {
	err := New(ErrValidation, "invalid input").WithContext("field", "username").WithContext("value", "test@example.com")

	field, exists := GetContext(err, "field")
	if !exists || field != "username" {
		t.Errorf("GetContext('field') = %v, %v; expected 'username', true", field, exists)
	}

	value, exists := GetContext(err, "value")
	if !exists || value != "test@example.com" {
		t.Errorf("GetContext('value') = %v, %v; expected 'test@example.com', true", value, exists)
	}

	missing, exists := GetContext(err, "missing")
	if exists || missing != nil {
		t.Errorf("GetContext('missing') = %v, %v; expected nil, false", missing, exists)
	}
}

// TestIsType tests error type checking
func TestIsType(t *testing.T) {
	err := New(ErrValidation, "invalid input")

	if !IsType(err, ErrValidation) {
		t.Errorf("IsType(err, ErrValidation) = false, expected true")
	}

	if IsType(err, ErrConfig) {
		t.Errorf("IsType(err, ErrConfig) = true, expected false")
	}

	// Test with standard error
	stdErr := errors.New("standard error")
	if IsType(stdErr, ErrValidation) {
		t.Errorf("IsType(stdErr, ErrValidation) = true, expected false")
	}
}

// TestErrorTypes tests all defined error types
func TestErrorTypes(t *testing.T) {
	errorTypes := []*ErrorType{
		ErrConfig,
		ErrCommunication,
		ErrProtocol,
		ErrValidation,
		ErrParsing,
		ErrNotFound,
	}

	for _, errType := range errorTypes {
		if errType.Code == "" {
			t.Errorf("Error type has empty Code: %+v", errType)
		}

		if errType.Description == "" {
			t.Errorf("Error type has empty Description: %+v", errType)
		}
	}
}
