package modbus

import (
	"testing"
	"time"
)

func TestBackoffStrategy_Exponential(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffExponential,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
	}

	// Test exponential progression: 10ms, 20ms, 40ms, 80ms, ...
	expected := []time.Duration{
		10 * time.Millisecond,   // attempt 0: 10 * 2^0 = 10
		20 * time.Millisecond,   // attempt 1: 10 * 2^1 = 20
		40 * time.Millisecond,   // attempt 2: 10 * 2^2 = 40
		80 * time.Millisecond,   // attempt 3: 10 * 2^3 = 80
		160 * time.Millisecond,  // attempt 4: 10 * 2^4 = 160
		320 * time.Millisecond,  // attempt 5: 10 * 2^5 = 320
		640 * time.Millisecond,  // attempt 6: 10 * 2^6 = 640
		1280 * time.Millisecond, // attempt 7: 10 * 2^7 = 1280
		2560 * time.Millisecond, // attempt 8: 10 * 2^8 = 2560
		5 * time.Second,         // attempt 9: 10 * 2^9 = 5120ms, capped at 5s
		5 * time.Second,         // attempt 10: capped at 5s
	}

	for i, want := range expected {
		got := strategy.Next(i)
		if got != want {
			t.Errorf("attempt %d: got %v, want %v", i, got, want)
		}
	}
}

func TestBackoffStrategy_Linear(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffLinear,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      1.0,
	}

	// Test linear progression: 10ms, 20ms, 30ms, 40ms, ...
	expected := []time.Duration{
		10 * time.Millisecond,  // attempt 0: 10 * (1 + 0*1.0) = 10
		20 * time.Millisecond,  // attempt 1: 10 * (1 + 1*1.0) = 20
		30 * time.Millisecond,  // attempt 2: 10 * (1 + 2*1.0) = 30
		40 * time.Millisecond,  // attempt 3: 10 * (1 + 3*1.0) = 40
		50 * time.Millisecond,  // attempt 4: 10 * (1 + 4*1.0) = 50
		60 * time.Millisecond,  // attempt 5: 10 * (1 + 5*1.0) = 60
		70 * time.Millisecond,  // attempt 6: 10 * (1 + 6*1.0) = 70
		80 * time.Millisecond,  // attempt 7: 10 * (1 + 7*1.0) = 80
		90 * time.Millisecond,  // attempt 8: 10 * (1 + 8*1.0) = 90
		100 * time.Millisecond, // attempt 9: 10 * (1 + 9*1.0) = 100, at cap
		100 * time.Millisecond, // attempt 10: capped at 100ms
	}

	for i, want := range expected {
		got := strategy.Next(i)
		if got != want {
			t.Errorf("attempt %d: got %v, want %v", i, got, want)
		}
	}
}

func TestBackoffStrategy_Fixed(t *testing.T) {
	interval := 100 * time.Millisecond
	strategy := &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: interval,
		MaxInterval:     interval,
	}

	// All attempts should return the same interval
	for i := 0; i < 10; i++ {
		got := strategy.Next(i)
		if got != interval {
			t.Errorf("attempt %d: got %v, want %v", i, got, interval)
		}
	}
}

func TestBackoffStrategy_ShouldRetry_Timeout(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: 10 * time.Millisecond,
		Timeout:         100 * time.Millisecond,
		MaxAttempts:     0, // unlimited attempts
	}

	// Should allow retries within timeout
	if !strategy.ShouldRetry(0, 0) {
		t.Error("should retry at start")
	}
	if !strategy.ShouldRetry(5, 50*time.Millisecond) {
		t.Error("should retry at 50ms (within 100ms timeout)")
	}
	if !strategy.ShouldRetry(10, 99*time.Millisecond) {
		t.Error("should retry at 99ms (just under timeout)")
	}

	// Should not retry after timeout
	if strategy.ShouldRetry(15, 100*time.Millisecond) {
		t.Error("should not retry at exact timeout")
	}
	if strategy.ShouldRetry(20, 150*time.Millisecond) {
		t.Error("should not retry past timeout")
	}
}

func TestBackoffStrategy_ShouldRetry_MaxAttempts(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: 10 * time.Millisecond,
		Timeout:         0, // unlimited time
		MaxAttempts:     5,
	}

	// Should allow retries up to max attempts
	if !strategy.ShouldRetry(0, 0) {
		t.Error("should retry attempt 0")
	}
	if !strategy.ShouldRetry(4, time.Second) {
		t.Error("should retry attempt 4 (just under max)")
	}

	// Should not retry at or past max attempts
	if strategy.ShouldRetry(5, time.Second) {
		t.Error("should not retry at max attempts")
	}
	if strategy.ShouldRetry(10, time.Second) {
		t.Error("should not retry past max attempts")
	}
}

