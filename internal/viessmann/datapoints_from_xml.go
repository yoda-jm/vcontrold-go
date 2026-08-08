// Package viessmann - Construction des points de mesure a partir de vito.xml
package viessmann

import (
	"fmt"
	"sort"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// AddressSpec designe une adresse a lire, avec une longueur facultative.
type AddressSpec struct {
	Address uint16
	Size    int    // 0 = prendre la longueur de vito.xml
	Name    string // vide = prendre la description de vito.xml
}

// DataTypeForSize traduit une longueur en octets vers le type attendu par le
// formateur de valeurs.
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

// BuildDataPointsFromXML construit les points de mesure des adresses demandees
// en resolvant longueur, unite et libelle depuis vito.xml.
//
// C'est ce qui remplace la liste de datapoints DP_Listen_2.zip : ce fichier
// n'existe sur aucune branche publique de optolink-splitter, et vito.xml porte
// deja tout ce dont on a besoin. Voir PLAN.md, tache 7.
func BuildDataPointsFromXML(specs []AddressSpec) ([]config.DataPoint, error) {
	if len(specs) == 0 {
		return nil, nil
	}

	// Deduplication : une meme adresse peut figurer dans un groupe de
	// rafraichissement et dans la liste MQTT. La specification la plus precise
	// (taille forcee, nom explicite) l'emporte.
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
			// Ni le XML ni la configuration ne donnent de longueur : deviner
			// serait publier une valeur fausse sans le dire.
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

// LoadDataPointsFromXML remplit les points de mesure du lecteur a partir des
// adresses referencees par la configuration YAML.
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

// AddressSpecsFromYaml collecte les adresses a surveiller depuis la
// configuration : groupes de rafraichissement et liste MQTT.
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
