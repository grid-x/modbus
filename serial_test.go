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

	err := s.reconnect(context.Background(), io.EOF)
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if !strings.Contains(err.Error(), "link recovery exhausted") {
		t.Fatalf("expected link recovery timeout error, got %v", err)
	}
	if count := strings.Count(logs.String(), "reconnect attempt"); count != 1 {
		t.Fatalf("expected exactly one reconnect attempt before timeout, got %d logs: %q", count, logs.String())
	}
	if !port.closed.Load() || s.port != nil {
		t.Fatalf("expected reconnect to close the original port: closed=%v port=%v", port.closed.Load(), s.port)
	}
}

func TestSerialReconnect_DefaultRetryIntervalRetriesMultipleTimes(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 250 * time.Millisecond // Longer timeout to accommodate 100ms default interval

	s := serialPort{
		Logger:              log.New(&logs, "", 0),
		port:                &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryTimeout: recoveryTimeout,
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF)
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected reconnect to preserve the original EOF, got %v", err)
	}
	if count := strings.Count(logs.String(), "reconnect attempt"); count < 2 {
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

	err := s.reconnect(context.Background(), io.EOF)
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
	if count := strings.Count(logs.String(), "reconnect attempt"); count < 1 {
		t.Fatalf("expected reconnect to log failed reopen attempts before success, got %d logs: %q", count, logs.String())
	}
}

func TestSerialReconnect_WithExponentialBackoff(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 100 * time.Millisecond

	s := serialPort{
		Logger: log.New(&logs, "", 0),
		port:   &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryBackoff: &BackoffStrategy{
			Type:            BackoffExponential,
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     50 * time.Millisecond,
			Multiplier:      2.0,
			Timeout:         recoveryTimeout,
			MaxAttempts:     0,
		},
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	start := time.Now()
	err := s.reconnect(context.Background(), io.EOF)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if !strings.Contains(err.Error(), "link recovery exhausted") {
		t.Fatalf("expected link recovery exhausted error, got %v", err)
	}

	// With exponential backoff (5ms, 10ms, 20ms, 40ms, 50ms, 50ms...),
	// we should get fewer attempts than with fixed 5ms interval
	reconnectCount := strings.Count(logs.String(), "reconnect attempt")
	if reconnectCount < 2 {
		t.Fatalf("expected at least 2 reconnect attempts, got %d", reconnectCount)
	}
	if reconnectCount > 8 {
		t.Fatalf("expected exponential backoff to limit attempts, got %d", reconnectCount)
	}

	// Should respect the recovery timeout (with some tolerance for timing variance)
	if elapsed > recoveryTimeout+30*time.Millisecond {
		t.Fatalf("reconnect took too long: %v > %v", elapsed, recoveryTimeout)
	}
}

func TestSerialReconnect_WithLinearBackoff(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 80 * time.Millisecond

	s := serialPort{
		Logger: log.New(&logs, "", 0),
		port:   &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryBackoff: &BackoffStrategy{
			Type:            BackoffLinear,
			InitialInterval: 5 * time.Millisecond,
			MaxInterval:     30 * time.Millisecond,
			Multiplier:      1.0,
			Timeout:         recoveryTimeout,
			MaxAttempts:     0,
		},
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF)
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}
	if !strings.Contains(err.Error(), "link recovery exhausted") {
		t.Fatalf("expected link recovery timeout error, got %v", err)
	}

	// With linear backoff (5ms, 10ms, 15ms, 20ms, 25ms, 30ms, 30ms...),
	// should get a moderate number of attempts
	reconnectCount := strings.Count(logs.String(), "reconnect attempt")
	if reconnectCount < 2 {
		t.Fatalf("expected at least 2 reconnect attempts, got %d", reconnectCount)
	}
}

func TestSerialReconnect_BackoffRespectsDeadline(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 50 * time.Millisecond

	s := serialPort{
		Logger: log.New(&logs, "", 0),
		port:   &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryBackoff: &BackoffStrategy{
			Type:            BackoffExponential,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     5 * time.Second,
			Multiplier:      2.0,
			Timeout:         recoveryTimeout,
			MaxAttempts:     0,
		},
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	start := time.Now()
	err := s.reconnect(context.Background(), io.EOF)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected reconnect to fail")
	}
	if !strings.Contains(err.Error(), "link recovery exhausted") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	// Must not exceed recovery timeout significantly (with tolerance for timing variance)
	if elapsed > recoveryTimeout+30*time.Millisecond {
		t.Fatalf("reconnect exceeded timeout: elapsed=%v timeout=%v", elapsed, recoveryTimeout)
	}
}

func TestSerialReconnect_BackwardCompatibility(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 40 * time.Millisecond

	// Using deprecated ReconnectRetryInterval (no BackoffStrategy set)
	s := serialPort{
		Logger:                 log.New(&logs, "", 0),
		port:                   &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryTimeout:    recoveryTimeout,
		ReconnectRetryInterval: 50 * time.Millisecond, // Will limit to 1 attempt
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF)
	if err == nil {
		t.Fatal("expected reconnect to fail when the serial device is missing")
	}

	// Should log deprecation warning
	if !strings.Contains(logs.String(), "deprecated") {
		t.Fatalf("expected deprecation warning, got %q", logs.String())
	}

	// Should still respect the old field and use 50ms interval
	if count := strings.Count(logs.String(), "reconnect attempt"); count != 1 {
		t.Fatalf("expected exactly one reconnect attempt with 50ms interval, got %d", count)
	}
}

func TestSerialReconnect_BackoffStrategyTakesPrecedence(t *testing.T) {
	var logs bytes.Buffer
	recoveryTimeout := 100 * time.Millisecond

	// Both fields set - LinkRecoveryBackoff should take precedence
	s := serialPort{
		Logger:                 log.New(&logs, "", 0),
		port:                   &nopCloser{ReadWriter: &bytes.Buffer{}},
		LinkRecoveryTimeout:    recoveryTimeout,      // This should be ignored
		ReconnectRetryInterval: 50 * time.Millisecond, // This should be ignored
		LinkRecoveryBackoff: &BackoffStrategy{
			Type:            BackoffFixed,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     10 * time.Millisecond,
			Timeout:         recoveryTimeout,
			MaxAttempts:     0,
		},
	}
	s.Address = filepath.Join(t.TempDir(), "missing-serial")

	err := s.reconnect(context.Background(), io.EOF)
	if err == nil {
		t.Fatal("expected reconnect to fail")
	}

	// Should NOT log deprecation warning when LinkRecoveryBackoff is set
	if strings.Contains(logs.String(), "deprecated") {
		t.Fatalf("should not log deprecation when LinkRecoveryBackoff is set, got %q", logs.String())
	}

	// Should use LinkRecoveryBackoff (10ms fixed), not ReconnectRetryInterval (50ms)
	// Expect multiple attempts with 10ms interval
	reconnectCount := strings.Count(logs.String(), "reconnect attempt")
	if reconnectCount < 5 {
		t.Fatalf("expected multiple attempts with 10ms backoff, got %d", reconnectCount)
	}
}
