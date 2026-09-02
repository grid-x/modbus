// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestTCPEncoding(t *testing.T) {
	packager := tcpPackager{}
	pdu := ProtocolDataUnit{}
	pdu.FunctionCode = 3
	pdu.Data = []byte{0, 4, 0, 3}

	adu, err := packager.Encode(&pdu)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{0, 1, 0, 0, 0, 6, 0, 3, 0, 4, 0, 3}
	if !bytes.Equal(expected, adu) {
		t.Fatalf("Expected %v, actual %v", expected, adu)
	}
}

func TestTCPDecoding(t *testing.T) {
	packager := tcpPackager{}
	packager.transactionID = 1
	packager.SlaveID = 17
	adu := []byte{0, 1, 0, 0, 0, 6, 17, 3, 0, 120, 0, 3}

	pdu, err := packager.Decode(adu)
	if err != nil {
		t.Fatal(err)
	}

	if pdu.FunctionCode != 3 {
		t.Fatalf("Function code: expected %v, actual %v", 3, pdu.FunctionCode)
	}
	expected := []byte{0, 120, 0, 3}
	if !bytes.Equal(expected, pdu.Data) {
		t.Fatalf("Data: expected %v, actual %v", expected, adu)
	}
}

func TestTCPTransporter(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, err = io.Copy(conn, conn)
		if err != nil {
			t.Error(err)
			return
		}
	}()
	client := &tcpTransporter{
		Address:     ln.Addr().String(),
		Timeout:     1 * time.Second,
		IdleTimeout: 100 * time.Millisecond,
		Dial:        defaultDialFunc(1 * time.Second),
	}
	req := []byte{0, 1, 0, 2, 0, 2, 1, 2}
	rsp, err := client.Send(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(req, rsp) {
		t.Fatalf("unexpected response: %x", rsp)
	}
	time.Sleep(150 * time.Millisecond)
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.conn != nil {
		t.Fatalf("connection is not closed: %+v", client.conn)
	}
}

// failWriteConn wraps a [net.Conn] so that every Write call fails with
// io.ErrClosedPipe. SetDeadline and all other methods delegate to the
// underlying Conn so that the transporter's connection-setup code succeeds
// normally; only Write is intercepted. This lets tests exercise the
// write-error code path without depending on OS TCP-buffer timing or
// net.Pipe's synchronous-close behavior.
type failWriteConn struct{ net.Conn }

func (c *failWriteConn) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

// failReadConn wraps a [net.Conn] so that every Read call fails with the given
// error and every Write call succeeds (data is discarded). SetDeadline and all
// other methods delegate to the underlying Conn so that connection-setup code
// works normally; only Read is intercepted.
type failReadConn struct {
	net.Conn
	readErr error
}

func (c *failReadConn) Read(_ []byte) (int, error)  { return 0, c.readErr }
func (c *failReadConn) Write(b []byte) (int, error) { return len(b), nil }

// TestTCPWriteErrorClosesConnection any write error must set
// mb.conn to nil so that the next Send() dials a fresh connection rather than
// reusing a dead socket.
func TestTCPWriteErrorClosesConnection(t *testing.T) {
	_, cliConn := net.Pipe()
	t.Cleanup(func() { cliConn.Close() })

	dialCalls := 0
	handler := NewTCPClientHandler("irrelevant", WithDialer(
		func(_ context.Context, _, _ string) (net.Conn, error) {
			dialCalls++
			return &failWriteConn{cliConn}, nil
		},
	))
	handler.Timeout = time.Second
	tr := &handler.tcpTransporter

	getConn := func() net.Conn {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		return tr.conn
	}

	req := []byte{0, 1, 0, 0, 0, 2, 0, 3} // TID=1, PID=0, Len=2, Unit=0, FC=3
	if _, err := tr.Send(context.Background(), req); err == nil {
		t.Fatal("expected write error, got nil")
	}

	// conn must be nil so the next Send() re-dials
	// via connect() rather than writing on a dead socket.
	if conn := getConn(); conn != nil {
		t.Fatalf("conn must be nil after write error, got %v", conn)
	}
	if dialCalls != 1 {
		t.Fatalf("expected exactly 1 dial, got %d", dialCalls)
	}
}

