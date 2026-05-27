package modbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
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

type errCloser struct {
	io.ReadWriter

	closed atomic.Bool
	err    error
}

func (e *errCloser) Close() error {
	e.closed.Store(true)
	return e.err
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
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected reconnect to preserve the original EOF, got %v", err)
	}
	if count := strings.Count(logs.String(), "error reconnecting"); count < 2 {
		t.Fatalf("expected default retry interval to attempt reconnect multiple times, got %d logs: %q", count, logs.String())
	}
	if count := strings.Count(err.Error(), "could not open"); count < 2 {
		t.Fatalf("expected reconnect error to include multiple open failures, got %d in %q", count, err.Error())
	}
}

func TestSerialReconnectHotPlug_EventuallySucceedsWithinRecoveryWindow_PTY(t *testing.T) {
	var logs bytes.Buffer

	initialPort := &errCloser{
		ReadWriter: &bytes.Buffer{},
		err:        errors.New("device disappeared"),
	}
	recoveryTimeout := 200 * time.Millisecond
	stablePath := filepath.Join(t.TempDir(), "recovering-serial")

	type reopenResult struct {
		master *os.File
		err    error
	}

	reopenReady := make(chan reopenResult, 1)
	go func() {
		time.Sleep(45 * time.Millisecond)

		master, slavePath, err := openPTY()
		if err != nil {
			reopenReady <- reopenResult{err: err}
			return
		}
		if err := os.Symlink(slavePath, stablePath); err != nil {
			_ = master.Close()
			reopenReady <- reopenResult{err: err}
			return
		}
		reopenReady <- reopenResult{master: master}
	}()

	s := serialPort{
		Logger:                 log.New(&logs, "", 0),
		port:                   initialPort,
		LinkRecoveryTimeout:    recoveryTimeout,
		ReconnectRetryInterval: 10 * time.Millisecond,
	}
	s.Address = stablePath
	s.BaudRate = 19200
	s.Timeout = 50 * time.Millisecond

	err := s.reconnect(context.Background(), io.EOF, time.Now().Add(recoveryTimeout))
	if err != nil {
		t.Fatalf("expected reconnect to succeed before timeout, got %v", err)
	}
	if s.port == nil || s.port == initialPort {
		t.Fatalf("expected reconnect to replace the original port, got %v", s.port)
	}

	select {
	case result := <-reopenReady:
		if result.err != nil {
			t.Fatal(result.err)
		}
		t.Cleanup(func() {
			_ = s.Close()
			_ = result.master.Close()
		})
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for PTY reopen setup")
	}

	if !strings.Contains(logs.String(), "error closing connection") {
		t.Fatalf("expected close error to be logged, got %q", logs.String())
	}
	if count := strings.Count(logs.String(), "error reconnecting"); count < 1 {
		t.Fatalf("expected reconnect to log failed reopen attempts before success, got %d logs: %q", count, logs.String())
	}
}
