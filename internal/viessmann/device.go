// Package viessmann - identifying the controller we are talking to.
package viessmann

import (
	"fmt"

	"github.com/yourusername/vcontrold-go/internal/errors"
)

// devTypeAddress is where every KW controller reports what it is. The command
// is getDevType in the community vito.xml, unit DT, two bytes.
const devTypeAddress = 0x00F8

// SupportLevel says how much trust a device identification deserves.
//
// This distinction is the point of the whole file. This project was written
// against exactly one appliance, and everything else is inference from the
// community XML. Saying so plainly is more useful to someone else's boiler
// than a confident guess would be.
type SupportLevel int

const (
	// SupportUnknown: the controller did not identify itself as anything we
	// have a name for. Reads may still work — the KW protocol is the same —
	// but no address list has been checked against it.
	SupportUnknown SupportLevel = iota

	// SupportInferred: the identifier appears in the community XML, so the
	// address list is probably right, but nobody has run this code against
	// such an appliance. Treat surprising values with suspicion.
	SupportInferred

	// SupportVerified: read and write have been exercised against this exact
	// controller, on real hardware.
	SupportVerified
)

func (s SupportLevel) String() string {
	switch s {
	case SupportVerified:
		return "verified"
	case SupportInferred:
		return "inferred"
	default:
		return "unknown"
	}
}

// DeviceInfo is what the controller says about itself.
type DeviceInfo struct {
	// ID is the raw two-byte identifier, e.g. 0x2098.
	ID uint16

	// Model is the community name, e.g. "V200KW2". Empty when unrecognised.
	Model string

	// Support says whether that model has been tried for real.
	Support SupportLevel

	// XMLProfile is the vito.xml directory this device belongs to.
	XMLProfile string

	// Notes carries what a user should know before trusting the readings.
	Notes string
}

// knownDevices maps the identifiers the community XML lists.
//
// Only 0x2098 is marked verified, because only 0x2098 has been on the other end
// of an actual serial cable here. The rest are transcribed from the DT unit of
// vcontrold.xml and are honest guesses, nothing more.
var knownDevices = map[uint16]DeviceInfo{
	0x2098: {
		ID:         0x2098,
		Model:      "V200KW2",
		Support:    SupportVerified,
		XMLProfile: "kw",
		Notes: "Reads and writes exercised on real hardware (Vitola 200 VB2A). " +
			"Note that vito.xml understates getBrennerStarts (0x088A) as two " +
			"bytes; see overrides/vitola200-kw2.xml.",
	},
	0x2094: {
		ID:         0x2094,
		Model:      "V200KW1",
		Support:    SupportInferred,
		XMLProfile: "kw",
		Notes: "Same KW protocol and address list as the KW2 per the community " +
			"XML, but never run against real hardware here. Check the counter " +
			"lengths before trusting them.",
	},
	0x2053: {
		ID:         0x2053,
		Model:      "V200WB2",
		Support:    SupportInferred,
		XMLProfile: "kw",
		Notes: "The community XML routes this device through GWG_VBEM, a " +
			"different protocol from the KW this project implements. Reads are " +
			"unlikely to work as-is.",
	},
	0x20CB: {
		ID:         0x20CB,
		Model:      "VScotHO1",
		Support:    SupportInferred,
		XMLProfile: "300",
		Notes: "Listed as KW,300 in the community XML. The 300 profile is a " +
			"different protocol; this project only speaks KW.",
	},
}

// IdentifyDevice asks the controller what it is.
//
// Deliberately a plain read of one address rather than anything clever: if this
// fails, the link itself is suspect, and saying that early is more useful than
// a wall of failed parameter reads.
func IdentifyDevice(comm SerialCommunicatorInterface) (DeviceInfo, error) {
	raw, err := comm.ReadFromDevice(devTypeAddress, 2)
	if err != nil {
		return DeviceInfo{}, errors.Wrap(errors.ErrCommunication,
			"failed to read the device type at 0x00F8", err)
	}

	if IsUnavailableValue(raw) {
		return DeviceInfo{}, errors.New(errors.ErrProtocol,
			fmt.Sprintf("the controller reports no device type (raw % X)", raw))
	}
	if len(raw) < 2 {
		return DeviceInfo{}, errors.New(errors.ErrProtocol,
			fmt.Sprintf("short device type response (raw % X)", raw))
	}

	// Big-endian here, unlike the little-endian measurements: the DT unit
	// matches on the bytes in wire order, "20 98".
	id := uint16(raw[0])<<8 | uint16(raw[1])

	if known, ok := knownDevices[id]; ok {
		return known, nil
	}

	return DeviceInfo{
		ID:      id,
		Support: SupportUnknown,
		Notes: "This identifier is absent from the community XML. Reads may " +
			"still work, since the KW protocol does not vary, but no address " +
			"list has been checked against this controller.",
	}, nil
}

// Describe renders a one-line summary suitable for a startup log.
func (d DeviceInfo) Describe() string {
	if d.Model == "" {
		return fmt.Sprintf("unrecognised controller ID=0x%04X (support: %s)", d.ID, d.Support)
	}
	return fmt.Sprintf("%s ID=0x%04X (support: %s)", d.Model, d.ID, d.Support)
}
