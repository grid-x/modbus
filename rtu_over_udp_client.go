package modbus

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// ErrADURequestLength informs about a wrong ADU request length.
type ErrADURequestLength int

func (length ErrADURequestLength) Error() string {
	return fmt.Sprintf("modbus: ADU request length '%d' must not be less than 2", length)
}

// ErrADUResponseLength informs about a wrong ADU request length.
type ErrADUResponseLength int

func (length ErrADUResponseLength) Error() string {
	return fmt.Sprintf("modbus: ADU response length '%d' must not be less than 2", length)
}

// RTUOverUDPClientHandler implements Packager and Transporter interface.
type RTUOverUDPClientHandler struct {
	rtuPackager
	rtuUDPTransporter
}

// NewRTUOverUDPClientHandler allocates and initializes a RTUOverUDPClientHandler.
func NewRTUOverUDPClientHandler(address string) *RTUOverUDPClientHandler {
	handler := &RTUOverUDPClientHandler{}
	handler.Address = address
	handler.Timeout = tcpTimeout
	handler.IdleTimeout = tcpIdleTimeout
	return handler
}

// RTUOverUDPClient creates RTU over UDP client with default handler and given connect string.
func RTUOverUDPClient(address string) Client {
	handler := NewRTUOverUDPClientHandler(address)
	return NewClient(handler)
}

// rtuUDPTransporter implements Transporter interface.
type rtuUDPTransporter struct {
	// Connect string
	Address string
	// Write & read timeout of a request
	Timeout time.Duration
	// Idle timeout to close the connection
	IdleTimeout time.Duration
	// Transmission logger
	Logger Logger

	// UDP connection
	mu           sync.Mutex
	conn         net.Conn
	lastActivity time.Time
	closeTimer   *time.Timer
}

// Send sends data to server and ensures adequate response for request type
func (mb *rtuUDPTransporter) Send(ctx context.Context, aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Check ADU request length
	if len(aduRequest) < 2 {
		err = ErrADURequestLength(len(aduRequest))
		return
	}

	// Establish a new connection if not connected
	if err = mb.connect(ctx); err != nil {
		return
	}

	// Set timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()

	// Set write and read timeout, without it a device that does not answer
	// blocks the read forever as UDP has no connection that could break.
	if mb.Timeout > 0 {
		if err = mb.conn.SetDeadline(mb.lastActivity.Add(mb.Timeout)); err != nil {
			return
		}
	}

	// Send the request
	mb.logf("modbus: send % x\n", aduRequest)
	if _, err = mb.conn.Write(aduRequest); err != nil {
		return
	}
	function := aduRequest[1]
	functionFail := aduRequest[1] & 0x80
	bytesToRead := calculateResponseLength(aduRequest)

	var n, n1 int
	var data [rtuMaxSize]byte
	// We first read the minimum length and then read either the full package
	// or the error package, depending on the error status (byte 2 of the response)
	n, err = io.ReadAtLeast(mb.conn, data[:], rtuMinSize)
	if err != nil {
		return
	}

	// Check ADU response length
	if len(data) < 2 {
		err = ErrADUResponseLength(len(data))
		return
	}

	// if the function is correct
	if data[1] == function {
		// we read the rest of the bytes
		if n < bytesToRead {
			if bytesToRead > rtuMinSize && bytesToRead <= rtuMaxSize {
				n1, err = io.ReadFull(mb.conn, data[n:bytesToRead])
				n += n1
			}
		}
	} else if data[1] == functionFail {
		// for error we need to read 5 bytes
		if n < rtuExceptionSize {
			n1, err = io.ReadFull(mb.conn, data[n:rtuExceptionSize])
		}
		n += n1
	}

	if err != nil {
		return
	}
	aduResponse = data[:n]
	mb.logf("modbus: recv % x\n", aduResponse)
	return
}

func (mb *rtuUDPTransporter) logf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

// Connect establishes a new connection to the address in Address.
func (mb *rtuUDPTransporter) Connect(ctx context.Context) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect(ctx)
}

// connect establishes a new connection to the address in Address. Caller must hold the mutex before calling this method.
// Since UDP is connectionless this does little more than setting up the connection object.
func (mb *rtuUDPTransporter) connect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if mb.conn == nil {
		dialer := net.Dialer{Timeout: mb.Timeout}
		conn, err := dialer.DialContext(ctx, "udp", mb.Address)
		if err != nil {
			return err
		}
		mb.conn = conn
	}
	return nil
}

// Close closes current connection.
func (mb *rtuUDPTransporter) Close() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.close()
}

// close closes current connection. Caller must hold the mutex before calling this method.
// Since UDP is connectionless this does little more than freeing up the connection object.
func (mb *rtuUDPTransporter) close() (err error) {
	if mb.conn != nil {
		err = mb.conn.Close()
		mb.conn = nil
	}
	return
}

func (mb *rtuUDPTransporter) startCloseTimer() {
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
func (mb *rtuUDPTransporter) closeIdle() {
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