// TestTCPReadErrorClosesConnection verifies that a fatal read error (readResultDone
// with err != nil) sets mb.conn to nil so the next Send() dials a fresh
// connection rather than reusing a socket with an unknown receive-buffer state.
func TestTCPReadErrorClosesConnection(t *testing.T) {
	_, cliConn := net.Pipe()
	t.Cleanup(func() { cliConn.Close() })

	dialCalls := 0
	handler := NewTCPClientHandler("irrelevant", WithDialer(
		func(_ context.Context, _, _ string) (net.Conn, error) {
			dialCalls++
			return &failReadConn{Conn: cliConn, readErr: io.ErrClosedPipe}, nil
		},
	))
	handler.Timeout = time.Second
	tr := &handler.tcpTransporter

	getConn := func() net.Conn {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		return tr.conn
	}

	req := []byte{0, 1, 0, 0, 0, 2, 0, 3} // TID=1, PID=0, Len=2, Unit=0, FC=3
	if _, err := tr.Send(context.Background(), req); err == nil {
		t.Fatal("expected read error, got nil")
	}

	// conn must be nil so the next Send() re-dials rather than reading
	// stale bytes from a socket whose receive buffer is in an unknown state.
	if conn := getConn(); conn != nil {
		t.Fatalf("conn must be nil after read error, got %v", conn)
	}
	if dialCalls != 1 {
		t.Fatalf("expected exactly 1 dial, got %d", dialCalls)
	}
}

func TestErrTCPHeaderLength_Error(t *testing.T) {
	// should not explode
	_ = ErrTCPHeaderLength(1000).Error()
}

func TestTCPTransactionMismatchRetry(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	defer close(done)
	data := []byte{0xCA, 0xFE}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		// ensure that answer is only written after second read attempt failed
		time.Sleep(2500 * time.Millisecond)
		packager := &tcpPackager{SlaveID: 0}
		pdu := &ProtocolDataUnit{
			FunctionCode: FuncCodeReadInputRegisters,
			Data:         append([]byte{0x02}, data...),
		}
		data1, err := packager.Encode(pdu)
		if err != nil {
			t.Error(err)
			return
		}
		// encoding same PDU twice will increment the transaction id
		data2, err := packager.Encode(pdu)
		if err != nil {
			t.Error(err)
			return
		}
		// encoding same PDU twice will increment the transaction id
		data3, err := packager.Encode(pdu)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := conn.Write(data1); err != nil {
			t.Error(err)
			return
		}
		if _, err := conn.Write(data2); err != nil {
			t.Error(err)
			return
		}
		if _, err := conn.Write(data3); err != nil {
			t.Error(err)
			return
		}
		// keep the connection open until the main routine is finished
		<-done
	}()
	handler := NewTCPClientHandler(ln.Addr().String())
	handler.Timeout = 1 * time.Second
	handler.ProtocolRecoveryTimeout = 50 * time.Millisecond

	ctx := context.Background()
	client := NewClient(handler)

	// first two attempts should timeout
	_, err = client.ReadInputRegisters(ctx, 0, 1)
	var opError *net.OpError
	if !errors.As(err, &opError) || !opError.Timeout() {
		t.Fatalf("expected timeout error, got %q", err)
	}
	_, err = client.ReadInputRegisters(ctx, 0, 1)
	if !errors.As(err, &opError) || !opError.Timeout() {
		t.Fatalf("expected timeout error, got %q", err)
	}

	// Wait for the server to be ready
	time.Sleep(500 * time.Millisecond)

	// third attempt should succeed
	resp, err := client.ReadInputRegisters(ctx, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resp, data) {
		t.Fatalf("got wrong response: got %q wanted %q", resp, data)
	}
}

