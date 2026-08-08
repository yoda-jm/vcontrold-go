// Package viessmann contains decoding functions for Viessmann-specific data formats
package viessmann

// These methods delegate to the shared ValueDecoder to eliminate code duplication

// parseValueOriginal is the original parsing logic as fallback
func (r *Reader) parseValueOriginal(rawValue interface{}, dataType string) string {
	return r.decoder.ParseValueOriginal(rawValue, dataType)
}

// parseBCDDateTime parses BCD-encoded date/time data (8 bytes)
func (r *Reader) parseBCDDateTime(data []byte) string {
	return r.decoder.ParseBCDDateTime(data)
}

// decodeCycleTime decodes cycle time data into a readable format
func (r *Reader) decodeCycleTime(data []byte, dataType string) string {
	return r.decoder.DecodeCycleTime(data, dataType)
}

// decodeErrorState decodes error state data
func (r *Reader) decodeErrorState(data []byte, unitType string) string {
	return r.decoder.DecodeErrorState(data, unitType)
}

// decodeBCDTimestamp decodes timestamp data
func (r *Reader) decodeBCDTimestamp(data []byte) string {
	return r.decoder.DecodeBCDTimestamp(data)
}