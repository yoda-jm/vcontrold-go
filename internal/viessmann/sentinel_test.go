package viessmann

import "testing"

// Les motifs testes ici sont ceux du profil de reference du V200KW2
// (doc/examples/sim-2098.ini) et les lectures faites sur la machine.
func TestIsUnavailableValue(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		// Absent : documente tel quel dans le profil de reference.
		{"mischer absent (FF)", []byte{0xFF}, true},
		{"consigne depart absente (FF FF)", []byte{0xFF, 0xFF}, true},
		{"quatre octets a FF", []byte{0xFF, 0xFF, 0xFF, 0xFF}, true},

		// Capteur solaire non raccorde : lu FF 7F sur la machine, soit 0x7FFF,
		// que le decodage rendait en 3276,7 °C.
		{"capteur non raccorde (FF 7F)", []byte{0xFF, 0x7F}, true},

		// Mesures reelles relevees sur la chaudiere : aucune ne doit etre
		// prise pour une absence.
		{"temperature exterieure 27,6", []byte{0x14, 0x01}, false},
		{"consigne ECS 50", []byte{0x32}, false},
		{"bruleur a l arret", []byte{0x00}, false},
		{"heures bruleur", []byte{0x92, 0xE2, 0x5F, 0x02}, false},
		{"demarrages bruleur", []byte{0xEB, 0x25, 0x03, 0x00}, false},

		// 255 sur un octet EST une absence, mais 0x00FF sur deux octets est
		// une valeur legitime (25,5 °C).
		{"25,5 degres (FF 00)", []byte{0xFF, 0x00}, false},

		// Sur quatre octets, 0x7FFF en tete est un compteur plausible.
		{"compteur 32767", []byte{0xFF, 0x7F, 0x00, 0x00}, false},

		{"vide", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUnavailableValue(tt.data); got != tt.want {
				t.Errorf("IsUnavailableValue(% X) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}
