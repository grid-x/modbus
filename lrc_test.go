// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

package modbus

import (
	"testing"
)

func TestLRC(t *testing.T) {
	var lrc lrc
	lrc.push(0x01).push(0x03)
	lrc.push([]byte{0x01, 0x0A}...)

	if 0xF1 != lrc.value() {
		t.Fatalf("lrc expected %v, actual %v", 0xF1, lrc.value())
	}
}
