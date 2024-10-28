// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	asciiEnd     = "\r\n"
	asciiMinSize = 3
	asciiMaxSize = 513

	hexTable = "0123456789ABCDEF"
)

// Modbus ASCII defines ':' but in the field often '>' is seen.
var asciiStart = []string{":", ">"}

// ASCIIClientHandler implements Packager and Transporter interface.
type ASCIIClientHandler struct {
	asciiPackager
	*asciiSerialTransporter
}

// NewASCIIClientHandler allocates and initializes a ASCIIClientHandler.
func NewASCIIClientHandler(address string) *ASCIIClientHandler {
	return &ASCIIClientHandler{
		asciiSerialTransporter: &asciiSerialTransporter{
			serialPort: defaultSerialPort(address),
		},
	}
}

// ASCIIClient creates ASCII client with default handler and given connect string.
func ASCIIClient(address string) Client {
	handler := NewASCIIClientHandler(address)
	return NewClient(handler)
}

// Clone creates a new client handler with the same underlying shared transport.
func (mb *ASCIIClientHandler) Clone() *ASCIIClientHandler {
	return &ASCIIClientHandler{
		asciiSerialTransporter: mb.asciiSerialTransporter,
	}
}

// asciiPackager implements Packager interface.
type asciiPackager struct {
	SlaveID byte
}

// SetSlave sets modbus slave id for the next client operations
func (mb *asciiPackager) SetSlave(slaveID byte) {
	mb.SlaveID = slaveID
}

// Encode encodes PDU in an ASCII frame:
//
//	Start           : 1 char
//	Address         : 2 chars
//	Function        : 2 chars
//	Data            : 0 up to 2x252 chars
//	LRC             : 2 chars
//	End             : 2 chars
func (mb *asciiPackager) Encode(pdu *ProtocolDataUnit) (adu []byte, err error) {
	var buf bytes.Buffer

	if _, err = buf.WriteString(asciiStart[0]); err != nil {
		return
	}
	if err = writeHex(&buf, []byte{mb.SlaveID, pdu.FunctionCode}); err != nil {
		return
	}
	if err = writeHex(&buf, pdu.Data); err != nil {
		return
	}
	// Exclude the beginning colon and terminating CRLF pair characters
	var lrc lrc
	lrc.reset()
	lrc.pushByte(mb.SlaveID).pushByte(pdu.FunctionCode).pushBytes(pdu.Data)
	if err = writeHex(&buf, []byte{lrc.value()}); err != nil {
		return
	}
	if _, err = buf.WriteString(asciiEnd); err != nil {
		return
	}
	adu = buf.Bytes()
	return
}

// Verify verifies response length, frame boundary and slave id.
func (mb *asciiPackager) Verify(aduRequest []byte, aduResponse []byte) (err error) {
	length := len(aduResponse)
	// Minimum size (including address, function and LRC)
	if length < asciiMinSize+6 {
		err = fmt.Errorf("modbus: response length '%v' does not meet minimum '%v'", length, 9)
		return
	}
	// Length excluding colon must be an even number
	if length%2 != 1 {
		err = fmt.Errorf("modbus: response length '%v' is not an even number", length-1)
		return
	}
	// First char must be a colon
	str := string(aduResponse[0:len(asciiStart[0])])
	if !isStartCharacter(str) {
		err = fmt.Errorf("modbus: response frame '%v'... is not started with '%v'", str, asciiStart)
		return
	}
	// 2 last chars must be \r\n
	str = string(aduResponse[len(aduResponse)-len(asciiEnd):])
	if str != asciiEnd {
		err = fmt.Errorf("modbus: response frame ...'%v' is not ended with '%v'", str, asciiEnd)
		return
	}
	// Slave id
	responseVal, err := readHex(aduResponse[1:])
	if err != nil {
		return
	}
	requestVal, err := readHex(aduRequest[1:])
	if err != nil {
		return
	}
	if responseVal != requestVal {
		err = fmt.Errorf("modbus: response slave id '%v' does not match request '%v'", responseVal, requestVal)
		return
	}
	return
}

// Decode extracts PDU from ASCII frame and verify LRC.
func (mb *asciiPackager) Decode(adu []byte) (pdu *ProtocolDataUnit, err error) {
	pdu = &ProtocolDataUnit{}
	// Slave address
	address, err := readHex(adu[1:])
	if err != nil {
		return
	}
	// Function code
	if pdu.FunctionCode, err = readHex(adu[3:]); err != nil {
		return
	}
	// Data
	dataEnd := len(adu) - 4
	data := adu[5:dataEnd]
	pdu.Data = make([]byte, hex.DecodedLen(len(data)))
	if _, err = hex.Decode(pdu.Data, data); err != nil {
		return
	}
	// LRC
	lrcVal, err := readHex(adu[dataEnd:])
	if err != nil {
		return
	}
	// Calculate checksum
	var lrc lrc
	lrc.reset()
	lrc.pushByte(address).pushByte(pdu.FunctionCode).pushBytes(pdu.Data)
	if lrcVal != lrc.value() {
		err = fmt.Errorf("modbus: response lrc '%v' does not match expected '%v'", lrcVal, lrc.value())
		return
	}
	return
}

// asciiSerialTransporter implements Transporter interface.
type asciiSerialTransporter struct {
	serialPort
}

func (mb *asciiSerialTransporter) Send(aduRequest []byte) (aduResponse []byte, err error) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	// Make sure port is connected
	if err = mb.connect(); err != nil {
		return
	}
	// Start the timer to close when idle
	mb.lastActivity = time.Now()
	mb.startCloseTimer()

	// Send the request
	mb.logf("modbus: send % x\n", aduRequest)
	if _, err = mb.port.Write(aduRequest); err != nil {
		return
	}
	// Get the response
	var n, length int
	var data [asciiMaxSize]byte
	for {
		if n, err = mb.port.Read(data[length:]); err != nil {
			return
		}
		length += n
		if length >= asciiMaxSize || n == 0 {
			break
		}
		// Expect end of frame in the data received
		if length > asciiMinSize {
			if string(data[length-len(asciiEnd):length]) == asciiEnd {
				break
			}
		}
	}
	aduResponse = data[:length]
	mb.logf("modbus: recv % x\n", aduResponse)
	return
}

// writeHex encodes byte to string in hexadecimal, e.g. 0xA5 => "A5"
// (encoding/hex only supports lowercase string).
func writeHex(buf *bytes.Buffer, value []byte) (err error) {
	var str [2]byte
	for _, v := range value {
		str[0] = hexTable[v>>4]
		str[1] = hexTable[v&0x0F]

		if _, err = buf.Write(str[:]); err != nil {
			return
		}
	}
	return
}

// readHex decodes hex string to byte, e.g. "8C" => 0x8C.
func readHex(data []byte) (value byte, err error) {
	var dst [1]byte
	if _, err = hex.Decode(dst[:], data[0:2]); err != nil {
		return
	}
	value = dst[0]
	return
}

// isStartCharacter confirms that the given character is a Modbus ASCII start character.
func isStartCharacter(str string) bool {
	for i := range asciiStart {
		if str == asciiStart[i] {
			return true
		}
	}
	return false
}
