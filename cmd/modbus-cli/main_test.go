package main

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConvertBytes(t *testing.T) {
	type testCase struct {
		name        string
		eType       string
		order       binary.ByteOrder
		val         float64
		expected    []byte
		expectError bool
	}

	tests := []testCase{
		// Int 16
		{
			name:     "convert int16 - be - valid value",
			eType:    "int16",
			order:    binary.BigEndian,
			val:      42,
			expected: []byte{0x00, 0x2A},
		},
		{
			name:     "convert int16 - le - valid value",
			eType:    "int16",
			order:    binary.LittleEndian,
			val:      42,
			expected: []byte{0x2A, 0x00},
		},
		{
			name:     "convert int16 - be - negative value",
			eType:    "int16",
			order:    binary.BigEndian,
			val:      -42,
			expected: []byte{0xFF, 0xD6},
		},
		{
			name:     "convert int16 - le - negative value",
			eType:    "int16",
			order:    binary.LittleEndian,
			val:      -42,
			expected: []byte{0xD6, 0xFF},
		},
		{
			name:        "convert int16 - overflow positive value",
			eType:       "int16",
			order:       binary.BigEndian,
			val:         math.MaxUint16 + 1,
			expectError: true,
		},
		{
			name:        "convert int16 - overflow negative value",
			eType:       "int16",
			order:       binary.BigEndian,
			val:         math.MinInt16 - 1,
			expectError: true,
		},
		// Int32
		{
			name:     "convert int32 - be - valid value",
			eType:    "int32",
			order:    binary.BigEndian,
			val:      42,
			expected: []byte{0x00, 0x00, 0x00, 0x2A},
		},
		{
			name:     "convert int32 - le - valid value",
			eType:    "int32",
			order:    binary.LittleEndian,
			val:      42,
			expected: []byte{0x2A, 0x00, 0x00, 0x00},
		},
		{
			name:     "convert int32 - be - negative value",
			eType:    "int32",
			order:    binary.BigEndian,
			val:      -42,
			expected: []byte{0xFF, 0xFF, 0xFF, 0xD6},
		},
		{
			name:     "convert int32 - le - negative value",
			eType:    "int32",
			order:    binary.LittleEndian,
			val:      -42,
			expected: []byte{0xD6, 0xFF, 0xFF, 0xFF},
		},
		{
			name:        "convert int32 - overflow positive value",
			eType:       "int32",
			order:       binary.BigEndian,
			val:         math.MaxUint32 + 1,
			expectError: true,
		},
		{
			name:        "convert int32 - overflow negative value",
			eType:       "int32",
			order:       binary.BigEndian,
			val:         math.MinInt32 - 1,
			expectError: true,
		},

		// UInt 16
		{
			name:     "convert uint16 - be - valid value",
			eType:    "uint16",
			order:    binary.BigEndian,
			val:      42,
			expected: []byte{0x00, 0x2A},
		},
		{
			name:     "convert uint16 - le - valid value",
			eType:    "uint16",
			order:    binary.LittleEndian,
			val:      42,
			expected: []byte{0x2A, 0x00},
		},
		{
			name:     "convert uint16 - be - max",
			eType:    "uint16",
			order:    binary.BigEndian,
			val:      math.MaxUint16,
			expected: []byte{0xFF, 0xFF},
		},
		{
			name:     "convert uint16 - le - max",
			eType:    "uint16",
			order:    binary.LittleEndian,
			val:      math.MaxUint16,
			expected: []byte{0xFF, 0xFF},
		},
		{
			name:        "convert uint16 - negative value",
			eType:       "uint16",
			order:       binary.LittleEndian,
			val:         -42,
			expectError: true,
		},
		// UInt32
		{
			name:     "convert uint32 - be - valid value",
			eType:    "uint32",
			order:    binary.BigEndian,
			val:      42,
			expected: []byte{0x00, 0x00, 0x00, 0x2A},
		},
		{
			name:     "convert uint32 - le - valid value",
			eType:    "uint32",
			order:    binary.LittleEndian,
			val:      42,
			expected: []byte{0x2A, 0x00, 0x00, 0x00},
		},
		{
			name:     "convert uint32 - be - max",
			eType:    "uint32",
			order:    binary.BigEndian,
			val:      math.MaxUint32,
			expected: []byte{0xFF, 0xFF, 0xFF, 0xFF},
		},
		{
			name:     "convert uint32 - le - max",
			eType:    "uint32",
			order:    binary.LittleEndian,
			val:      math.MaxUint32,
			expected: []byte{0xFF, 0xFF, 0xFF, 0xFF},
		},
		{
			name:        "convert uint32 - negative value",
			eType:       "uint32",
			order:       binary.LittleEndian,
			val:         -42,
			expectError: true,
		},
		// Float32
		{
			name:     "convert float32 - be - valid value",
			eType:    "float32",
			order:    binary.BigEndian,
			val:      42,
			expected: []byte{0x42, 0x28, 0x00, 0x00},
		},
		{
			name:     "convert float32 - le - valid value",
			eType:    "float32",
			order:    binary.LittleEndian,
			val:      42,
			expected: []byte{0x00, 0x00, 0x28, 0x42},
		},
		{
			name:     "convert float32 - be - max",
			eType:    "float32",
			order:    binary.BigEndian,
			val:      math.MaxFloat32,
			expected: []byte{0x7F, 0x7F, 0xFF, 0xFF},
		},
		{
			name:     "convert float32 - le - max",
			eType:    "float32",
			order:    binary.LittleEndian,
			val:      math.MaxFloat32,
			expected: []byte{0xFF, 0xFF, 0x7F, 0x7F},
		},
		{
			name:     "convert float32 - negative value",
			eType:    "float32",
			order:    binary.BigEndian,
			val:      -42,
			expected: []byte{0xC2, 0x28, 0x00, 0x00},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bytes, err := convertToBytes(tc.eType, tc.order, "", tc.val)
			if err != nil && tc.expectError == false {
				t.Errorf("exepcted no error but got %v", err)
				return
			}

			if tc.expectError && err == nil {
				t.Error("expected an error but didn't get one")
				return
			}

			// when in error, we don't need to compare anything
			if err != nil {
				return
			}

			if !cmp.Equal(bytes, tc.expected) {
				t.Errorf("expected %v but got %v. Diff: %v", tc.expected, bytes, cmp.Diff(tc.expected, bytes))
			}
		})
	}
}

