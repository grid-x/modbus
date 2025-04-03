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
	rsp, err := client.Send(req)
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
	client := NewClient(handler)
	_, err = client.ReadInputRegisters(0, 1)
	opError, ok := err.(*net.OpError)
	if !ok || !opError.Timeout() {
		t.Fatalf("expected timeout error, got %q", err)
	}
	_, err = client.ReadInputRegisters(0, 1)
	opError, ok = err.(*net.OpError)
	if !ok || !opError.Timeout() {
		t.Fatalf("expected timeout error, got %q", err)
	}
	resp, err := client.ReadInputRegisters(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(resp, data) {
		t.Fatalf("got wrong response: got %q wanted %q", resp, data)
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
		res, err := c.ReadInputRegisters(tRegisterNum, qtyUint32)
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
		_, err := c.ReadInputRegisters(0xCAFE, qtyUint32)
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
