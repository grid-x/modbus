// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"io"
	"net"
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
	var transactionID uint32 = 1
	packager := tcpPackager{}
	packager.transactionID = &transactionID
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
