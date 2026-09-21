package image_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/kairos-io/kairos/v4/sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// emptyConfigDigest is the digest of the two-byte `{}` blob that OCI's empty
// descriptor points at, and that oras uses as the config of an artifact.
const emptyConfig = "{}"

// pushBlob uploads content to the fake registry and returns its digest.
func pushBlob(host, repo string, content []byte) (string, error) {
	client := &http.Client{Transport: insecureTransport()}
	start := fmt.Sprintf("https://%s/v2/%s/blobs/uploads/", host, repo)
	response, err := client.Post(start, "application/octet-stream", nil)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(response.Body)
		return "", fmt.Errorf("start upload: %s: %s", response.Status, body)
	}

	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(content))
	upload := fmt.Sprintf("https://%s%s?digest=%s", host, response.Header.Get("Location"), url.QueryEscape(digest))
	request, err := http.NewRequest(http.MethodPut, upload, bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	finish, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer finish.Body.Close()
	if finish.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(finish.Body)
		return "", fmt.Errorf("finish upload: %s: %s", finish.Status, body)
	}
	return digest, nil
}

// pushRawExtensionArtifact publishes an artifact shaped exactly like the ones
// hadron-layers pushes with `oras push --artifact-type
// application/vnd.kairos.sysext.raw`: an image manifest with an artifactType,
// an empty config and one layer whose media type is the artifact type and whose
// title annotation carries the `.raw` file name.
//
// declaredDigest overrides the digest recorded in the manifest, so a test can
// publish an artifact whose manifest disagrees with its blob.
func pushRawExtensionArtifact(host, repo, tag, artifactType, title string, payload []byte, declaredDigest string) error {
	configDigest, err := pushBlob(host, repo, []byte(emptyConfig))
	if err != nil {
		return err
	}
	payloadDigest, err := pushBlob(host, repo, payload)
	if err != nil {
		return err
	}
	if declaredDigest != "" {
		payloadDigest = declaredDigest
	}

	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     string(types.OCIManifestSchema1),
		"artifactType":  artifactType,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.empty.v1+json",
			"digest":    configDigest,
			"size":      len(emptyConfig),
		},
		"layers": []map[string]any{{
			"mediaType":   artifactType,
			"digest":      payloadDigest,
			"size":        len(payload),
			"annotations": map[string]string{"org.opencontainers.image.title": title},
		}},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPut, fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, repo, tag), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", string(types.OCIManifestSchema1))
	response, err := (&http.Client{Transport: insecureTransport()}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		message, _ := io.ReadAll(response.Body)
		return fmt.Errorf("put manifest: %s: %s", response.Status, message)
	}
	return nil
}

