// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/grid-x/serial"
)

const (
	// Default timeout
	serialTimeout     = 5 * time.Second
	serialIdleTimeout = 60 * time.Second
)

// SerialPort has configuration and I/O controller.
type SerialPort struct {
	// Serial port configuration.
	serial.Config

	Logger      logger
	IdleTimeout time.Duration

	mu sync.Mutex
	// port is platform-dependent data structure for serial port.
	port         io.ReadWriteCloser
	lastActivity time.Time
	closeTimer   *time.Timer
}

// NewSerialPort creates a serial port with default configuration.
func NewSerialPort(address string) *SerialPort {
	return &SerialPort{
		Config: serial.Config{
			Address: address,
			Timeout: serialTimeout,
		},
		IdleTimeout: serialIdleTimeout,
	}
}

// Connect opens the port.
func (mb *SerialPort) Connect() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect()
}

// connect connects to the serial port if it is not connected. Caller must hold the mutex.
func (mb *SerialPort) connect() error {
	if mb.port == nil {
		port, err := serial.Open(&mb.Config)
		if err != nil {
			return fmt.Errorf("could not open %s: %w", mb.Config.Address, err)
		}
		mb.port = port
	}
	return nil
}

// Close closes the port.
func (mb *SerialPort) Close() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.close()
}

// close closes the serial port if it is connected. Caller must hold the mutex.
func (mb *SerialPort) close() (err error) {
	if mb.port != nil {
		err = mb.port.Close()
		mb.port = nil
	}
	return
}

func (mb *SerialPort) logf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

func (mb *SerialPort) startCloseTimer() {
	if mb.IdleTimeout <= 0 {
		return
	}
	if mb.closeTimer == nil {
		mb.closeTimer = time.AfterFunc(mb.IdleTimeout, mb.closeIdle)
	} else {
		mb.closeTimer.Reset(mb.IdleTimeout)
	}
}

// closeIdle closes the connection if last activity is passed behind IdleTimeout.
func (mb *SerialPort) closeIdle() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.IdleTimeout <= 0 {
		return
	}

	if idle := time.Since(mb.lastActivity); idle >= mb.IdleTimeout {
		mb.logf("modbus: closing connection due to idle timeout: %v", idle)
		mb.close()
	}
}
