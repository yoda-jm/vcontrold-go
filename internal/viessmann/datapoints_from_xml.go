// Package viessmann - building data points from vito.xml.
package viessmann

import (
	"fmt"
	"sort"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// AddressSpec names an address to read, with an optional length.
type AddressSpec struct {
	Address uint16
	Size    int    // 0 = take the length from vito.xml
	Name    string // empty = take the description from vito.xml
}

// DataTypeForSize maps a byte length to the type the value formatter expects.
func DataTypeForSize(size int) string {
	switch {
	case size >= 4:
		return "Int4"
	case size >= 2:
		return "Int"
	default:
		return "Byte"
	}
}

// BuildDataPointsFromXML builds the data points for the requested addresses,
// resolving length, unit and label from vito.xml.
//
// This replaces the DP_Listen_2.zip datapoint list: that file exists on no
// public branch of optolink-splitter, and vito.xml already carries everything
// needed.
func BuildDataPointsFromXML(specs []AddressSpec) ([]config.DataPoint, error) {
	if len(specs) == 0 {
		return nil, nil
	}

	// Deduplicate: the same address may appear both in a refresh group and in
	// the MQTT list. The more specific spec — forced size, explicit name —
	// wins.
	merged := make(map[uint16]AddressSpec, len(specs))
	for _, spec := range specs {
		existing, seen := merged[spec.Address]
		if !seen {
			merged[spec.Address] = spec
			continue
		}
		if spec.Size > 0 {
			existing.Size = spec.Size
		}
		if spec.Name != "" {
			existing.Name = spec.Name
		}
		merged[spec.Address] = existing
	}

	addresses := make([]uint16, 0, len(merged))
	for addr := range merged {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })

	var (
		dataPoints []config.DataPoint
		unresolved []string
	)

	for _, addr := range addresses {
		spec := merged[addr]

		size := spec.Size
		name := spec.Name
		unit := ""

		if mapping, exists := xml.GetAddressMapping(addr); exists {
			if size == 0 && mapping.Length > 0 {
				size = mapping.Length
			}
			if name == "" {
				name = mapping.Description
			}
			unit = mapping.Unit
		} else if size == 0 {
			// Neither the XML nor the configuration gives a length. Guessing
			// would publish a wrong value without saying so.
			unresolved = append(unresolved, fmt.Sprintf("0x%04X", addr))
			continue
		}

		if name == "" {
			name = fmt.Sprintf("Parameter 0x%04X", addr)
		}

		dataPoints = append(dataPoints, config.DataPoint{
			Name:     name,
			Address:  addr,
			Size:     size,
			DataType: DataTypeForSize(size),
			Unit:     unit,
		})
	}

	if len(unresolved) > 0 {
		return nil, errors.New(errors.ErrConfig, fmt.Sprintf(
			"addresses absent from vito.xml and without an explicit size: %v", unresolved))
	}

	return dataPoints, nil
}

// LoadDataPointsFromXML fills the reader's data points from the addresses the
// YAML configuration refers to.
func (r *Reader) LoadDataPointsFromXML(specs []AddressSpec) error {
	dataPoints, err := BuildDataPointsFromXML(specs)
	if err != nil {
		return err
	}

	if len(dataPoints) == 0 {
		return errors.New(errors.ErrConfig, "no addresses to monitor: check refresh_groups and mqtt.addresses")
	}

	r.dataPoints = dataPoints
	for _, dp := range dataPoints {
		r.logger.Info("Data point 0x%04X %-40s %d bytes (%s)", dp.Address, dp.Name, dp.Size, dp.DataType)
	}
	r.logger.Info("Built %d data points from vito.xml", len(dataPoints))

	return nil
}

// AddressSpecsFromYaml collects the addresses to monitor from the
// configuration: refresh groups and the MQTT list.
func AddressSpecsFromYaml(yamlConfig *config.YamlConfig) ([]AddressSpec, error) {
	if yamlConfig == nil {
		return nil, nil
	}

	var specs []AddressSpec

	for _, group := range yamlConfig.RefreshGroups {
		if !group.Enabled {
			continue
		}
		for _, addrStr := range group.Addresses {
			addr, err := config.ParseHexAddress(addrStr)
			if err != nil {
				return nil, errors.Wrap(errors.ErrValidation,
					fmt.Sprintf("refresh group '%s': invalid address '%s'", group.Name, addrStr), err)
			}
			specs = append(specs, AddressSpec{Address: addr})
		}
	}

	for _, mqttAddr := range yamlConfig.MQTT.Addresses {
		addr, err := config.ParseHexAddress(mqttAddr.Address)
		if err != nil {
			return nil, errors.Wrap(errors.ErrValidation,
				fmt.Sprintf("mqtt address '%s' is not valid hex", mqttAddr.Address), err)
		}
		specs = append(specs, AddressSpec{
			Address: addr,
			Name:    mqttAddr.Alias,
		})
	}

	return specs, nil
}
