// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
)

// Logger is the interface to the required logging functions
type Logger interface {
	Printf(format string, v ...any)
}

// DataSizeError represents an error for invalid data-sizes i.e. for cases
// where the data-size does not match the expectation.
type DataSizeError struct {
	ExpectedBytes int
	ActualBytes   int
}

func (e *DataSizeError) Error() string {
	return fmt.Sprintf("modbus: response data size '%d' does not match count '%d'", e.ActualBytes, e.ExpectedBytes)
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
func (mb *client) ReadCoils(ctx context.Context, address, quantity uint16) (results []byte, err error) {
	if quantity < 1 || quantity > 2000 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 2000)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadCoils,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	results = response.Data[1 : count+1]
	return
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
func (mb *client) ReadDiscreteInputs(ctx context.Context, address, quantity uint16) (results []byte, err error) {
	if quantity < 1 || quantity > 2000 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 2000)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadDiscreteInputs,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	results = response.Data[1 : count+1]
	return
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
func (mb *client) ReadHoldingRegisters(ctx context.Context, address, quantity uint16) (results []byte, err error) {
	if quantity < 1 || quantity > 125 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 125)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadHoldingRegisters,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	if count != 2*int(quantity) {
		err = fmt.Errorf("modbus: response data size '%v' does not match request quantity '%v'", length, quantity)
		return
	}
	results = response.Data[1 : count+1]
	return
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
func (mb *client) ReadInputRegisters(ctx context.Context, address, quantity uint16) (results []byte, err error) {
	if quantity < 1 || quantity > 125 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 125)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadInputRegisters,
		Data:         dataBlock(address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	if count != 2*int(quantity) {
		err = fmt.Errorf("modbus: response data size '%v' does not match request quantity '%v'", length, quantity)
		return
	}
	results = response.Data[1 : count+1]
	return
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
func (mb *client) WriteSingleCoil(ctx context.Context, address, value uint16) (results []byte, err error) {
	// The requested ON/OFF state can only be 0xFF00 and 0x0000
	if value != 0xFF00 && value != 0x0000 {
		err = fmt.Errorf("modbus: state '%v' must be either 0xFF00 (ON) or 0x0000 (OFF)", value)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteSingleCoil,
		Data:         dataBlock(address, value),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	// Fixed response length
	if len(response.Data) != 4 {
		err = &DataSizeError{ExpectedBytes: 4, ActualBytes: len(response.Data)}
		return
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		err = fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
		return
	}
	results = response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if value != respValue {
		err = fmt.Errorf("modbus: response value '%v' does not match request '%v'", respValue, value)
		return
	}
	return
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
func (mb *client) WriteSingleRegister(ctx context.Context, address, value uint16) (results []byte, err error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteSingleRegister,
		Data:         dataBlock(address, value),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	// Fixed response length
	if len(response.Data) != 4 {
		err = &DataSizeError{ExpectedBytes: 4, ActualBytes: len(response.Data)}
		return
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		err = fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
		return
	}
	results = response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if value != respValue {
		err = fmt.Errorf("modbus: response value '%v' does not match request '%v'", respValue, value)
		return
	}
	return
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
func (mb *client) WriteMultipleCoils(ctx context.Context, address, quantity uint16, value []byte) (results []byte, err error) {
	if quantity < 1 || quantity > 1968 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 1968)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteMultipleCoils,
		Data:         dataBlockSuffix(value, address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	// Fixed response length
	if len(response.Data) != 4 {
		err = &DataSizeError{ExpectedBytes: 4, ActualBytes: len(response.Data)}
		return
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		err = fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
		return
	}
	results = response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if quantity != respValue {
		err = fmt.Errorf("modbus: response quantity '%v' does not match request '%v'", respValue, quantity)
		return
	}
	return
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
func (mb *client) WriteMultipleRegisters(ctx context.Context, address, quantity uint16, value []byte) (results []byte, err error) {
	if quantity < 1 || quantity > 123 {
		err = fmt.Errorf("modbus: quantity '%v' must be between '%v' and '%v',", quantity, 1, 123)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeWriteMultipleRegisters,
		Data:         dataBlockSuffix(value, address, quantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	// Fixed response length
	if len(response.Data) != 4 {
		err = &DataSizeError{ExpectedBytes: 4, ActualBytes: len(response.Data)}
		return
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		err = fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
		return
	}
	results = response.Data[2:]
	respValue = binary.BigEndian.Uint16(results)
	if quantity != respValue {
		err = fmt.Errorf("modbus: response quantity '%v' does not match request '%v'", respValue, quantity)
		return
	}
	return
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
func (mb *client) MaskWriteRegister(ctx context.Context, address, andMask, orMask uint16) (results []byte, err error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeMaskWriteRegister,
		Data:         dataBlock(address, andMask, orMask),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	// Fixed response length
	if len(response.Data) != 6 {
		err = &DataSizeError{ExpectedBytes: 6, ActualBytes: len(response.Data)}
		return
	}
	respValue := binary.BigEndian.Uint16(response.Data)
	if address != respValue {
		err = fmt.Errorf("modbus: response address '%v' does not match request '%v'", respValue, address)
		return
	}
	respValue = binary.BigEndian.Uint16(response.Data[2:])
	if andMask != respValue {
		err = fmt.Errorf("modbus: response AND-mask '%v' does not match request '%v'", respValue, andMask)
		return
	}
	respValue = binary.BigEndian.Uint16(response.Data[4:])
	if orMask != respValue {
		err = fmt.Errorf("modbus: response OR-mask '%v' does not match request '%v'", respValue, orMask)
		return
	}
	results = response.Data[2:]
	return
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
func (mb *client) ReadWriteMultipleRegisters(ctx context.Context, readAddress, readQuantity, writeAddress, writeQuantity uint16, value []byte) (results []byte, err error) {
	if readQuantity < 1 || readQuantity > 125 {
		err = fmt.Errorf("modbus: quantity to read '%v' must be between '%v' and '%v',", readQuantity, 1, 125)
		return
	}
	if writeQuantity < 1 || writeQuantity > 121 {
		err = fmt.Errorf("modbus: quantity to write '%v' must be between '%v' and '%v',", writeQuantity, 1, 121)
		return
	}
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadWriteMultipleRegisters,
		Data:         dataBlockSuffix(value, readAddress, readQuantity, writeAddress, writeQuantity),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	count := int(response.Data[0])
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	results = response.Data[1 : count+1]
	return
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
func (mb *client) ReadFIFOQueue(ctx context.Context, address uint16) (results []byte, err error) {
	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadFIFOQueue,
		Data:         dataBlock(address),
	}
	response, err := mb.send(ctx, &request)
	if err != nil {
		return
	}
	if len(response.Data) < 4 {
		err = fmt.Errorf("modbus: response data size '%v' is less than expected '%v'", len(response.Data), 4)
		return
	}
	count := int(binary.BigEndian.Uint16(response.Data))
	length := len(response.Data) - 1
	if count != length {
		err = &DataSizeError{ExpectedBytes: count, ActualBytes: length}
		if length < count {
			return
		}
	}
	count = int(binary.BigEndian.Uint16(response.Data[2 : count+2]))
	if count > 31 {
		err = fmt.Errorf("modbus: fifo count '%v' is greater than expected '%v'", count, 31)
		return
	}
	results = response.Data[4:]
	return
}

// Request:
//
//	Function code         : 1 byte (0x2B)
//	MEI Type              : 1 byte (0x0E)
//	Read Device ID Code   : 1 byte (01 for basic, 02 for regular, 03 for extended, 04 for specific)
//	Object ID             : 1 byte (0x00 to 0xFF)
//
// Response:
//
//	Function code         : 1 byte (0x2B)
//	MEI Type              : 1 byte (0x0E)
//	Read Device ID Code   : 1 byte (01 for basic, 02 for regular, 03 for extended, 04 for specific)
//	Conformity level 	  : 1 byte (0x01 / 0x02 / 0x03 / 0x81 / 0x82 / 0x83)
//	More Follows          : 1 byte (0x00 for no, 0xFF for yes)
//	Next Object ID        : 1 byte
//	Number of Objects     : 1 byte
//	List of (length = Number of Objects):
//		Object ID         : 1 byte
//		Object length     : 1 byte
//		Object value      : Object length (see above)
func (mb *client) ReadDeviceIdentification(ctx context.Context, readDeviceIDCode ReadDeviceIDCode) (map[byte][]byte, error) {
	return mb.ReadDeviceIdentificationWithObjectIDOffset(ctx, readDeviceIDCode, 0)
}

func (mb *client) ReadDeviceIdentificationWithObjectIDOffset(ctx context.Context, readDeviceIDCode ReadDeviceIDCode, objectIDOffset int) (map[byte][]byte, error) {
	var objectID byte
	switch readDeviceIDCode {
	case ReadDeviceIDCodeBasic:
		objectID = 0x00
	case ReadDeviceIDCodeRegular:
		objectID = 0x03
	case ReadDeviceIDCodeExtended:
		objectID = 0x80
	default:
		return nil, fmt.Errorf("unsupported readDeviceIDCode %d", readDeviceIDCode)
	}

	objectID += byte(objectIDOffset)

	return mb.readDeviceIdentificationWithObjectID(ctx, readDeviceIDCode, objectID)
}

func (mb *client) readDeviceIdentificationWithObjectID(ctx context.Context, readDeviceIDCode ReadDeviceIDCode, objectID byte) (map[byte][]byte, error) {
	const meiType = meiTypeReadDeviceIdentification

	request := ProtocolDataUnit{
		FunctionCode: FuncCodeReadDeviceIdentification,
		Data:         []byte{byte(meiType), byte(readDeviceIDCode), objectID},
	}

	response, err := mb.send(ctx, &request)
	if err != nil {
		return nil, err
	}

	if got, want := len(response.Data), 6; got < want {
		return nil, fmt.Errorf("missing required headers, got %d, want %d", got, want)
	}

	results := make(map[byte][]byte)

	moreFollows := response.Data[3]
	nextObjectID := response.Data[4]
	numObjects := int(response.Data[5])

	offset := 5
	for i := 0; i < numObjects; i++ {
		offset++
		objectID := response.Data[offset]

		// Read object length
		offset++
		if len(response.Data)-1 < offset {
			return nil, fmt.Errorf("missing object length for object #%d", i)
		}
		objectLength := response.Data[offset]

		// Read object value
		offset++
		end := offset + int(objectLength)
		if len(response.Data) < end {
			return nil, fmt.Errorf("data too short to read object #%d at index %d", i, end)
		}
		objectValue := response.Data[offset:end]

		// Set new offset for next iteration
		offset = end - 1

		results[objectID] = objectValue
	}

	if moreFollows != 0xFF {
		return results, nil
	}

	if nextObjectID == 0x00 {
		return results, nil
	}

	nextResults, err := mb.readDeviceIdentificationWithObjectID(ctx, readDeviceIDCode, nextObjectID)
	if err != nil {
		return nil, err
	}

	for key, val := range nextResults {
		results[key] = val
	}

	return results, nil
}

// Helpers

// send sends request and checks possible exception in the response.
func (mb *client) send(ctx context.Context, request *ProtocolDataUnit) (response *ProtocolDataUnit, err error) {
	aduRequest, err := mb.packager.Encode(request)
	if err != nil {
		return
	}
	aduResponse, err := mb.transporter.Send(ctx, aduRequest)
	if err != nil {
		return
	}
	if err = mb.packager.Verify(aduRequest, aduResponse); err != nil {
		return
	}
	response, err = mb.packager.Decode(aduResponse)
	if err != nil {
		return
	}
	// Check correct function code returned (exception)
	if response.FunctionCode != request.FunctionCode {
		err = responseError(response)
		return
	}
	if response.Data == nil || len(response.Data) == 0 {
		// Empty response
		err = fmt.Errorf("modbus: response data is empty")
		return
	}
	return
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
