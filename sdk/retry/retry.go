// Package retry covers exactly the retry shapes used in this repo: a
// bounded or context-bound retry of a fallible operation (Do), plus a
// poll-a-condition loop (PollUntil) for call sites waiting on a path or a
// partition label to show up rather than retrying an operation that fails.
package retry

import (
	"context"
	"errors"
	"time"
)

// unrecoverableError marks an error as permanent: Do stops retrying and
// returns the wrapped err immediately instead of exhausting Attempts.
type unrecoverableError struct{ err error }

func (u *unrecoverableError) Error() string { return u.err.Error() }
func (u *unrecoverableError) Unwrap() error { return u.err }

// Unrecoverable wraps err so Do stops retrying immediately.
func Unrecoverable(err error) error {
	if err == nil {
		return nil
	}
	return &unrecoverableError{err}
}

// IsUnrecoverable reports whether err was wrapped by Unrecoverable.
func IsUnrecoverable(err error) bool {
	var u *unrecoverableError
	return errors.As(err, &u)
}

// Config configures a Do call.
type Config struct {
	Attempts uint                       // 0 = unlimited (Ctx-bound)
	Delay    func(n uint) time.Duration // sleep before retry n+1 (n 0-based)
	Ctx      context.Context            // nil = context.Background()
	OnRetry  func(n uint, err error)    // called before each retry's sleep
}

// Do calls fn until it returns nil, an Unrecoverable error, ctx is done, or
// Attempts is reached. Returns the last error (or nil). Never sleeps after
// the final attempt.
func Do(fn func() error, cfg Config) error {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	var err error
	for n := uint(0); cfg.Attempts == 0 || n < cfg.Attempts; n++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = fn()
		if err == nil {
			return nil
		}
		var uerr *unrecoverableError
		if errors.As(err, &uerr) {
			return uerr.err
		}
		if cfg.Attempts != 0 && n+1 >= cfg.Attempts {
			return err
		}
		if cfg.OnRetry != nil {
			cfg.OnRetry(n, err)
		}
		var d time.Duration
		if cfg.Delay != nil {
			d = cfg.Delay(n)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	return err
}

// Fixed waits d before every retry.
func Fixed(d time.Duration) func(uint) time.Duration {
	return func(uint) time.Duration { return d }
}

// Linear waits (n+1)*base before retry n+1 (n 0-based), capped at max
// (max <= 0 means uncapped).
func Linear(base, max time.Duration) func(uint) time.Duration {
	return func(n uint) time.Duration { return capped(time.Duration(n+1)*base, max) }
}

// LinearFromZero waits n*base before retry n+1 (n 0-based) -- the first
// retry has no delay, the second waits base, the third 2*base -- capped at
// max (max <= 0 means uncapped).
func LinearFromZero(base, max time.Duration) func(uint) time.Duration {
	return func(n uint) time.Duration { return capped(time.Duration(n)*base, max) }
}

// Exponential waits base*2^n before retry n+1 (n 0-based), capped at max
// (max <= 0 means uncapped).
func Exponential(base, max time.Duration) func(uint) time.Duration {
	return func(n uint) time.Duration {
		d := base
		for i := uint(0); i < n; i++ {
			d *= 2
			if max > 0 && d >= max {
				return max
			}
		}
		return capped(d, max)
	}
}

func capped(d, max time.Duration) time.Duration {
	if max > 0 && d > max {
		return max
	}
	return d
}

// ErrTimeout is returned by PollUntil when timeout elapses before check
// reports done.
var ErrTimeout = errors.New("timeout exhausted")

// PollUntil calls check every interval until it reports done, returns an
// error (returned immediately, never retried), ctx is done, or timeout
// elapses (ErrTimeout). If checkFirst, check also runs once immediately
// before any wait.
func PollUntil(ctx context.Context, interval, timeout time.Duration, checkFirst bool, check func() (done bool, err error)) error {
	if checkFirst {
		done, err := check()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	deadline := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return ErrTimeout
		case <-time.After(interval):
			done, err := check()
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}
