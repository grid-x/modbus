// Copyright 2018 xft. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license.  See the LICENSE file for details.

package test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/grid-x/modbus"
)

const (
	rtuOverTCPDevice = "localhost:5021"
)

func TestRTUOverTCPClient(t *testing.T) {
	// Diagslave does not support broadcast id.
	handler := modbus.NewRTUOverTCPClientHandler(rtuOverTCPDevice)
	handler.SlaveID = 17
	ClientTestAll(t, modbus.NewClient(handler))
}

func TestRTUOverTCPClientAdvancedUsage(t *testing.T) {
	handler := modbus.NewRTUOverTCPClientHandler(rtuOverTCPDevice)
	handler.Timeout = 5 * time.Second
	handler.SlaveID = 1
	handler.Logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler.Connect()
	defer handler.Close()

	client := modbus.NewClient(handler)
	results, err := client.ReadDiscreteInputs(15, 2)
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
	results, err = client.WriteMultipleRegisters(1, 2, []byte{0, 3, 0, 4})
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
	results, err = client.WriteMultipleCoils(5, 10, []byte{4, 3})
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
}