var _ = Describe("raw extension artifacts", func() {
	// A squashfs image starts with the "hsqs" magic, which is emphatically not
	// a tar header. Any byte string does for the test, but this is what the
	// real blobs look like.
	payload := append([]byte("hsqs"), bytes.Repeat([]byte{0xAB, 0x03, 0x7F}, 512)...)

	var (
		server  *httptest.Server
		host    string
		destDir string
	)

	BeforeEach(func() {
		server = httptest.NewTLSServer(ggcrregistry.New())
		parsed, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		host = parsed.Host

		destDir, err = os.MkdirTemp("", "sdk-rawextension-*")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		Expect(os.RemoveAll(destDir)).To(Succeed())
	})

	It("writes the sysext image out under the name the artifact carries", func() {
		Expect(pushRawExtensionArtifact(host, "hadron-layers/sysext/fwupd", "2.1.7-amd64",
			image.RawSysextArtifactType, "fwupd.sysext.raw", payload, "")).To(Succeed())

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(
			host+"/hadron-layers/sysext/fwupd:2.1.7-amd64", destDir, "linux/amd64")
		Expect(err).ToNot(HaveOccurred())

		written, err := os.ReadFile(filepath.Join(destDir, "fwupd.sysext.raw"))
		Expect(err).ToNot(HaveOccurred())
		Expect(written).To(Equal(payload))

		// The temporary file used while downloading must not survive.
		entries, err := os.ReadDir(destDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(1))
	})

	It("writes confext images out too", func() {
		Expect(pushRawExtensionArtifact(host, "hadron-layers/confext/tuning", "1.0.0-amd64",
			image.RawConfextArtifactType, "tuning.confext.raw", payload, "")).To(Succeed())

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(
			host+"/hadron-layers/confext/tuning:1.0.0-amd64", destDir, "linux/amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(filepath.Join(destDir, "tuning.confext.raw")).To(BeAnExistingFile())
	})

	It("refuses an artifact whose blob does not match the manifest digest", func() {
		other := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("a different image")))
		Expect(pushRawExtensionArtifact(host, "hadron-layers/sysext/fwupd", "tampered",
			image.RawSysextArtifactType, "fwupd.sysext.raw", payload, other)).To(Succeed())

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(
			host+"/hadron-layers/sysext/fwupd:tampered", destDir, "linux/amd64")
		Expect(err).To(HaveOccurred())

		// Nothing at all may be left behind, not even a partial download.
		entries, err := os.ReadDir(destDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("refuses a title annotation that points outside the destination", func() {
		Expect(pushRawExtensionArtifact(host, "hadron-layers/sysext/evil", "1.0.0-amd64",
			image.RawSysextArtifactType, "../../etc/systemd/system/evil.sysext.raw", payload, "")).To(Succeed())

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(
			host+"/hadron-layers/sysext/evil:1.0.0-amd64", destDir, "linux/amd64")
		Expect(err).To(MatchError(ContainSubstring("must be a plain file name")))

		entries, err := os.ReadDir(destDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("refuses an artifact with no title to name the file after", func() {
		Expect(pushRawExtensionArtifact(host, "hadron-layers/sysext/anonymous", "1.0.0-amd64",
			image.RawSysextArtifactType, "", payload, "")).To(Succeed())

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(
			host+"/hadron-layers/sysext/anonymous:1.0.0-amd64", destDir, "linux/amd64")
		Expect(err).To(MatchError(ContainSubstring("org.opencontainers.image.title")))
	})

	It("leaves ordinary container images to tar extraction", func() {
		img, err := currentUserImage()
		Expect(err).ToNot(HaveOccurred())

		_, err = image.ExtractRawExtension(img, destDir)
		Expect(err).To(MatchError(image.ErrNotRawExtension))
	})

	It("does not mistake an uncompressed layer for a raw extension", func() {
		// Same uncompressed single layer, but no artifactType: this is an
		// ordinary image and must still go through tar extraction.
		img, err := mutate.AppendLayers(empty.Image, static.NewLayer(payload, types.MediaType(image.RawSysextArtifactType)))
		Expect(err).ToNot(HaveOccurred())

		_, err = image.ExtractRawExtension(img, destDir)
		Expect(err).To(MatchError(image.ErrNotRawExtension))
	})

	It("reports the artifact types it recognises", func() {
		Expect(image.IsRawExtensionArtifactType(image.RawSysextArtifactType)).To(BeTrue())
		Expect(image.IsRawExtensionArtifactType(image.RawConfextArtifactType)).To(BeTrue())
		Expect(image.IsRawExtensionArtifactType(string(types.OCIManifestSchema1))).To(BeFalse())
		Expect(image.IsRawExtensionArtifactType("")).To(BeFalse())
	})

	It("rejects an artifact carrying more than one layer", func() {
		manifest := &v1.Manifest{
			ArtifactType: image.RawSysextArtifactType,
			Layers: []v1.Descriptor{
				{MediaType: types.MediaType(image.RawSysextArtifactType)},
				{MediaType: types.MediaType(image.RawSysextArtifactType)},
			},
		}
		_, err := image.ExtractRawExtension(fixedManifestImage{manifest: manifest}, destDir)
		Expect(err).To(MatchError(ContainSubstring("exactly one layer")))
	})
})

// fixedManifestImage is a v1.Image that only knows its manifest, which is all
// ExtractRawExtension needs to reject a malformed artifact.
type fixedManifestImage struct {
	v1.Image
	manifest *v1.Manifest
}

func (i fixedManifestImage) Manifest() (*v1.Manifest, error) { return i.manifest, nil }
