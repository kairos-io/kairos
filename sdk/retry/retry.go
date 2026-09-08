// Package retry is a thin, opinionated wrapper over github.com/avast/retry-go
// (already a transitive dependency via immucore) that gives every "call X;
// if it fails or isn't ready yet, wait a bit and try again" loop in this repo
// a single place to live.
//
// It does not reimplement retry-go's engine: Do is retry-go's Do, Option is
// retry-go's Option, and every raw retry-go option (retry.Delay,
// retry.RetryIf, retry.Context, ...) can be passed alongside the helpers
// below. What this package adds is:
//   - delay shapes the call sites actually use (linear, exponential+cap)
//     that retry-go doesn't ship, expressed as self-contained DelayTypeFuncs
//     rather than ones that reach into retry-go's unexported Config fields
//   - WithUnlimitedAttempts for context/timeout-bound loops that have no
//     natural attempt cap (reconnect loops, "poll until ready" loops)
//   - PollUntil for the sibling shape: polling a condition function instead
//     of retrying a fallible operation
//   - Defaults, a sane starting point for call sites that don't care about
//     the exact shape of the backoff
package retry

import (
	"context"
	"errors"
	"time"

	retrygo "github.com/avast/retry-go"
)

// RetryableFunc is the function Do retries: return nil for success, an error
// to retry, or Unrecoverable(err) to fail permanently without retrying.
type RetryableFunc = retrygo.RetryableFunc

// OnRetryFunc is called after attempt n (0-indexed: "n failures observed so
// far") fails, before the delay preceding attempt n+1 is computed and slept.
type OnRetryFunc = retrygo.OnRetryFunc

// RetryIfFunc decides whether an error should be retried. The default,
// IsRecoverable, retries everything that wasn't wrapped with Unrecoverable.
type RetryIfFunc = retrygo.RetryIfFunc

// Option configures a Do call. This is a type alias (not a new type), so any
// retry-go option (retry.Delay, retry.RandomDelay, retry.MaxJitter, ...)
// composes with the helpers below.
type Option = retrygo.Option

// Do runs fn, retrying it per opts. With no opts it falls back to retry-go's
// own defaults (10 attempts, exponential backoff with jitter starting at
// 100ms) -- callers that want this repo's conventions should pass Defaults()
// or one of the WithXxxBackoff helpers explicitly.
func Do(fn RetryableFunc, opts ...Option) error {
	return retrygo.Do(fn, opts...)
}

// Unrecoverable marks err as permanent: Do stops retrying immediately and
// returns the unwrapped err instead of exhausting attempts. Use this for
// errors that another attempt cannot possibly fix (bad arguments, "disk does
// not exist"), as opposed to transient ones (device not settled yet, network
// blip).
func Unrecoverable(err error) error { return retrygo.Unrecoverable(err) }

// IsRecoverable reports whether err was NOT wrapped with Unrecoverable. It is
// the default RetryIf.
func IsRecoverable(err error) bool { return retrygo.IsRecoverable(err) }

// unlimitedAttempts stands in for "keep going forever" in a context- or
// timeout-bound loop. retry-go allocates an []error of length Attempts
// up front unless LastErrorOnly is set, so WithUnlimitedAttempts always
// pairs this with LastErrorOnly(true) -- never use a huge Attempts value
// without also setting LastErrorOnly.
const unlimitedAttempts = 1 << 20

// --- thin re-exports of the retry-go options call sites actually need ---

// WithAttempts sets the maximum number of tries (default from retry-go is
// 10 if unset).
func WithAttempts(n uint) Option { return retrygo.Attempts(n) }

// WithMaxDelay caps whatever DelayType computes. Combine with
// WithExponentialBackoff for "double the wait every retry, up to a ceiling".
func WithMaxDelay(d time.Duration) Option { return retrygo.MaxDelay(d) }

// WithLastErrorOnly controls what Do returns when attempts are exhausted:
// true returns just the final attempt's error, false (retry-go's default)
// returns an aggregate retry.Error wrapping every attempt's error.
func WithLastErrorOnly(v bool) Option { return retrygo.LastErrorOnly(v) }

// WithContext ties the retry loop to ctx: Do returns ctx.Err() as soon as ctx
// is done, instead of waiting out the next delay.
func WithContext(ctx context.Context) Option { return retrygo.Context(ctx) }

// WithOnRetry runs fn between a failed attempt and the delay before the next
// one -- the usual place to log "attempt N failed, retrying in ...".
func WithOnRetry(fn OnRetryFunc) Option { return retrygo.OnRetry(fn) }

// WithRetryIf overrides which errors are retried at all (default:
// IsRecoverable, i.e. everything not wrapped with Unrecoverable).
func WithRetryIf(fn RetryIfFunc) Option { return retrygo.RetryIf(fn) }

