package mqtt

import "testing"

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Heures brûleur":         "heures_bruleur",
		"Démarrages brûleur":     "demarrages_bruleur",
		"Température extérieure": "temperature_exterieure",
		"Circulateur M1":         "circulateur_m1",
		"Mode de fonctionnement": "mode_de_fonctionnement",
		"  espaces  multiples  ": "espaces_multiples",
	}
	for in, want := range tests {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
