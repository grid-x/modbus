// Copyright 2018 xft. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"time"
)

// ASCIIOverTCPClientHandler implements Packager and Transporter interface.
type ASCIIOverTCPClientHandler struct {
	ASCIIPackager
	ASCIITCPTransporter
}

// NewASCIIOverTCPClientHandler allocates and initializes a ASCIIOverTCPClientHandler.
func NewASCIIOverTCPClientHandler(address string) *ASCIIOverTCPClientHandler {
	handler := &ASCIIOverTCPClientHandler{
		ASCIIPackager:       ASCIIPackager{},
		ASCIITCPTransporter: NewASCIITCPTransporter(address),
	}
	return handler
}

// ASCIIOverTCPClient creates ASCII over TCP client with default handler and given connect string.
func ASCIIOverTCPClient(address string) Client {
	handler := NewASCIIOverTCPClientHandler(address)
	return NewClient(handler)
}

var _ Transporter = (*ASCIITCPTransporter)(nil)

// ASCIITCPTransporter implements Transporter interface.
type ASCIITCPTransporter struct {
	TCPTransporter
}

// NewASCIITCPTransporter creates ASCIITCPTransporter with default values
func NewASCIITCPTransporter(address string) ASCIITCPTransporter {
	return ASCIITCPTransporter{
		TCPTransporter: NewTCPTransporter(address),
	}
}

// Send sends data to server and ensures response has required length.
func (mb *ASCIITCPTransporter) Send(aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Make sure port is connected
	if err = mb.connect(); err != nil {
		return
	}
	// Start the timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()
	// Set write and read timeout
	var timeout time.Time
	if mb.Timeout > 0 {
		timeout = mb.lastActivity.Add(mb.Timeout)
	}
	if err = mb.conn.SetDeadline(timeout); err != nil {
		return
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
