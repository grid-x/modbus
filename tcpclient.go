// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	tcpProtocolIdentifier uint16 = 0x0000

	// Modbus Application Protocol
	tcpHeaderSize = 7
	tcpMaxLength  = 260
	// Default TCP timeout is not set
	tcpTimeout     = 10 * time.Second
	tcpIdleTimeout = 60 * time.Second
)

// ErrTCPHeaderLength informs about a wrong header length.
type ErrTCPHeaderLength int

func (length ErrTCPHeaderLength) Error() string {
	return fmt.Sprintf("modbus: length in response header '%d' must not be zero or greater than '%v'",
		length, tcpMaxLength-tcpHeaderSize+1)
}

// ErrStreamDesynced reports that responses on the connection can no longer be
// paired with the requests that were sent. It is returned wrapped around the
// underlying cause, and covers two conditions that cannot be told apart from the
// outside:
//
//   - The byte stream is misaligned. A read failed part-way through a frame, so
//     the rest of that frame is still queued and the next read would parse those
//     leftover bytes as a header.
//   - A complete, well-formed frame arrived that cannot be attributed to the
//     request that was sent, and is not a late response that can be accounted
//     for. Here the bytes are still frame-aligned, but a request is left
//     outstanding whose answer would corrupt the next exchange, and there is no
//     way to tell how many such orphans are queued.
//
// Neither is recoverable in place: the transporter closes the connection so that
// the next Send dials afresh. Callers that want the request retried must issue it
// again, which encodes a new transaction ID. The transporter deliberately does
// not retry by itself — re-writing the same ADU would put a duplicate frame with
// an identical transaction ID on the wire, and for a write function code that
// makes the device execute the command twice.
var ErrStreamDesynced = errors.New("modbus: response stream out of sync with requests")

// desynced marks err as a loss of request/response pairing, see
// [ErrStreamDesynced].
func desynced(err error) error {
	return fmt.Errorf("%w: %w", ErrStreamDesynced, err)
}

// TCPClientHandler implements Packager and Transporter interface.
type TCPClientHandler struct {
	tcpPackager
	tcpTransporter
}

// NewTCPClientHandler allocates a new TCPClientHandler with the given options.
func NewTCPClientHandler(address string, options ...TCPClientHandlerOption) *TCPClientHandler {
	h := &TCPClientHandler{}
	for _, o := range options {
		o(h)
	}
	h.Address = address
	h.Timeout = tcpTimeout
	h.IdleTimeout = tcpIdleTimeout
	if h.Dial == nil {
		h.Dial = defaultDialFunc(h.Timeout)
	}
	return h
}

// TCPClientHandlerOption configures a TCPClientHandler.
type TCPClientHandlerOption func(*TCPClientHandler)

// WithDialer returns a TCPClientHandlerOption that sets a custom Dial function.
func WithDialer(d DialFunc) TCPClientHandlerOption {
	return func(h *TCPClientHandler) {
		h.Dial = d
	}
}

// DialFunc is the prototype of a function that connects to an address on a
// named network. It Satisfies the [net.Dialer.DialContext] function signature.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func defaultDialFunc(timeout time.Duration) DialFunc {
	return (&net.Dialer{Timeout: timeout}).DialContext
}

// WithTLSConfig returns a TCPClientHandlerOption that enables TLS encryption with the given options.
func WithTLSConfig(config *tls.Config) TCPClientHandlerOption {
	return func(h *TCPClientHandler) {
		h.tlsConfig = config
	}
}

// TCPClient creates TCP client with default handler and given connect string.
func TCPClient(address string) Client {
	handler := NewTCPClientHandler(address)
	return NewClient(handler)
}

// tcpPackager implements Packager interface.
type tcpPackager struct {
	// For synchronization between messages of server & client
	transactionID uint32
	// Broadcast address is 0
	SlaveID byte
}

// SetSlave sets modbus slave id for the next client operations
func (mb *tcpPackager) SetSlave(slaveID byte) {
	mb.SlaveID = slaveID
}

