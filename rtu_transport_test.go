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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	serialpkg "github.com/grid-x/serial"
)

// openPTY opens a PTY pair and returns the master file and the slave path.
func openPTY() (master *os.File, slavePath string, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, "", err
	}

	// unlockpt
	var unlock int32
	// TIOCSPTLCK
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		master.Close()
		return nil, "", errno
	}

	// ptsname
	var ptyno int32
	// TIOCGPTN
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptyno)))
	if errno != 0 {
		master.Close()
		return nil, "", errno
	}

	slavePath = fmt.Sprintf("/dev/pts/%d", ptyno)
	return master, slavePath, nil
}

type scriptedPort struct {
	mu       sync.Mutex
	readData []byte
	readErr  error
	writeErr error
	written  bytes.Buffer
	closed   bool
}

func (p *scriptedPort) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.readData) > 0 {
		b[0] = p.readData[0]
		p.readData = p.readData[1:]
		return 1, nil
	}
	if p.readErr != nil {
		return 0, p.readErr
	}
	return 0, io.EOF
}

func (p *scriptedPort) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.writeErr != nil {
		return 0, p.writeErr
	}
	return p.written.Write(b)
}

func (p *scriptedPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func TestRTUSerialTransporter_Send_PTY(t *testing.T) {
	master, slavePath, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master.Close()

	// Request: 01 03 00 00 00 01 84 0A (Read Holding Registers)
	// Response: 01 03 02 00 00 B8 44
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	resp := []byte{0x01, 0x03, 0x02, 0x00, 0x00, 0xB8, 0x44}

	transporter := &rtuSerialTransporter{}
	transporter.Address = slavePath
	transporter.BaudRate = 19200
	transporter.Timeout = 1 * time.Second

	// Start a goroutine to read request and write response to master
	go func() {
		buf := make([]byte, 1024)
		n, err := master.Read(buf)
		if err != nil {
			return
		}
		if !bytes.Equal(buf[:n], req) {
			// t.Errorf would be racy here, just log or ignore
			return
		}
		// Write response
		_, err = master.Write(resp)
		if err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}()

	ctx := context.Background()
	aduResponse, err := transporter.Send(ctx, req)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !bytes.Equal(aduResponse, resp) {
		t.Errorf("Expected response %x, got %x", resp, aduResponse)
	}
}

func TestRTUSerialTransporter_Timeout_PTY(t *testing.T) {
	master, slavePath, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master.Close()

	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}

	transporter := &rtuSerialTransporter{}
	transporter.Address = slavePath
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond

	// Don't write anything to master

	ctx := context.Background()
	_, err = transporter.Send(ctx, req)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}
}

func TestRTUSerialTransporter_ReconnectOnMidCommunicationEOF_PTY(t *testing.T) {
	master1, slavePath1, err := openPTY()
	if err != nil {
		t.Skipf("Skipping PTY test: %v", err)
	}
	defer master1.Close()

	// Request: 01 03 00 00 00 01 84 0A (Read Holding Registers)
	// Response: 01 03 02 00 00 B8 44
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	resp := []byte{0x01, 0x03, 0x02, 0x00, 0x00, 0xB8, 0x44}
	partialResp := resp[:len(resp)-1]
	var logs bytes.Buffer

	transporter := &rtuSerialTransporter{}
	transporter.Address = slavePath1
	transporter.BaudRate = 19200
	transporter.Timeout = 200 * time.Millisecond
	transporter.IdleTimeout = serialIdleTimeout
	transporter.LinkRecoveryTimeout = 200 * time.Millisecond
	transporter.Logger = log.New(&logs, "", 0)

	serverDone := make(chan error, 1)

	go func() {
		buf := make([]byte, 1024)
		n, err := master1.Read(buf)
		if err != nil {
			serverDone <- fmt.Errorf("failed to read initial request: %w", err)
			return
		}
		if !bytes.Equal(buf[:n], req) {
			serverDone <- fmt.Errorf("unexpected initial request: got %x want %x", buf[:n], req)
			return
		}
		if _, err := master1.Write(partialResp); err != nil {
			serverDone <- fmt.Errorf("failed to write partial response: %w", err)
			return
		}
		if err := master1.Close(); err != nil {
			serverDone <- fmt.Errorf("failed to close initial PTY master: %w", err)
			return
		}
		serverDone <- nil
	}()

	ctx := context.Background()
	_, err = transporter.Send(ctx, req)
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

func TestRTUSerialTransporter_PartialResponseThenTimeout(t *testing.T) {
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	resp := []byte{0x01, 0x03, 0x02, 0x00, 0x00, 0xB8, 0x44}

	port := &scriptedPort{
		readData: resp[:len(resp)-2],
		readErr:  serialpkg.ErrTimeout,
	}

	transporter := &rtuSerialTransporter{}
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond

	_, err := transporter.Send(context.Background(), req)
	if !errors.Is(err, serialpkg.ErrTimeout) {
		t.Fatalf("expected timeout after partial response, got %v", err)
	}
	if got := port.written.Bytes(); !bytes.Equal(got, req) {
		t.Fatalf("expected request %x, got %x", req, got)
	}
}

func TestRTUSerialTransporter_RecoveryDisabledOnReadEOF(t *testing.T) {
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	port := &scriptedPort{readErr: io.EOF}

	transporter := &rtuSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = 0

	_, err := transporter.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected link recovery timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "link recovery timeout reached") || !errors.Is(err, io.EOF) {
		t.Fatalf("expected link recovery timeout wrapping EOF, got %v", err)
	}
	if got := port.written.Bytes(); !bytes.Equal(got, req) {
		t.Fatalf("expected request %x, got %x", req, got)
	}
}

func TestRTUSerialTransporter_ReconnectBudgetExhaustedOnReadEOF(t *testing.T) {
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	port := &scriptedPort{readErr: io.EOF}
	recoveryTimeout := 80 * time.Millisecond

	transporter := &rtuSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = recoveryTimeout

	start := time.Now()
	_, err := transporter.Send(context.Background(), req)
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
	if got := port.written.Bytes(); !bytes.Equal(got, req) {
		t.Fatalf("expected request %x, got %x", req, got)
	}
}

func TestRTUSerialTransporter_ReconnectOnWriteEOF(t *testing.T) {
	req := []byte{0x01, 0x03, 0x00, 0x00, 0x00, 0x01, 0x84, 0x0A}
	port := &scriptedPort{writeErr: io.EOF}
	recoveryTimeout := 80 * time.Millisecond

	transporter := &rtuSerialTransporter{}
	transporter.Address = filepath.Join(t.TempDir(), "missing-serial")
	transporter.port = port
	transporter.BaudRate = 19200
	transporter.Timeout = 100 * time.Millisecond
	transporter.LinkRecoveryTimeout = recoveryTimeout

	start := time.Now()
	_, err := transporter.Send(context.Background(), req)
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