// TestTCPCloseResetsLateResponseWindow verifies that closing the connection
// collapses the late-response window. A transaction ID that was in flight on the
// old socket must not be accepted as a late response afterwards: draining it
// would leave the read to time out cleanly, which keeps the very connection the
// close was meant to discard.
func TestTCPCloseResetsLateResponseWindow(t *testing.T) {
	handler := NewTCPClientHandler("irrelevant")
	tr := &handler.tcpTransporter

	tr.lastSuccessfulTransactionID = 10
	tr.lastAttemptedTransactionID = 20
	if !tr.isLateResponse(15) {
		t.Fatal("precondition: 15 should be inside the window before the close")
	}

	tr.mu.Lock()
	err := tr.close()
	tr.mu.Unlock()
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	if tr.isLateResponse(15) {
		t.Error("no transaction ID may be treated as late after a close: nothing can still be in flight on a connection that is gone")
	}
}

// TestTCPMidFrameTimeoutClosesConnection verifies that a read timeout which hit
// after part of a response header had already been consumed closes the
// connection. Keeping such a connection leaves the remainder of that frame
// queued, so the next Send would parse those leftover bytes as a header and the
// stream would stay misaligned for good.
func TestTCPMidFrameTimeoutClosesConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	defer close(done)
	// The server reports over a channel rather than calling t.Error itself:
	// nothing joins this goroutine before the test returns, and a t.Error after
	// that point panics the whole run.
	srvErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	t.Cleanup(func() {
		wg.Wait()
		select {
		case err := <-srvErr:
			t.Errorf("server: %v", err)
		default:
		}
	})
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Write a partial header - 3 of tcpHeaderSize bytes - and then stall, so
		// that the client's ReadFull consumes those bytes and then times out.
		if _, err := conn.Write([]byte{0x00, 0x01, 0x00}); err != nil {
			srvErr <- err
			return
		}
		// keep the connection open until the main routine is finished
		<-done
	}()

	handler := NewTCPClientHandler(ln.Addr().String())
	handler.Timeout = 200 * time.Millisecond
	tr := &handler.tcpTransporter

	getConn := func() net.Conn {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		return tr.conn
	}

	_, err = NewClient(handler).ReadInputRegisters(context.Background(), 0, 1)
	if !errors.Is(err, ErrStreamDesynced) {
		t.Fatalf("expected error wrapping %v, got %q", ErrStreamDesynced, err)
	}
	if conn := getConn(); conn != nil {
		t.Fatalf("conn must be nil after a mid-frame timeout, got %v", conn)
	}
}

// TestTCPNoResendOnUnattributableResponse verifies that a response which cannot
// be attributed to the request closes the connection and is reported, rather than
// re-sending: an identical frame delivers a duplicate request, and for a write
// function code the device would execute the command twice.
//
// Scoped to the protocol-desync path. Link recovery still reissues a *read* after
// reconnecting - see TestTCPLinkRecovery* below.
func TestTCPNoResendOnUnattributableResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// ADU size of a ReadInputRegisters request: header, function code, address
	// and quantity.
	const requestLen = tcpHeaderSize + 1 + 2 + 2

	var (
		mu       sync.Mutex
		observed []uint16
		wg       sync.WaitGroup
	)
	// See TestTCPMidFrameTimeoutClosesConnection: the server must not touch t
	// from a goroutine the test does not join.
	srvErr := make(chan error, 1)
	wg.Add(1)
	t.Cleanup(func() {
		wg.Wait()
		select {
		case err := <-srvErr:
			t.Errorf("server: %v", err)
		default:
		}
	})
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		packager := &tcpPackager{SlaveID: 0}
		for {
			req := make([]byte, requestLen)
			if _, err := io.ReadFull(conn, req); err != nil {
				return // client closed the connection
			}
			mu.Lock()
			observed = append(observed, binary.BigEndian.Uint16(req))
			isFirst := len(observed) == 1
			mu.Unlock()

			resp, err := packager.Encode(&ProtocolDataUnit{
				FunctionCode: FuncCodeReadInputRegisters,
				Data:         []byte{0x02, 0xCA, 0xFE},
			})
			if err != nil {
				srvErr <- err
				return
			}
			if isFirst {
				// Answer with a transaction ID that is neither the requested one nor a
				// plausible late response, so that verify fails outside the leniency
				// window. This is where the transporter used to re-write the identical
				// request onto the wire.
				binary.BigEndian.PutUint16(resp, 0xBEEF)
			} else {
				binary.BigEndian.PutUint16(resp, binary.BigEndian.Uint16(req))
			}
			if _, err := conn.Write(resp); err != nil {
				return
			}
		}
	}()

	handler := NewTCPClientHandler(ln.Addr().String())
	handler.Timeout = time.Second
	// Non-zero, so that the leniency window is evaluated at all.
	handler.ProtocolRecoveryTimeout = 500 * time.Millisecond

	_, err = NewClient(handler).ReadInputRegisters(context.Background(), 0, 1)
	if !errors.Is(err, ErrStreamDesynced) {
		t.Fatalf("expected error wrapping %v, got %q", ErrStreamDesynced, err)
	}
	handler.Close()

	mu.Lock()
	defer mu.Unlock()
	// A resend would show up as a second entry carrying the same transaction ID.
	if len(observed) != 1 {
		t.Fatalf("expected exactly 1 request on the wire, got %d: %v", len(observed), observed)
	}
}

