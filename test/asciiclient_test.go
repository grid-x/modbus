// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license.  See the LICENSE file for details.

package test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/grid-x/modbus"
)

const (
	asciiDevice = "/dev/pts/2"
)

func TestASCIIClient(t *testing.T) {
	// Diagslave does not support broadcast id.
	handler := modbus.NewASCIIClientHandler(asciiDevice)
	handler.SlaveID = 17
	ClientTestAll(t, modbus.NewClient(handler))
}

func TestASCIIClientAdvancedUsage(t *testing.T) {
	handler := modbus.NewASCIIClientHandler(asciiDevice)
	handler.BaudRate = 19200
	handler.DataBits = 8
	handler.Parity = "E"
	handler.StopBits = 1
	handler.SlaveID = 12
	handler.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	err := handler.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()

	client := modbus.NewClient(handler)
	results, err := client.ReadDiscreteInputs(15, 2)
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
	results, err = client.ReadWriteMultipleRegisters(0, 2, 2, 2, []byte{1, 2, 3, 4})
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
}
