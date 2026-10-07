package webui_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/kairos-io/kairos/v4/installer/internal/webui"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// syncBuffer is a bytes.Buffer the server goroutine can write to while the
// spec reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// addressFrom pulls the listen address out of echo's "http(s) server started"
// line, which is the only place a ":0" bind reports the port it landed on.
func addressFrom(logged string) string {
	for _, line := range strings.Split(logged, "\n") {
		var rec struct {
			Msg     string `json:"msg"`
			Address string `json:"address"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Msg == "http(s) server started" {
			return rec.Address
		}
	}
	return ""
}

// The installer runs the web UI in the same process and on the same terminal
// as the bubbletea TUI, so everything the server says has to reach
// Options.Logger. Anything that goes to stdout instead lands on top of the
// alt screen the user is installing from.
var _ = Describe("Options.Logger", func() {
	var (
		logged  *syncBuffer
		cancel  context.CancelFunc
		errChan chan error
	)

	BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		logged = &syncBuffer{}
		errChan = make(chan error, 1)

		go func() {
			errChan <- webui.StartWith(ctx, webui.Options{
				Listen: "127.0.0.1:0",
				Logger: slog.New(slog.NewJSONHandler(logged, nil)),
			})
		}()
	})

	AfterEach(func() {
		cancel()
		Eventually(errChan).Should(Receive(BeNil()))
	})

	It("receives echo's own startup output", func() {
		Eventually(logged.String).Should(ContainSubstring("http(s) server started"))
	})

	It("receives a validation error instead of stdout", func() {
		Eventually(logged.String).Should(ContainSubstring("http(s) server started"))
		addr := addressFrom(logged.String())
		Expect(addr).NotTo(BeEmpty())

		resp, err := http.Post("http://"+addr+"/validate",
			"application/x-www-form-urlencoded",
			strings.NewReader("cloud-config=invalid config"))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		// The handler answers the browser with the validation error and
		// logs the same text, so pinning the two together keeps this from
		// passing on any unrelated ERROR the server happens to emit.
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).NotTo(BeEmpty())

		want, err := json.Marshal(string(body))
		Expect(err).NotTo(HaveOccurred())
		Eventually(logged.String).Should(SatisfyAll(
			ContainSubstring(`"level":"ERROR"`),
			ContainSubstring(`"msg":`+string(want)),
		))
	})
})
