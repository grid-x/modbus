// Copyright 2018 xft. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"time"
)

// ASCIIOverTCPClientHandler implements Packager and Transporter interface.
type ASCIIOverTCPClientHandler struct {
	asciiPackager
	asciiTCPTransporter
}

// NewASCIIOverTCPClientHandler allocates and initializes a ASCIIOverTCPClientHandler.
// The handler uses exponential backoff (10ms-5s) with 30s timeout by default for link recovery.
// This is appropriate for TCP network links. For custom backoff, set LinkRecoveryBackoff explicitly.
func NewASCIIOverTCPClientHandler(address string) *ASCIIOverTCPClientHandler {
	handler := &ASCIIOverTCPClientHandler{}
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

// ASCIIOverTCPClient creates ASCII over TCP client with default handler and given connect string.
func ASCIIOverTCPClient(address string) Client {
	handler := NewASCIIOverTCPClientHandler(address)
	return NewClient(handler)
}

// asciiTCPTransporter implements Transporter interface.
type asciiTCPTransporter struct {
	tcpTransporter
}

func (mb *asciiTCPTransporter) Send(ctx context.Context, aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Make sure port is connected
	if err = mb.connect(ctx); err != nil {
		return
	}
	// Start the timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()

	// Set write and read timeout
	if mb.Timeout > 0 {
		if err = mb.conn.SetDeadline(mb.lastActivity.Add(mb.Timeout)); err != nil {
			return
		}
	}

	// Send the request
	mb.logf("modbus: send %q\n", aduRequest)
	if _, err = mb.conn.Write(aduRequest); err != nil {
		return
	}
	// Get the response
	var n, length int
	var data [asciiMaxSize]byte
	for {
		if n, err = mb.conn.Read(data[length:]); err != nil {
			return
		}
		length += n
		if length >= asciiMaxSize || n == 0 {
			break
		}
		// Expect end of frame in the data received
		if length > asciiMinSize {
			if string(data[length-len(asciiEnd):length]) == asciiEnd {
				break
			}
		}
	}
	aduResponse = data[:length]
	mb.logf("modbus: recv %q\n", aduResponse)
	return
}
