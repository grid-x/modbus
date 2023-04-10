// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"encoding/binary"
	"fmt"
)

// logger is the interface to the required logging functions
type logger interface {
	Printf(format string, v ...interface{})
}

// ClientHandler is the interface that groups the Packager and Transporter methods.
type ClientHandler interface {
	Packager
	Transporter
	Connector
}

type client struct {
	packager    Packager
	transporter Transporter
}

// NewClient creates a new modbus client with given backend handler.
func NewClient(handler ClientHandler) Client {
	return &client{packager: handler, transporter: handler}
}

// NewClient2 creates a new modbus client with given backend packager and transporter.
func NewClient2(packager Packager, transporter Transporter) Client {
	return &client{packager: packager, transporter: transporter}
}

// Request:
//
//	Function code         : 1 byte (0x01)
//	Starting address      : 2 bytes
//	Quantity of coils     : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x01)
//	Byte count            : 1 byte
//	Coil status           : N* bytes (=N or N+1)
func (mb *client) ReadCoils(address, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 2000)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadCoils,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", length, count)
	}
	return response.Data[1:], nil
}

// Request:
//
//	Function code         : 1 byte (0x02)
//	Starting address      : 2 bytes
//	Quantity of inputs    : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x02)
//	Byte count            : 1 byte
//	Input status          : N* bytes (=N or N+1)
func (mb *client) ReadDiscreteInputs(address, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 2000)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadDiscreteInputs,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", length, count)
	}
	return response.Data[1:], nil
}

// Request:
//
//	Function code         : 1 byte (0x03)
//	Starting address      : 2 bytes
//	Quantity of registers : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x03)
//	Byte count            : 1 byte
//	Register value        : Nx2 bytes
func (mb *client) ReadHoldingRegisters(address, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 125)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadHoldingRegisters,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", length, count)
	}
	return response.Data[1:], nil
}

// Request:
//
//	Function code         : 1 byte (0x04)
//	Starting address      : 2 bytes
//	Quantity of registers : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x04)
//	Byte count            : 1 byte
//	Input registers       : N bytes
func (mb *client) ReadInputRegisters(address, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 125)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadInputRegisters,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", length, count)
	}
	return response.Data[1:], nil
}

