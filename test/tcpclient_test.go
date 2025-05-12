// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license.  See the LICENSE file for details.

package test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/grid-x/modbus"
)

const (
	tcpDevice = "localhost:5020"
)

func TestTCPClient(t *testing.T) {
	client := modbus.TCPClient(tcpDevice)
	ClientTestAll(t, client)
}

func TestTCPClientAdvancedUsage(t *testing.T) {
	handler := modbus.NewTCPClientHandler(tcpDevice)
	handler.Timeout = 5 * time.Second
	handler.SlaveID = 1
	handler.Logger = log.Default()
	ctx := context.Background()
	handler.Connect(ctx)
	defer handler.Close()

	client := modbus.NewClient(handler)
	results, err := client.ReadDiscreteInputs(ctx, 15, 2)
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
	results, err = client.WriteMultipleRegisters(ctx, 1, 2, []byte{0, 3, 0, 4})
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
	results, err = client.WriteMultipleCoils(ctx, 5, 10, []byte{4, 3})
	if err != nil || results == nil {
		t.Fatal(err, results)
	}
}
