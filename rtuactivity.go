package modbus

import (
	"sync"
	"time"

	"github.com/grid-x/serial"
)

type rtuActivityTracker struct {
	// Serial port configuration.
	serial.Config

	Logger      logger
	IdleTimeout time.Duration

	lastActivity time.Time
	closeTimer   *time.Timer

	mu sync.Mutex

	// port is platform-dependent data structure for serial port.
	// rw lock
	Port
}

func (mb *rtuActivityTracker) logf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

func (mb *rtuActivityTracker) startCloseTimer() {
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
func (mb *rtuActivityTracker) closeIdle() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.IdleTimeout <= 0 {
		return
	}
	idle := time.Now().Sub(mb.lastActivity)
	if idle >= mb.IdleTimeout {
		mb.logf("modbus: closing connection due to idle timeout: %v", idle)
		mb.Port.Close()
	}
}

func (mb *rtuActivityTracker) Close() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	err = mb.Port.Close()
	mb.Port = nil
	return
}

func (mb *rtuActivityTracker) Connect() (err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return mb.connect()
}

func (mb *rtuActivityTracker) connect() (err error) {
	if mb.Port == nil {
		port, err := serial.Open(&mb.Config)
		if err != nil {
			return err
		}
		mb.Port = &serialPort{ReadWriteCloser: port}
	}
	return
}
