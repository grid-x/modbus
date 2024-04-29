// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/grid-x/serial"
)

const (
	// Default timeout
	serialTimeout     = 5 * time.Second
	serialIdleTimeout = 60 * time.Second
)

// serialPort has configuration and I/O controller.
type serialPort struct {
	// Serial port configuration.
	serial.Config

	Logger      *slog.Logger
	IdleTimeout time.Duration

	mu sync.Mutex
	// port is platform-dependent data structure for serial port.
	port         io.ReadWriteCloser
	lastActivity time.Time
	closeTimer   *time.Timer
}

func (mb *serialPort) Connect() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect()
}

// connect connects to the serial port if it is not connected. Caller must hold the mutex.
func (mb *serialPort) connect() error {
	if mb.port == nil {
		port, err := serial.Open(&mb.Config)
		if err != nil {
			return fmt.Errorf("could not open %s: %w", mb.Config.Address, err)
		}
		mb.port = port
	}
	return nil
}

func (mb *serialPort) Close() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.close()
}

// close closes the serial port if it is connected. Caller must hold the mutex.
func (mb *serialPort) close() (err error) {
	if mb.port != nil {
		err = mb.port.Close()
		mb.port = nil
	}
	return
}

func (mb *serialPort) Debug(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Debug(format, v...)
	}
}

func (mb *serialPort) Info(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Info(format, v...)
	}
}

func (mb *serialPort) Error(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Error(format, v...)
	}
}

func (mb *serialPort) DebugContext(ctx context.Context, format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.DebugContext(ctx, format, v...)
	}
}

func (mb *serialPort) InfoContext(ctx context.Context, format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.InfoContext(ctx, format, v...)
	}
}

func (mb *serialPort) ErrorContext(ctx context.Context, format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.ErrorContext(ctx, format, v...)
	}
}

func (mb *serialPort) startCloseTimer() {
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
func (mb *serialPort) closeIdle() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.IdleTimeout <= 0 {
		return
	}

	if idle := time.Since(mb.lastActivity); idle >= mb.IdleTimeout {
		mb.Debug("modbus: closing connection due to idle timeout: %v", idle)
		mb.close()
	}
}
