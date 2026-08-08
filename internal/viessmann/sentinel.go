// Package viessmann - detection of "no data" markers.
package viessmann

// The controller does not report a missing sensor or circuit as an error: it
// returns a reserved value. Published as-is it becomes an absurd measurement —
// 3276.7 °C for a solar sensor that does not exist, 6553.5 °C for the flow
// setpoint of an absent mixing circuit.
//
// Both patterns are documented by the V200KW2 reference profile
// (doc/examples/sim-2098.ini): the datapoints this appliance does not carry
// appear there with exactly these values.
//
//	FF / FF FF    datapoint absent      (getTempVLsoll*, getMischer*)
//	FF 7F         sensor not connected  (0x7FFF, signed temperatures)
//
// Distinct from the <error> mechanism of vito.xml, which describes an error
// specific to one address; these patterns hold for the whole appliance.

// unavailableInt16 is the reserved value for two-byte signed temperatures:
// 0x7FFF, the maximum of an int16. No real temperature reaches it — divided by
// ten it would read 3276.7 °C.
const unavailableInt16 = 0x7FFF

// IsUnavailableValue reports whether raw bytes mean "no data" rather than a
// measurement.
func IsUnavailableValue(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// All bytes FF: this datapoint does not exist on this appliance.
	allFF := true
	for _, b := range data {
		if b != 0xFF {
			allFF = false
			break
		}
	}
	if allFF {
		return true
	}

	// 0x7FFF little-endian on a two-byte value: sensor absent. Restricted to
	// two bytes, because across four bytes that same combination is a
	// perfectly plausible counter reading.
	if len(data) == 2 {
		value := uint16(data[0]) | uint16(data[1])<<8
		if value == unavailableInt16 {
			return true
		}
	}

	return false
}
