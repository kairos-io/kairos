package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/retry"
)

func TestDoSucceedsWithoutRetrying(t *testing.T) {
	calls := 0
	err := retry.Do(func() error {
		calls++
		return nil
	}, retry.WithAttempts(3), retry.WithFixedDelay(time.Millisecond))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDoRetriesUntilAttemptsExhausted(t *testing.T) {
	calls := 0
	boom := errors.New("boom")
	err := retry.Do(func() error {
		calls++
		return boom
	}, retry.WithAttempts(4), retry.WithFixedDelay(time.Millisecond), retry.WithLastErrorOnly(true))
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom, got %v", err)
	}
	if calls != 4 {
		t.Fatalf("expected 4 calls, got %d", calls)
	}
}

func TestDoStopsOnUnrecoverable(t *testing.T) {
	calls := 0
	boom := errors.New("permanent")
	err := retry.Do(func() error {
		calls++
		return retry.Unrecoverable(boom)
	}, retry.WithAttempts(5), retry.WithFixedDelay(time.Millisecond), retry.WithLastErrorOnly(true))
	if !errors.Is(err, boom) {
		t.Fatalf("expected unwrapped permanent error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call for an unrecoverable error, got %d", calls)
	}
}

func TestWithLinearBackoffDelaysGrowByOneUnitEachRetry(t *testing.T) {
	var starts []time.Time
	unit := 20 * time.Millisecond
	_ = retry.Do(func() error {
		starts = append(starts, time.Now())
		return errors.New("retry me")
	}, retry.WithAttempts(3), retry.WithLinearBackoff(unit), retry.WithLastErrorOnly(true))

	if len(starts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(starts))
	}
	firstGap := starts[1].Sub(starts[0])
	secondGap := starts[2].Sub(starts[1])
	if firstGap < unit {
		t.Fatalf("expected first gap >= %s, got %s", unit, firstGap)
	}
	if secondGap < 2*unit {
		t.Fatalf("expected second gap >= %s, got %s", 2*unit, secondGap)
	}
}

func TestExponentialDelayDoublesAndCaps(t *testing.T) {
	base := 100 * time.Millisecond
	cases := []struct {
		n    uint
		max  time.Duration
		want time.Duration
	}{
		{0, 0, 100 * time.Millisecond},
		{1, 0, 200 * time.Millisecond},
		{2, 0, 400 * time.Millisecond},
		{3, 350 * time.Millisecond, 350 * time.Millisecond}, // would be 800ms, capped
		{0, 350 * time.Millisecond, 100 * time.Millisecond},
	}
	for _, c := range cases {
		got := retry.ExponentialDelay(base, c.n, c.max)
		if got != c.want {
			t.Errorf("ExponentialDelay(%s, %d, %s) = %s, want %s", base, c.n, c.max, got, c.want)
		}
	}
}

func TestWithUnlimitedAttemptsStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	calls := 0
	err := retry.Do(func() error {
		calls++
		return errors.New("never succeeds")
	}, retry.WithUnlimitedAttempts(), retry.WithFixedDelay(5*time.Millisecond), retry.WithContext(ctx))

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected several attempts before the context deadline, got %d", calls)
	}
}

func TestPollUntilCheckFirstSkipsInitialWait(t *testing.T) {
	calls := 0
	err := retry.PollUntil(context.Background(), time.Hour, time.Hour, true, func() (bool, error) {
		calls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 check, got %d", calls)
	}
}

func TestPollUntilTimesOut(t *testing.T) {
	err := retry.PollUntil(context.Background(), 5*time.Millisecond, 20*time.Millisecond, false, func() (bool, error) {
		return false, nil
	})
	if !errors.Is(err, retry.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestPollUntilRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := retry.PollUntil(ctx, 5*time.Millisecond, time.Hour, false, func() (bool, error) {
		return false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPollUntilPropagatesCheckError(t *testing.T) {
	boom := errors.New("permanent check failure")
	err := retry.PollUntil(context.Background(), time.Millisecond, time.Hour, true, func() (bool, error) {
		return false, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
}
