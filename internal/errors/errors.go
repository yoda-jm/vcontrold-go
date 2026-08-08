// Package errors provides standardized error types and handling for vcontrold-go
package errors

import (
	"fmt"
)

// Error types for consistent error handling across the application
var (
	// ErrConfig represents configuration-related errors
	ErrConfig = &ErrorType{Code: "CONFIG_ERROR", Description: "Configuration error"}

	// ErrCommunication represents serial communication errors
	ErrCommunication = &ErrorType{Code: "COMM_ERROR", Description: "Communication error"}

	// ErrProtocol represents KW2 protocol errors
	ErrProtocol = &ErrorType{Code: "PROTOCOL_ERROR", Description: "Protocol error"}

	// ErrValidation represents input validation errors
	ErrValidation = &ErrorType{Code: "VALIDATION_ERROR", Description: "Validation error"}

	// ErrParsing represents data parsing errors
	ErrParsing = &ErrorType{Code: "PARSING_ERROR", Description: "Parsing error"}

	// ErrNotFound represents resource not found errors
	ErrNotFound = &ErrorType{Code: "NOT_FOUND", Description: "Resource not found"}
)

// ErrorType represents a category of errors
type ErrorType struct {
	Code        string
	Description string
}

// AppError represents a standardized application error
type AppError struct {
	Type       *ErrorType
	Message    string
	Underlying error
	Context    map[string]interface{}
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("%s: %s: %v", e.Type.Code, e.Message, e.Underlying)
	}
	return fmt.Sprintf("%s: %s", e.Type.Code, e.Message)
}

// Unwrap returns the underlying error for error unwrapping
func (e *AppError) Unwrap() error {
	return e.Underlying
}

// New creates a new standardized error
func New(errorType *ErrorType, message string) *AppError {
	return &AppError{
		Type:    errorType,
		Message: message,
		Context: make(map[string]interface{}),
	}
}

// Wrap creates a new error wrapping an existing error
func Wrap(errorType *ErrorType, message string, err error) *AppError {
	return &AppError{
		Type:       errorType,
		Message:    message,
		Underlying: err,
		Context:    make(map[string]interface{}),
	}
}

// WithContext adds context information to an error
func (e *AppError) WithContext(key string, value interface{}) *AppError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// IsType checks if an error is of a specific type
func IsType(err error, errorType *ErrorType) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Type == errorType
	}
	return false
}

// GetContext retrieves context value from an error
func GetContext(err error, key string) (interface{}, bool) {
	if appErr, ok := err.(*AppError); ok && appErr.Context != nil {
		value, exists := appErr.Context[key]
		return value, exists
	}
	return nil, false
}
