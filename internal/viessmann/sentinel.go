// Package viessmann - Detection des valeurs « pas de donnee »
package viessmann

// La regulation ne signale pas l'absence d'un capteur ou d'un circuit par une
// erreur : elle rend une valeur reservee. Publiee telle quelle, elle devient
// une mesure absurde -- 3276,7 °C pour un capteur solaire inexistant, 6553,5 °C
// pour la consigne de depart d'un circuit melangeur absent.
//
// Les deux motifs sont documentes par le profil de reference du V200KW2
// (doc/examples/sim-2098.ini) : les points que l'appareil ne porte pas y
// figurent avec ces valeurs exactes.
//
//	FF / FF FF    donnee absente         (getTempVLsoll*, getMischer*)
//	FF 7F         capteur non raccorde   (0x7FFF, temperatures signees)
//
// Distinct du mecanisme <error> de vito.xml, qui decrit une erreur propre a une
// adresse ; ces motifs-ci valent pour tout l'appareil.

// unavailableInt16 est la valeur reservee des temperatures signees sur deux
// octets : 0x7FFF, soit le maximum d'un int16. Aucune temperature reelle ne
// l'atteint -- elle vaudrait 3276,7 °C apres division par dix.
const unavailableInt16 = 0x7FFF

// IsUnavailableValue indique si des octets bruts signifient « pas de donnee »
// plutot qu'une mesure.
func IsUnavailableValue(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	// Tous les octets a FF : le point de mesure n'existe pas sur cet appareil.
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

	// 0x7FFF en petit-boutiste sur une grandeur de deux octets : capteur
	// absent. Limite a deux octets, car sur quatre octets cette combinaison
	// est une valeur de compteur parfaitement plausible.
	if len(data) == 2 {
		value := uint16(data[0]) | uint16(data[1])<<8
		if value == unavailableInt16 {
			return true
		}
	}

	return false
}
