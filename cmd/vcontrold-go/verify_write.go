package main

import (
	"bytes"
	"fmt"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// verifyWrite exerce le chemin d'ecriture sans rien changer sur la chaudiere.
//
// C'est le protocole de CLAUDE.md, « Prudence sur l'ecriture » : lire la
// valeur, la reecrire a l'identique, verifier le retour 00, relire.
//
// Les octets relus sont reecrits tels quels, sans repasser par une valeur
// numerique : aucune conversion ne peut donc introduire d'ecart, et l'ecriture
// est un no-op par construction. Elle prouve malgre tout que la trame SETADDR
// est acceptee -- ce qu'aucun test unitaire ne peut etablir.
func verifyWrite(reader *viessmann.Reader, addressStr string) error {
	address, err := config.ParseHexAddress(addressStr)
	if err != nil {
		return errors.Wrap(errors.ErrValidation, fmt.Sprintf("invalid hex address '%s'", addressStr), err)
	}

	mapping, exists := xml.GetAddressMapping(address)
	if !exists {
		return errors.New(errors.ErrConfig,
			fmt.Sprintf("address 0x%04X is absent from vito.xml: refusing to write to an address of unknown length", address))
	}

	comm := reader.GetSerialComm()
	writer, ok := comm.(interface{ WriteToDevice(uint16, []byte) error })
	if !ok {
		return errors.New(errors.ErrCommunication, "this serial communicator does not support writing")
	}

	fmt.Printf("Adresse 0x%04X (%s), %d octet(s), unite %s\n",
		address, mapping.Description, mapping.Length, mapping.Unit)

	// 1. Lire la valeur courante.
	before, err := comm.ReadFromDevice(address, mapping.Length)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "step 1: failed to read the current value", err)
	}
	fmt.Printf("  1. lecture initiale    : % X\n", before)

	// 2. La reecrire a l'identique. WriteToDevice echoue si le statut n'est
	//    pas 00, ce qui couvre l'etape 3 du protocole.
	if err := writer.WriteToDevice(address, before); err != nil {
		return errors.Wrap(errors.ErrCommunication, "steps 2-3: the write was refused", err)
	}
	fmt.Printf("  2. reecriture a l'identique : % X\n", before)
	fmt.Printf("  3. statut                   : 00 (OK)\n")

	// 4. Relire et comparer.
	after, err := comm.ReadFromDevice(address, mapping.Length)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "step 4: failed to read back", err)
	}
	fmt.Printf("  4. relecture           : % X\n", after)

	if !bytes.Equal(before, after) {
		return errors.New(errors.ErrProtocol, fmt.Sprintf(
			"read-back mismatch: wrote % X but read % X -- the write path altered the value", before, after))
	}

	fmt.Printf("\nChemin d'ecriture valide : la trame SETADDR est acceptee et la valeur est inchangee.\n")
	return nil
}