// Encode adds modbus application protocol header:
//
//	Transaction identifier: 2 bytes
//	Protocol identifier: 2 bytes
//	Length: 2 bytes
//	Unit identifier: 1 byte
//	Function code: 1 byte
//	Data: n bytes
func (mb *tcpPackager) Encode(pdu *ProtocolDataUnit) (adu []byte, err error) {
	adu = make([]byte, tcpHeaderSize+1+len(pdu.Data))

	// Transaction identifier
	transactionID := atomic.AddUint32(&mb.transactionID, 1)
	binary.BigEndian.PutUint16(adu, uint16(transactionID))
	// Protocol identifier
	binary.BigEndian.PutUint16(adu[2:], tcpProtocolIdentifier)
	// Length = sizeof(SlaveID) + sizeof(FunctionCode) + Data
	length := uint16(1 + 1 + len(pdu.Data))
	binary.BigEndian.PutUint16(adu[4:], length)
	// Unit identifier
	adu[6] = mb.SlaveID

	// PDU
	adu[tcpHeaderSize] = pdu.FunctionCode
	copy(adu[tcpHeaderSize+1:], pdu.Data)
	return
}

// Verify confirms transaction, protocol and unit id.
func (mb *tcpPackager) Verify(aduRequest []byte, aduResponse []byte) error {
	return verify(aduRequest, aduResponse)
}

// Decode extracts PDU from TCP frame:
//
//	Transaction identifier: 2 bytes
//	Protocol identifier: 2 bytes
//	Length: 2 bytes
//	Unit identifier: 1 byte
func (mb *tcpPackager) Decode(adu []byte) (pdu *ProtocolDataUnit, err error) {
	// Read length value in the header
	length := binary.BigEndian.Uint16(adu[4:])
	pduLength := len(adu) - tcpHeaderSize
	if pduLength <= 0 || pduLength != int(length-1) {
		err = fmt.Errorf("modbus: length in response '%v' does not match pdu data length '%v'", length-1, pduLength)
		return
	}
	pdu = &ProtocolDataUnit{}
	// The first byte after header is function code
	pdu.FunctionCode = adu[tcpHeaderSize]
	pdu.Data = adu[tcpHeaderSize+1:]
	return
}

// tcpTransporter implements Transporter interface.
type tcpTransporter struct {
	// Connect string
	Address string
	// Connect & Read timeout
	Timeout time.Duration
	// Idle timeout to close the connection.
	// If negative, cached connections do not time out.
	// If zero, disables the caching of TCP connections and only uses dialed
	// connections for a single Send.
	IdleTimeout time.Duration
	// Recovery timeout if tcp communication misbehaves
	LinkRecoveryTimeout time.Duration
	// Recovery timeout if the protocol is malformed, e.g. wrong transaction ID
	ProtocolRecoveryTimeout time.Duration
	// Silent period after successful connection
	ConnectDelay time.Duration
	// Transmission logger
	Logger Logger

	// Dial specifies the dial function for creating TCP connections.
	// If nil, the transporter dials using the net package.
	Dial DialFunc

	// TCP connection
	mu           sync.Mutex
	conn         net.Conn
	closeTimer   *time.Timer
	lastActivity time.Time

	lastAttemptedTransactionID  uint16
	lastSuccessfulTransactionID uint16

	tlsConfig *tls.Config
}

// helper value to signify what to do in Send
type readResult int

const (
	readResultDone readResult = iota
	readResultCloseRetry
)

