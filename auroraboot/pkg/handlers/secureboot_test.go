package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// seedKeySet writes a handful of fake key files into baseDir/<name>/ and
// registers the corresponding SecureBootKeySet in the fake store. It returns
// the persisted record so tests can read its ID.
func seedKeySet(
	ctx context.Context,
	fs *fakeSecureBootKeySetStore,
	baseDir, name string,
	files map[string]fileSpec,
) *store.SecureBootKeySet {
	dir := filepath.Join(baseDir, name)
	Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
	for rel, spec := range files {
		full := filepath.Join(dir, rel)
		Expect(os.MkdirAll(filepath.Dir(full), 0o700)).To(Succeed())
		Expect(os.WriteFile(full, spec.data, spec.mode)).To(Succeed())
	}
	ks := &store.SecureBootKeySet{
		Name:             name,
		KeysDir:          dir,
		TPMPCRKeyPath:    filepath.Join(dir, "tpm2-pcr-private.pem"),
		SecureBootEnroll: "if-safe",
	}
	Expect(fs.Create(ctx, ks)).To(Succeed())
	return ks
}

type fileSpec struct {
	data []byte
	mode os.FileMode
}

// buildMultipart wraps a tar.gz body as a multipart upload with a "file"
// field, matching what the UI sends to /secureboot-keys/import.
func buildMultipart(body []byte, filename string) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred())
	_, err = fw.Write(body)
	Expect(err).NotTo(HaveOccurred())
	Expect(w.Close()).To(Succeed())
	return &buf, w.FormDataContentType()
}

// makeCustomTarGz produces a tar.gz stream with exactly the given entries,
// for testing malformed-archive rejection paths.
func makeCustomTarGz(entries []tarEntry) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     int64(e.mode),
			Size:     int64(len(e.data)),
			Typeflag: tar.TypeReg,
		}
		Expect(tw.WriteHeader(hdr)).To(Succeed())
		_, err := tw.Write(e.data)
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

type tarEntry struct {
	name string
	data []byte
	mode os.FileMode
}

