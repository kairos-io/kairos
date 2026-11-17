package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/labstack/echo/v4"
)

// overlayEntry is a single tar member for building test overlay archives.
type overlayEntry struct {
	name     string
	data     []byte
	mode     os.FileMode
	typeflag byte
	linkname string
}

// makeOverlayTarGz builds a gzipped tar from the given entries.
func makeOverlayTarGz(entries []overlayEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     int64(e.mode),
			Size:     int64(len(e.data)),
			Typeflag: tf,
			Linkname: e.linkname,
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		if len(e.data) > 0 {
			_, err := tw.Write(e.data)
			Expect(err).NotTo(HaveOccurred())
		}
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

// uploadOverlayMultipart wraps an archive as a multipart upload under the
// "files" field, matching what UploadOverlay reads.
func uploadOverlayMultipart(body []byte, filename string) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("files", filename)
	Expect(err).NotTo(HaveOccurred())
	_, err = fw.Write(body)
	Expect(err).NotTo(HaveOccurred())
	Expect(w.Close()).To(Succeed())
	return &buf, w.FormDataContentType()
}

var _ = Describe("ArtifactHandler UploadOverlay extraction", func() {
	var (
		e            *echo.Echo
		handler      *handlers.ArtifactHandler
		artifactsDir string
	)

	BeforeEach(func() {
		e = echo.New()
		var err error
		artifactsDir, err = os.MkdirTemp("", "overlay-test-")
		Expect(err).NotTo(HaveOccurred())
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, nil, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
	})

	AfterEach(func() {
		os.RemoveAll(artifactsDir)
	})

	// doUpload posts the archive and returns the recorder.
	doUpload := func(body []byte) *httptest.ResponseRecorder {
		buf, contentType := uploadOverlayMultipart(body, "overlay.tar.gz")
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload-overlay", buf)
		req.Header.Set(echo.HeaderContentType, contentType)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(handler.UploadOverlay(c)).To(Succeed())
		return rec
	}

	// postForm posts a hand-built multipart body and returns the recorder.
	postForm := func(build func(w *multipart.Writer)) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		build(w)
		Expect(w.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload-overlay", &buf)
		req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
		rec := httptest.NewRecorder()
		Expect(handler.UploadOverlay(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	addFile := func(w *multipart.Writer, name string, data []byte) {
		fw, err := w.CreateFormFile("files", name)
		Expect(err).NotTo(HaveOccurred())
		_, err = fw.Write(data)
		Expect(err).NotTo(HaveOccurred())
	}

	// overlayDirs lists what exists under <artifactsDir>/overlays.
	overlayDirs := func() []string {
		entries, err := os.ReadDir(filepath.Join(artifactsDir, "overlays"))
		if os.IsNotExist(err) {
			return nil
		}
		Expect(err).NotTo(HaveOccurred())
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		return names
	}

	It("saves plain files by base name and returns the overlay ID", func() {
		rec := postForm(func(w *multipart.Writer) {
			addFile(w, "motd", []byte("hello\n"))
			addFile(w, "../../issue", []byte("kairos\n"))
		})
		Expect(rec.Code).To(Equal(http.StatusOK))

		var resp struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(overlayDirs()).To(Equal([]string{resp.ID}))
		overlayDir := filepath.Join(artifactsDir, "overlays", resp.ID)
		Expect(os.ReadFile(filepath.Join(overlayDir, "motd"))).To(Equal([]byte("hello\n")))
		Expect(os.ReadFile(filepath.Join(overlayDir, "issue"))).To(Equal([]byte("kairos\n")))
	})

	It("rejects a form without files and creates no overlay", func() {
		rec := postForm(func(w *multipart.Writer) {
			Expect(w.WriteField("note", "no files here")).To(Succeed())
		})
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).NotTo(ContainSubstring(`"id"`))
		Expect(overlayDirs()).To(BeEmpty())
	})

	It("rejects a body that is not a multipart form and creates no overlay", func() {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/upload-overlay", bytes.NewBufferString("not a form"))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		Expect(handler.UploadOverlay(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(overlayDirs()).To(BeEmpty())
	})

	It("removes the overlay when an archive is rejected after other files were saved", func() {
		rec := postForm(func(w *multipart.Writer) {
			addFile(w, "motd", []byte("hello\n"))
			addFile(w, "overlay.tar.gz", makeOverlayTarGz([]overlayEntry{
				{name: "../escape", data: []byte("PWNED"), mode: 0o644},
			}))
		})
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).NotTo(ContainSubstring(`"id"`))
		Expect(overlayDirs()).To(BeEmpty())
	})

	It("rejects an archive that is not gzip and leaves no overlay behind", func() {
		rec := postForm(func(w *multipart.Writer) {
			addFile(w, "overlay.tgz", []byte("plain text, not gzip"))
		})
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(overlayDirs()).To(BeEmpty())
	})

	It("fails the upload when a file cannot be saved and leaves no overlay behind", func() {
		rec := postForm(func(w *multipart.Writer) {
			addFile(w, "overlay.tar.gz", makeOverlayTarGz([]overlayEntry{
				{name: "etc/", typeflag: tar.TypeDir, mode: 0o755},
			}))
			// A plain file named like the extracted directory cannot be created.
			addFile(w, "etc", []byte("clash\n"))
		})
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(rec.Body.String()).NotTo(ContainSubstring(`"id"`))
		Expect(overlayDirs()).To(BeEmpty())
	})

	It("extracts a valid overlay archive correctly", func() {
		body := makeOverlayTarGz([]overlayEntry{
			{name: "etc/", typeflag: tar.TypeDir, mode: 0o755},
			{name: "etc/config.yaml", data: []byte("foo: bar\n"), mode: 0o644},
			{name: "opt/app/run.sh", data: []byte("#!/bin/sh\n"), mode: 0o755},
		})
		rec := doUpload(body)
		Expect(rec.Code).To(Equal(http.StatusOK))

		var resp struct {
			ID string `json:"id"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.ID).NotTo(BeEmpty())
		overlayDir := filepath.Join(artifactsDir, "overlays", resp.ID)

		data, err := os.ReadFile(filepath.Join(overlayDir, "etc/config.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal([]byte("foo: bar\n")))
		data, err = os.ReadFile(filepath.Join(overlayDir, "opt/app/run.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal([]byte("#!/bin/sh\n")))
	})

	It("rejects an archive with a ../ traversal member and writes nothing outside overlayDir", func() {
		body := makeOverlayTarGz([]overlayEntry{
			{name: "../escape", data: []byte("PWNED"), mode: 0o644},
		})
		rec := doUpload(body)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))

		// The escape target lives one level above overlays/<uuid> i.e. under
		// artifactsDir/overlays. Nothing should have been written anywhere
		// outside the per-upload overlay dir.
		_, err := os.Stat(filepath.Join(artifactsDir, "overlays", "escape"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(filepath.Join(artifactsDir, "escape"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("rejects an archive with an absolute-path member", func() {
		escape := filepath.Join(artifactsDir, "abs-escape")
		body := makeOverlayTarGz([]overlayEntry{
			{name: escape, data: []byte("PWNED"), mode: 0o644},
		})
		rec := doUpload(body)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		_, err := os.Stat(escape)
		Expect(os.IsNotExist(err)).To(BeTrue())
	})

	It("rejects an archive containing a symlink member", func() {
		body := makeOverlayTarGz([]overlayEntry{
			{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd", mode: 0o777},
		})
		rec := doUpload(body)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects an oversized archive at the cap", func() {
		// A single member declaring a size beyond the per-file cap is rejected
		// without materializing the bytes.
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		hdr := &tar.Header{
			Name:     "big.bin",
			Mode:     0o644,
			Size:     int64(256*1024*1024) + 1, // one byte over maxOverlayFileSize
			Typeflag: tar.TypeReg,
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		// Write a little real data; the header size is what triggers the cap.
		_, err := tw.Write(bytes.Repeat([]byte{0}, 1024))
		Expect(err).NotTo(HaveOccurred())
		// Do not close tw cleanly with full data — we only need the header read.
		_ = tw.Flush()
		_ = gz.Close()

		rec := doUpload(buf.Bytes())
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
