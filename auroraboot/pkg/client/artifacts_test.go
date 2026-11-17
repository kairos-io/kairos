package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing/iotest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gleak"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

// endlessReader yields zero bytes forever.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// gatedReader blocks in its first Read until release is closed, then
// reports EOF.
type gatedReader struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	opened  sync.Once
}

func (g *gatedReader) open() {
	g.opened.Do(func() { close(g.release) })
}

func newGatedReader() *gatedReader {
	return &gatedReader{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gatedReader) Read([]byte) (int, error) {
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return 0, io.EOF
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type receivedPart struct {
	field    string
	filename string
	content  string
}

var _ = Describe("ArtifactsService", func() {
	Describe("UploadOverlay", func() {
		var (
			server        *httptest.Server
			requests      atomic.Int32
			gotMethod     string
			gotPath       string
			gotAuth       string
			gotLength     int64
			gotParts      []receivedPart
			status        int
			replyBody     string
			parseErrMsg   string
			ctx           context.Context
			cli           *client.Client
			uploadRunning = func() bool {
				for _, g := range gleak.Goroutines() {
					if strings.Contains(g.Backtrace, "(*ArtifactsService).UploadOverlay") {
						return true
					}
				}
				return false
			}
		)

		BeforeEach(func() {
			requests.Store(0)
			gotMethod, gotPath, gotAuth, parseErrMsg = "", "", "", ""
			gotLength = 0
			gotParts = nil
			status = http.StatusOK
			replyBody = `{"id":"6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"}`
			ctx = context.Background()
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				gotMethod = r.Method
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				gotLength = r.ContentLength
				mr, err := r.MultipartReader()
				if err != nil {
					parseErrMsg = err.Error()
				} else {
					for {
						p, err := mr.NextPart()
						if err == io.EOF {
							break
						}
						if err != nil {
							parseErrMsg = err.Error()
							break
						}
						b, _ := io.ReadAll(p)
						gotParts = append(gotParts, receivedPart{
							field:    p.FormName(),
							filename: p.FileName(),
							content:  string(b),
						})
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, replyBody)
			}))
			cli = client.New(server.URL, client.WithAdminPassword("s3cret"))
		})

		AfterEach(func() {
			server.Close()
		})

		It("streams every file as a multipart part named files and returns the ID", func() {
			id, err := cli.Artifacts.UploadOverlay(ctx,
				client.OverlayFile{Name: "motd", Content: strings.NewReader("hello\n")},
				client.OverlayFile{Name: "overlay.tar.gz", Content: strings.NewReader("\x1f\x8bbinary\x00data")},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal("6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"))
			Expect(parseErrMsg).To(BeEmpty())
			Expect(gotMethod).To(Equal(http.MethodPost))
			Expect(gotPath).To(Equal("/api/v1/artifacts/upload-overlay"))
			Expect(gotAuth).To(Equal("Bearer s3cret"))
			Expect(gotLength).To(Equal(int64(-1)), "a buffered body would carry a Content-Length")
			Expect(gotParts).To(Equal([]receivedPart{
				{field: "files", filename: "motd", content: "hello\n"},
				{field: "files", filename: "overlay.tar.gz", content: "\x1f\x8bbinary\x00data"},
			}))
			Eventually(uploadRunning).Should(BeFalse())
		})

		It("returns an APIError carrying the status for a non-2xx response", func() {
			status = http.StatusBadRequest
			replyBody = `{"error":"invalid multipart form"}`

			id, err := cli.Artifacts.UploadOverlay(ctx,
				client.OverlayFile{Name: "motd", Content: strings.NewReader("hello\n")},
			)

			Expect(id).To(BeEmpty())
			var apiErr *client.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusBadRequest))
			Expect(apiErr.ErrorMsg).To(Equal("invalid multipart form"))
		})

		It("fails when the reply carries no ID", func() {
			replyBody = `{}`

			id, err := cli.Artifacts.UploadOverlay(ctx,
				client.OverlayFile{Name: "motd", Content: strings.NewReader("hello\n")},
			)

			Expect(err).To(HaveOccurred())
			Expect(id).To(BeEmpty())
		})

		It("returns the error of a file that cannot be read", func() {
			readErr := errors.New("disk on fire")

			_, err := cli.Artifacts.UploadOverlay(ctx,
				client.OverlayFile{Name: "motd", Content: iotest.ErrReader(readErr)},
			)

			Expect(err).To(MatchError(readErr))
			Eventually(uploadRunning).Should(BeFalse())
		})

		DescribeTable("refuses an invalid file list without sending a request",
			func(files []client.OverlayFile) {
				_, err := cli.Artifacts.UploadOverlay(ctx, files...)

				Expect(err).To(HaveOccurred())
				Expect(requests.Load()).To(BeZero())
			},
			Entry("no files", nil),
			Entry("a file without a name", []client.OverlayFile{{Content: strings.NewReader("x")}}),
			Entry("a file without content", []client.OverlayFile{{Name: "motd"}}),
		)

		Context("when the server does not consume the body", func() {
			var (
				stalled *httptest.Server
				stop    chan struct{}
			)

			BeforeEach(func() {
				stop = make(chan struct{})
				stalled = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					select {
					case <-r.Context().Done():
					case <-stop:
					}
				}))
				cli = client.New(stalled.URL, client.WithAdminPassword("s3cret"))
			})

			AfterEach(func() {
				close(stop)
				stalled.Close()
			})

			It("returns on context cancellation and stops reading the files", func() {
				cctx, cancel := context.WithCancel(ctx)
				result := make(chan error, 1)
				go func() {
					defer GinkgoRecover()
					_, err := cli.Artifacts.UploadOverlay(cctx,
						client.OverlayFile{Name: "big.img", Content: endlessReader{}},
					)
					result <- err
				}()

				Eventually(uploadRunning).Should(BeTrue())
				cancel()

				Eventually(result).Should(Receive(MatchError(context.Canceled)))
				Eventually(uploadRunning).Should(BeFalse())
			})
		})

		It("does not return while a file read is still in progress", func() {
			// RoundTrippers may return before they are done with the
			// request body, so the guarantee has to come from the client.
			early := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := r.Body
				go func() { _, _ = io.Copy(io.Discard, body) }()
				<-r.Context().Done()
				go func() { _ = body.Close() }()
				return nil, r.Context().Err()
			})
			cli = client.New(server.URL, client.WithHTTPClient(&http.Client{Transport: early}))
			reader := newGatedReader()
			DeferCleanup(reader.open)
			cctx, cancel := context.WithCancel(ctx)
			result := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				_, err := cli.Artifacts.UploadOverlay(cctx,
					client.OverlayFile{Name: "motd", Content: reader},
				)
				result <- err
			}()

			Eventually(reader.entered).Should(BeClosed())
			cancel()
			Consistently(result, "200ms").ShouldNot(Receive())

			reader.open()
			Eventually(result).Should(Receive(MatchError(context.Canceled)))
			Eventually(uploadRunning).Should(BeFalse())
		})
	})

	Describe("CreateArtifactRequest", func() {
		It("sends the overlay by ID and carries no server path fields", func() {
			req := client.CreateArtifactRequest{
				OverlayID: "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a",
				Signing: client.ArtifactSigning{
					UKIKeySetID:         "ks-1",
					UKISecureBootEnroll: "if-safe",
				},
			}

			b, err := json.Marshal(req)
			Expect(err).NotTo(HaveOccurred())

			var body map[string]interface{}
			Expect(json.Unmarshal(b, &body)).To(Succeed())
			Expect(body).To(HaveKeyWithValue("overlayId", "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"))
			Expect(body).NotTo(HaveKey("overlayRootfs"))
			Expect(body["signing"]).To(Equal(map[string]interface{}{
				"ukiKeySetId":         "ks-1",
				"ukiSecureBootEnroll": "if-safe",
			}))
		})

		It("omits overlayId when no overlay is set", func() {
			b, err := json.Marshal(client.CreateArtifactRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(string(b)).NotTo(ContainSubstring("overlayId"))
		})
	})

	Describe("Artifact", func() {
		It("decodes the record's overlayId", func() {
			var a client.Artifact
			Expect(json.Unmarshal([]byte(`{"id":"a1","overlayId":"6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"}`), &a)).To(Succeed())
			Expect(a.OverlayID).To(Equal("6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a"))
		})
	})
})
