package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

// declareOnly sends a request that declares contentLength but carries no
// body, and returns the server's response. It goes over a raw connection so
// the spec sees what the server answers to the declaration alone, without
// the client having to stream the bytes.
func declareOnly(method, path, contentType string, contentLength int64) *http.Response {
	GinkgoHelper()
	u, err := url.Parse(testServerURL)
	Expect(err).NotTo(HaveOccurred())
	conn, err := net.Dial("tcp", u.Host)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(conn.Close)
	// A server that waits for the declared bytes would never answer.
	Expect(conn.SetDeadline(time.Now().Add(10 * time.Second))).To(Succeed())

	_, err = fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n",
		method, path, u.Host, testAdminPassword, contentType, contentLength)
	Expect(err).NotTo(HaveOccurred())

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(resp.Body.Close)
	return resp
}

var _ = Describe("Request body limits", func() {
	expectTooLarge := func(resp *http.Response) {
		GinkgoHelper()
		Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
		var body map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body).To(HaveKeyWithValue("error", Not(BeEmpty())))
	}

	It("refuses a JSON request over 4 MiB with 413 and an error key", func() {
		expectTooLarge(declareOnly(http.MethodPost, "/api/v1/groups", "application/json", 4*1024*1024+1))
	})

	It("accepts an overlay over 4 MiB", func() {
		id, err := adminClient.Artifacts.UploadOverlay(context.Background(),
			client.OverlayFile{Name: "big.bin", Content: bytes.NewReader(make([]byte, 5*1024*1024))})
		Expect(err).NotTo(HaveOccurred())
		Expect(id).NotTo(BeEmpty())
	})

	It("refuses an overlay over 512 MiB with 413 and an error key", func() {
		expectTooLarge(declareOnly(http.MethodPost, "/api/v1/artifacts/upload-overlay",
			"multipart/form-data; boundary=x", 512*1024*1024+1))
	})
})
