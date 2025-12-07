//go:build darwin || linux || freebsd || openbsd || netbsd
// +build darwin linux freebsd openbsd netbsd

// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
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
