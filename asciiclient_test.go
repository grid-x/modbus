// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type asciiTestReader struct {
	readData []byte
	readErr  error
}

func (r *asciiTestReader) Read(b []byte) (int, error) {
	if len(r.readData) > 0 {
		b[0] = r.readData[0]
		r.readData = r.readData[1:]
		return 1, nil
	}
	if r.readErr != nil {
		return 0, r.readErr
	}
	return 0, io.EOF
}

func TestASCIIEncoding(t *testing.T) {
	encoder := asciiPackager{}
	encoder.SlaveID = 17

	pdu := ProtocolDataUnit{}
	pdu.FunctionCode = 3
	pdu.Data = []byte{0, 107, 0, 3}

	adu, err := encoder.Encode(&pdu)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte(":1103006B00037E\r\n")
	if !bytes.Equal(expected, adu) {
		t.Fatalf("adu actual: %v, expected %v", adu, expected)
	}
}

func TestASCIIDecoding(t *testing.T) {
	decoder := asciiPackager{}
	decoder.SlaveID = 247
	adu := []byte(":F7031389000A60\r\n")

	pdu, err := decoder.Decode(adu)
	if err != nil {
		t.Fatal(err)
	}

	if pdu.FunctionCode != 3 {
		t.Fatalf("Function code: expected %v, actual %v", 15, pdu.FunctionCode)
	}
	expected := []byte{0x13, 0x89, 0, 0x0A}
	if !bytes.Equal(expected, pdu.Data) {
		t.Fatalf("Data: expected %v, actual %v", expected, pdu.Data)
	}
}

func TestASCIIDecodeStartCharacter(t *testing.T) {
	decoder := asciiPackager{}
	aduReq := []byte(":010300010002F9\r\n")
	aduRespGreaterThan := []byte(">010304010F1509CA\r\n")
	aduRespColon := []byte(":010304010F1509CA\r\n")
	aduRespFail := []byte("!010304010F1509CA\r\n")

	// Modbus ASCII conform.
	if err := decoder.Verify(aduReq, aduRespColon); err != nil {
		t.Fatal(err)
	}

	// Not Modbus ASCII conform but common in the field.
	if err := decoder.Verify(aduReq, aduRespGreaterThan); err != nil {
		t.Fatal(err)
	}

	if err := decoder.Verify(aduReq, aduRespFail); err == nil {
		t.Fatalf("expected '%s' to fail but does not", aduRespFail)
	}
}

func TestASCIIDecodeInvalidLRC(t *testing.T) {
	decoder := asciiPackager{}
	adu := []byte(":F7031389000A61\r\n")

	_, err := decoder.Decode(adu)
	if err == nil {
		t.Fatal("expected invalid LRC error, got nil")
	}
	if !strings.Contains(err.Error(), "response lrc") {
		t.Fatalf("expected LRC mismatch error, got %v", err)
	}
}

func TestASCIIVerifyErrors(t *testing.T) {
	decoder := asciiPackager{}
	aduReq := []byte(":010300010002F9\r\n")

	testcases := []struct {
		name        string
		aduResp     []byte
		expectedErr string
	}{
		{
			name:        "too short",
			aduResp:     []byte(":01\r\n"),
			expectedErr: "does not meet minimum",
		},
		{
			name:        "odd payload length",
			aduResp:     []byte(":010304010F1509CA\r"),
			expectedErr: "is not an even number",
		},
		{
			name:        "invalid start character",
			aduResp:     []byte("!010304010F1509CA\r\n"),
			expectedErr: "is not started",
		},
		{
			name:        "invalid frame terminator",
			aduResp:     []byte(":010304010F1509CA\n\n"),
			expectedErr: "is not ended",
		},
		{
			name:        "slave mismatch",
			aduResp:     []byte(":020304010F1509CA\r\n"),
			expectedErr: "does not match request",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := decoder.Verify(aduReq, tc.aduResp)
			if err == nil {
				t.Fatal("expected Verify to fail, got nil")
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("expected error containing %q, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestReadASCII(t *testing.T) {
	testcases := []struct {
		name     string
		reader   *asciiTestReader
		deadline time.Time
		want     []byte
		wantErr  error
	}{
		{
			name:     "complete frame",
			reader:   &asciiTestReader{readData: []byte(":0103020000FA\r\n")},
			deadline: time.Now().Add(time.Second),
			want:     []byte(":0103020000FA\r\n"),
		},
		{
			name:     "stops at terminator",
			reader:   &asciiTestReader{readData: []byte(":0103020000FA\r\nignored")},
			deadline: time.Now().Add(time.Second),
			want:     []byte(":0103020000FA\r\n"),
		},
		{
			name:     "reader timeout after partial frame",
			reader:   &asciiTestReader{readData: []byte(":0103020000FA\r"), readErr: context.DeadlineExceeded},
			deadline: time.Now().Add(time.Second),
			wantErr:  context.DeadlineExceeded,
		},
		{
			name:     "deadline exceeded before read",
			reader:   &asciiTestReader{readData: []byte(":0103020000FA\r\n")},
			deadline: time.Now().Add(-time.Millisecond),
			wantErr:  context.DeadlineExceeded,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readASCII(tc.reader, tc.deadline)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readASCII returned error: %v", err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func BenchmarkASCIIEncoder(b *testing.B) {
	encoder := asciiPackager{
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

func BenchmarkASCIIDecoder(b *testing.B) {
	decoder := asciiPackager{
		SlaveID: 10,
	}
	adu := []byte(":F7031389000A60\r\n")
	for i := 0; i < b.N; i++ {
		_, err := decoder.Decode(adu)
		if err != nil {
			b.Fatal(err)
		}
	}
}
