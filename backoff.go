package modbus

import (
	"math"
	"time"
)

// BackoffType defines the retry interval growth algorithm
type BackoffType int

const (
	BackoffFixed       BackoffType = iota // Constant interval
	BackoffLinear                         // Linear growth
	BackoffExponential                    // Exponential growth
)

// BackoffStrategy defines a unified retry strategy for connection recovery.
// It controls retry intervals, total timeout budget, and maximum attempt count.
//
// Example usage with exponential backoff and timeout:
//
//	handler := modbus.NewRTUClientHandler("/dev/ttyUSB0")
//	handler.LinkRecoveryBackoff = &modbus.BackoffStrategy{
//	    Type:            modbus.BackoffExponential,
//	    InitialInterval: 10 * time.Millisecond,
//	    MaxInterval:     5 * time.Second,
//	    Multiplier:      2.0,
//	    Timeout:         30 * time.Second,
//	    MaxAttempts:     0, // unlimited attempts within timeout
//	}
//	// Retry progression: 10ms → 20ms → 40ms → 80ms → ... → 5s (capped)
//
// Example with max attempts:
//
//	handler.LinkRecoveryBackoff = &modbus.BackoffStrategy{
//	    Type:            modbus.BackoffFixed,
//	    InitialInterval: 100 * time.Millisecond,
//	    MaxInterval:     100 * time.Millisecond,
//	    Timeout:         0, // unlimited time
//	    MaxAttempts:     10,
//	}
type BackoffStrategy struct {
	// Type specifies the interval growth algorithm
	Type BackoffType
	// InitialInterval is the starting delay for the first retry attempt
	InitialInterval time.Duration
	// MaxInterval caps the maximum delay to prevent unbounded growth (0 = unlimited)
	MaxInterval time.Duration
	// Multiplier is the growth factor for exponential/linear backoff
	// - Exponential: interval = InitialInterval * (Multiplier ^ attempt)
	// - Linear: interval = InitialInterval * (1 + attempt * Multiplier)
	// - Fixed: Multiplier is ignored
	Multiplier float64

	// Timeout is the total time budget for all retry attempts (0 = unlimited)
	Timeout time.Duration
	// MaxAttempts is the maximum number of retry attempts (0 = unlimited)
	MaxAttempts int
}

// Next calculates the backoff interval for the given attempt number.
// attempt is zero-indexed (0 = first retry, 1 = second retry, etc.)
// The returned duration is capped at MaxInterval if MaxInterval > 0,
// otherwise growth is unlimited.
func (b *BackoffStrategy) Next(attempt int) time.Duration {
	if b == nil {
		return 0
	}

	var interval time.Duration

	switch b.Type {
	case BackoffFixed:
		interval = b.InitialInterval

	case BackoffLinear:
		// Linear: InitialInterval * (1 + attempt * Multiplier)
		multiplier := b.Multiplier
		if multiplier <= 0 {
			multiplier = 1.0 // Default linear growth
		}
		factor := 1.0 + float64(attempt)*multiplier
		interval = time.Duration(float64(b.InitialInterval) * factor)

	case BackoffExponential:
		// Exponential: InitialInterval * (Multiplier ^ attempt)
		multiplier := b.Multiplier
		if multiplier <= 1.0 {
			multiplier = 2.0 // Default exponential growth
		}
		factor := math.Pow(multiplier, float64(attempt))
		interval = time.Duration(float64(b.InitialInterval) * factor)

	default:
		// Default to fixed if type is unrecognized
		interval = b.InitialInterval
	}

	// Cap at MaxInterval
	if b.MaxInterval > 0 && interval > b.MaxInterval {
		interval = b.MaxInterval
	}

	return interval
}

// ShouldRetry determines whether another retry attempt should be made.
// It checks both the timeout and max attempts conditions.
// Returns false if either limit has been reached, true otherwise.
func (b *BackoffStrategy) ShouldRetry(attempt int, elapsed time.Duration) bool {
	if b == nil {
		return false
	}

	// Check timeout limit
	if b.Timeout > 0 && elapsed >= b.Timeout {
		return false
	}

	// Check max attempts limit
	if b.MaxAttempts > 0 && attempt >= b.MaxAttempts {
		return false
	}

	return true
}

// Clone creates an independent copy of the BackoffStrategy.
func (b *BackoffStrategy) Clone() *BackoffStrategy {
	if b == nil {
		return nil
	}
	return &BackoffStrategy{
		Type:            b.Type,
		InitialInterval: b.InitialInterval,
		MaxInterval:     b.MaxInterval,
		Multiplier:      b.Multiplier,
		Timeout:         b.Timeout,
		MaxAttempts:     b.MaxAttempts,
	}
}

// NewExponentialBackoff returns an exponential backoff strategy with custom intervals and timeout.
// Progression: initial → initial*2 → initial*4 → ... → max (capped)
func NewExponentialBackoff(initialInterval, maxInterval, timeout time.Duration) *BackoffStrategy {
	return &BackoffStrategy{
		Type:            BackoffExponential,
		InitialInterval: initialInterval,
		MaxInterval:     maxInterval,
		Multiplier:      2.0,
		Timeout:         timeout,
		MaxAttempts:     0,
	}
}

// NewLinearBackoff returns a linear backoff strategy with custom intervals and timeout.
// Progression: initial → initial*2 → initial*3 → ... → max (capped)
func NewLinearBackoff(initialInterval, maxInterval, timeout time.Duration) *BackoffStrategy {
	return &BackoffStrategy{
		Type:            BackoffLinear,
		InitialInterval: initialInterval,
		MaxInterval:     maxInterval,
		Multiplier:      1.0,
		Timeout:         timeout,
		MaxAttempts:     0,
	}
}

// NewFixedBackoff returns a backoff strategy with a constant retry interval.
func NewFixedBackoff(interval, timeout time.Duration) *BackoffStrategy {
	return &BackoffStrategy{
		Type:            BackoffFixed,
		InitialInterval: interval,
		MaxInterval:     interval,
		Multiplier:      0,
		Timeout:         timeout,
		MaxAttempts:     0,
	}
}

// NewLinearBackoffWithMaxAttempts returns a linear backoff strategy with max attempt limit instead of timeout.
func NewLinearBackoffWithMaxAttempts(initialInterval, maxInterval time.Duration, maxAttempts int) *BackoffStrategy {
	return &BackoffStrategy{
		Type:            BackoffLinear,
		InitialInterval: initialInterval,
		MaxInterval:     maxInterval,
		Multiplier:      1.0,
		Timeout:         0,
		MaxAttempts:     maxAttempts,
	}
}

// NewExponentialBackoffWithMaxAttempts returns an exponential backoff strategy with max attempt limit instead of timeout.
func NewExponentialBackoffWithMaxAttempts(initialInterval, maxInterval time.Duration, maxAttempts int) *BackoffStrategy {
	return &BackoffStrategy{
		Type:            BackoffExponential,
		InitialInterval: initialInterval,
		MaxInterval:     maxInterval,
		Multiplier:      2.0,
		Timeout:         0,
		MaxAttempts:     maxAttempts,
	}
}
