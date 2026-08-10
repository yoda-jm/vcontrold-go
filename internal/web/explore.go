// Package web - discovering and probing datapoints the daemon does not poll.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/gorilla/mux"

	"github.com/yoda-jm/vcontrold-go/internal/config"
	"github.com/yoda-jm/vcontrold-go/internal/logging"
	"github.com/yoda-jm/vcontrold-go/internal/viessmann"
	"github.com/yoda-jm/vcontrold-go/internal/xml"
)

// xmlDatapoint is one address the community XML declares.
type xmlDatapoint struct {
	Address     string `json:"address"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
	Polled      bool   `json:"polled"`
}

// getXMLDatapointsHandler lists everything the XML declares, flagging what is
// actually polled.
//
// The appliance exposes far more than any sensible refresh list covers —
// schedules, fault history, a second heating circuit — and there is no way to
// discover that from the dashboard. Listing it makes the gap visible instead of
// leaving it to a reading of vito.xml.
func (s *Server) getXMLDatapointsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	polled := make(map[uint16]bool)
	for _, dp := range s.reader.GetDataPoints() {
		polled[dp.Address] = true
	}

	mappings := xml.GetAllAddressMappings()
	out := make([]xmlDatapoint, 0, len(mappings))
	for address, mapping := range mappings {
		out = append(out, xmlDatapoint{
			Address:     fmt.Sprintf("0x%04X", address),
			Name:        mapping.Description,
			Size:        mapping.Length,
			Unit:        mapping.Unit,
			Description: mapping.Description,
			Polled:      polled[address],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })

	json.NewEncoder(w).Encode(out)
}

// readAddressHandler reads one address on demand.
//
// Deliberately a single read rather than anything scheduled: these are the
// datapoints nobody wants polled every minute — a schedule that changes twice a
// year, a fault history that is usually empty — but that are worth looking at
// when a question comes up.
//
// It shares the serial port with the refresh groups. The communicator
// serialises transactions and enforces a rest between them, so an occasional
// manual read costs a little latency on the next scheduled one and nothing
// worse.
func (s *Server) readAddressHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	address, err := config.ParseHexAddress(mux.Vars(r)["address"])
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid address: %v", err), http.StatusBadRequest)
		return
	}

	mapping, exists := xml.GetAddressMapping(address)
	if !exists {
		http.Error(w, "address is not declared in vito.xml; its length is unknown",
			http.StatusNotFound)
		return
	}

	dataPoint := config.DataPoint{
		Name:     mapping.Description,
		Address:  address,
		Size:     mapping.Length,
		DataType: viessmann.DataTypeForSize(mapping.Length),
	}

	logging.Info("On-demand read of 0x%04X (%s)", address, mapping.Description)
	reading := s.reader.ReadParameter(dataPoint, -1)

	response := map[string]interface{}{
		"address": fmt.Sprintf("0x%04X", address),
		"name":    mapping.Description,
		"unit":    mapping.Unit,
		"size":    mapping.Length,
		"success": reading.Success,
		"value":   reading.FormattedValue,
	}
	if raw, ok := reading.RawValue.([]byte); ok {
		response["raw"] = fmt.Sprintf("% X", raw)
	}
	if reading.Error != "" {
		response["error"] = reading.Error
	}

	json.NewEncoder(w).Encode(response)
}
