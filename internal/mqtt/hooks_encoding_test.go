package mqtt

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/yourusername/vcontrold-go/internal/xml"
)

func TestEncodeLittleEndian(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		length  int
		want    []byte
		wantErr bool
	}{
		// 48 degres sur un octet : exactement setTempWWsoll, unite ST.
		{name: "consigne ECS 48 C", value: 48, length: 1, want: []byte{0x30}},
		{name: "deux octets", value: 480, length: 2, want: []byte{0xE0, 0x01}},
		{name: "quatre octets", value: 206315, length: 4, want: []byte{0xEB, 0x25, 0x03, 0x00}},
		{name: "borne haute un octet", value: 255, length: 1, want: []byte{0xFF}},

		// Le cas qui a motive le correctif : 48 x 10 ne tient pas dans un octet.
		// Tronquer donnerait 224, soit une consigne absurde sur une chaudiere
		// en service. L'erreur est le seul comportement acceptable.
		{name: "debordement un octet", value: 480, length: 1, wantErr: true},
		{name: "valeur negative", value: -1, length: 1, wantErr: true},
		{name: "longueur nulle", value: 1, length: 0, wantErr: true},
		{name: "longueur excessive", value: 1, length: 5, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeLittleEndian(tt.value, tt.length)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("encodeLittleEndian(%d, %d) = % X, want an error", tt.value, tt.length, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("encodeLittleEndian(%d, %d) = % X, want % X", tt.value, tt.length, got, tt.want)
			}
		})
	}
}

// La longueur d'ecriture doit venir de vito.xml, pas d'une supposition a deux
// octets : ecrire deux octets sur 0x6300 deborderait sur l'adresse suivante.
func TestWriteLengthFor(t *testing.T) {
	path, err := filepath.Abs("../../third_party/vcontrold/xml/kw/vito.xml")
	if err != nil {
		t.Fatalf("failed to resolve vito.xml path: %v", err)
	}
	if err := xml.LoadAddressMappings(path); err != nil {
		t.Skipf("vito.xml unavailable (run `make deps`): %v", err)
	}

	tests := []struct {
		address uint16
		want    int
		comment string
	}{
		{0x6300, 1, "setTempWWsoll, unite ST"},
		{0x2301, 1, "setBetriebArtM1, unite BA"},
		{0x0802, 2, "getTempKist, unite UT"},
		{0x08A7, 4, "getBrennerStunden1, unite CS"},
		{0xDEAD, 1, "adresse inconnue : un octet, le choix le moins destructeur"},
	}

	for _, tt := range tests {
		if got := writeLengthFor(tt.address); got != tt.want {
			t.Errorf("writeLengthFor(0x%04X) = %d, want %d (%s)", tt.address, got, tt.want, tt.comment)
		}
	}
}