// Send sends data to server and ensures response length is greater than header length.
func (mb *tcpTransporter) Send(ctx context.Context, aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.IdleTimeout == 0 {
		defer mb.close()
	}

	var data [tcpMaxLength]byte
	linkRecoveryDeadline := time.Now().Add(mb.LinkRecoveryTimeout)
	protocolRecoveryDeadline := time.Now().Add(mb.ProtocolRecoveryTimeout)

	for {
		// Establish a new connection if not connected
		if err = mb.connect(ctx); err != nil {
			err = fmt.Errorf("modbus: connect: %w", err)
			return
		}

		// Set timer to close when idle
		mb.lastActivity = time.Now()
		mb.startCloseTimer()

		// Set write and read timeout
		if mb.Timeout > 0 {
			if err = mb.conn.SetDeadline(mb.lastActivity.Add(mb.Timeout)); err != nil {
				err = fmt.Errorf("modbus: set deadline: %w", err)
				return
			}
		}

		// Send data
		mb.logf("modbus: send % x", aduRequest)
		if _, err = mb.conn.Write(aduRequest); err != nil {
			// Close on any write error, regardless of its type. [net.Conn.Write]
			// has no context parameter, so every error it returns is an OS- or
			// network-level condition:
			//
			//   - EPIPE / ECONNRESET – peer closed the connection.
			//   - Timeout (net.Error.Timeout) – deadline expired mid-write; TCP
			//     may have flushed 0..N bytes. The server received a partial
			//     request and the stream is desynchronized.
			//   - Any other error (ENETDOWN, EHOSTUNREACH, partial write, …) –
			//     the connection state is equally unknown.
			//
			// For a request/response protocol like Modbus there is no
			// recoverable write error: either the server got nothing (broken
			// pipe) or it got a partial frame (stream corruption). In both cases
			// the only safe action is to close and reconnect, so that the next
			// Send() dials fresh via connect() (which is a no-op when
			// mb.conn != nil).
			mb.logf("modbus: write error, closing connection: %v", err)
			mb.close()
			err = fmt.Errorf("modbus: write: %w", err)
			return
		}

		mb.lastAttemptedTransactionID = binary.BigEndian.Uint16(aduRequest)
		var res readResult
		aduResponse, res, err = mb.readResponse(aduRequest, data[:], linkRecoveryDeadline, protocolRecoveryDeadline)
		if err != nil {
			mb.logf("modbus: read response error: %v, res: %v", err, res)
		}
		switch res {
		case readResultDone:
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() && !errors.Is(err, ErrStreamDesynced) {
					// Clean timeout: nothing of the response was consumed, so the
					// stream is still aligned to a frame boundary and the device may
					// simply be slow. Keep the connection open so the late response
					// can be drained on the next Send via ProtocolRecoveryTimeout
					// transaction-ID matching.
					//
					// A [ErrStreamDesynced] is excluded: it leaves a partial
					// frame in the receive buffer. Such a connection has to
					// be closed.
					mb.logf("modbus: read response timeout, keeping connection: %v", err)
				} else {
					mb.logf("modbus: read response error: closing connection: %v", err)
					mb.close()
				}
				err = fmt.Errorf("modbus: read response: %w", err)
			} else {
				mb.lastSuccessfulTransactionID = binary.BigEndian.Uint16(aduResponse)
			}
			return
		case readResultCloseRetry:
			mb.logf("modbus: close connection and retry reading response, because of %v", err)
			mb.close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				continue
			}
		default:
			mb.logf("modbus: unhandled read result %v", res)
			return nil, fmt.Errorf("modbus: unhandled read result %v", res)
		}
	}
}

func (mb *tcpTransporter) shouldRecover(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET)
}