// Request:
//
//	Function code         : 1 byte (0x05)
//	Output address        : 2 bytes
//	Output value          : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x05)
//	Output address        : 2 bytes
//	Output value          : 2 bytes
func (mb *client) WriteSingleCoil(address, value uint16) ([]byte, error) {
	// The requested ON/OFF state can only be 0xFF00 and 0x0000
	if value != 0xFF00 && value != 0x0000 {
		return nil, fmt.Errorf("modbus: state '%v' must be either 0xFF00 (ON) or 0x0000 (OFF)", value)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteSingleCoil,
		Data:         dataBlock(address, value),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	// Fixed response length
	if len(response.Data) != 4 {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match expected '%v'", len(response.Data), 4)
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		return nil, fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
	}
	results := response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if value != respValue {
		return nil, fmt.Errorf("modbus: response value '%v' does not match request '%v'", respValue, value)
	}
	return results, nil
}

// Request:
//
//	Function code         : 1 byte (0x06)
//	Register address      : 2 bytes
//	Register value        : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x06)
//	Register address      : 2 bytes
//	Register value        : 2 bytes
func (mb *client) WriteSingleRegister(address, value uint16) ([]byte, error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteSingleRegister,
		Data:         dataBlock(address, value),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	// Fixed response length
	if len(response.Data) != 4 {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match expected '%v'", len(response.Data), 4)
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		return nil, fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
	}
	results := response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if value != respValue {
		return nil, fmt.Errorf("modbus: response value '%v' does not match request '%v'", respValue, value)
	}
	return results, nil
}

// Request:
//
//	Function code         : 1 byte (0x0F)
//	Starting address      : 2 bytes
//	Quantity of outputs   : 2 bytes
//	Byte count            : 1 byte
//	Outputs value         : N* bytes
//
// Response:
//
//	Function code         : 1 byte (0x0F)
//	Starting address      : 2 bytes
//	Quantity of outputs   : 2 bytes
func (mb *client) WriteMultipleCoils(address, quantity uint16, value []byte) ([]byte, error) {
	if quantity < 1 || quantity > 1968 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 1968)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteMultipleCoils,
		Data:         dataBlockSuffix(value, address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	// Fixed response length
	if len(response.Data) != 4 {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match expected '%v'", len(response.Data), 4)
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		return nil, fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
	}
	results := response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if quantity != respValue {
		return nil, fmt.Errorf("modbus: response quantity '%v' does not match request '%v'", respValue, quantity)
	}
	return results, nil
}

// Request:
//
//	Function code         : 1 byte (0x10)
//	Starting address      : 2 bytes
//	Quantity of outputs   : 2 bytes
//	Byte count            : 1 byte
//	Registers value       : N* bytes
//
// Response:
//
//	Function code         : 1 byte (0x10)
//	Starting address      : 2 bytes
//	Quantity of registers : 2 bytes
func (mb *client) WriteMultipleRegisters(address, quantity uint16, value []byte) ([]byte, error) {
	if quantity < 1 || quantity > 123 {
		return nil, fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 123)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteMultipleRegisters,
		Data:         dataBlockSuffix(value, address, quantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	// Fixed response length
	if len(response.Data) != 4 {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match expected '%v'", len(response.Data), 4)
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		return nil, fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
	}
	results := response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if quantity != respValue {
		return nil, fmt.Errorf("modbus: response quantity '%v' does not match request '%v'", respValue, quantity)
	}
	return results, nil
}

// Request:
//
//	Function code         : 1 byte (0x16)
//	Reference address     : 2 bytes
//	AND-mask              : 2 bytes
//	OR-mask               : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x16)
//	Reference address     : 2 bytes
//	AND-mask              : 2 bytes
//	OR-mask               : 2 bytes
func (mb *client) MaskWriteRegister(address, andMask, orMask uint16) ([]byte, error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeMaskWriteRegister,
		Data:         dataBlock(address, andMask, orMask),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	// Fixed response length
	if len(response.Data) != 6 {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match expected '%v'", len(response.Data), 6)
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		return nil, fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
	}
	respValue = binary.BigEndian.Uint16(response.Data[2:])
	if andMask != respValue {
		return nil, fmt.Errorf("modbus: response AND-mask '%v' does not match request '%v'", respValue, andMask)
	}
	respValue = binary.BigEndian.Uint16(response.Data[4:])
	if orMask != respValue {
		return nil, fmt.Errorf("modbus: response OR-mask '%v' does not match request '%v'", respValue, orMask)
	}
	return response.Data[2:], nil
}

// Request:
//
//	Function code         : 1 byte (0x17)
//	Read starting address : 2 bytes
//	Quantity to read      : 2 bytes
//	Write starting address: 2 bytes
//	Quantity to write     : 2 bytes
//	Write byte count      : 1 byte
//	Write registers value : N* bytes
//
// Response:
//
//	Function code         : 1 byte (0x17)
//	Byte count            : 1 byte
//	Read registers value  : Nx2 bytes
func (mb *client) ReadWriteMultipleRegisters(readAddress, readQuantity, writeAddress, writeQuantity uint16, value []byte) ([]byte, error) {
	if readQuantity < 1 || readQuantity > 125 {
		return nil, fmt.Errorf("modbus: quantity to read '%v' must be between '%v' and '%v',", readQuantity, 1, 125)
	}
	if writeQuantity < 1 || writeQuantity > 121 {
		return nil, fmt.Errorf("modbus: quantity to write '%v' must be between '%v' and '%v',", writeQuantity, 1, 121)
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadWriteMultipleRegisters,
		Data:         dataBlockSuffix(value, readAddress, readQuantity, writeAddress, writeQuantity),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	count := int(response.Data[0])
	if count != (len(response.Data) - 1) {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", len(response.Data)-1, count)
	}
	return response.Data[1:], nil
}

// Request:
//
//	Function code         : 1 byte (0x18)
//	FIFO pointer address  : 2 bytes
//
// Response:
//
//	Function code         : 1 byte (0x18)
//	Byte count            : 2 bytes
//	FIFO count            : 2 bytes
//	FIFO count            : 2 bytes (<=31)
//	FIFO value register   : Nx2 bytes
func (mb *client) ReadFIFOQueue(address uint16) ([]byte, error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadFIFOQueue,
		Data:         dataBlock(address),
	}
	response, err := mb.send(&request)
	if err != nil {
		return nil, err
	}
	if len(response.Data) < 4 {
		return nil, fmt.Errorf("modbus: response data size '%v' is less than expected '%v'", len(response.Data), 4)
	}
	count := int(binary.BigEndian.Uint16(response.Data))
	if count != (len(response.Data) - 1) {
		return nil, fmt.Errorf("modbus: response data size '%v' does not match count '%v'", len(response.Data)-1, count)
	}
	count = int(binary.BigEndian.Uint16(response.Data[2:]))
	if count > 31 {
		return nil, fmt.Errorf("modbus: fifo count '%v' is greater than expected '%v'", count, 31)
	}
	return response.Data[4:], nil
}

// Helpers

// send sends request and checks possible exception in the response.
func (mb *client) send(request *ProtocolDataUnit) (*ProtocolDataUnit, error) {
	aduRequest, err := mb.packager.Encode(request)
	if err != nil {
		return nil, err
	}
	aduResponse, err := mb.transporter.Send(aduRequest)
	if err != nil {
		return nil, err
	}
	if err := mb.packager.Verify(aduRequest, aduResponse); err != nil {
		return nil, err
	}
	response, err := mb.packager.Decode(aduResponse)
	if err != nil {
		return nil, err
	}
	// Check correct function code returned (exception)
	if response.FunctionCode != request.FunctionCode {
		return nil, responseError(response)
	}
	if response.Data == nil || len(response.Data) == 0 {
		// Empty response
		return nil, fmt.Errorf("modbus: response data is empty")
	}
	return response, nil
}

// dataBlock creates a sequence of uint16 data.
func dataBlock(value ...uint16) []byte {
	data := make([]byte, 2*len(value))
	for i, v := range value {
		binary.BigEndian.PutUint16(data[i*2:], v)
	}
	return data
}

// dataBlockSuffix creates a sequence of uint16 data and append the suffix plus its length.
func dataBlockSuffix(suffix []byte, value ...uint16) []byte {
	length := 2 * len(value)
	data := make([]byte, length+1+len(suffix))
	for i, v := range value {
		binary.BigEndian.PutUint16(data[i*2:], v)
	}
	data[length] = uint8(len(suffix))
	copy(data[length+1:], suffix)
	return data
}

func responseError(response *ProtocolDataUnit) error {
	mbError := &Error{FunctionCode: response.FunctionCode}
	if response.Data != nil && len(response.Data) > 0 {
		mbError.ExceptionCode = response.Data[0]
	}
	return mbError
}
