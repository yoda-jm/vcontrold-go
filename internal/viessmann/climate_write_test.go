package viessmann

import (
	"bytes"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/config"
)

// recordingComm captures what would be written to the appliance.
type recordingComm struct {
	*MockSerialCommunicator
	address uint16
	written []byte
	calls   int
}

func (r *recordingComm) WriteToDevice(address uint16, data []byte) error {
	r.address = address
	r.written = append([]byte(nil), data...)
	r.calls++
	return nil
}

func newClimateFixture(dataPoints []config.DataPoint) (*ClimateController, *recordingComm) {
	comm := &recordingComm{MockSerialCommunicator: NewMockSerialCommunicator()}
	reader := &Reader{
		serialComm: comm,
		logger:     &MockLogger{},
		dataPoints: dataPoints,
	}
	return NewClimateController(reader), comm
}

// Both setpoints are single bytes in whole degrees (unit ST). Scaling by ten
// was the original defect: 48 °C became 480, which does not fit in a byte, and
// a two-byte write spills into the neighbouring address.
func TestClimateController_SetpointsAreOneUnscaledByte(t *testing.T) {
	tests := []struct {
		name        string
		zone        string
		value       string
		dataPoints  []config.DataPoint
		wantAddress uint16
		wantBytes   []byte
	}{
		{
			name:  "hot water setpoint",
			zone:  "hotwater",
			value: "48",
			dataPoints: []config.DataPoint{
				{Name: "Warmwasser Solltemperatur", Address: 0x6300, Size: 1, DataType: "Byte"},
			},
			wantAddress: 0x6300,
			wantBytes:   []byte{0x30}, // 48
		},
		{
			name:  "room setpoint",
			zone:  "heating",
			value: "22",
			dataPoints: []config.DataPoint{
				{Name: "Raumtemperatur Normal Sollwert", Address: 0x2306, Size: 1, DataType: "Byte"},
			},
			wantAddress: 0x2306,
			wantBytes:   []byte{0x16}, // 22
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, comm := newClimateFixture(tt.dataPoints)

			err := controller.ProcessClimateCommand(ClimateCommand{
				Type: "temperature", Zone: tt.zone, Value: tt.value,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if comm.address != tt.wantAddress {
				t.Errorf("address = 0x%04X, want 0x%04X", comm.address, tt.wantAddress)
			}
			if !bytes.Equal(comm.written, tt.wantBytes) {
				t.Errorf("payload = % X, want % X", comm.written, tt.wantBytes)
			}
			if len(comm.written) != 1 {
				t.Errorf("payload is %d bytes, want exactly 1 — a longer write "+
					"overruns the next address", len(comm.written))
			}
		})
	}
}

// Out-of-range setpoints must never reach the appliance: this runs against a
// boiler in service.
func TestClimateController_RefusesOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		zone  string
		value string
	}{
		{"hot water far too hot", "hotwater", "90"},
		{"hot water far too cold", "hotwater", "5"},
		{"room far too hot", "heating", "45"},
		{"room far too cold", "heating", "2"},
		{"not a number", "hotwater", "chaud"},
	}

	dataPoints := []config.DataPoint{
		{Name: "Warmwasser Solltemperatur", Address: 0x6300, Size: 1, DataType: "Byte"},
		{Name: "Raumtemperatur Normal Sollwert", Address: 0x2306, Size: 1, DataType: "Byte"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, comm := newClimateFixture(dataPoints)

			err := controller.ProcessClimateCommand(ClimateCommand{
				Type: "temperature", Zone: tt.zone, Value: tt.value,
			})
			if err == nil {
				t.Fatal("expected the command to be refused")
			}
			if comm.calls != 0 {
				t.Errorf("%d write(s) reached the appliance, want none", comm.calls)
			}
		})
	}
}