func (mb *tcpTransporter) readResponse(aduRequest []byte, data []byte, recoveryDeadline time.Time, protocolDeadline time.Time) (aduResponse []byte, res readResult, err error) {
	// res is readResultDone by default, which either means we succeeded or err contains the fatal error
	for {
		var n int
		if n, err = io.ReadFull(mb.conn, data[:tcpHeaderSize]); err != nil {
			if n > 0 {
				// A partial header was consumed, so the remaining bytes of that
				// frame are still queued and the stream is misaligned.
				err = desynced(err)
				return
			}
			// recovery disabled or deadline reached - report error
			if mb.LinkRecoveryTimeout == 0 || time.Until(recoveryDeadline) < 0 {
				return
			}
			if mb.shouldRecover(err) {
				mb.logf("modbus: connection closed by remote side: %v", err)
				res = readResultCloseRetry
			}
			return
		}
		aduResponse, err = mb.processResponse(data[:]) // this also does io
		if err != nil {
			// The header was consumed in full, so any failure past this point
			// leaves the stream mid-frame - whether the announced length was
			// nonsense (which means the "header" was really the tail of an earlier
			// frame) or the body read failed part-way through.
			err = desynced(err)
			return
		}

		err = verify(aduRequest, aduResponse)
		if err != nil {
			mb.logf("modbus: verify error: %v", err)

			var mismatch errTransactionIDMismatch
			if errors.As(err, &mismatch) && mb.ProtocolRecoveryTimeout > 0 &&
				time.Until(protocolDeadline) >= 0 && mb.isLateResponse(mismatch.got) {
				// This is the (late) response to an earlier query that already timed
				// out. The frame boundary is intact, so discard it and keep reading:
				// the response we are waiting for should follow *without* sending
				// another query. (If we sent another query with transaction ID X+1
				// here, we would again get a mismatch for the response to X already
				// sitting in the buffer.)
				continue
			}

			// A complete frame belonging to neither this request nor an earlier one
			// we can account for. The bytes are still frame-aligned, but the
			// request/response pairing is lost: our own request stays outstanding and
			// its answer would corrupt the next exchange. This is also where a
			// genuinely misaligned stream ends up, when the tail of an earlier frame
			// happens to parse as a plausible header. See [ErrStreamDesynced].
			err = desynced(err)
			return
		}
		mb.logf("modbus: recv % x\n", aduResponse)
		return // everything is OK

	}
}

// isLateResponse reports whether got identifies the response to an earlier
// request that we already gave up on, i.e. whether it lies strictly between the
// last successfully verified and the currently attempted transaction ID, taking
// uint16 wraparound into account.
func (mb *tcpTransporter) isLateResponse(got uint16) bool {
	if mb.lastAttemptedTransactionID < mb.lastSuccessfulTransactionID {
		// The counter wrapped between the two, so the open interval is the union
		// of the two ends of the uint16 range.
		return got > mb.lastSuccessfulTransactionID || got < mb.lastAttemptedTransactionID
	}
	return got > mb.lastSuccessfulTransactionID && got < mb.lastAttemptedTransactionID
}

func (mb *tcpTransporter) processResponse(data []byte) (aduResponse []byte, err error) {
	// Read length, ignore transaction & protocol id (4 bytes)
	length := int(binary.BigEndian.Uint16(data[4:]))
	if length <= 0 {
		mb.flush(data[:])
		err = ErrTCPHeaderLength(length)
		return
	}
	if length > (tcpMaxLength - (tcpHeaderSize - 1)) {
		mb.flush(data[:])
		err = ErrTCPHeaderLength(length)
		return
	}
	// Skip unit id
	length += tcpHeaderSize - 1
	if _, err = io.ReadFull(mb.conn, data[tcpHeaderSize:length]); err != nil {
		return
	}
	aduResponse = data[:length]
	if len(aduResponse) == 0 {
		err = io.EOF
		return
	}
	return
}

type errTransactionIDMismatch struct {
	got, expected uint16
}

func (e errTransactionIDMismatch) Error() string {
	return fmt.Sprintf("modbus: response transaction id '%v' does not match request '%v'", e.got, e.expected)
}

type errProtocolIDMismatch struct {
	got, expected uint16
}

func (e errProtocolIDMismatch) Error() string {
	return fmt.Sprintf("modbus: response protocol id '%v' does not match request '%v'", e.got, e.expected)
}

type errUnitIDMismatch struct {
	got, expected byte
}

func (e errUnitIDMismatch) Error() string {
	return fmt.Sprintf("modbus: response unit id '%v' does not match request '%v'", e.got, e.expected)
}

const sizeInt16 = 2