// countRequestsUntilClientGivesUp accepts connections on ln and records the
// function code of every request it reads, dropping each connection immediately
// without answering so that the client's link recovery engages. It returns a stop
// function that waits for the server goroutine and yields the recorded codes.
func countRequestsUntilClientGivesUp(t *testing.T, ln net.Listener) func() []byte {
	t.Helper()

	var (
		mu    sync.Mutex
		codes []byte
		wg    sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			// header + function code is enough to identify the request
			req := make([]byte, tcpHeaderSize+1)
			if _, err := io.ReadFull(conn, req); err == nil {
				mu.Lock()
				codes = append(codes, req[tcpHeaderSize])
				mu.Unlock()
			}
			conn.Close() // drop without answering
		}
	}()

	return func() []byte {
		ln.Close()
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		return append([]byte{}, codes...)
	}
}

// TestTCPLinkRecoveryDoesNotReissueWrites verifies that a write is not reissued
// after the remote side drops the connection without answering. The device may
// have executed it already, and link recovery would repeat it verbatim on every
// attempt until the recovery budget expires.
func TestTCPLinkRecoveryDoesNotReissueWrites(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := countRequestsUntilClientGivesUp(t, ln)

	handler := NewTCPClientHandler(ln.Addr().String())
	handler.Timeout = 500 * time.Millisecond
	handler.LinkRecoveryTimeout = 500 * time.Millisecond

	if _, err := NewClient(handler).WriteSingleRegister(context.Background(), 0, 1); err == nil {
		t.Fatal("expected an error, got nil")
	}
	handler.Close()

	if codes := stop(); len(codes) != 1 {
		t.Errorf("a write must reach the device at most once, got %d attempts: %v", len(codes), codes)
	}
}

// TestTCPLinkRecoveryReissuesReads is the counterpart: gating link recovery on
// the function code must not disable it for reads, where reissuing the request
// has no effect on the device.
func TestTCPLinkRecoveryReissuesReads(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stop := countRequestsUntilClientGivesUp(t, ln)

	handler := NewTCPClientHandler(ln.Addr().String())
	handler.Timeout = 500 * time.Millisecond
	handler.LinkRecoveryTimeout = 200 * time.Millisecond

	if _, err := NewClient(handler).ReadHoldingRegisters(context.Background(), 0, 1); err == nil {
		t.Fatal("expected an error, got nil")
	}
	handler.Close()

	if codes := stop(); len(codes) < 2 {
		t.Errorf("link recovery should have reissued the read, got %d attempts: %v", len(codes), codes)
	}
}

