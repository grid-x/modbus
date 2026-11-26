package modbus

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// startUDPServer listens on a local UDP port and answers every request with
// the result of respond. A nil result sends no answer, like a device that is
// offline or ignores the request.
func startUDPServer(t *testing.T, respond func(request []byte) []byte) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, rtuMaxSize)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if response := respond(buf[:n]); response != nil {
				conn.WriteTo(response, addr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

// rtuFrame appends the CRC to the given slave ID, function code and data.
func rtuFrame(pdu ...byte) []byte {
	var c crc
	checksum := c.reset().pushBytes(pdu).value()
	return append(pdu, byte(checksum), byte(checksum>>8))
}

func TestRTUOverUDPClientHandlerDefaults(t *testing.T) {
	handler := NewRTUOverUDPClientHandler("127.0.0.1:502")
	if handler.Timeout != tcpTimeout {
		t.Errorf("expected default timeout %v, got %v", tcpTimeout, handler.Timeout)
	}
	if handler.IdleTimeout != tcpIdleTimeout {
		t.Errorf("expected default idle timeout %v, got %v", tcpIdleTimeout, handler.IdleTimeout)
	}
}

func TestRTUOverUDPReadHoldingRegisters(t *testing.T) {
	address := startUDPServer(t, func(request []byte) []byte {
		// Slave ID, function code, byte count and the register value
		return rtuFrame(request[0], request[1], 0x02, 0x12, 0x34)
	})

	handler := NewRTUOverUDPClientHandler(address)
	handler.SlaveID = 1
	handler.Timeout = time.Second
	defer handler.Close()

	results, err := NewClient(handler).ReadHoldingRegisters(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(results, []byte{0x12, 0x34}) {
		t.Errorf("expected % x, got % x", []byte{0x12, 0x34}, results)
	}
}

func TestRTUOverUDPTimeout(t *testing.T) {
	address := startUDPServer(t, func([]byte) []byte { return nil })

	handler := NewRTUOverUDPClientHandler(address)
	handler.SlaveID = 1
	handler.Timeout = 100 * time.Millisecond
	defer handler.Close()

	done := make(chan error, 1)
	go func() {
		_, err := NewClient(handler).ReadHoldingRegisters(context.Background(), 0, 1)
		done <- err
	}()

	select {
	case err := <-done:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("expected a timeout error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request to a silent device did not time out")
	}
}

func TestRTUOverUDPCloseIdle(t *testing.T) {
	address := startUDPServer(t, func(request []byte) []byte {
		return rtuFrame(request[0], request[1], 0x02, 0x00, 0x01)
	})

	handler := NewRTUOverUDPClientHandler(address)
	handler.SlaveID = 1
	handler.Timeout = time.Second
	handler.IdleTimeout = 100 * time.Millisecond
	defer handler.Close()

	if _, err := NewClient(handler).ReadHoldingRegisters(context.Background(), 0, 1); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.conn != nil {
		t.Fatal("connection is not closed after the idle timeout")
	}
}
