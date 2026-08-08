package main

import (
	"bytes"
	"fmt"

	"github.com/yourusername/vcontrold-go/internal/config"
	"github.com/yourusername/vcontrold-go/internal/errors"
	"github.com/yourusername/vcontrold-go/internal/viessmann"
	"github.com/yourusername/vcontrold-go/internal/xml"
)

// verifyWrite exercises the write path without changing anything on the
// boiler.
//
// The protocol is: read the value, write it back unchanged, check the 00
// status, read again.
//
// The bytes read back are rewritten as-is, never round-tripped through a
// numeric value, so no conversion can introduce a discrepancy and the write is
// a no-op by construction. It still proves the SETADDR frame is accepted —
// something no unit test can establish.
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

	fmt.Printf("Address 0x%04X (%s), %d byte(s), unit %s\n",
		address, mapping.Description, mapping.Length, mapping.Unit)

	// 1. Read the current value.
	before, err := comm.ReadFromDevice(address, mapping.Length)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "step 1: failed to read the current value", err)
	}
	fmt.Printf("  1. initial read      : % X\n", before)

	// 2. Write it back unchanged. WriteToDevice fails unless the status is 00,
	//    which covers step 3 of the protocol.
	if err := writer.WriteToDevice(address, before); err != nil {
		return errors.Wrap(errors.ErrCommunication, "steps 2-3: the write was refused", err)
	}
	fmt.Printf("  2. write back unchanged: % X\n", before)
	fmt.Printf("  3. status              : 00 (OK)\n")

	// 4. Read back and compare.
	after, err := comm.ReadFromDevice(address, mapping.Length)
	if err != nil {
		return errors.Wrap(errors.ErrCommunication, "step 4: failed to read back", err)
	}
	fmt.Printf("  4. read back         : % X\n", after)

	if !bytes.Equal(before, after) {
		return errors.New(errors.ErrProtocol, fmt.Sprintf(
			"read-back mismatch: wrote % X but read % X -- the write path altered the value", before, after))
	}

	fmt.Printf("\nWrite path verified: the SETADDR frame is accepted and the value is unchanged.\n")
	return nil
}
