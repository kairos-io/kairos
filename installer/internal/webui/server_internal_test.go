package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/websocket"

	"github.com/kairos-io/kairos/v4/sdk/constants"
)

var _ = Describe("the install endpoint and its progress stream", func() {
	var srv *httptest.Server
	var client *http.Client

	BeforeEach(func() {
		srv = httptest.NewServer(newServer(Options{}))
		DeferCleanup(srv.Close)

		// Do not follow the redirect to progress.html: the test is about
		// what the endpoint decided, not about serving the page again.
		client = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	})

	// postInstall submits the install form the way the browser does.
	postInstall := func(form url.Values) *http.Response {
		resp, err := client.PostForm(srv.URL+"/install", form)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		return resp
	}

	// readProgress opens the websocket and reads until the run ends.
	readProgress := func() []Message {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
		conn, err := websocket.Dial(wsURL, "", srv.URL)
		Expect(err).ToNot(HaveOccurred())
		defer conn.Close()

		var msgs []Message
		for {
			var raw string
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return msgs
			}
			var m Message
			Expect(json.Unmarshal([]byte(raw), &m)).To(Succeed())
			msgs = append(msgs, m)
			if m.Type == MessageDone {
				return msgs
			}
		}
	}

	It("runs the agent and streams its progress as typed messages", func() {
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
echo '{"event":"step","step":"partition"}'
echo 'writing the active image'
echo '{"event":"step","step":"done"}'
exit 0
`))

		resp := postInstall(url.Values{
			"cloud-config":        {"#cloud-config\n"},
			"installation-device": {"/dev/sda"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusSeeOther))
		Expect(resp.Header.Get("Location")).To(Equal("progress.html"))

		// The stream is replayed from the start of the run, so opening the
		// socket after the agent has already finished still shows all of it.
		// That is what a reload of progress.html does.
		msgs := readProgress()
		Expect(msgs).To(ContainElement(Message{Type: MessageStep, Step: "partition"}))
		Expect(msgs).To(ContainElement(Message{Type: MessageLog, Message: "writing the active image"}))
		Expect(msgs[len(msgs)-1]).To(Equal(Message{Type: MessageDone, OK: true}))
	})

	It("says so when nothing has been installed yet", func() {
		msgs := readProgress()
		Expect(msgs).To(HaveLen(2))
		Expect(msgs[0].Type).To(Equal(MessageError))
		Expect(msgs[0].Message).To(ContainSubstring("no installation has been started"))
		Expect(msgs[1].Type).To(Equal(MessageDone))
	})

	It("does not start a second install while one is running", func() {
		started := filepath.Join(GinkgoT().TempDir(), "runs")
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
echo run >> `+started+`
echo '{"event":"step","step":"done"}'
exit 0
`))

		form := url.Values{
			"cloud-config":        {"#cloud-config\n"},
			"installation-device": {"/dev/sda"},
		}
		Expect(postInstall(form).StatusCode).To(Equal(http.StatusSeeOther))
		Expect(readProgress()).ToNot(BeEmpty())

		// A resubmit of the form after a successful install is a reload,
		// not a request to repartition the disk a second time.
		Expect(postInstall(form).StatusCode).To(Equal(http.StatusSeeOther))
		Consistently(func() int {
			b, err := os.ReadFile(started)
			Expect(err).ToNot(HaveOccurred())
			return strings.Count(string(b), "run")
		}, "1s").Should(Equal(1))
	})

	It("lets a failed install be retried", func() {
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
echo '{"event":"error","message":"no such device"}'
exit 1
`))
		form := url.Values{
			"cloud-config":        {"#cloud-config\n"},
			"installation-device": {"/dev/nope"},
		}
		Expect(postInstall(form).StatusCode).To(Equal(http.StatusSeeOther))
		msgs := readProgress()
		Expect(msgs[len(msgs)-1]).To(Equal(Message{Type: MessageDone, OK: false}))

		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
echo '{"event":"step","step":"done"}'
exit 0
`))
		Expect(postInstall(form).StatusCode).To(Equal(http.StatusSeeOther))
		msgs = readProgress()
		Expect(msgs[len(msgs)-1]).To(Equal(Message{Type: MessageDone, OK: true}))
	})

	It("reports a missing agent on the form instead of redirecting", func() {
		// Nothing streams progress for a run that never started, so the
		// browser has to be told on the page it submitted from.
		if _, err := os.Stat(constants.AgentDefaultPath); err == nil {
			Skip(constants.AgentDefaultPath + " is present on this system")
		}
		empty := GinkgoT().TempDir()
		GinkgoT().Setenv("KAIROS_AGENT_BIN", filepath.Join(empty, "absent"))
		GinkgoT().Setenv("PATH", empty)

		resp := postInstall(url.Values{
			"cloud-config":        {"#cloud-config\n"},
			"installation-device": {"/dev/sda"},
		})
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("kairos-agent not found"))
	})
})

