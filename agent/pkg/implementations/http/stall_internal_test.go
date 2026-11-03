package http

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func shortClient(idle time.Duration) *Client {
	c := NewClient()
	c.client.HTTPClient = &http.Client{Transport: &stallTransport{
		base:    newBaseTransport(idle),
		timeout: idle,
	}}
	return c
}

// A download that takes longer than the idle timeout overall, but keeps
// sending data, must complete.
func TestSlowSteadyDownloadCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		for i := 0; i < 10; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out")
	start := time.Now()
	if err := shortClient(400*time.Millisecond).GetURL(logger.NewNullLogger(), srv.URL, dest); err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if time.Since(start) < 800*time.Millisecond {
		t.Fatalf("download finished too fast to exercise the timeout")
	}
	if b, _ := os.ReadFile(dest); len(b) != 10 {
		t.Fatalf("got %d bytes, want 10", len(b))
	}
}

// A body that stops sending data must be aborted once the idle timeout passes.
func TestStalledBodyAborts(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	dest := filepath.Join(t.TempDir(), "out")
	start := time.Now()
	err := shortClient(300*time.Millisecond).GetURL(logger.NewNullLogger(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected the stalled download to fail")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("stalled download took %s to abort", time.Since(start))
	}
}

// A server that never sends response headers must be aborted too.
func TestNoHeadersAborts(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	req, _ := grab.NewRequest(filepath.Join(t.TempDir(), "out"), srv.URL)
	start := time.Now()
	resp := shortClient(300 * time.Millisecond).client.Do(req)
	<-resp.Done
	if resp.Err() == nil {
		t.Fatal("expected failure")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("took %s to abort", time.Since(start))
	}
}
