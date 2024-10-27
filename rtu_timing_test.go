package modbus

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRTUTiming(t *testing.T) {
	c := rtuSerialTransporter{}

	precision := 0.007 // 0.7%
	imprecise := func(a, b time.Duration) bool {
		return math.Abs(float64(a)/float64(b)-1) > precision
	}

	for _, baudRate := range []int{2400, 9600, 19200, 38400, 57600, 115200} {
		t.Log(baudRate)
		c.BaudRate = baudRate

		charDuration := time.Duration(float64(time.Second) / float64(baudRate) * 11)

		// if res := c.charDuration(); imprecise(res,charDuration) {
		// 	assert.Equal(t, charDuration, res, "character duration")
		// }

		// characterDelay := charDuration * 3 / 2 // 1.5
		// if baudRate > 19200 {
		// 	characterDelay = 750 * time.Microsecond
		// }

		// if res := c.characterDelay(); imprecise(res,characterDelay) {
		// 	assert.Equal(t, characterDelay, res, "character delay")
		// }

		frameDelay := charDuration * 7 / 2 // 3.5
		if baudRate > 19200 {
			frameDelay = 1750 * time.Microsecond
		}

		if res := c.frameDelay(); imprecise(res, frameDelay) {
			assert.Equal(t, frameDelay, res, "frame delay")
		}
	}
}
