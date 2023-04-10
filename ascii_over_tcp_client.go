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
	asciiTCPTransporter
}

// NewASCIIOverTCPClientHandler allocates and initializes a ASCIIOverTCPClientHandler.
func NewASCIIOverTCPClientHandler(address string) *ASCIIOverTCPClientHandler {
	handler := &ASCIIOverTCPClientHandler{}
	handler.Address = address
	handler.Timeout = tcpTimeout
	handler.IdleTimeout = tcpIdleTimeout
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

func (mb *asciiTCPTransporter) Send(aduRequest []byte) ([]byte, error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Make sure port is connected
	if err := mb.connect(); err != nil {
		return nil, err
	}
	// Start the timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()
	// Set write and read timeout
	var timeout time.Time
	if mb.Timeout > 0 {
		timeout = mb.lastActivity.Add(mb.Timeout)
	}
	if err := mb.conn.SetDeadline(timeout); err != nil {
		return nil, err
	}

	// Send the request
	mb.logf("modbus: send %q\n", aduRequest)
	if _, err := mb.conn.Write(aduRequest); err != nil {
		return nil, err
	}

	// Get the response
	var length int
	var data [asciiMaxSize]byte
	for {
		n, err := mb.conn.Read(data[length:])
		if err != nil {
			return nil, err
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

	mb.logf("modbus: recv %q\n", data[:length])
	return data[:length], nil
}
