// Copyright 2018 xft. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"io"
	"time"
)

// RTUOverTCPClientHandler implements Packager and Transporter interface.
type RTUOverTCPClientHandler struct {
	rtuPackager
	rtuTCPTransporter
}

// NewRTUOverTCPClientHandler allocates and initializes a RTUOverTCPClientHandler.
// The handler uses exponential backoff (10ms-5s) with 30s timeout by default for link recovery.
// This is appropriate for TCP network links. For custom backoff, set LinkRecoveryBackoff explicitly.
func NewRTUOverTCPClientHandler(address string) *RTUOverTCPClientHandler {
	handler := &RTUOverTCPClientHandler{}
	handler.Address = address
	handler.Timeout = tcpTimeout
	handler.IdleTimeout = tcpIdleTimeout
	handler.Dial = defaultDialFunc(handler.Timeout)
	// Default exponential backoff for TCP: 10ms initial, suitable for faster network recovery
	handler.LinkRecoveryBackoff = NewExponentialBackoff(
		10*time.Millisecond, // Initial interval (network-appropriate)
		5*time.Second,       // Max interval
		30*time.Second,      // Timeout
	)
	// Default protocol recovery for transaction ID mismatches: 10ms with 100ms timeout
	// Protocol recovery is just processing junk data, fail fast if it persists
	handler.ProtocolRecoveryBackoff = NewExponentialBackoff(
		10*time.Millisecond,  // Initial interval
		50*time.Millisecond,  // Max interval
		100*time.Millisecond, // Timeout (fail fast for junk data)
	)
	return handler
}

// RTUOverTCPClient creates RTU over TCP client with default handler and given connect string.
func RTUOverTCPClient(address string) Client {
	handler := NewRTUOverTCPClientHandler(address)
	return NewClient(handler)
}

// rtuTCPTransporter implements Transporter interface.
type rtuTCPTransporter struct {
	tcpTransporter
}

// Send sends data to server and ensures adequate response for request type
func (mb *rtuTCPTransporter) Send(ctx context.Context, aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Establish a new connection if not connected
	if err = mb.connect(ctx); err != nil {
		return
	}
	// Set timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()

	// Set write and read timeout
	if mb.Timeout > 0 {
		if err = mb.conn.SetDeadline(mb.lastActivity.Add(mb.Timeout)); err != nil {
			return
		}
	}

	// Send the request
	mb.logf("modbus: send % x\n", aduRequest)
	if _, err = mb.conn.Write(aduRequest); err != nil {
		return
	}
	function := aduRequest[1]
	functionFail := aduRequest[1] & 0x80
	bytesToRead := calculateResponseLength(aduRequest)

	var n, n1 int
	var data [rtuMaxSize]byte
	// We first read the minimum length and then read either the full package
	// or the error package, depending on the error status (byte 2 of the response)
	n, err = io.ReadAtLeast(mb.conn, data[:], rtuMinSize)
	if err != nil {
		return
	}
	// if the function is correct
	if data[1] == function {
		// we read the rest of the bytes
		if n < bytesToRead {
			if bytesToRead > rtuMinSize && bytesToRead <= rtuMaxSize {
				n1, err = io.ReadFull(mb.conn, data[n:bytesToRead])
				n += n1
			}
		}
	} else if data[1] == functionFail {
		// for error we need to read 5 bytes
		if n < rtuExceptionSize {
			n1, err = io.ReadFull(mb.conn, data[n:rtuExceptionSize])
		}
		n += n1
	}

	if err != nil {
		return
	}
	aduResponse = data[:n]
	mb.logf("modbus: recv % x\n", aduResponse)
	return
}