func TestBackoffStrategy_ShouldRetry_Combined(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: 10 * time.Millisecond,
		Timeout:         100 * time.Millisecond,
		MaxAttempts:     10,
	}

	// Should retry when both conditions are met
	if !strategy.ShouldRetry(5, 50*time.Millisecond) {
		t.Error("should retry when both within limits")
	}

	// Should stop when timeout is reached (even if attempts under limit)
	if strategy.ShouldRetry(8, 100*time.Millisecond) {
		t.Error("should stop when timeout reached")
	}

	// Should stop when max attempts reached (even if time under limit)
	if strategy.ShouldRetry(10, 50*time.Millisecond) {
		t.Error("should stop when max attempts reached")
	}
}

func TestBackoffStrategy_ShouldRetry_Unlimited(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: 10 * time.Millisecond,
		Timeout:         0, // unlimited
		MaxAttempts:     0, // unlimited
	}

	// Should always retry when both are unlimited
	if !strategy.ShouldRetry(0, 0) {
		t.Error("should retry at start")
	}
	if !strategy.ShouldRetry(1000, time.Hour) {
		t.Error("should retry even after long time and many attempts")
	}
}

func TestBackoffStrategy_NilHandling(t *testing.T) {
	var strategy *BackoffStrategy

	got := strategy.Next(0)
	if got != 0 {
		t.Errorf("nil strategy Next() should return 0, got %v", got)
	}

	if strategy.ShouldRetry(0, 0) {
		t.Error("nil strategy ShouldRetry() should return false")
	}

	cloned := strategy.Clone()
	if cloned != nil {
		t.Errorf("cloning nil strategy should return nil, got %v", cloned)
	}
}

func TestBackoffStrategy_Clone(t *testing.T) {
	original := &BackoffStrategy{
		Type:            BackoffExponential,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
		Timeout:         30 * time.Second,
		MaxAttempts:     10,
	}

	cloned := original.Clone()

	// Verify all fields are copied
	if cloned.Type != original.Type {
		t.Errorf("Type: got %v, want %v", cloned.Type, original.Type)
	}
	if cloned.InitialInterval != original.InitialInterval {
		t.Errorf("InitialInterval: got %v, want %v", cloned.InitialInterval, original.InitialInterval)
	}
	if cloned.MaxInterval != original.MaxInterval {
		t.Errorf("MaxInterval: got %v, want %v", cloned.MaxInterval, original.MaxInterval)
	}
	if cloned.Multiplier != original.Multiplier {
		t.Errorf("Multiplier: got %v, want %v", cloned.Multiplier, original.Multiplier)
	}
	if cloned.Timeout != original.Timeout {
		t.Errorf("Timeout: got %v, want %v", cloned.Timeout, original.Timeout)
	}
	if cloned.MaxAttempts != original.MaxAttempts {
		t.Errorf("MaxAttempts: got %v, want %v", cloned.MaxAttempts, original.MaxAttempts)
	}

	// Verify independence: modifying clone doesn't affect original
	cloned.Type = BackoffLinear
	cloned.Multiplier = 3.0
	cloned.Timeout = time.Hour
	cloned.MaxAttempts = 100

	if original.Type == cloned.Type {
		t.Error("modifying clone affected original Type")
	}
	if original.Multiplier == cloned.Multiplier {
		t.Error("modifying clone affected original Multiplier")
	}
	if original.Timeout == cloned.Timeout {
		t.Error("modifying clone affected original Timeout")
	}
	if original.MaxAttempts == cloned.MaxAttempts {
		t.Error("modifying clone affected original MaxAttempts")
	}
}

func TestNewExponentialBackoff(t *testing.T) {
	strategy := NewExponentialBackoff(10*time.Millisecond, 5*time.Second, 30*time.Second)

	if strategy.Type != BackoffExponential {
		t.Errorf("Type: got %v, want BackoffExponential", strategy.Type)
	}
	if strategy.InitialInterval != 10*time.Millisecond {
		t.Errorf("InitialInterval: got %v, want 10ms", strategy.InitialInterval)
	}
	if strategy.MaxInterval != 5*time.Second {
		t.Errorf("MaxInterval: got %v, want 5s", strategy.MaxInterval)
	}
	if strategy.Multiplier != 2.0 {
		t.Errorf("Multiplier: got %v, want 2.0", strategy.Multiplier)
	}
	if strategy.Timeout != 30*time.Second {
		t.Errorf("Timeout: got %v, want 30s", strategy.Timeout)
	}
	if strategy.MaxAttempts != 0 {
		t.Errorf("MaxAttempts: got %v, want 0 (unlimited)", strategy.MaxAttempts)
	}

	// Verify it produces expected progression
	first := strategy.Next(0)
	second := strategy.Next(1)
	if first != 10*time.Millisecond {
		t.Errorf("first attempt: got %v, want 10ms", first)
	}
	if second != 20*time.Millisecond {
		t.Errorf("second attempt: got %v, want 20ms", second)
	}
}

