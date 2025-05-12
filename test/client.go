// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license.  See the LICENSE file for details.

package test

import (
	"context"
	"testing"

	"github.com/grid-x/modbus"
)

func ClientTestReadCoils(t *testing.T, client modbus.Client) {
	// Read discrete outputs 20-38:
	address := uint16(0x0013)
	quantity := uint16(0x0013)
	results, err := client.ReadCoils(context.Background(), address, quantity)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 3, len(results))
}

func ClientTestReadDiscreteInputs(t *testing.T, client modbus.Client) {
	// Read discrete inputs 197-218
	address := uint16(0x00C4)
	quantity := uint16(0x0016)
	results, err := client.ReadDiscreteInputs(context.Background(), address, quantity)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 3, len(results))
}

func ClientTestReadHoldingRegisters(t *testing.T, client modbus.Client) {
	// Read registers 108-110
	address := uint16(0x006B)
	quantity := uint16(0x0003)
	results, err := client.ReadHoldingRegisters(context.Background(), address, quantity)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 6, len(results))
}

func ClientTestReadInputRegisters(t *testing.T, client modbus.Client) {
	// Read input register 9
	address := uint16(0x0008)
	quantity := uint16(0x0001)
	results, err := client.ReadInputRegisters(context.Background(), address, quantity)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 2, len(results))
}

func ClientTestWriteSingleCoil(t *testing.T, client modbus.Client) {
	// Write coil 173 ON
	address := uint16(0x00AC)
	value := uint16(0xFF00)
	results, err := client.WriteSingleCoil(context.Background(), address, value)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 2, len(results))
}

func ClientTestWriteSingleRegister(t *testing.T, client modbus.Client) {
	// Write register 2 to 00 03 hex
	address := uint16(0x0001)
	value := uint16(0x0003)
	results, err := client.WriteSingleRegister(context.Background(), address, value)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 2, len(results))
}

func ClientTestWriteMultipleCoils(t *testing.T, client modbus.Client) {
	// Write a series of 10 coils starting at coil 20
	address := uint16(0x0013)
	quantity := uint16(0x000A)
	values := []byte{0xCD, 0x01}
	results, err := client.WriteMultipleCoils(context.Background(), address, quantity, values)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 2, len(results))
}

func ClientTestWriteMultipleRegisters(t *testing.T, client modbus.Client) {
	// Write two registers starting at 2 to 00 0A and 01 02 hex
	address := uint16(0x0001)
	quantity := uint16(0x0002)
	values := []byte{0x00, 0x0A, 0x01, 0x02}
	results, err := client.WriteMultipleRegisters(context.Background(), address, quantity, values)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 2, len(results))
}

func ClientTestMaskWriteRegisters(t *testing.T, client modbus.Client) {
	// Mask write to register 5
	address := uint16(0x0004)
	andMask := uint16(0x00F2)
	orMask := uint16(0x0025)
	results, err := client.MaskWriteRegister(context.Background(), address, andMask, orMask)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 4, len(results))
}

func ClientTestReadWriteMultipleRegisters(t *testing.T, client modbus.Client) {
	// read six registers starting at register 4, and to write three registers starting at register 15
	address := uint16(0x0003)
	quantity := uint16(0x0006)
	writeAddress := uint16(0x000E)
	writeQuantity := uint16(0x0003)
	values := []byte{0x00, 0xFF, 0x00, 0xFF, 0x00, 0xFF}
	results, err := client.ReadWriteMultipleRegisters(context.Background(), address, quantity, writeAddress, writeQuantity, values)
	if err != nil {
		t.Fatal(err)
	}
	AssertEquals(t, 12, len(results))
}

func ClientTestReadFIFOQueue(t *testing.T, client modbus.Client) {
	// Read queue starting at the pointer register 1246
	address := uint16(0x04DE)
	results, err := client.ReadFIFOQueue(context.Background(), address)
	// Server not implemented
	if err != nil {
		AssertEquals(t, "modbus: exception '1' (illegal function), function '24'", err.Error())
	} else {
		AssertEquals(t, 0, len(results))
	}
}

func ClientTestAll(t *testing.T, client modbus.Client) {
	t.Logf("testing ReadCoils")
	ClientTestReadCoils(t, client)

	t.Logf("testing ReadDiscreteInputs")
	ClientTestReadDiscreteInputs(t, client)

	t.Logf("testing ReadHoldingRegisters")
	ClientTestReadHoldingRegisters(t, client)

	t.Logf("testing ReadInputRegisters")
	ClientTestReadInputRegisters(t, client)

	t.Logf("testing WriteSingleCoil")
	ClientTestWriteSingleCoil(t, client)

	t.Logf("testing WriteSingleRegister")
	ClientTestWriteSingleRegister(t, client)

	t.Logf("testing WriteMultipleCoils")
	ClientTestWriteMultipleCoils(t, client)

	t.Logf("testing WriteMultipleRegisters")
	ClientTestWriteMultipleRegisters(t, client)

	t.Logf("testing MaskWriteRegisters")
	ClientTestMaskWriteRegisters(t, client)

	t.Logf("testing ReadWriteMultipleRegisters")
	ClientTestReadWriteMultipleRegisters(t, client)

	t.Logf("testing ReadFifoQueue")
	ClientTestReadFIFOQueue(t, client)
}
