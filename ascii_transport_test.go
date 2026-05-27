//go:build linux || freebsd || openbsd || netbsd
// +build linux freebsd openbsd netbsd

// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	serialpkg "github.com/grid-x/serial"
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

func TestASCIISerialTransporter_ReconnectOnMidCommunicationEOF_PTY(t *testing.T) {
	master, slavePath, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master.Close()

	reqASCII := []byte(":010300000001FB\r\n")
	respASCII := []byte(":0103020000FA\r\n")
	partialResp := respASCII[:len(respASCII)-2]
	var logs bytes.Buffer

	transporter := &asciiSerialTransporter{}
	transporter.Address = slavePath
	transporter.BaudRate = 19200
	transporter.Timeout = 200 * time.Millisecond
	transporter.IdleTimeout = serialIdleTimeout
	transporter.LinkRecoveryTimeout = 200 * time.Millisecond
	transporter.Logger = log.New(&logs, "", 0)

	serverDone := make(chan error, 1)

	go func() {
		buf := make([]byte, 1024)
		n, err := master.Read(buf)
		if err != nil {
			serverDone <- fmt.Errorf("failed to read initial request: %w", err)
			return
		}
		if !bytes.Equal(buf[:n], reqASCII) {
			serverDone <- fmt.Errorf("unexpected initial request: got %q want %q", buf[:n], reqASCII)
			return
		}
		if _, err := master.Write(partialResp); err != nil {
			serverDone <- fmt.Errorf("failed to write partial response: %w", err)
			return
		}
		if err := master.Close(); err != nil {
			serverDone <- fmt.Errorf("failed to close initial PTY master: %w", err)
			return
		}
		serverDone <- nil
	}()

	_, err = transporter.Send(context.Background(), reqASCII)
	if err == nil {
		t.Fatal("expected Send to fail after reconnect attempt, got nil")
	}

	if !strings.Contains(logs.String(), "connection reset, reconnecting") {
		t.Fatalf("expected reconnect log entry, got %q", logs.String())
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for PTY server")
	}

	if !strings.Contains(err.Error(), "could not open") && !strings.Contains(err.Error(), "link recovery timeout reached") {
		t.Fatalf("expected reconnect-related error, got %v", err)
	}
}

func TestASCIISerialTransporter_PartialResponseThenTimeout(t *testing.T) {
	reqASCII := []byte(":010300000001FB\r\n")
	respASCII := []byte(":0103020000FA\r\n")

	port := &scriptedPort{
		readData: respASCII[:len(respASCII)-2],
		readErr:  serialpkg.ErrTimeout,
	}

	transporter := &asciiSerialTransporter{}
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond

	_, err := transporter.Send(context.Background(), reqASCII)
	if !errors.Is(err, serialpkg.ErrTimeout) {
		t.Fatalf("expected timeout after partial response, got %v", err)
	}
	if got := port.written.Bytes(); !bytes.Equal(got, reqASCII) {
		t.Fatalf("expected request %q, got %q", reqASCII, got)
	}
}

func TestASCIISerialTransporter_RecoveryDisabledOnReadEOF(t *testing.T) {
	reqASCII := []byte(":010300000001FB\r\n")
	port := &scriptedPort{readErr: io.EOF}

	transporter := &asciiSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = 0

	_, err := transporter.Send(context.Background(), reqASCII)
	if err == nil {
		t.Fatal("expected link recovery timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "link recovery timeout reached") || !errors.Is(err, io.EOF) {
		t.Fatalf("expected link recovery timeout wrapping EOF, got %v", err)
	}
	if got := port.written.Bytes(); !bytes.Equal(got, reqASCII) {
		t.Fatalf("expected request %q, got %q", reqASCII, got)
	}
}

func TestASCIISerialTransporter_ReconnectBudgetExhaustedOnReadEOF(t *testing.T) {
	reqASCII := []byte(":010300000001FB\r\n")
	port := &scriptedPort{readErr: io.EOF}
	recoveryTimeout := 80 * time.Millisecond

	transporter := &asciiSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = recoveryTimeout

	start := time.Now()
	_, err := transporter.Send(context.Background(), reqASCII)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected link recovery timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "link recovery timeout reached") {
		t.Fatalf("expected link recovery timeout error, got %v", err)
	}
	if !strings.Contains(err.Error(), "could not open") {
		t.Fatalf("expected reconnect open failure to be wrapped, got %v", err)
	}
	if elapsed < recoveryTimeout-20*time.Millisecond {
		t.Fatalf("expected recovery to keep retrying for about %v, returned after %v", recoveryTimeout, elapsed)
	}
	if !port.closed {
		t.Fatal("expected reconnect to close the original port after read EOF")
	}
	if got := port.written.Bytes(); !bytes.Equal(got, reqASCII) {
		t.Fatalf("expected request %q, got %q", reqASCII, got)
	}
}

func TestASCIISerialTransporter_ReconnectOnWriteEOF(t *testing.T) {
	reqASCII := []byte(":010300000001FB\r\n")
	port := &scriptedPort{writeErr: io.EOF}
	recoveryTimeout := 80 * time.Millisecond

	transporter := &asciiSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = recoveryTimeout

	start := time.Now()
	_, err := transporter.Send(context.Background(), reqASCII)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected reconnect error after write EOF, got nil")
	}
	if !strings.Contains(err.Error(), "link recovery timeout reached") || !strings.Contains(err.Error(), "could not open") {
		t.Fatalf("expected timed-out reconnect open failure, got %v", err)
	}
	if elapsed < recoveryTimeout-20*time.Millisecond {
		t.Fatalf("expected recovery to keep retrying for about %v, returned after %v", recoveryTimeout, elapsed)
	}
	if !port.closed {
		t.Fatal("expected reconnect to close the original port after write EOF")
	}
}
