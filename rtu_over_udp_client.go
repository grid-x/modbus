package modbus

import (
	"io"
	"net"
	"sync"
)

// RTUOverUDPClientHandler implements Packager and Transporter interface.
type RTUOverUDPClientHandler struct {
	rtuPackager
	rtuUDPTransporter
}

// NewRTUOverUDPClientHandler allocates and initializes a RTUOverUDPClientHandler.
func NewRTUOverUDPClientHandler(address string) *RTUOverUDPClientHandler {
	handler := &RTUOverUDPClientHandler{}
	handler.Address = address
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
	// Transmission logger
	Logger logger

	// UDP connection
	mu   sync.Mutex
	conn net.Conn
}

// Send sends data to server and ensures adequate response for request type
func (mb *rtuUDPTransporter) Send(aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Establish a new connection if not connected
	if err = mb.connect(); err != nil {
		return
	}

	// Set write and read timeout
	// var timeout time.Time
	// if mb.Timeout > 0 {
	// 	timeout = mb.lastActivity.Add(mb.Timeout)
	// }
	// if err = mb.conn.SetDeadline(timeout); err != nil {
	// 	return
	// }

	// Send the request
	mb.logf("modbus: send % x\n", aduRequest)
	if _, err = mb.conn.Write(aduRequest); err != nil {
		return
	}
	function := aduRequest[1]
	functionFail := aduRequest[1] & 0x80
	bytesToRead := calculateResponseLength(aduRequest)

	var n int
	var n1 int
	var data [rtuMaxSize]byte
	//We first read the minimum length and then read either the full package
	//or the error package, depending on the error status (byte 2 of the response)
	n, err = io.ReadAtLeast(mb.conn, data[:], rtuMinSize)
	if err != nil {
		return
	}
	//if the function is correct
	if data[1] == function {
		//we read the rest of the bytes
		if n < bytesToRead {
			if bytesToRead > rtuMinSize && bytesToRead <= rtuMaxSize {
				n1, err = io.ReadFull(mb.conn, data[n:bytesToRead])
				n += n1
			}
		}
	} else if data[1] == functionFail {
		//for error we need to read 5 bytes
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
func (mb *rtuUDPTransporter) Connect() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect()
}

// connect establishes a new connection to the address in Address. Caller must hold the mutex before calling this method.
// Since UDP is connectionless this does little more than setting up the connection object.
func (mb *rtuUDPTransporter) connect() error {
	if mb.conn == nil {
		dialer := net.Dialer{}
		conn, err := dialer.Dial("udp", mb.Address)
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
