// Package viessmann - logger constructors.
//
// The implementation lives in internal/logging, so that every package writes
// through the same one and -verbose reaches all of them. These constructors
// remain because the reader takes its logger by injection, which is what lets
// tests silence it.
package viessmann

import "github.com/yourusername/vcontrold-go/internal/logging"

// NewDefaultLogger returns the shared logger, tagged for this package.
func NewDefaultLogger() LoggerInterface {
	return logging.For("viessmann")
}

// NewNoOpLogger returns a logger that discards everything, for tests.
func NewNoOpLogger() LoggerInterface {
	return logging.NoOp{}
}
