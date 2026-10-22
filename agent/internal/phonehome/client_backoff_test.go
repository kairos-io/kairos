package phonehome_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// backoffServer records when the reconnect loop reaches /api/v1/ws and when
// each attempt ends, so a spec can read the delay the loop waited between two
// attempts rather than infer it from the total run time.
type backoffServer struct {
	server  *httptest.Server
	handler func(w http.ResponseWriter, r *http.Request)

	mu      sync.Mutex
	starts  []time.Time
	ends    []time.Time
	stopped bool
}

func newBackoffServer(wsHandler func(w http.ResponseWriter, r *http.Request)) *backoffServer {
	bs := &backoffServer{handler: wsHandler}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/nodes/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(phonehome.Credentials{NodeID: "node", APIKey: "key"})
	})
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		bs.mu.Lock()
		stopped := bs.stopped
		if !stopped {
			bs.starts = append(bs.starts, time.Now())
		}
		bs.mu.Unlock()
		if stopped {
			return
		}

		bs.handler(w, r)

		bs.mu.Lock()
		bs.ends = append(bs.ends, time.Now())
		bs.mu.Unlock()
	})

	bs.server = httptest.NewServer(mux)
	return bs
}

// stop makes the server ignore further attempts, so the recorded series does
// not keep growing while a spec reads it.
func (bs *backoffServer) stop() {
	bs.mu.Lock()
	bs.stopped = true
	bs.mu.Unlock()
}

func (bs *backoffServer) attempts() int {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return len(bs.starts)
}

// waits returns the delay the reconnect loop waited before each attempt after
// the first: the time from the end of one attempt to the start of the next.
func (bs *backoffServer) waits() []time.Duration {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	var out []time.Duration
	for i := 1; i < len(bs.starts) && i <= len(bs.ends); i++ {
		out = append(out, bs.starts[i].Sub(bs.ends[i-1]))
	}
	return out
}

var _ = Describe("PhoneHome reconnect backoff", func() {
	const (
		heartbeat = 60 * time.Millisecond
		base      = 40 * time.Millisecond
	)

	var (
		tmpDir string
		bs     *backoffServer
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-backoff")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if bs != nil {
			bs.server.Close()
			bs = nil
		}
		os.RemoveAll(tmpDir)
	})

	runFor := func(d time.Duration) {
		cfg := &phonehome.Config{
			URL:               bs.server.URL,
			RegistrationToken: "token",
			HeartbeatInterval: heartbeat,
			ReconnectBackoff:  base,
		}
		client := phonehome.NewClient(cfg,
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "machine" }),
			phonehome.WithLogger(sdkLogger.NewNullLogger()),
		)

		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		Expect(client.Run(ctx)).To(Succeed())
		bs.stop()
	}

	// This is the regression. Every session below comes up, carries a
	// heartbeat and is closed cleanly by the server, which is what a rolling
	// restart looks like from the node. The delay before the next attempt has
	// to stay at the configured base, not grow towards MaxReconnectBackoff.
	It("keeps the delay at the base after sessions that worked", func() {
		bs = newBackoffServer(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			go func() {
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}()
			time.Sleep(2 * heartbeat)
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		})

		runFor(time.Second)

		waits := bs.waits()
		Expect(len(waits)).To(BeNumerically(">=", 3),
			"expected the loop to reconnect at least three times, attempts: %d", bs.attempts())
		for i, w := range waits {
			Expect(w).To(BeNumerically("<", 3*base),
				"wait %d was %s, the delay grew past the base: %v", i+1, w, waits)
		}
	})

	// Guard: the reset must not swallow the backoff on a server the node
	// cannot reach at all. Nothing here ever gets a session, so every attempt
	// has to wait longer than the one before it.
	It("still backs off when the websocket endpoint refuses the node", func() {
		bs = newBackoffServer(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		})

		runFor(time.Second)

		waits := bs.waits()
		Expect(len(waits)).To(BeNumerically(">=", 3),
			"expected at least three attempts, attempts: %d", bs.attempts())
		Expect(waits[2]).To(BeNumerically(">", 3*base),
			"the delay did not grow across failed attempts: %v", waits)
	})

	// Guard: a session the server accepts and drops at once is not a working
	// session, so it must not reset the delay either. This is what separates
	// "the websocket was established" from "the websocket was useful", and it
	// is the reason the reset is gated on the heartbeat interval.
	It("still backs off when the server drops the session before a heartbeat", func() {
		bs = newBackoffServer(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			conn.Close()
		})

		runFor(time.Second)

		waits := bs.waits()
		Expect(len(waits)).To(BeNumerically(">=", 3),
			"expected at least three attempts, attempts: %d", bs.attempts())
		Expect(waits[2]).To(BeNumerically(">", 3*base),
			"the delay did not grow across sessions that died at once: %v", waits)
	})
})
