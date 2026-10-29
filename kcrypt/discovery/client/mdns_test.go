package client

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// fakeQuery stands in for mdns.QueryContext: it blocks for the query window
// like the real one and answers only from the nth call on, as a challenger
// that comes up late would.
func fakeQuery(answerOn int32, calls *atomic.Int32, fail bool) func(context.Context, *mdns.QueryParam) error {
	return func(ctx context.Context, p *mdns.QueryParam) error {
		n := calls.Add(1)
		if fail {
			return errors.New("no interface")
		}
		if n >= answerOn {
			p.Entries <- &mdns.ServiceEntry{Host: "challenger.local.", AddrV4: net.ParseIP("192.0.2.10"), Port: 8082}
		}
		select {
		case <-ctx.Done():
		case <-time.After(p.Timeout):
		}
		return nil
	}
}

func withMDNS(t *testing.T, timeout, window time.Duration, q func(context.Context, *mdns.QueryParam) error) {
	t.Helper()
	oldT, oldQ, oldW := mdnsTimeout, mdnsQuery, mdnsQueryWindow
	mdnsTimeout, mdnsQuery, mdnsQueryWindow = timeout, q, window
	t.Cleanup(func() { mdnsTimeout, mdnsQuery, mdnsQueryWindow = oldT, oldQ, oldW })
}

func TestDiscoverMDNSServerFindsLateResponder(t *testing.T) {
	var calls atomic.Int32
	withMDNS(t, 2*time.Second, 20*time.Millisecond, fakeQuery(4, &calls, false))

	ip, port := discoverMDNSServer("challenger.local", loggerpkg.NewNullLogger())
	if ip != "192.0.2.10" || port != "8082" {
		t.Fatalf("got %q:%q, want 192.0.2.10:8082", ip, port)
	}
	if calls.Load() < 4 {
		t.Fatalf("expected the query to be repeated, got %d calls", calls.Load())
	}
}

func TestDiscoverMDNSServerTimesOutAndStopsQuerying(t *testing.T) {
	var calls atomic.Int32
	withMDNS(t, 200*time.Millisecond, 20*time.Millisecond, fakeQuery(1<<30, &calls, true))

	start := time.Now()
	ip, port := discoverMDNSServer("challenger.local", loggerpkg.NewNullLogger())
	if ip != "" || port != "" {
		t.Fatalf("got %q:%q, want no result", ip, port)
	}
	if el := time.Since(start); el < 180*time.Millisecond || el > time.Second {
		t.Fatalf("returned after %v, want about the 200ms timeout", el)
	}
	after := calls.Load()
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != after {
		t.Fatal("query goroutine still running after return")
	}
}
