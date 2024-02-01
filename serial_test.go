package modbus

import (
	"bytes"
	"io"
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
