// Package viessmann - Dependency injection and configuration for Reader
package viessmann

// ReaderConfig holds all dependencies and configuration for Reader creation
type ReaderConfig struct {
	Device       string
	SerialComm   SerialCommunicatorInterface
	Formatter    ValueFormatterInterface
	Decoder      ValueDecoderInterface
	Cache        ParameterCacheInterface
	Logger       LoggerInterface
	Metrics      MetricsInterface
}

// NewReaderWithDependencies creates a Reader with injected dependencies
func NewReaderWithDependencies(config *ReaderConfig) *Reader {
	return &Reader{
		device:     config.Device,
		serialComm: config.SerialComm,
		formatter:  config.Formatter,
		decoder:    config.Decoder,
		cache:      config.Cache,
		logger:     config.Logger,
		metrics:    config.Metrics,
	}
}

// NewDefaultReaderConfig creates a ReaderConfig with default implementations
func NewDefaultReaderConfig(device string) (*ReaderConfig, error) {
	// Create serial communicator
	serialComm, err := NewSerialCommunicator(device)
	if err != nil {
		return nil, err
	}
	
	return &ReaderConfig{
		Device:     device,
		SerialComm: serialComm,
		Formatter:  NewValueFormatter(),
		Decoder:    NewValueDecoder(),
		Cache:      NewParameterCache(),
		Logger:     NewDefaultLogger(),
		Metrics:    NewMetrics(),
	}, nil
}

// NewTestReaderConfig creates a ReaderConfig with test-friendly implementations
func NewTestReaderConfig(device string) *ReaderConfig {
	return &ReaderConfig{
		Device:     device,
		SerialComm: NewMockSerialCommunicator(),
		Formatter:  NewValueFormatter(),
		Decoder:    NewValueDecoder(),
		Cache:      NewParameterCache(),
		Logger:     NewNoOpLogger(),
		Metrics:    NewNoOpMetrics(),
	}
}