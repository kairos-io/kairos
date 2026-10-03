package image_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	registrytypes "github.com/moby/moby/api/types/registry"

	"github.com/kairos-io/kairos/v4/sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testRegistryUser = "kairos-test"
	testRegistryPass = "correct-password"
)

var _ = Describe("OCIImageExtractor registry authentication", func() {
	var server *httptest.Server
	var imageRef string

	BeforeEach(func() {
		isolateCredentialEnvironment()
		server = httptest.NewServer(requireBasicAuth(ggcrregistry.New(), testRegistryUser, testRegistryPass))
		DeferCleanup(server.Close)
		imageRef = seedRegistryImage(server, false)
	})

	It("uses explicit auth for extraction and size", func() {
		extractor := image.OCIImageExtractor{Insecure: true, Auth: testAuth()}
		destDir, err := os.MkdirTemp("", "sdk-image-auth-dest-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, destDir)
		Expect(extractor.ExtractImage(imageRef, destDir, "linux/amd64")).To(Succeed())
		content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("hello from the insecure registry"))
		size, err := extractor.GetOCIImageSize(imageRef, "linux/amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(BeNumerically(">", 0))
	})

	It("fails extraction and size when auth is absent or wrong", func() {
		for _, extractor := range []image.OCIImageExtractor{
			{Insecure: true},
			{Insecure: true, Auth: &registrytypes.AuthConfig{Username: testRegistryUser, Password: "wrong-password"}},
		} {
			destDir, err := os.MkdirTemp("", "sdk-image-auth-dest-*")
			Expect(err).ToNot(HaveOccurred())
			err = extractor.ExtractImage(imageRef, destDir, "linux/amd64")
			expectUnauthorized(err)
			Expect(os.RemoveAll(destDir)).To(Succeed())
			_, err = extractor.GetOCIImageSize(imageRef, "linux/amd64")
			expectUnauthorized(err)
		}
	})

	It("uses Docker credentials when auth is nil", func() {
		writeDockerAuth(serverHost(server), testRegistryUser, testRegistryPass)
		expectPullsSucceed(image.OCIImageExtractor{Insecure: true}, imageRef)
	})

	It("uses Podman REGISTRY_AUTH_FILE credentials when auth is nil", func() {
		writeAuthFile(os.Getenv("REGISTRY_AUTH_FILE"), serverHost(server), testRegistryUser, testRegistryPass)
		expectPullsSucceed(image.OCIImageExtractor{Insecure: true}, imageRef)
	})

	It("uses Podman XDG runtime credentials when auth is nil", func() {
		path := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "containers", "auth.json")
		writeAuthFile(path, serverHost(server), testRegistryUser, testRegistryPass)
		expectPullsSucceed(image.OCIImageExtractor{Insecure: true}, imageRef)
	})

	It("uses explicit auth when Docker credentials are wrong", func() {
		writeDockerAuth(serverHost(server), testRegistryUser, "wrong-password")
		expectPullsSucceed(image.OCIImageExtractor{Insecure: true, Auth: testAuth()}, imageRef)
	})

	It("gives explicit auth precedence over the default keychain", func() {
		writeDockerAuth(serverHost(server), testRegistryUser, testRegistryPass)
		wrong := image.OCIImageExtractor{Insecure: true, Auth: &registrytypes.AuthConfig{Username: testRegistryUser, Password: "wrong-password"}}
		destDir, err := os.MkdirTemp("", "sdk-image-auth-dest-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, destDir)
		err = wrong.ExtractImage(imageRef, destDir, "linux/amd64")
		expectUnauthorized(err)
		_, err = wrong.GetOCIImageSize(imageRef, "linux/amd64")
		expectUnauthorized(err)
	})
})

var _ = Describe("OCIImageExtractor authenticated self-signed TLS", func() {
	var server *httptest.Server
	var imageRef string
	BeforeEach(func() {
		isolateCredentialEnvironment()
		server = httptest.NewTLSServer(requireBasicAuth(ggcrregistry.New(), testRegistryUser, testRegistryPass))
		DeferCleanup(server.Close)
		imageRef = seedRegistryImage(server, true)
	})

	It("requires Insecure and accepts explicit auth for extraction and size", func() {
		secure := image.OCIImageExtractor{Auth: testAuth()}
		destDir, err := os.MkdirTemp("", "sdk-image-auth-tls-dest-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, destDir)
		err = secure.ExtractImage(imageRef, destDir, "linux/amd64")
		Expect(err).To(HaveOccurred())
		Expect(strings.ToLower(err.Error())).To(Or(ContainSubstring("certificate"), ContainSubstring("tls"), ContainSubstring("x509")))
		_, err = secure.GetOCIImageSize(imageRef, "linux/amd64")
		Expect(err).To(HaveOccurred())

		insecure := image.OCIImageExtractor{Insecure: true, Auth: testAuth()}
		Expect(insecure.ExtractImage(imageRef, destDir, "linux/amd64")).To(Succeed())
		content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("hello from the insecure registry"))
		size, err := insecure.GetOCIImageSize(imageRef, "linux/amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(BeNumerically(">", 0))

		wrong := image.OCIImageExtractor{Insecure: true, Auth: &registrytypes.AuthConfig{Username: testRegistryUser, Password: "wrong-password"}}
		err = wrong.ExtractImage(imageRef, destDir, "linux/amd64")
		expectUnauthorized(err)
		_, err = wrong.GetOCIImageSize(imageRef, "linux/amd64")
		expectUnauthorized(err)
	})
})

func testAuth() *registrytypes.AuthConfig {
	return &registrytypes.AuthConfig{Username: testRegistryUser, Password: testRegistryPass}
}

func expectPullsSucceed(extractor image.OCIImageExtractor, imageRef string) {
	destDir, err := os.MkdirTemp("", "sdk-image-auth-dest-*")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(os.RemoveAll, destDir)
	Expect(extractor.ExtractImage(imageRef, destDir, "linux/amd64")).To(Succeed())
	content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	Expect(err).ToNot(HaveOccurred())
	Expect(string(content)).To(Equal("hello from the insecure registry"))
	size, err := extractor.GetOCIImageSize(imageRef, "linux/amd64")
	Expect(err).ToNot(HaveOccurred())
	Expect(size).To(BeNumerically(">", 0))
}

func seedRegistryImage(server *httptest.Server, tlsServer bool) string {
	u, err := url.Parse(server.URL)
	Expect(err).ToNot(HaveOccurred())
	refString := u.Host + "/test/auth:latest"
	ref, err := name.ParseReference(refString, name.Insecure)
	Expect(err).ToNot(HaveOccurred())
	img, err := currentUserImage()
	Expect(err).ToNot(HaveOccurred())
	options := []remote.Option{remote.WithAuth(&authn.Basic{Username: testRegistryUser, Password: testRegistryPass})}
	if tlsServer {
		options = append(options, remote.WithTransport(insecureTransport()))
	}
	Expect(remote.Write(ref, img, options...)).To(Succeed())
	return refString
}

func serverHost(server *httptest.Server) string {
	u, err := url.Parse(server.URL)
	Expect(err).ToNot(HaveOccurred())
	return u.Host
}

func writeDockerAuth(host, username, password string) {
	writeAuthFile(filepath.Join(os.Getenv("DOCKER_CONFIG"), "config.json"), host, username, password)
}

func writeAuthFile(path, host, username, password string) {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	contents, err := json.Marshal(map[string]any{"auths": map[string]any{host: map[string]string{"auth": auth}}})
	Expect(err).ToNot(HaveOccurred())
	Expect(os.WriteFile(path, contents, 0o600)).To(Succeed())
}

func isolateCredentialEnvironment() {
	tmpHome, err := os.MkdirTemp("", "sdk-image-auth-home-*")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(os.RemoveAll, tmpHome)
	keys := []string{"HOME", "USERPROFILE", "DOCKER_CONFIG", "REGISTRY_AUTH_FILE", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"}
	previous := make(map[string]struct {
		value string
		set   bool
	}, len(keys))
	for _, key := range keys {
		value, set := os.LookupEnv(key)
		previous[key] = struct {
			value string
			set   bool
		}{value: value, set: set}
		Expect(os.Unsetenv(key)).To(Succeed())
	}
	DeferCleanup(func() {
		for key, old := range previous {
			if old.set {
				Expect(os.Setenv(key, old.value)).To(Succeed())
			} else {
				Expect(os.Unsetenv(key)).To(Succeed())
			}
		}
	})
	Expect(os.Setenv("HOME", tmpHome)).To(Succeed())
	Expect(os.Setenv("USERPROFILE", tmpHome)).To(Succeed())
	dockerConfig := filepath.Join(tmpHome, ".docker")
	Expect(os.MkdirAll(dockerConfig, 0o700)).To(Succeed())
	Expect(os.Setenv("DOCKER_CONFIG", dockerConfig)).To(Succeed())
	Expect(os.Setenv("REGISTRY_AUTH_FILE", filepath.Join(tmpHome, "containers-auth.json"))).To(Succeed())
	runtimeDir := filepath.Join(tmpHome, "runtime")
	Expect(os.MkdirAll(filepath.Join(runtimeDir, "containers"), 0o700)).To(Succeed())
	Expect(os.Setenv("XDG_RUNTIME_DIR", runtimeDir)).To(Succeed())
	Expect(os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, "config"))).To(Succeed())
	Expect(os.Setenv("DOCKER_HOST", "unix:///kairos-test-no-daemon.sock")).To(Succeed())
	Expect(os.Setenv("DOCKER_TLS_VERIFY", "1")).To(Succeed())
	Expect(os.Setenv("DOCKER_CERT_PATH", filepath.Join(tmpHome, "no-docker-certs"))).To(Succeed())
}

func requireBasicAuth(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != username || pass != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func expectUnauthorized(err error) {
	var transportErr *transport.Error
	Expect(errors.As(err, &transportErr)).To(BeTrue(), "expected a remote transport error, got %v", err)
	Expect(transportErr.StatusCode).To(Equal(http.StatusUnauthorized))
}
