// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

/*
Package modbus provides a client for MODBUS TCP and RTU/ASCII.
*/
package modbus

import (
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
	Send(aduRequest []byte) (aduResponse []byte, err error)
}

// Connector exposes the underlying handler capability for open/connect and close the transport channel.
type Connector interface {
	Connect() error
	Close() error
}
