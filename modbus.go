// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

/*
Package modbus provides a client for MODBUS TCP and RTU/ASCII.
*/
package modbus

import (
	"context"
	"fmt"
)

const (
	// FuncCodeReadDiscreteInputs for bit wise access
	FuncCodeReadDiscreteInputs = 2
	// FuncCodeReadCoils for bit wise access
	FuncCodeReadCoils = 1
	// FuncCodeWriteSingleCoil for bit wise access
	FuncCodeWriteSingleCoil = 5
	// FuncCodeWriteMultipleCoils for bit wise access
	FuncCodeWriteMultipleCoils = 15

	// FuncCodeReadInputRegisters 16-bit wise access
	FuncCodeReadInputRegisters = 4
	// FuncCodeReadHoldingRegisters 16-bit wise access
	FuncCodeReadHoldingRegisters = 3
	// FuncCodeWriteSingleRegister 16-bit wise access
	FuncCodeWriteSingleRegister = 6
	// FuncCodeWriteMultipleRegisters 16-bit wise access
	FuncCodeWriteMultipleRegisters = 16
	// FuncCodeReadWriteMultipleRegisters 16-bit wise access
	FuncCodeReadWriteMultipleRegisters = 23
	// FuncCodeMaskWriteRegister 16-bit wise access
	FuncCodeMaskWriteRegister = 22
	// FuncCodeReadFIFOQueue 16-bit wise access
	FuncCodeReadFIFOQueue = 24
	// FuncCodeReadDeviceIdentification for byte wise access
	FuncCodeReadDeviceIdentification = 43
)

// funcCodeRepeatable reports whether a request carrying this function code may be
// written to the wire more than once. Link recovery reissues a request verbatim
// after reconnecting, which is harmless for a read but means a device that
// processed a write before the connection dropped executes it again - once per
// recovery attempt.
//
// Only purely reading function codes are repeatable. Everything else counts as a
// write, including FuncCodeReadWriteMultipleRegisters (which also writes) and any
// vendor-specific code we cannot reason about: misjudging a read costs one failed
// request, misjudging a write duplicates a command on live equipment.
//
// FuncCodeReadDeviceIdentification is excluded for the same reason. It is
// Encapsulated Interface Transport, and only MEI type
// meiTypeReadDeviceIdentification is a read - MEI type 13 (CANopen General
// Reference) can write. Deciding that needs the byte after the function code,
// which is past what this predicate looks at.
//
// Transport-agnostic on purpose - the function code sits at a different offset in
// each framing (adu[1] for RTU and ASCII, adu[tcpHeaderSize] for TCP), so each
// transport extracts it and this decides the policy.
func funcCodeRepeatable(fc byte) bool {
	switch fc {
	case FuncCodeReadCoils,
		FuncCodeReadDiscreteInputs,
		FuncCodeReadHoldingRegisters,
		FuncCodeReadInputRegisters,
		FuncCodeReadFIFOQueue:
		return true
	default:
		return false
	}
}

// meiType specifies a MEI Type as defined in https://www.modbus.org/docs/Modbus_Application_Protocol_V1_1b.pdf#page=44
type meiType byte

const (
	// meiTypeReadDeviceIdentification is used together with FuncCodeReadDeviceIdentification
	meiTypeReadDeviceIdentification meiType = 14
)

// ReadDeviceIDCode specifies a Read Device ID Code as defined in https://www.modbus.org/docs/Modbus_Application_Protocol_V1_1b.pdf#page=45
type ReadDeviceIDCode byte

const (
	// ReadDeviceIDCodeBasic queries for VendorName, ProductCode, and MajorMinorRevision.
	ReadDeviceIDCodeBasic ReadDeviceIDCode = iota + 1

	// ReadDeviceIDCodeRegular queries for VendorURL, ProductName, ModelName, and UserApplicationName.
	ReadDeviceIDCodeRegular

	// ReadDeviceIDCodeExtended queries for regular and private (custom) objects.
	ReadDeviceIDCodeExtended

	// ReadDeviceIDCodeSpecific queries for specific objects.
	ReadDeviceIDCodeSpecific
)

const (
	// ExceptionCodeIllegalFunction error code
	ExceptionCodeIllegalFunction = 1
	// ExceptionCodeIllegalDataAddress error code
	ExceptionCodeIllegalDataAddress = 2
	// ExceptionCodeIllegalDataValue error code
	ExceptionCodeIllegalDataValue = 3
	// ExceptionCodeServerDeviceFailure error code
	ExceptionCodeServerDeviceFailure = 4
	// ExceptionCodeAcknowledge error code
	ExceptionCodeAcknowledge = 5
	// ExceptionCodeServerDeviceBusy error code
	ExceptionCodeServerDeviceBusy = 6
	// ExceptionCodeMemoryParityError error code
	ExceptionCodeMemoryParityError = 8
	// ExceptionCodeGatewayPathUnavailable error code
	ExceptionCodeGatewayPathUnavailable = 10
	// ExceptionCodeGatewayTargetDeviceFailedToRespond error code
	ExceptionCodeGatewayTargetDeviceFailedToRespond = 11
)

// Error implements error interface.
type Error struct {
	FunctionCode  byte
	ExceptionCode byte
}

// Error converts known modbus exception code to error message.
func (e *Error) Error() string {
	var name string
	switch e.ExceptionCode {
	case ExceptionCodeIllegalFunction:
		name = "illegal function"
	case ExceptionCodeIllegalDataAddress:
		name = "illegal data address"
	case ExceptionCodeIllegalDataValue:
		name = "illegal data value"
	case ExceptionCodeServerDeviceFailure:
		name = "server device failure"
	case ExceptionCodeAcknowledge:
		name = "acknowledge"
	case ExceptionCodeServerDeviceBusy:
		name = "server device busy"
	case ExceptionCodeMemoryParityError:
		name = "memory parity error"
	case ExceptionCodeGatewayPathUnavailable:
		name = "gateway path unavailable"
	case ExceptionCodeGatewayTargetDeviceFailedToRespond:
		name = "gateway target device failed to respond"
	default:
		name = "unknown"
	}
	return fmt.Sprintf("modbus: exception '%v' (%s), function '%v'", e.ExceptionCode, name, e.FunctionCode&0x7F)
}

// ProtocolDataUnit (PDU) is independent of underlying communication layers.
type ProtocolDataUnit struct {
	FunctionCode byte
	Data         []byte
}

// Packager specifies the communication layer.
type Packager interface {
	SetSlave(slaveID byte)
	Encode(pdu *ProtocolDataUnit) (adu []byte, err error)
	Decode(adu []byte) (pdu *ProtocolDataUnit, err error)
	Verify(aduRequest []byte, aduResponse []byte) (err error)
}

// Transporter specifies the transport layer.
type Transporter interface {
	Send(ctx context.Context, aduRequest []byte) (aduResponse []byte, err error)
}

// Connector exposes the underlying handler capability for open/connect and close the transport channel.
type Connector interface {
	Connect(ctx context.Context) error
	Close() error
}