var _ = Describe("SecureBootHandler export/import", func() {
	var (
		e       *echo.Echo
		handler *handlers.SecureBootHandler
		fakeSB  *fakeSecureBootKeySetStore
		keysDir string
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		e = echo.New()
		var err error
		keysDir, err = os.MkdirTemp("", "sb-handler-test-")
		Expect(err).NotTo(HaveOccurred())
		fakeSB = &fakeSecureBootKeySetStore{}
		handler = handlers.NewSecureBootHandler(fakeSB, keysDir)
	})

	AfterEach(func() {
		os.RemoveAll(keysDir)
	})

	Describe("GenerateKeys", func() {
		It("returns the generated key set without server paths", func() {
			if _, err := exec.LookPath("openssl"); err != nil {
				Skip("openssl not on PATH")
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/generate", strings.NewReader(`{"name":"generated"}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			Expect(handler.GenerateKeys(e.NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusCreated))

			Expect(rec.Body.String()).NotTo(ContainSubstring("keysDir"))
			var resp map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp).NotTo(HaveKey("keysDir"))
			Expect(resp).NotTo(HaveKey("tpmPcrKeyPath"))
			Expect(resp).To(HaveKeyWithValue("hasTpmPcrKey", true))
			Expect(resp).To(HaveKeyWithValue("name", "generated"))
		})

		It("returns 409 and leaves the existing keys alone when the name is taken", func() {
			seedKeySet(ctx, fakeSB, keysDir, "prod", map[string]fileSpec{
				"PK.key": {data: []byte("ORIGINAL"), mode: 0o600},
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/generate", strings.NewReader(`{"name":"prod"}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			Expect(handler.GenerateKeys(e.NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusConflict))

			data, err := os.ReadFile(filepath.Join(keysDir, "prod", "PK.key"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("ORIGINAL"))
		})

		It("returns 409 when the directory exists on disk without a record", func() {
			dir := filepath.Join(keysDir, "orphan")
			Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "PK.key"), []byte("ORIGINAL"), 0o600)).To(Succeed())
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secureboot-keys/generate", strings.NewReader(`{"name":"orphan"}`))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			Expect(handler.GenerateKeys(e.NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusConflict))

			data, err := os.ReadFile(filepath.Join(dir, "PK.key"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("ORIGINAL"))
		})
	})

	Describe("ListKeys", func() {
		list := func() []map[string]any {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/secureboot-keys", nil)
			rec := httptest.NewRecorder()
			Expect(handler.ListKeys(echo.New().NewContext(req, rec))).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).NotTo(ContainSubstring(keysDir))
			var out []map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
			return out
		}

		It("returns no server paths and says whether the set holds a TPM PCR key", func() {
			seedKeySet(ctx, fakeSB, keysDir, "with-tpm", map[string]fileSpec{
				"db.key":               {data: []byte("K"), mode: 0o600},
				"tpm2-pcr-private.pem": {data: []byte("T"), mode: 0o600},
			})
			seedKeySet(ctx, fakeSB, keysDir, "without-tpm", map[string]fileSpec{
				"db.key": {data: []byte("K"), mode: 0o600},
			})

			byName := map[string]map[string]any{}
			for _, ks := range list() {
				Expect(ks).NotTo(HaveKey("keysDir"))
				Expect(ks).NotTo(HaveKey("tpmPcrKeyPath"))
				byName[ks["name"].(string)] = ks
			}
			Expect(byName["with-tpm"]).To(HaveKeyWithValue("hasTpmPcrKey", true))
			Expect(byName["without-tpm"]).To(HaveKeyWithValue("hasTpmPcrKey", false))
		})
	})

	Describe("ExportKeys", func() {
		It("streams a tar.gz with manifest + keys and preserves file modes", func() {
			ks := seedKeySet(ctx, fakeSB, keysDir, "prod", map[string]fileSpec{
				"db.key":  {data: []byte("DBKEY"), mode: 0o600},
				"db.pem":  {data: []byte("DBCERT"), mode: 0o644},
				"PK.auth": {data: bytes.Repeat([]byte{0xAB}, 32), mode: 0o644},
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/secureboot-keys/"+ks.ID+"/export", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(ks.ID)

			Expect(handler.ExportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(Equal("application/gzip"))
			Expect(rec.Header().Get("Content-Disposition")).To(ContainSubstring("prod.secureboot-keyset.tar.gz"))

			// Pick the tar.gz apart and verify contents.
			gz, err := gzip.NewReader(rec.Body)
			Expect(err).NotTo(HaveOccurred())
			tr := tar.NewReader(gz)

			found := map[string]tarEntry{}
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).NotTo(HaveOccurred())
				data, err := io.ReadAll(tr)
				Expect(err).NotTo(HaveOccurred())
				found[hdr.Name] = tarEntry{name: hdr.Name, data: data, mode: os.FileMode(hdr.Mode)}
			}

			// Manifest is present and well-formed.
			Expect(found).To(HaveKey("manifest.json"))
			var manifest struct {
				Version          int    `json:"version"`
				Kind             string `json:"kind"`
				Name             string `json:"name"`
				SecureBootEnroll string `json:"secureBootEnroll"`
			}
			Expect(json.Unmarshal(found["manifest.json"].data, &manifest)).To(Succeed())
			Expect(manifest.Version).To(Equal(1))
			Expect(manifest.Kind).To(Equal("auroraboot.secureboot-keyset"))
			Expect(manifest.Name).To(Equal("prod"))
			Expect(manifest.SecureBootEnroll).To(Equal("if-safe"))

			// All key files present under keys/, with original contents and modes.
			Expect(found).To(HaveKey("keys/db.key"))
			Expect(found["keys/db.key"].data).To(Equal([]byte("DBKEY")))
			Expect(found["keys/db.key"].mode.Perm()).To(Equal(os.FileMode(0o600)))
			Expect(found).To(HaveKey("keys/db.pem"))
			Expect(found["keys/db.pem"].mode.Perm()).To(Equal(os.FileMode(0o644)))
			Expect(found).To(HaveKey("keys/PK.auth"))
		})

		It("returns 404 when the id is unknown", func() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/secureboot-keys/nope/export", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues("nope")

			Expect(handler.ExportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 404 when the keys directory is missing on disk", func() {
			ks := seedKeySet(ctx, fakeSB, keysDir, "ghost", map[string]fileSpec{
				"db.key": {data: []byte("X"), mode: 0o600},
			})
			Expect(os.RemoveAll(ks.KeysDir)).To(Succeed())

			req := httptest.NewRequest(http.MethodGet, "/api/v1/secureboot-keys/"+ks.ID+"/export", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(ks.ID)

			Expect(handler.ExportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("ImportKeys", func() {
		// importExported is a helper that round-trips: seed → export → extract
		// bytes → feed into ImportKeys. Returns the HTTP status and response
		// body so individual tests can assert on them.
		importExported := func(nameOverride string, sourceName string, files map[string]fileSpec) (*httptest.ResponseRecorder, []byte) {
			ks := seedKeySet(ctx, fakeSB, keysDir, sourceName, files)

			// Export first.
			exportReq := httptest.NewRequest(http.MethodGet, "/export", nil)
			exportRec := httptest.NewRecorder()
			ec := e.NewContext(exportReq, exportRec)
			ec.SetParamNames("id")
			ec.SetParamValues(ks.ID)
			Expect(handler.ExportKeys(ec)).To(Succeed())
			Expect(exportRec.Code).To(Equal(http.StatusOK))

			// Feed bytes into ImportKeys.
			archive := exportRec.Body.Bytes()
			body, ct := buildMultipart(archive, "keyset.tar.gz")
			url := "/import"
			if nameOverride != "" {
				url += "?name=" + nameOverride
			}
			req := httptest.NewRequest(http.MethodPost, url, body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			if nameOverride != "" {
				q := req.URL.Query()
				c.QueryParams().Set("name", q.Get("name"))
			}
			Expect(handler.ImportKeys(c)).To(Succeed())
			return rec, archive
		}

		It("round-trips an exported archive under a new name and preserves private key mode", func() {
			rec, _ := importExported("imported", "source", map[string]fileSpec{
				"db.key":               {data: []byte("PRIVATE"), mode: 0o600},
				"db.pem":               {data: []byte("CERT"), mode: 0o644},
				"tpm2-pcr-private.pem": {data: []byte("TPMKEY"), mode: 0o600},
			})
			Expect(rec.Code).To(Equal(http.StatusCreated))

			Expect(rec.Body.String()).NotTo(ContainSubstring("keysDir"))
			var resp map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp).NotTo(HaveKey("keysDir"))
			Expect(resp).NotTo(HaveKey("tpmPcrKeyPath"))
			Expect(resp).To(HaveKeyWithValue("hasTpmPcrKey", true))
			Expect(resp).To(HaveKeyWithValue("name", "imported"))
			Expect(resp).To(HaveKeyWithValue("secureBootEnroll", "if-safe"))

			stored, err := fakeSB.GetByName(ctx, "imported")
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.KeysDir).To(Equal(filepath.Join(keysDir, "imported")))
			Expect(stored.TPMPCRKeyPath).To(Equal(filepath.Join(keysDir, "imported", "tpm2-pcr-private.pem")))

			// Files on disk with correct content and modes.
			dbKeyPath := filepath.Join(stored.KeysDir, "db.key")
			data, err := os.ReadFile(dbKeyPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(data).To(Equal([]byte("PRIVATE")))

			info, err := os.Stat(dbKeyPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		})

		It("reports no TPM PCR key when the archive puts a directory at its path", func() {
			rec, _ := importExported("imported", "source", map[string]fileSpec{
				"db.key":                     {data: []byte("PRIVATE"), mode: 0o600},
				"tpm2-pcr-private.pem/child": {data: []byte("NOT A KEY"), mode: 0o600},
			})
			Expect(rec.Code).To(Equal(http.StatusCreated))
			var out map[string]any
			Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
			Expect(out).To(HaveKeyWithValue("hasTpmPcrKey", false))
		})

		It("names the key set, not the directory, when its directory already exists", func() {
			Expect(os.MkdirAll(filepath.Join(keysDir, "imported"), 0o700)).To(Succeed())
			rec, _ := importExported("imported", "source", map[string]fileSpec{
				"db.key": {data: []byte("PRIVATE"), mode: 0o600},
			})
			Expect(rec.Code).To(Equal(http.StatusConflict))
			Expect(rec.Body.String()).NotTo(ContainSubstring(keysDir))
			Expect(rec.Body.String()).To(ContainSubstring(`imported`))
		})

		It("returns 409 when a key set with the target name already exists", func() {
			// Seed first; exporting picks up the manifest name "prod".
			seedKeySet(ctx, fakeSB, keysDir, "prod", map[string]fileSpec{
				"db.key": {data: []byte("KEY"), mode: 0o600},
			})
			// Export via the handler.
			ks, _ := fakeSB.GetByName(ctx, "prod")
			exportReq := httptest.NewRequest(http.MethodGet, "/export", nil)
			exportRec := httptest.NewRecorder()
			ec := e.NewContext(exportReq, exportRec)
			ec.SetParamNames("id")
			ec.SetParamValues(ks.ID)
			Expect(handler.ExportKeys(ec)).To(Succeed())

			// Try to import without an override; manifest says "prod" which
			// collides with the existing record.
			body, ct := buildMultipart(exportRec.Body.Bytes(), "keyset.tar.gz")
			req := httptest.NewRequest(http.MethodPost, "/import", body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusConflict))
		})

		It("rejects archives with path traversal entries", func() {
			manifest := []byte(`{"version":1,"kind":"auroraboot.secureboot-keyset","name":"evil"}`)
			archive := makeCustomTarGz([]tarEntry{
				{name: "manifest.json", data: manifest, mode: 0o644},
				{name: "keys/../../etc/passwd", data: []byte("root:x:0:0"), mode: 0o600},
			})

			body, ct := buildMultipart(archive, "evil.tar.gz")
			req := httptest.NewRequest(http.MethodPost, "/import", body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("unsafe path"))
		})

		It("rejects archives without a manifest", func() {
			archive := makeCustomTarGz([]tarEntry{
				{name: "keys/db.key", data: []byte("KEY"), mode: 0o600},
			})
			body, ct := buildMultipart(archive, "no-manifest.tar.gz")
			req := httptest.NewRequest(http.MethodPost, "/import", body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("manifest"))
		})

		It("rejects archives whose manifest kind doesn't match", func() {
			manifest := []byte(`{"version":1,"kind":"something-else","name":"foo"}`)
			archive := makeCustomTarGz([]tarEntry{
				{name: "manifest.json", data: manifest, mode: 0o644},
				{name: "keys/db.key", data: []byte("KEY"), mode: 0o600},
			})
			body, ct := buildMultipart(archive, "bad-kind.tar.gz")
			req := httptest.NewRequest(http.MethodPost, "/import", body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(rec.Body.String()).To(ContainSubstring("kind"))
		})

		It("rejects uploads that are not gzip streams", func() {
			body, ct := buildMultipart([]byte("not a gzip"), "plain.txt")
			req := httptest.NewRequest(http.MethodPost, "/import", body)
			req.Header.Set(echo.HeaderContentType, ct)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(handler.ImportKeys(c)).To(Succeed())
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(strings.ToLower(rec.Body.String())).To(ContainSubstring("gzip"))
		})
	})
})
