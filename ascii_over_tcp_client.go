// Copyright 2018 xft. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"time"
)

// ASCIIOverTCPClientHandler implements Packager and Transporter interface.
type ASCIIOverTCPClientHandler struct {
	asciiPackager
	*asciiTCPTransporter
}

// NewASCIIOverTCPClientHandler allocates and initializes a ASCIIOverTCPClientHandler.
func NewASCIIOverTCPClientHandler(address string) *ASCIIOverTCPClientHandler {
	return &ASCIIOverTCPClientHandler{
		asciiTCPTransporter: &asciiTCPTransporter{
			defaultTCPTransporter(address),
		},
	}
}

// ASCIIOverTCPClient creates ASCII over TCP client with default handler and given connect string.
func ASCIIOverTCPClient(address string) Client {
	handler := NewASCIIOverTCPClientHandler(address)
	return NewClient(handler)
}

// Clone creates a new client handler with the same underlying shared transport.
func (mb *ASCIIOverTCPClientHandler) Clone() *ASCIIOverTCPClientHandler {
	return &ASCIIOverTCPClientHandler{
		asciiTCPTransporter: mb.asciiTCPTransporter,
	}
}

// asciiTCPTransporter implements Transporter interface.
type asciiTCPTransporter struct {
	tcpTransporter
}

func (mb *asciiTCPTransporter) Send(aduRequest []byte) (aduResponse []byte, err error) {
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
