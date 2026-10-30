package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/retry"
)

func TestDo(t *testing.T) {
	boom := errors.New("boom")

	t.Run("succeeds without retrying", func(t *testing.T) {
		calls := 0
		err := retry.Do(func() error { calls++; return nil }, retry.Config{Attempts: 3, Delay: retry.Fixed(time.Millisecond)})
		if err != nil || calls != 1 {
			t.Fatalf("err=%v calls=%d, want nil/1", err, calls)
		}
	})

	t.Run("retries until attempts exhausted, no sleep after final attempt", func(t *testing.T) {
		calls, delayCalls := 0, 0
		err := retry.Do(func() error { calls++; return boom }, retry.Config{
			Attempts: 4,
			Delay:    func(uint) time.Duration { delayCalls++; return time.Millisecond },
		})
		if !errors.Is(err, boom) || calls != 4 {
			t.Fatalf("err=%v calls=%d, want boom/4", err, calls)
		}
		if delayCalls != 3 {
			t.Fatalf("Delay called %d times, want 3 (never after the final attempt)", delayCalls)
		}
	})

	t.Run("stops immediately on Unrecoverable", func(t *testing.T) {
		calls := 0
		err := retry.Do(func() error { calls++; return retry.Unrecoverable(boom) }, retry.Config{Attempts: 5, Delay: retry.Fixed(time.Millisecond)})
		if !errors.Is(err, boom) || calls != 1 {
			t.Fatalf("err=%v calls=%d, want boom/1", err, calls)
		}
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		calls := 0
		err := retry.Do(func() error { calls++; return boom }, retry.Config{Delay: retry.Fixed(5 * time.Millisecond), Ctx: ctx})
		if !errors.Is(err, context.DeadlineExceeded) || calls < 2 {
			t.Fatalf("err=%v calls=%d, want DeadlineExceeded/>=2", err, calls)
		}
	})

	t.Run("OnRetry runs once per retry, never after the final attempt", func(t *testing.T) {
		var seen []uint
		_ = retry.Do(func() error { return boom }, retry.Config{
			Attempts: 3, Delay: retry.Fixed(time.Millisecond),
			OnRetry: func(n uint, _ error) { seen = append(seen, n) },
		})
		if len(seen) != 2 || seen[0] != 0 || seen[1] != 1 {
			t.Fatalf("seen=%v, want [0 1]", seen)
		}
	})
}

func TestDelayShapes(t *testing.T) {
	base := 100 * time.Millisecond
	cases := []struct {
		name string
		fn   func(uint) time.Duration
		n    []uint
		want []time.Duration
	}{
		{"Fixed", retry.Fixed(base), []uint{0, 1, 5}, []time.Duration{base, base, base}},
		{"Linear capped", retry.Linear(base, 250*time.Millisecond), []uint{0, 1, 2},
			[]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 250 * time.Millisecond}},
		{"LinearFromZero capped", retry.LinearFromZero(base, 150*time.Millisecond), []uint{0, 1, 2},
			[]time.Duration{0, 100 * time.Millisecond, 150 * time.Millisecond}},
		{"Exponential capped", retry.Exponential(base, 350*time.Millisecond), []uint{0, 1, 2, 3},
			[]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 350 * time.Millisecond, 350 * time.Millisecond}},
		{"Exponential uncapped", retry.Exponential(base, 0), []uint{0, 1, 2, 3},
			[]time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for i, n := range c.n {
				if got := c.fn(n); got != c.want[i] {
					t.Errorf("n=%d: got %s, want %s", n, got, c.want[i])
				}
			}
		})
	}
}

func TestPollUntil(t *testing.T) {
	boom := errors.New("boom")

	t.Run("checkFirst skips the initial wait", func(t *testing.T) {
		calls := 0
		err := retry.PollUntil(context.Background(), time.Hour, time.Hour, true, func() (bool, error) { calls++; return true, nil })
		if err != nil || calls != 1 {
			t.Fatalf("err=%v calls=%d, want nil/1", err, calls)
		}
	})

	t.Run("without checkFirst waits one interval first", func(t *testing.T) {
		start := time.Now()
		interval := 20 * time.Millisecond
		err := retry.PollUntil(context.Background(), interval, time.Hour, false, func() (bool, error) { return true, nil })
		if err != nil || time.Since(start) < interval {
			t.Fatalf("err=%v elapsed=%s, want nil/>=%s", err, time.Since(start), interval)
		}
	})

	t.Run("times out", func(t *testing.T) {
		err := retry.PollUntil(context.Background(), 5*time.Millisecond, 20*time.Millisecond, false, func() (bool, error) { return false, nil })
		if !errors.Is(err, retry.ErrTimeout) {
			t.Fatalf("err=%v, want ErrTimeout", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := retry.PollUntil(ctx, 5*time.Millisecond, time.Hour, false, func() (bool, error) { return false, nil })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v, want context.Canceled", err)
		}
	})

	t.Run("propagates a check error immediately", func(t *testing.T) {
		err := retry.PollUntil(context.Background(), time.Millisecond, time.Hour, true, func() (bool, error) { return false, boom })
		if !errors.Is(err, boom) {
			t.Fatalf("err=%v, want boom", err)
		}
	})
}