func TestIsRepeatable(t *testing.T) {
	adu := func(fc byte) []byte {
		return []byte{0, 1, 0, 0, 0, 2, 0, fc}
	}

	cases := []struct {
		name string
		adu  []byte
		want bool
	}{
		{"read holding registers", adu(FuncCodeReadHoldingRegisters), true},
		{"read input registers", adu(FuncCodeReadInputRegisters), true},
		{"read coils", adu(FuncCodeReadCoils), true},
		{"read discrete inputs", adu(FuncCodeReadDiscreteInputs), true},
		{"read FIFO queue", adu(FuncCodeReadFIFOQueue), true},
		{"read device identification", adu(FuncCodeReadDeviceIdentification), true},
		{"write single register", adu(FuncCodeWriteSingleRegister), false},
		{"write multiple registers", adu(FuncCodeWriteMultipleRegisters), false},
		{"write single coil", adu(FuncCodeWriteSingleCoil), false},
		{"write multiple coils", adu(FuncCodeWriteMultipleCoils), false},
		{"mask write register", adu(FuncCodeMaskWriteRegister), false},
		// Reads as well as writes, so it must not be repeated.
		{"read/write multiple registers", adu(FuncCodeReadWriteMultipleRegisters), false},
		// Unknown vendor code: assume it writes.
		{"vendor specific", adu(0x41), false},
		// Malformed: no function code to inspect.
		{"truncated adu", []byte{0, 1, 0, 0, 0, 2, 0}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRepeatable(tc.adu); got != tc.want {
				t.Errorf("isRepeatable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCustomDialer(t *testing.T) {
	const tRegisterNum uint16 = 0xCAFE

	const tSentinelVal uint32 = 0xBADC0DE
	const qtyUint32 = 2

	// Processes a single cli.ReadInputRegisters() and returns a static integer value.
	acceptConnAndRespond := func(srvLn net.Listener) error {
		conn, err := srvLn.Accept()
		if err != nil {
			return fmt.Errorf("accepting server connection: %w", err)
		}

		readBuf := make([]byte, bytes.MinRead)
		n, err := conn.Read(readBuf)
		if err != nil {
			return fmt.Errorf("reading from server connection: %w", err)
		}

		const fnc = FuncCodeReadInputRegisters

		// Ensure that the request originates from the test.
		requestAdu, err := (&tcpPackager{}).Decode(readBuf[:n])
		if err != nil {
			return fmt.Errorf("decoding ProtocolDataUnit: %w", err)
		}
		if requestAdu.FunctionCode != fnc {
			return fmt.Errorf("unexpected request function code (%v/%v)", requestAdu.FunctionCode, fnc)
		}
		var expectData []byte
		expectData = binary.BigEndian.AppendUint16(expectData, tRegisterNum)
		expectData = binary.BigEndian.AppendUint16(expectData, qtyUint32)
		if !slices.Equal(expectData, requestAdu.Data) {
			return fmt.Errorf("unexpected request data (%v/%v)", requestAdu.Data, expectData)
		}

		const sizeUint32 = 4
		var writeData []byte
		writeData = append(writeData, sizeUint32)
		writeData = binary.BigEndian.AppendUint32(writeData, tSentinelVal)
		pdu := &ProtocolDataUnit{
			FunctionCode: fnc,
			Data:         writeData,
		}
		responseData, err := (&tcpPackager{}).Encode(pdu)
		if err != nil {
			return fmt.Errorf("encoding ProtocolDataUnit: %w", err)
		}

		_, err = conn.Write(responseData)
		return err
	}
	mustAcceptConnAndRespond := func(srvLn net.Listener) {
		// cli.ReadInputRegisters() performs non cancellable I/O operations, so we
		// panic in case of error to avoid having to wait for the Client to time out.
		if err := acceptConnAndRespond(srvLn); err != nil {
			panic("server failed: " + err.Error())
		}
	}

	// Asserts that the response comes from the expected server.
	assertResponse := func(t *testing.T, c Client) {
		t.Helper()
		res, err := c.ReadInputRegisters(context.Background(), tRegisterNum, qtyUint32)
		if err != nil {
			t.Fatal("ReadInputRegisters:", err)
		}
		got := binary.BigEndian.Uint32(res)
		if expect := tSentinelVal; expect != got {
			t.Errorf("Expected %d, got %d", expect, got)
		}
	}

	// Creates a Client that uses a pre-dialed connection instead of calling
	// net.Dial itself.
	newClient := func(t *testing.T, srvLn net.Listener, opts ...TCPClientHandlerOption) Client {
		// Invalid server IP (TEST-NET-1, RFC5737); ensures that all I/O operations
		// are going over the pre-dialed connection instead of a connection dialed
		// by the client.
		const tAddr = "192.0.2.1"

		srvAddr := srvLn.Addr()
		conn, err := net.Dial(srvAddr.Network(), srvAddr.String())
		if err != nil {
			t.Fatal(err)
		}
		dialFn := func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		}

		return NewClient(NewTCPClientHandler(tAddr, append([]TCPClientHandlerOption{
			WithDialer(dialFn)},
			opts...,
		)...))
	}

	// Generates a new TLS certificate suitable for a test server.
	newTLSServerCert := func(t *testing.T, srvName string) tls.Certificate {
		t.Helper()
		pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			DNSNames:     []string{srvName},
			NotAfter:     time.Now().Add(10 * time.Second),
		}
		crtDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pk.Public(), pk)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{
			Certificate: [][]byte{crtDER},
			PrivateKey:  pk,
		}
	}

	t.Run("Without TLS config", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })

		cli := newClient(t, ln)

		go mustAcceptConnAndRespond(ln)
		assertResponse(t, cli)
	})

	t.Run("With TLS config", func(t *testing.T) {
		const tServerName = "test-server"

		srvCrt := newTLSServerCert(t, tServerName)

		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
			Certificates: []tls.Certificate{srvCrt},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })

		x509SrvCrt, err := x509.ParseCertificate(srvCrt.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}

		rootCAs := x509.NewCertPool()
		rootCAs.AddCert(x509SrvCrt)
		cli := newClient(t, ln,
			WithTLSConfig(&tls.Config{
				ServerName: tServerName,
				RootCAs:    rootCAs,
			}),
		)

		go mustAcceptConnAndRespond(ln)
		assertResponse(t, cli)
	})
}

