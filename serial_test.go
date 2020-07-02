package modbus

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

type nopCloser struct {
	sync.Mutex
	io.ReadWriter

	closed bool
}

func (n *nopCloser) Close() error {
	n.closed = true
	return nil
}

func TestSerialCloseIdle(t *testing.T) {
	port := &nopCloser{
		ReadWriter: &bytes.Buffer{},
	}
	s := rtuActivityTracker{
		Port:        port,
		IdleTimeout: 100 * time.Millisecond,
	}
	s.lastActivity = time.Now()
	s.startCloseTimer()

	time.Sleep(150 * time.Millisecond)
	if !port.closed && s.Port == nil {
		t.Fatalf("serial port is not closed when inactivity: %+v", port)
	}
}
