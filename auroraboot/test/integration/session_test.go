package integration_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	Expect(err).NotTo(HaveOccurred())
	return u
}

func createArtifactRecord(_ string) string {
	art, err := adminClient.Artifacts.Create(context.Background(), client.CreateArtifactRequest{
		BaseImage: "quay.io/kairos/ubuntu:24.04",
		Outputs:   client.ArtifactOutputs{ISO: true},
	})
	Expect(err).NotTo(HaveOccurred())
	return art.ID
}

var _ = Describe("Admin session", func() {
	It("downloads an artifact file and opens the UI socket with only the session cookie", func() {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		browser := &http.Client{Jar: jar}

		resp, err := browser.Post(testServerURL+"/api/v1/session", "application/json",
			strings.NewReader(`{"password":"`+testAdminPassword+`"}`))
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

		id := createArtifactRecord("session-download")
		Expect(os.MkdirAll(filepath.Join(testArtifactsDir, id), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(testArtifactsDir, id, "kairos.iso"), []byte("iso"), 0o644)).To(Succeed())
		Expect(nodeGormStore.ArtifactUpdateFiles(context.Background(), id, []string{filepath.Join(testArtifactsDir, id, "kairos.iso")})).To(Succeed())

		resp, err = browser.Get(testServerURL + "/api/v1/artifacts/" + id + "/download/kairos.iso")
		Expect(err).NotTo(HaveOccurred())
		resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		header := http.Header{"Origin": {testServerURL}}
		for _, c := range jar.Cookies(mustParseURL(testServerURL)) {
			header.Add("Cookie", c.String())
		}
		wsURL := "ws" + strings.TrimPrefix(testServerURL, "http") + "/api/v1/ws/ui"
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		Expect(err).NotTo(HaveOccurred(), "status %v", resp)
		conn.Close()

		_, resp, err = websocket.DefaultDialer.Dial(wsURL,
			http.Header{"Origin": {"https://evil.example"}, "Cookie": header.Values("Cookie")})
		Expect(err).To(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})
})