func TestNewLinearBackoff(t *testing.T) {
	strategy := NewLinearBackoff(10*time.Millisecond, 5*time.Second, 30*time.Second)

	if strategy.Type != BackoffLinear {
		t.Errorf("Type: got %v, want BackoffLinear", strategy.Type)
	}
	if strategy.InitialInterval != 10*time.Millisecond {
		t.Errorf("InitialInterval: got %v, want 10ms", strategy.InitialInterval)
	}
	if strategy.MaxInterval != 5*time.Second {
		t.Errorf("MaxInterval: got %v, want 5s", strategy.MaxInterval)
	}
	if strategy.Timeout != 30*time.Second {
		t.Errorf("Timeout: got %v, want 30s", strategy.Timeout)
	}
}

func TestNewFixedBackoff(t *testing.T) {
	interval := 250 * time.Millisecond
	timeout := 10 * time.Second
	strategy := NewFixedBackoff(interval, timeout)

	if strategy.Type != BackoffFixed {
		t.Errorf("Type: got %v, want BackoffFixed", strategy.Type)
	}
	if strategy.Timeout != timeout {
		t.Errorf("Timeout: got %v, want %v", strategy.Timeout, timeout)
	}

	// Verify it returns constant interval
	for i := 0; i < 5; i++ {
		got := strategy.Next(i)
		if got != interval {
			t.Errorf("attempt %d: got %v, want %v", i, got, interval)
		}
	}
}

func TestNewFixedBackoff_NoTimeout(t *testing.T) {
	interval := 200 * time.Millisecond
	strategy := NewFixedBackoff(interval, 0)

	if strategy.Type != BackoffFixed {
		t.Errorf("Type: got %v, want BackoffFixed", strategy.Type)
	}
	if strategy.Timeout != 0 {
		t.Errorf("Timeout: got %v, want 0 (unlimited)", strategy.Timeout)
	}

	// Verify it returns constant interval
	for i := 0; i < 5; i++ {
		got := strategy.Next(i)
		if got != interval {
			t.Errorf("attempt %d: got %v, want %v", i, got, interval)
		}
	}
}

func TestNewLinearBackoffWithMaxAttempts(t *testing.T) {
	strategy := NewLinearBackoffWithMaxAttempts(100*time.Millisecond, 1*time.Second, 20)

	if strategy.Type != BackoffLinear {
		t.Errorf("Type: got %v, want BackoffLinear", strategy.Type)
	}
	if strategy.MaxAttempts != 20 {
		t.Errorf("MaxAttempts: got %v, want 20", strategy.MaxAttempts)
	}
	if strategy.Timeout != 0 {
		t.Errorf("Timeout: got %v, want 0 (unlimited)", strategy.Timeout)
	}
}

func TestNewExponentialBackoffWithMaxAttempts(t *testing.T) {
	strategy := NewExponentialBackoffWithMaxAttempts(50*time.Millisecond, 2*time.Second, 10)

	if strategy.Type != BackoffExponential {
		t.Errorf("Type: got %v, want BackoffExponential", strategy.Type)
	}
	if strategy.MaxAttempts != 10 {
		t.Errorf("MaxAttempts: got %v, want 10", strategy.MaxAttempts)
	}
	if strategy.Timeout != 0 {
		t.Errorf("Timeout: got %v, want 0 (unlimited)", strategy.Timeout)
	}
}

func TestBackoffType_InvalidType(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffType(99), // Invalid type
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
	}

	// Should default to fixed behavior for unknown types
	for i := 0; i < 3; i++ {
		got := strategy.Next(i)
		if got != 50*time.Millisecond {
			t.Errorf("attempt %d: got %v, want 50ms (fixed fallback)", i, got)
		}
	}
}

func TestBackoffStrategy_UnlimitedMaxInterval(t *testing.T) {
	strategy := &BackoffStrategy{
		Type:            BackoffExponential,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     0, // Unlimited growth
		Multiplier:      2.0,
		Timeout:         0,
		MaxAttempts:     0,
	}

	// Verify exponential growth continues without cap
	expected := []time.Duration{
		10 * time.Millisecond,   // 10ms * 2^0
		20 * time.Millisecond,   // 10ms * 2^1
		40 * time.Millisecond,   // 10ms * 2^2
		80 * time.Millisecond,   // 10ms * 2^3
		160 * time.Millisecond,  // 10ms * 2^4
		320 * time.Millisecond,  // 10ms * 2^5
		640 * time.Millisecond,  // 10ms * 2^6
		1280 * time.Millisecond, // 10ms * 2^7
		2560 * time.Millisecond, // 10ms * 2^8
	}

	for i, want := range expected {
		got := strategy.Next(i)
		if got != want {
			t.Errorf("attempt %d: got %v, want %v", i, got, want)
		}
	}
}
