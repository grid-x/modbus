package modbus

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nopCloser struct {
	io.ReadWriter

	closed atomic.Bool
}

func (n *nopCloser) Close() error {
	n.closed.Store(true)
	return nil
}

func TestSerialCloseIdle(t *testing.T) {
	port := &nopCloser{
		ReadWriter: &bytes.Buffer{},
	}
	s := serialPort{
		port:        port,
		IdleTimeout: 100 * time.Millisecond,
	}
	s.lastActivity = time.Now()
	s.startCloseTimer()

	time.Sleep(150 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !port.closed.Load() || s.port != nil {
		t.Fatalf("serial port is not closed when inactivity: %+v", port)
	}
}

func TestSerialReconnect_UsesConfiguredRetryInterval(t *testing.T) {
	var logs bytes.Buffer
	port := &nopCloser{ReadWriter: &bytes.Buffer{}}
	recoveryTimeout := 40 * time.Millisecond

	s := serialPort{
		Logger:                 log.New(&logs, "", 0),
		port:                   port,
		LinkRecoveryTimeout:    recoveryTimeout,
		ReconnectRetryInterval: 50 * time.Millisecond,
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF, time.Now().Add(recoveryTimeout))
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if !strings.Contains(err.Error(), "link recovery timeout reached") {
		t.Fatalf("expected link recovery timeout error, got %v", err)
	}
	if count := strings.Count(logs.String(), "error reconnecting"); count != 1 {
		t.Fatalf("expected exactly one reconnect attempt before timeout, got %d logs: %q", count, logs.String())
	}
	if !port.closed.Load() || s.port != nil {
		t.Fatalf("expected reconnect to close the original port: closed=%v port=%v", port.closed.Load(), s.port)
	}
}

func TestSerialReconnect_DefaultRetryIntervalRetriesMultipleTimes(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 45 * time.Millisecond

	s := serialPort{
		Logger:              log.New(&logs, "", 0),
		port:                &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryTimeout: recoveryTimeout,
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF, time.Now().Add(recoveryTimeout))
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if count := strings.Count(logs.String(), "error reconnecting"); count < 2 {
		t.Fatalf("expected default retry interval to attempt reconnect multiple times, got %d logs: %q", count, logs.String())
	}
}
