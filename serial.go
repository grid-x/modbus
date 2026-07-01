// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"errors"
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

// serialPort has configuration and I/O controller.
type serialPort struct {
	// Serial port configuration.
	serial.Config

	Logger Logger
	// IdleTimeout is the duration to close the connection when no activity.
	IdleTimeout time.Duration
	// Silent period after successful connection
	ConnectDelay time.Duration
	// Deprecated: Use LinkRecoveryBackoff.Timeout instead.
	LinkRecoveryTimeout time.Duration
	// Deprecated: LinkRecoveryBackoff with FixedBackoff().
	ReconnectRetryInterval time.Duration

	// LinkRecoveryBackoff defines the unified retry strategy for link recovery.
	// Controls retry intervals, timeout budget, and max attempts.
	LinkRecoveryBackoff *BackoffStrategy

	mu sync.Mutex
	// port is platform-dependent data structure for serial port.
	port         io.ReadWriteCloser
	lastActivity time.Time
	closeTimer   *time.Timer
	// autoMigratedBackoff is a cached strategy created from deprecated fields
	autoMigratedBackoff *BackoffStrategy
}

func (mb *serialPort) Connect(ctx context.Context) (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect(ctx)
}

// connect connects to the serial port if it is not connected. Caller must hold the mutex.
// Note: caller must handle the connection close and recovery if the connection is lost.
func (mb *serialPort) connect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if mb.port == nil {
		port, err := serial.Open(&mb.Config)
		if err != nil {
			return fmt.Errorf("could not open %s: %w", mb.Address, err)
		}
		mb.port = port
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(mb.ConnectDelay): //silent period
		}
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

func (mb *serialPort) logf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

func (mb *serialPort) shouldRecover(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (mb *serialPort) reconnect(ctx context.Context, err error) error {
	strategy := mb.getLinkRecoveryBackoff()
	if strategy == nil {
		// No recovery configured
		return fmt.Errorf("modbus: no link recovery configured: %w", err)
	}

	mb.logf("modbus: connection reset, reconnecting")
	recoveryErr := err
	if cerr := mb.close(); cerr != nil {
		recoveryErr = errors.Join(recoveryErr, cerr)
		mb.logf("modbus: error closing connection: %v", cerr)
	}

	start := time.Now()
	attempt := 0

	for {
		elapsed := time.Since(start)
		if !strategy.ShouldRetry(attempt, elapsed) {
			return fmt.Errorf("modbus: link recovery exhausted: %w", recoveryErr)
		}

		if cerr := mb.connect(ctx); cerr == nil {
			return nil
		} else {
			recoveryErr = errors.Join(recoveryErr, cerr)
			mb.logf("modbus: reconnect attempt %d failed: %v", attempt, cerr)
		}

		interval := strategy.Next(attempt)
		attempt++

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func (mb *serialPort) getLinkRecoveryBackoff() *BackoffStrategy {
	if mb.LinkRecoveryBackoff != nil {
		return mb.LinkRecoveryBackoff
	}

	// Auto-migrate from deprecated fields
	if mb.LinkRecoveryTimeout > 0 || mb.ReconnectRetryInterval > 0 {
		if mb.autoMigratedBackoff == nil {
			mb.logf("modbus: LinkRecoveryTimeout and ReconnectRetryInterval are deprecated, use LinkRecoveryBackoff instead")
			interval := mb.ReconnectRetryInterval
			if interval <= 0 {
				interval = 100 * time.Millisecond
			}
			timeout := mb.LinkRecoveryTimeout
			if timeout <= 0 {
				timeout = 30 * time.Second // Default 30s timeout
			}
			// Use exponential backoff: 100ms→2s with configured timeout
			mb.autoMigratedBackoff = &BackoffStrategy{
				Type:            BackoffExponential,
				InitialInterval: interval,
				MaxInterval:     2 * time.Second,
				Multiplier:      2.0,
				Timeout:         timeout,
				MaxAttempts:     0,
			}
		}
		return mb.autoMigratedBackoff
	}

	// No recovery configured
	return nil
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
		mb.logf("modbus: closing connection due to idle timeout: %v", idle)
		_ = mb.close()
	}
}