var _ = Describe("the install endpoint under load and with a source", func() {
	// noRedirect is the browser's client with redirect following off: these
	// specs care what the endpoint decided, not about the page it points at.
	noRedirect := func() *http.Client {
		return &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}

	form := url.Values{
		"cloud-config":        {"#cloud-config\n"},
		"installation-device": {"/dev/sda"},
	}

	It("forwards the source the installer was started with", func() {
		argv := filepath.Join(GinkgoT().TempDir(), "argv")
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
printf '%s\n' "$@" > `+argv+`
echo '{"event":"step","step":"done"}'
exit 0
`))
		srv := httptest.NewServer(newServer(Options{Source: "oci://foo:bar"}))
		DeferCleanup(srv.Close)

		resp, err := noRedirect().PostForm(srv.URL+"/install", form)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		Expect(resp.StatusCode).To(Equal(http.StatusSeeOther))

		Eventually(func() string {
			b, _ := os.ReadFile(argv)
			return string(b)
		}, "30s").Should(ContainSubstring("oci://foo:bar"))
	})

	It("starts exactly one install when several POSTs race", func() {
		// Two POSTs that both get past the guard partition the same disk at
		// once, and only one of them is on the stream the operator is
		// watching. A double-clicked Install button is enough to do it.
		runs := filepath.Join(GinkgoT().TempDir(), "runs")
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
echo run >> `+runs+`
sleep 1
echo '{"event":"step","step":"done"}'
exit 0
`))
		srv := httptest.NewServer(newServer(Options{}))
		DeferCleanup(srv.Close)

		const clients = 8
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < clients; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				<-start
				resp, err := noRedirect().PostForm(srv.URL+"/install", form)
				Expect(err).ToNot(HaveOccurred())
				_ = resp.Body.Close()
			}()
		}
		close(start)
		wg.Wait()

		countRuns := func() int {
			b, err := os.ReadFile(runs)
			if err != nil {
				return 0
			}
			return strings.Count(string(b), "run")
		}
		Eventually(countRuns, "30s").Should(Equal(1))
		// And still one once every agent that was going to start has.
		Consistently(countRuns, "2s").Should(Equal(1))
	})

	It("does not echo markup from a cloud-config error back into the page", func() {
		// yaml.v3 quotes the offending document in its error, so a bare
		// scalar comes back through message.html. Rendering that with
		// text/template put the operator's own input into the page unescaped.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent("exit 0\n"))
		srv := httptest.NewServer(newServer(Options{}))
		DeferCleanup(srv.Close)

		resp, err := noRedirect().PostForm(srv.URL+"/install", url.Values{
			"cloud-config":        {`<img src=x onerror=alert(1)>`},
			"installation-device": {"/dev/sda"},
		})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		Expect(err).ToNot(HaveOccurred())
		// The error still names what went wrong...
		Expect(string(body)).To(ContainSubstring("not valid YAML"))
		// ...but the operator's input reaches the page as text, not markup.
		Expect(string(body)).ToNot(ContainSubstring("<img sr"))
		Expect(string(body)).To(ContainSubstring("&lt;img sr"))
	})
})
