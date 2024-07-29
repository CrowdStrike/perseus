package main

import (
	"crypto/rand"
	"math/big"
	"time"

	"connectrpc.com/connect"
)

var (
	// subsequent retry delays for retryOp()
	// - use the first 5 Fibonacci numbers for semi-exponential growth
	// - the extra 0 value is a sentinel so we don't do another wait after we've exhausted all 5 retries
	backoffDelays = []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		500 * time.Millisecond,
		800 * time.Millisecond,
		0,
	}
)

// retryOp performs the specified operation, retrying up to 5 times if the request returns a 502-Unavailable
// status to provide resiliency for transient failures due to LB flakiness (especially within K8S).
func retryOp[T any](op func() (T, error)) (result T, err error) {
	var zero T
	for _, wait := range backoffDelays {
		result, err = op()
		switch {
		case err == nil:
			return result, nil
		case connect.CodeOf(err) == connect.CodeUnavailable:
			if wait > 0 {
				// inject up to 20% jitter
				maxJitter := big.NewInt(int64(float64(int64(wait)) * 0.2))
				jitter, _ := rand.Int(rand.Reader, maxJitter)
				wait += time.Duration(jitter.Int64())
				<-time.After(wait)
			}
		default:
			return zero, err
		}
	}
	// if we get here, err is non-nil and from a 502 but we have exhausted all retries
	return zero, err
}
