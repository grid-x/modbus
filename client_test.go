package modbus

import "testing"

const localhost = ":502"

func TestTcp(t *testing.T) {
	pack := TCPPackager{SlaveID: 1}
	trans := NewTCPTransporter(localhost)
	_ = NewClient2(&pack, &trans)
}

func TestRtuOverTcp(t *testing.T) {
	pack := RtuPackager{SlaveID: 1}
	trans := NewRTUTCPTransporter(localhost)
	_ = NewClient2(&pack, &trans)
}

func TestAsciiOverTcp(t *testing.T) {
	pack := ASCIIPackager{SlaveID: 1}
	trans := NewASCIITCPTransporter(localhost)
	_ = NewClient2(&pack, &trans)
}

func TestRtu(t *testing.T) {
	pack := RtuPackager{SlaveID: 1}
	trans := NewRtuSerialTransporter(localhost)
	_ = NewClient2(&pack, &trans)
}

func TestAscii(t *testing.T) {
	pack := ASCIIPackager{SlaveID: 1}
	trans := NewASCIISerialTransporter(localhost)
	_ = NewClient2(&pack, &trans)
}