// WithUnlimitedAttempts is for loops with no natural attempt cap: the only
// thing that should stop them is WithContext's ctx being done. It forces
// LastErrorOnly(true) alongside it (see unlimitedAttempts).
func WithUnlimitedAttempts() Option {
	return func(c *retrygo.Config) {
		retrygo.Attempts(unlimitedAttempts)(c)
		retrygo.LastErrorOnly(true)(c)
	}
}

// --- delay shapes retry-go doesn't ship ---

// WithFixedDelay waits exactly d between every attempt.
//
// This is retry-go's own FixedDelay + Delay(d), just expressed as a single
// self-contained option so callers don't have to remember to pass both.
func WithFixedDelay(d time.Duration) Option {
	return retrygo.DelayType(func(_ uint, _ error, _ *retrygo.Config) time.Duration {
		return d
	})
}

// WithLinearBackoff waits (n+1)*base before attempt n+1, where n is the
// 0-indexed count of failures seen so far: base before the 1st retry,
// 2*base before the 2nd, 3*base before the 3rd, and so on. No cap and no
// jitter -- combine with WithMaxDelay if a ceiling is needed.
func WithLinearBackoff(base time.Duration) Option {
	return retrygo.DelayType(func(n uint, _ error, _ *retrygo.Config) time.Duration {
		return time.Duration(n+1) * base
	})
}

// WithLinearBackoffFromZero waits n*base before attempt n+1, where n is the
// 0-indexed count of failures seen so far -- so the very first retry has no
// delay at all, the second waits base, the third 2*base, and so on. This is
// the off-by-one sibling of WithLinearBackoff: several call sites in this
// repo grew their delay from a raw loop index (0, 1, 2, ...) rather than
// (index+1), giving the first retry a free pass. Kept as its own option
// rather than folded into WithLinearBackoff so migrating a call site
// preserves its exact original cadence instead of silently shifting it.
func WithLinearBackoffFromZero(base time.Duration) Option {
	return retrygo.DelayType(func(n uint, _ error, _ *retrygo.Config) time.Duration {
		return time.Duration(n) * base
	})
}

// WithExponentialBackoff waits base*2^n before attempt n+1 (n 0-indexed
// failure count): base, 2*base, 4*base, ... Uncapped by itself -- almost
// always combined with WithMaxDelay, since ExponentialDelay (used to compute
// this same value for logging) treats max<=0 as "no cap" too.
func WithExponentialBackoff(base time.Duration) Option {
	return retrygo.DelayType(func(n uint, _ error, _ *retrygo.Config) time.Duration {
		return ExponentialDelay(base, n, 0)
	})
}

// ExponentialDelay returns base*2^n, capped at max once it would reach or
// exceed it (max<=0 means uncapped). It's exported because retry-go's
// OnRetryFunc isn't told what delay is about to be applied, and several call
// sites log that duration ("reconnecting in %s") -- computing it here with
// the exact same doubling WithExponentialBackoff/WithMaxDelay use keeps the
// logged number accurate instead of duplicating the formula at each call
// site.
func ExponentialDelay(base time.Duration, n uint, max time.Duration) time.Duration {
	d := base
	for i := uint(0); i < n; i++ {
		d *= 2
		if max > 0 && d >= max {
			return max
		}
	}
	if max > 0 && d > max {
		d = max
	}
	return d
}

// Defaults is a sane starting point for call sites that don't need a
// specific backoff shape: 10 attempts, exponential backoff from 200ms capped
// at 10s, only the last error returned.
func Defaults() []Option {
	return []Option{
		WithAttempts(10),
		WithExponentialBackoff(200 * time.Millisecond),
		WithMaxDelay(10 * time.Second),
		WithLastErrorOnly(true),
	}
}

// ErrTimeout is returned by PollUntil when timeout elapses before check
// reports done. It is never wrapped, so callers can compare it with
// errors.Is directly.
var ErrTimeout = errors.New("timeout exhausted")

// PollUntil calls check every interval until it reports done, returns a
// non-nil error (treated as permanent -- PollUntil returns it immediately,
// unlike Do it is never retried), ctx is done, or timeout elapses.
//
// If checkFirst is true, check runs once immediately before any wait (use
// this when the first check is cheap and likely to already be true, so
// paying a full interval up front would be wasted); if false, PollUntil
// waits one interval before the first check.
//
// This is the "wait for a condition" sibling of Do (which retries a
// fallible operation): the two are not interchangeable because a condition
// check that returns "not ready yet" is not an error, and Do has no notion
// of an overall wall-clock deadline independent of its attempt count.
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
