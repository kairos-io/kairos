package phonehome_test

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The node API key is a bearer credential: it authenticates this node to the
// management server, and the command channel behind that socket runs upgrade
// and reboot by default. saveCredentials writes it with mode 0600 and
// wizard.Redact keeps it out of a debug bundle's cloud-config, so the one
// place it must never appear is the journal, which kairos-agent logs copies
// into every bundle for kairos-agent-phonehome.
var _ = Describe("PhoneHome client logging", func() {
	var (
		ms     *mockServer
		tmpDir string
		memLog *bytes.Buffer
		client *phonehome.Client
	)

	BeforeEach(func() {
		ms = newMockServer("test-token")
		var err error
		tmpDir, err = os.MkdirTemp("", "phonehome-logging-test")
		Expect(err).ToNot(HaveOccurred())

		memLog = &bytes.Buffer{}
		client = phonehome.NewClient(&phonehome.Config{
			URL:               ms.server.URL,
			RegistrationToken: "test-token",
			HeartbeatInterval: 100 * time.Millisecond,
			ReconnectBackoff:  50 * time.Millisecond,
		},
			phonehome.WithCredentialsPath(filepath.Join(tmpDir, "creds.yaml")),
			phonehome.WithMachineIDFunc(func() string { return "test-machine-id" }),
			phonehome.WithLogger(sdkLogger.NewBufferLogger(memLog)),
		)
	})

	AfterEach(func() {
		ms.close()
		os.RemoveAll(tmpDir)
	})

	It("does not write the node API key when it reports the connection", func() {
		Expect(client.Register(context.Background())).To(Succeed())
		// The mock server hands out this key and then demands it back in the
		// WebSocket query, which is the real server's rule too, so the value
		// does have to go on the wire.
		Expect(client.APIKey()).To(Equal("test-api-key"))

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer GinkgoRecover()
			_ = client.Connect(ctx)
		}()

		Eventually(ms.wsConnected, "5s").Should(Receive())
		cancel()
		// Connect waits for its goroutines before returning, so once done is
		// closed nothing else can write to memLog.
		Eventually(done, "5s").Should(BeClosed())

		out := memLog.String()
		Expect(out).To(ContainSubstring("/api/v1/ws"),
			"expected the connection to be reported at all")
		Expect(out).ToNot(ContainSubstring("test-api-key"),
			"the node API key reached the log: %s", out)

		// Percent-encoded too: url.Values.Encode escapes the value, so a log
		// line built from u.String() would carry the key in that form.
		host, err := url.Parse(ms.server.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring(host.Host),
			"the server is still named, so the line stays useful")
		Expect(out).ToNot(ContainSubstring(url.QueryEscape("test-api-key")))
		Expect(out).To(ContainSubstring("token=REDACTED"),
			"the parameter is still named, so the line shows a key was sent")
	})
})