func verify(aduRequest []byte, aduResponse []byte) (err error) {
	// len guard check for conversion
	if len(aduRequest) < sizeInt16 {
		return ErrADURequestLength(len(aduRequest))
	}
	if len(aduResponse) < sizeInt16 {
		return ErrADUResponseLength(len(aduResponse))
	}
	// Transaction id
	responseVal := binary.BigEndian.Uint16(aduResponse)
	requestVal := binary.BigEndian.Uint16(aduRequest)
	if responseVal != requestVal {
		err = errTransactionIDMismatch{got: responseVal, expected: requestVal}
		return
	}
	// Protocol id
	responseVal = binary.BigEndian.Uint16(aduResponse[2:])
	requestVal = binary.BigEndian.Uint16(aduRequest[2:])
	if responseVal != requestVal {
		err = errProtocolIDMismatch{got: responseVal, expected: requestVal}
		return
	}
	// Unit id (1 byte)
	if aduResponse[6] != aduRequest[6] {
		err = errUnitIDMismatch{got: aduResponse[6], expected: aduRequest[6]}
		return
	}
	return
}

// Connect establishes a new connection to the address in Address.
// Connect and Close are exported so that multiple requests can be done with one session
func (mb *tcpTransporter) Connect(ctx context.Context) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.connect(ctx)
}

func (mb *tcpTransporter) connect(ctx context.Context) error {
	if mb.conn == nil {
		conn, err := mb.Dial(ctx, "tcp", mb.Address)
		if err != nil {
			return err
		}
		if mb.tlsConfig != nil {
			conn = tls.Client(conn, mb.tlsConfig)
		}
		mb.conn = conn
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(mb.ConnectDelay): // silent period
		}
	}
	return nil
}

func (mb *tcpTransporter) startCloseTimer() {
	if mb.IdleTimeout <= 0 {
		return
	}
	if mb.closeTimer == nil {
		mb.closeTimer = time.AfterFunc(mb.IdleTimeout, mb.closeIdle)
	} else {
		mb.closeTimer.Reset(mb.IdleTimeout)
	}
}

// Close closes current connection.
func (mb *tcpTransporter) Close() error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	return mb.close()
}

// flush flushes pending data in the connection,
// returns io.EOF if connection is closed.
func (mb *tcpTransporter) flush(b []byte) (err error) {
	if err = mb.conn.SetReadDeadline(time.Now()); err != nil {
		return
	}
	// Timeout setting will be reset when reading
	if _, err = mb.conn.Read(b); err != nil {
		// Ignore timeout error
		if netError, ok := err.(net.Error); ok && netError.Timeout() {
			err = nil
		}
	}
	return
}

func (mb *tcpTransporter) logf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

// closeLocked closes current connection. Caller must hold the mutex before calling this method.
func (mb *tcpTransporter) close() (err error) {
	if mb.conn != nil {
		err = mb.conn.Close()
		mb.conn = nil
	}
	// Collapse the late-response window. Nothing can still be in flight on a
	// connection that no longer exists, so no transaction ID may be treated as a
	// late response until a new exchange succeeds on the next connection.
	//
	// Without this the window keeps spanning transactions that only ever existed
	// on the previous socket, and it widens with every further attempt because
	// lastAttemptedTransactionID advances while lastSuccessfulTransactionID stays
	// frozen. A stray frame on a fresh connection would then be taken for a late
	// response, silently drained, and the resulting clean timeout would keep a
	// connection this function had just decided to discard.
	mb.lastSuccessfulTransactionID = mb.lastAttemptedTransactionID
	return
}

// closeIdle closes the connection if last activity is passed behind IdleTimeout.
func (mb *tcpTransporter) closeIdle() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.IdleTimeout < 0 {
		return
	}

	if idle := time.Since(mb.lastActivity); idle >= mb.IdleTimeout {
		mb.logf("modbus: closing connection due to idle timeout: %v", idle)
		mb.close()
	}
}