func TestConnCaching(t *testing.T) {
	// Accepts exactly one connection and processes requests by returning a
	// static integer value until srvLn gets closed.
	serve := func(srvLn net.Listener) error {
		conn, err := srvLn.Accept()
		if err != nil {
			return fmt.Errorf("accepting server connection: %w", err)
		}

		var pkgr tcpPackager
		readBuf := make([]byte, bytes.MinRead)
		for {
			n, err := conn.Read(readBuf)
			if err != nil {
				if err == io.EOF {
					// test ended, srvLn was closed
					return nil
				}
				return fmt.Errorf("reading from server connection: %w", err)
			}

			requestAdu, err := pkgr.Decode(readBuf[:n])
			if err != nil {
				return fmt.Errorf("decoding ProtocolDataUnit: %w", err)
			}
			fnc := requestAdu.FunctionCode

			const sizeUint32 = 4
			var writeData []byte
			writeData = append(writeData, sizeUint32)
			writeData = binary.BigEndian.AppendUint32(writeData, 0xBADC0DE)
			pdu := &ProtocolDataUnit{
				FunctionCode: fnc,
				Data:         writeData,
			}
			responseData, err := pkgr.Encode(pdu)
			if err != nil {
				return fmt.Errorf("encoding ProtocolDataUnit: %w", err)
			}

			if _, err = conn.Write(responseData); err != nil {
				return fmt.Errorf("writing to server connection: %w", err)
			}
		}
	}
	mustServe := func(srvLn net.Listener) {
		// cli.ReadInputRegisters() performs non cancellable I/O operations, so we
		// panic in case of error to avoid having to wait for the Client to time out.
		if err := serve(srvLn); err != nil {
			panic("server failed: " + err.Error())
		}
	}

	// Calls ReadInputRegisters with test parameters.
	doSend := func(c Client) error {
		const qtyUint32 = 2
		_, err := c.ReadInputRegisters(context.Background(), 0xCAFE, qtyUint32)
		return err
	}

	// Reads tr.conn after acquiring a lock.
	getConn := func(tr *tcpTransporter) net.Conn {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		return tr.conn
	}

	// Creates a TCPClientHandler with timeouts suitable for testing.
	newHandler := func(addr string) *TCPClientHandler {
		h := NewTCPClientHandler(addr)
		h.Timeout = 5 * time.Millisecond
		return h
	}

	t.Run("With connection caching", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })

		go mustServe(ln)

		srvAddr := ln.Addr().String()
		h := newHandler(srvAddr)
		h.IdleTimeout = 5 * time.Millisecond // short, but long enough to pass on slow runners
		cli := NewClient(h)

		tr := &h.tcpTransporter
		if getConn(tr) != nil {
			t.Fatal("TCP connection should not exist on client creation")
		}

		// 1. Should succeed and result in a connection being created and cached.
		if err = doSend(cli); err != nil {
			t.Fatal("First Send failed:", err)
		}
		firstConn := getConn(tr)
		if firstConn == nil {
			t.Fatal("Connection was not created on first Send")
		}

		// 2. Should succeed and re-use the previously created connection.
		if err = doSend(cli); err != nil {
			t.Fatal("Second Send failed:", err)
		}
		if getConn(tr) != firstConn {
			t.Fatal("Connection differs from previous Send")
		}

		// 3. The connection should expire and be removed after IdleTimeout.
		time.Sleep(h.IdleTimeout + time.Millisecond)
		if getConn(tr) != nil {
			t.Fatal("Connection did not expire after sleeping for IdleTimeout")
		}

		// 4. Should create a new connection and time out due to creating a new connection.
		err = doSend(cli)
		if getConn(tr) == firstConn {
			t.Fatal("Connection was not recreated after sleeping for IdleTimeout")
		}
		if err == nil {
			t.Fatal("Third Send was expected to time out but succeeded")
		} else if netErr := (net.Error)(nil); errors.As(err, &netErr) && !netErr.Timeout() {
			t.Fatal("Third Send was expected to time out, but failed with:", err)
		}
	})

	t.Run("Without connection caching", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })

		go mustServe(ln)

		srvAddr := ln.Addr().String()
		h := newHandler(srvAddr)
		h.IdleTimeout = 0 // disable caching
		cli := NewClient(h)

		tr := &h.tcpTransporter
		if getConn(tr) != nil {
			t.Fatal("TCP connection should not exist on client creation")
		}

		// 1. Should succeed and not result in a connection being cached.
		if err = doSend(cli); err != nil {
			t.Fatal("First Send failed:", err)
		}
		if getConn(tr) != nil {
			t.Fatal("Connection unexpectedly created on Send")
		}

		// 2. Should time out due to creating a new connection.
		err = doSend(cli)
		if getConn(tr) != nil {
			t.Fatal("Connection unexpectedly created on Send")
		}
		if err == nil {
			t.Fatal("Second Send was expected to time out but succeeded")
		} else if netErr := (net.Error)(nil); errors.As(err, &netErr) && !netErr.Timeout() {
			t.Fatal("Second Send was expected to time out, but failed with:", err)
		}
	})
}

func BenchmarkTCPEncoder(b *testing.B) {
	encoder := tcpPackager{
		SlaveID: 10,
	}
	pdu := ProtocolDataUnit{
		FunctionCode: 1,
		Data:         []byte{2, 3, 4, 5, 6, 7, 8, 9},
	}
	for i := 0; i < b.N; i++ {
		_, err := encoder.Encode(&pdu)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTCPDecoder(b *testing.B) {
	decoder := tcpPackager{
		SlaveID: 10,
	}
	adu := []byte{0, 1, 0, 0, 0, 6, 17, 3, 0, 120, 0, 3}
	for i := 0; i < b.N; i++ {
		_, err := decoder.Decode(adu)
		if err != nil {
			b.Fatal(err)
		}
	}
}
