// Package mqtt - Decouverte Home Assistant
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// deviceIdentifier regroupe toutes les entites sous un seul appareil dans HA.
const (
	deviceIdentifier = "vitola200"
	deviceName       = "Vitola 200"
	deviceModel      = "Vitola 200 VB2A / Vitotronic 200 KW2"
	deviceVendor     = "Viessmann"
)

// discoveryPayload est le message de configuration attendu par Home Assistant
// sur homeassistant/sensor/<node>/<objet>/config.
type discoveryPayload struct {
	Name              string              `json:"name"`
	UniqueID          string              `json:"unique_id"`
	ObjectID          string              `json:"object_id"`
	StateTopic        string              `json:"state_topic"`
	UnitOfMeasurement string              `json:"unit_of_measurement,omitempty"`
	DeviceClass       string              `json:"device_class,omitempty"`
	StateClass        string              `json:"state_class,omitempty"`
	Icon              string              `json:"icon,omitempty"`
	PayloadOn         string              `json:"payload_on,omitempty"`
	PayloadOff        string              `json:"payload_off,omitempty"`
	Device            discoveryDevice     `json:"device"`
	Availability      []map[string]string `json:"availability,omitempty"`
}

type discoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Model        string   `json:"model"`
	Manufacturer string   `json:"manufacturer"`
}

// sensorMetadata porte ce que Home Assistant doit savoir d'une mesure.
type sensorMetadata struct {
	Unit        string
	DeviceClass string
	StateClass  string
	Icon        string
}

// metadataForUnit deduit les classes Home Assistant de l'unite vito.xml.
//
// state_class est le champ qui compte : sans lui, Home Assistant n'archive pas
// l'entite dans les statistiques long terme, et l'historique disparait avec la
// purge du recorder (dix jours par defaut). C'est ce qui a coute un an de
// releves la derniere fois.
//
//	total_increasing -> compteurs qui ne font que monter (heures, demarrages)
//	measurement      -> grandeurs instantanees (temperatures, etats)
func metadataForUnit(vitoUnit string) sensorMetadata {
	switch vitoUnit {
	case "CS": // CounterS : secondes cumulees, converties en heures (V/3600)
		return sensorMetadata{
			Unit:        "h",
			DeviceClass: "duration",
			StateClass:  "total_increasing",
			Icon:        "mdi:timer-outline",
		}
	case "CO": // Counter : nombre d'evenements cumules
		return sensorMetadata{
			StateClass: "total_increasing",
			Icon:       "mdi:counter",
		}
	case "UT", "UT1", "UT1U", "UTI": // Temperatures
		return sensorMetadata{
			Unit:        "°C",
			DeviceClass: "temperature",
			StateClass:  "measurement",
		}
	case "PR", "PR1": // Pourcentages
		return sensorMetadata{
			Unit:       "%",
			StateClass: "measurement",
		}
	case "ST": // Status numerique simple
		return sensorMetadata{StateClass: "measurement"}
	case "RT": // ReturnStatus : enum 0/1, marche/arret
		return sensorMetadata{Icon: "mdi:fire"}
	case "BA": // BetriebsArt : enum textuel, pas de state_class possible
		return sensorMetadata{Icon: "mdi:tune"}
	default:
		return sensorMetadata{}
	}
}

// sanitizeObjectID rend un alias utilisable dans un topic et un unique_id.
func sanitizeObjectID(alias string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r == '-', r == ' ', r == '.':
			return '_'
		default:
			return -1
		}
	}, alias)

	return strings.Trim(cleaned, "_")
}

// slugify reduit un libelle a un identifiant Home Assistant : minuscules,
// sans accents, mots separes par des soulignes.
func slugify(label string) string {
	// Les libelles sont francais : translitterer plutot que supprimer, sans
	// quoi « Brûleur » donnerait « brleur ».
	replacer := strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"î", "i", "ï", "i",
		"ô", "o", "ö", "o",
		"ù", "u", "û", "u", "ü", "u",
		"ç", "c",
	)
	folded := replacer.Replace(strings.ToLower(label))

	var b strings.Builder
	lastUnderscore := false
	for _, r := range folded {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	return strings.Trim(b.String(), "_")
}

