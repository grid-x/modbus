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
	asciiSerialTransporter
}

// NewASCIIClientHandler allocates and initializes a ASCIIClientHandler.
func NewASCIIClientHandler(address string) *ASCIIClientHandler {
	handler := &ASCIIClientHandler{}
	handler.Address = address
	handler.Timeout = serialTimeout
	handler.IdleTimeout = serialIdleTimeout
	handler.serialPort.Logger = handler // expose the logger
	return handler
}

// ASCIIClient creates ASCII client with default handler and given connect string.
func ASCIIClient(address string) Client {
	handler := NewASCIIClientHandler(address)
	return NewClient(handler)
}

// asciiPackager implements Packager interface.
type asciiPackager struct {
	SlaveID byte
}

// SetSlave sets modbus slave id for the next client operations
func (mb *asciiPackager) SetSlave(slaveID byte) {
	mb.SlaveID = slaveID
}

// Encode encodes PDU in a ASCII frame:
//
//	Start           : 1 char
//	Address         : 2 chars
//	Function        : 2 chars
//	Data            : 0 up to 2x252 chars
//	LRC             : 2 chars
//	End             : 2 chars
func (mb *asciiPackager) Encode(pdu *ProtocolDataUnit) ([]byte, error) {
	var buf bytes.Buffer

	if _, err := buf.WriteString(asciiStart[0]); err != nil {
		return nil, err
	}
	if err := writeHex(&buf, []byte{mb.SlaveID, pdu.FunctionCode}); err != nil {
		return nil, err
	}
	if err := writeHex(&buf, pdu.Data); err != nil {
		return nil, err
	}

	// Exclude the beginning colon and terminating CRLF pair characters
	var lrc lrc
	lrc.pushByte(mb.SlaveID).pushByte(pdu.FunctionCode).pushBytes(pdu.Data)
	if err := writeHex(&buf, []byte{lrc.value()}); err != nil {
		return nil, err
	}
	if _, err := buf.WriteString(asciiEnd); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Verify verifies response length, frame boundary and slave id.
func (mb *asciiPackager) Verify(aduRequest []byte, aduResponse []byte) error {
	length := len(aduResponse)
	// Minimum size (including address, function and LRC)
	if length < asciiMinSize+6 {
		return fmt.Errorf("modbus: response length '%v' does not meet minimum '%v'", length, 9)
	}
	// Length excluding colon must be an even number
	if length%2 != 1 {
		return fmt.Errorf("modbus: response length '%v' is not an even number", length-1)
	}
	// First char must be a colon
	str := string(aduResponse[0:len(asciiStart[0])])
	if !isStartCharacter(str) {
		return fmt.Errorf("modbus: response frame '%v'... is not started with '%v'", str, asciiStart)
	}
	// 2 last chars must be \r\n
	str = string(aduResponse[len(aduResponse)-len(asciiEnd):])
	if str != asciiEnd {
		return fmt.Errorf("modbus: response frame ...'%v' is not ended with '%v'", str, asciiEnd)
	}
	// Slave id
	responseVal, err := readHex(aduResponse[1:])
	if err != nil {
		return err
	}
	requestVal, err := readHex(aduRequest[1:])
	if err != nil {
		return err
	}
	if responseVal != requestVal {
		return fmt.Errorf("modbus: response slave id '%v' does not match request '%v'", responseVal, requestVal)
	}
	return nil
}

// Decode extracts PDU from ASCII frame and verify LRC.
func (mb *asciiPackager) Decode(adu []byte) (*ProtocolDataUnit, error) {
	// Slave address
	address, err := readHex(adu[1:])
	if err != nil {
		return nil, err
	}
	// Function code
	functionCode, err := readHex(adu[3:])
	if err != nil {
		return nil, err
	}
	// Data
	dataEnd := len(adu) - 4
	aduData := adu[5:dataEnd]
	data := make([]byte, hex.DecodedLen(len(aduData)))
	if _, err = hex.Decode(data, aduData); err != nil {
		return nil, err
	}
	// LRC
	lrcVal, err := readHex(adu[dataEnd:])
	if err != nil {
		return nil, err
	}
	// Calculate checksum
	var lrc lrc
	lrc.reset()
	lrc.pushByte(address).pushByte(functionCode).pushBytes(data)
	if lrcVal != lrc.value() {
		return nil, fmt.Errorf("modbus: response lrc '%v' does not match expected '%v'", lrcVal, lrc.value())
	}

	pdu := &ProtocolDataUnit{
		FunctionCode: functionCode,
		Data:         data,
	}

	return pdu, nil
}

// asciiSerialTransporter implements Transporter interface.
type asciiSerialTransporter struct {
	serialPort
	Logger logger
}

func (mb *asciiSerialTransporter) Printf(format string, v ...interface{}) {
	if mb.Logger != nil {
		mb.Logger.Printf(format, v...)
	}
}

func (mb *asciiSerialTransporter) Send(aduRequest []byte) ([]byte, error) {
	mb.serialPort.mu.Lock()
	defer mb.serialPort.mu.Unlock()

	// Make sure port is connected
	if err := mb.serialPort.connect(); err != nil {
		return nil, err
	}
	// Start the timer to close when idle
	mb.serialPort.lastActivity = time.Now()
	mb.serialPort.startCloseTimer()

	// Send the request
	mb.serialPort.logf("modbus: send % x\n", aduRequest)
	if _, err := mb.port.Write(aduRequest); err != nil {
		return nil, err
	}
	// Get the response
	var length int
	var data [asciiMaxSize]byte
	for {
		n, err := mb.port.Read(data[length:])
		if err != nil {
			return nil, err
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

	mb.serialPort.logf("modbus: recv % x\n", data[:length])
	return data[:length], nil
}

// writeHex encodes byte to string in hexadecimal, e.g. 0xA5 => "A5"
// (encoding/hex only supports lowercase string).
func writeHex(buf *bytes.Buffer, value []byte) error {
	var str [2]byte
	for _, v := range value {
		str[0] = hexTable[v>>4]
		str[1] = hexTable[v&0x0F]

		if _, err := buf.Write(str[:]); err != nil {
			return err
		}
	}
	return nil
}

// readHex decodes hex string to byte, e.g. "8C" => 0x8C.
func readHex(data []byte) (byte, error) {
	var dst [1]byte
	if _, err := hex.Decode(dst[:], data[0:2]); err != nil {
		return 0, err
	}
	return dst[0], nil
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
