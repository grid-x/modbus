package modbus

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"pgregory.net/rapid"
)

func TestTCPEncodeDecode(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		packager := &tcpPackager{
			transactionID: rapid.Uint32().Draw(t, "transactionID").(uint32),
			SlaveID:       rapid.Byte().Draw(t, "SlaveID").(byte),
		}

		pdu := &ProtocolDataUnit{
			FunctionCode: rapid.Byte().Draw(t, "FunctionCode").(byte),
			Data:         rapid.SliceOf(rapid.Byte()).Draw(t, "Data").([]byte),
		}

		raw, err := packager.Encode(pdu)
		if err != nil {
			t.Fatalf("error while encoding: %+v", err)
		}

		dpdu, err := packager.Decode(raw)
		if err != nil {
			t.Fatalf("error while decoding: %+v", err)
		}

		if !cmp.Equal(pdu, dpdu) {
			t.Errorf("invalid pdu: %s", cmp.Diff(pdu, dpdu))
		}
	})
}