// PublishDiscovery publie un message de decouverte RETENU par adresse surveillee.
//
// Le flag retain est indispensable : sans lui, Home Assistant perd toutes les
// entites au premier redemarrage et ne les retrouve qu'au prochain envoi.
func (p *Publisher) PublishDiscovery() error {
	if !p.enabled || p.config == nil {
		return nil
	}
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("cannot publish discovery: MQTT client not connected")
	}

	published := 0
	for _, addr := range p.config.Addresses {
		hexAddr, err := config.ParseHexAddress(addr.Address)
		if err != nil {
			log.Printf("MQTT discovery: skipping invalid address '%s': %v", addr.Address, err)
			continue
		}

		alias := addr.Alias
		if alias == "" {
			alias = fmt.Sprintf("addr_%04x", hexAddr)
		}
		objectID := sanitizeObjectID(alias)
		if objectID == "" {
			log.Printf("MQTT discovery: alias '%s' yields an empty object id, skipping", alias)
			continue
		}

		// L'unite vito.xml fournit le defaut ; la configuration peut le forcer.
		meta := sensorMetadata{}
		if mapping, exists := xml.GetAddressMapping(hexAddr); exists {
			meta = metadataForUnit(mapping.Unit)
		}
		if addr.Unit != "" {
			meta.Unit = addr.Unit
		}
		if addr.DeviceClass != "" {
			meta.DeviceClass = addr.DeviceClass
			// L'icone deduite de l'unite vito.xml est un pis-aller : l'unite RT
			// couvre aussi bien le bruleur que le circulateur, et donnait une
			// flamme aux deux. Des qu'une device_class est declaree, Home
			// Assistant choisit mieux que nous.
			meta.Icon = ""
		}
		if addr.Icon != "" {
			meta.Icon = addr.Icon
		}
		if addr.StateClass != "" {
			meta.StateClass = addr.StateClass
		}

		friendlyName := addr.FriendlyName
		if friendlyName == "" {
			friendlyName = alias
		}

		// Un etat 0/1 s'affiche bien mieux en binary_sensor : Home Assistant
		// montre « Marche / Arret » la ou un sensor montrerait « 0 ».
		component := addr.Component
		if component == "" {
			component = "sensor"
		}
		payloadOn, payloadOff := addr.PayloadOn, addr.PayloadOff
		if component == "binary_sensor" {
			if payloadOn == "" {
				payloadOn = "1"
			}
			if payloadOff == "" {
				payloadOff = "0"
			}
			// state_class n'a aucun sens sur un binaire, et Home Assistant
			// refuse la configuration si on l'y laisse.
			meta.StateClass = ""
			meta.Unit = ""
		}

		payload := discoveryPayload{
			Name:     friendlyName,
			UniqueID: fmt.Sprintf("%s_%s", deviceIdentifier, objectID),
			// object_id fixe l'entity_id cote Home Assistant. Sans lui, HA le
			// derive du libelle et le tableau de bord devrait deviner des
			// identifiants -- ils sont ici previsibles : sensor.vitola_<nom>.
			ObjectID:          fmt.Sprintf("vitola_%s", slugify(friendlyName)),
			StateTopic:        fmt.Sprintf("%s/%s", p.config.TopicRoot, alias),
			UnitOfMeasurement: meta.Unit,
			DeviceClass:       meta.DeviceClass,
			StateClass:        meta.StateClass,
			Icon:              meta.Icon,
			PayloadOn:         payloadOn,
			PayloadOff:        payloadOff,
			Availability: []map[string]string{{
				"topic":                 p.availabilityTopic(),
				"payload_available":     availabilityOnline,
				"payload_not_available": availabilityOffline,
			}},
			Device: discoveryDevice{
				Identifiers:  []string{deviceIdentifier},
				Name:         deviceName,
				Model:        deviceModel,
				Manufacturer: deviceVendor,
			},
		}

		body, err := json.Marshal(payload)
		if err != nil {
			log.Printf("MQTT discovery: failed to marshal payload for %s: %v", alias, err)
			continue
		}

		topic := fmt.Sprintf("homeassistant/%s/%s/%s/config", component, deviceIdentifier, objectID)
		token := p.client.Publish(topic, 0, true, body) // retain = true
		if token.Wait() && token.Error() != nil {
			log.Printf("MQTT discovery: failed to publish %s: %v", topic, token.Error())
			continue
		}

		published++
		log.Printf("MQTT discovery: %s (state_class=%q unit=%q)", topic, meta.StateClass, meta.Unit)
	}

	log.Printf("MQTT discovery: published %d retained sensor definitions", published)
	return nil
}
