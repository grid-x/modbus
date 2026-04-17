//go:build linux || freebsd || openbsd || netbsd
// +build linux freebsd openbsd netbsd

// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestASCIISerialTransporter_Send_PTY(t *testing.T) {
	master, slavePath, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master.Close()

	// Request: 01 03 00 00 00 01 (Read Holding Registers)
	// ASCII: :010300000001FB\r\n
	reqASCII := []byte(":010300000001FB\r\n")

	// Response: 01 03 02 00 00
	// ASCII: :0103020000FA\r\n
	respASCII := []byte(":0103020000FA\r\n")

	transporter := &asciiSerialTransporter{}
	transporter.Address = slavePath
	transporter.BaudRate = 19200
	transporter.Timeout = 1 * time.Second
	transporter.IdleTimeout = serialIdleTimeout

	// Start a goroutine to read request and write response to master
	go func() {
		buf := make([]byte, 1024)
		n, err := master.Read(buf)
		if err != nil {
			return
		}
		if !bytes.Equal(buf[:n], reqASCII) {
			// t.Errorf would be racy here, just log or ignore
			return
		}
		// Write response
		_, err = master.Write(respASCII)
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}()

	ctx := context.Background()
	aduResponse, err := transporter.Send(ctx, reqASCII)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !bytes.Equal(aduResponse, respASCII) {
		t.Errorf("Expected response %s, got %s", respASCII, aduResponse)
	}
}

func TestASCIISerialTransporter_Timeout_PTY(t *testing.T) {
	master, slavePath, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master.Close()

	reqASCII := []byte(":010300000001FB\r\n")

	transporter := &asciiSerialTransporter{}
	transporter.Address = slavePath
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.IdleTimeout = serialIdleTimeout

	// Don't write anything to master

	ctx := context.Background()
	_, err = transporter.Send(ctx, reqASCII)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
}