func TestForcedOrder(t *testing.T) {
	type testCase struct {
		name        string
		eType       string
		order       binary.ByteOrder
		forcedOrder string
		val         float64
		expected    []byte
		expectError bool
	}

	tests := []testCase{
		// UInt 16
		{
			name:        "convert uint16, AB",
			eType:       "uint16",
			order:       binary.LittleEndian, // this should be overwritten by the forced order
			forcedOrder: "AB",
			val:         42,
			expected:    []byte{0x00, 0x2A},
		},
		{
			name:        "convert uint16, BA",
			eType:       "uint16",
			order:       binary.BigEndian, // this should be overwritten by the forced order
			forcedOrder: "BA",
			val:         42,
			expected:    []byte{0x2A, 0x00},
		},
		// UInt 32
		{
			name:        "convert uint32, ABCD",
			eType:       "uint32",
			order:       binary.LittleEndian, // this should be overwritten by the forced order
			forcedOrder: "ABCD",
			val:         42,
			expected:    []byte{0x00, 0x00, 0x00, 0x2A},
		},
		{
			name:        "convert uint32, DCBA",
			eType:       "uint32",
			order:       binary.BigEndian, // this should be overwritten by the forced order
			forcedOrder: "DCBA",
			val:         42,
			expected:    []byte{0x2A, 0x00, 0x00, 0x00},
		},
		{
			name:        "convert uint32, BADC",
			eType:       "uint32",
			order:       binary.BigEndian, // this should be overwritten by the forced order
			forcedOrder: "BADC",
			val:         42,
			expected:    []byte{0x00, 0x00, 0x2A, 0x00},
		},
		{
			name:        "convert uint32, CDAB",
			eType:       "uint32",
			order:       binary.LittleEndian, // this should be overwritten by the forced order
			forcedOrder: "CDAB",
			val:         42,
			expected:    []byte{0x00, 0x2A, 0x00, 0x00},
		},
		// Float 32 - TBD
		{
			name:        "convert uint32, ABCD",
			eType:       "uint32",
			order:       binary.LittleEndian, // this should be overwritten by the forced order
			forcedOrder: "ABCD",
			val:         42,
			expected:    []byte{0x00, 0x00, 0x00, 0x2A},
		},
		{
			name:        "convert uint32, DCBA",
			eType:       "uint32",
			order:       binary.BigEndian, // this should be overwritten by the forced order
			forcedOrder: "DCBA",
			val:         42,
			expected:    []byte{0x2A, 0x00, 0x00, 0x00},
		},
		{
			name:        "convert uint32, BADC",
			eType:       "uint32",
			order:       binary.BigEndian, // this should be overwritten by the forced order
			forcedOrder: "BADC",
			val:         42,
			expected:    []byte{0x00, 0x00, 0x2A, 0x00},
		},
		{
			name:        "convert uint32, CDAB",
			eType:       "uint32",
			order:       binary.LittleEndian, // this should be overwritten by the forced order
			forcedOrder: "CDAB",
			val:         42,
			expected:    []byte{0x00, 0x2A, 0x00, 0x00},
		},
		// Error cases
		{
			name:        "invalid forced order",
			forcedOrder: "CDBA",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bytes, err := convertToBytes(tc.eType, tc.order, tc.forcedOrder, tc.val)
			if err != nil && tc.expectError == false {
				t.Errorf("exepcted no error but got %v", err)
				return
			}

			if tc.expectError && err == nil {
				t.Error("expected an error but didn't get one")
				return
			}

			// when in error, we don't need to compare anything
			if err != nil {
				return
			}

			if !cmp.Equal(bytes, tc.expected) {
				t.Errorf("expected %v but got %v. Diff: %v", tc.expected, bytes, cmp.Diff(tc.expected, bytes))
			}

		})
	}
}
