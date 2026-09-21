package config

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("registry authentication during image sizing", func() {
	DescribeTable("applies cloud-config before the first manifest request",
		func(operation string, uki, fromFile bool) {
			DeferCleanup(viper.Reset)
			// Never consult the developer's daemon or registry credential files.
			home := GinkgoT().TempDir()
			for _, key := range []string{"HOME", "USERPROFILE", "DOCKER_CONFIG", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME"} {
				GinkgoT().Setenv(key, home)
			}
			GinkgoT().Setenv("REGISTRY_AUTH_FILE", home+"/missing-auth.json")
			GinkgoT().Setenv("DOCKER_HOST", "unix://"+home+"/missing-docker.sock")
			GinkgoT().Setenv("DOCKER_TLS_VERIFY", "")
			GinkgoT().Setenv("DOCKER_CERT_PATH", "")

			var manifestRequests atomic.Int64
			backend := registry.New()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "registry-user" || pass != "registry-password" {
					w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/manifests/") {
					manifestRequests.Add(1)
				}
				backend.ServeHTTP(w, r)
			}))
			DeferCleanup(server.Close)
			imageRef := strings.TrimPrefix(server.URL, "https://") + "/system:test"
			ref, err := name.ParseReference(imageRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(remote.Write(ref, empty.Image,
				remote.WithTransport(server.Client().Transport),
				remote.WithAuth(&authn.Basic{Username: "registry-user", Password: "registry-password"}),
			)).To(Succeed())
			manifestRequests.Store(0)

			fs, cleanup, err := vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(cleanup)
			devices := &ghwMock.GhwMock{}
			devices.AddDisk(partitions.Disk{Name: "device", Partitions: []*partitions.Partition{
				{Name: "device1", FilesystemLabel: constants.EfiLabel, FS: "vfat", MountPoint: GinkgoT().TempDir()},
				{Name: "device2", FilesystemLabel: constants.StateLabel, FS: "ext4"},
			}})
			devices.CreateDevices()
			DeferCleanup(devices.Clean)
			cfg := NewConfig(WithFs(fs), WithLogger(logger.NewNullLogger()),
				WithRunner(v1mock.NewFakeRunner()), WithMounter(v1mock.NewErrorMounter()), WithPlatform("linux/amd64"))
			cfg.Install = &install.Install{Source: "oci:" + imageRef}
			body := fmt.Sprintf("#cloud-config\n%s:\n  allow-insecure-registries: true\n  registry-auth:\n    username: registry-user\n    password: registry-password\n  system:\n    source: oci:%s\n", operation, imageRef)
			if fromFile {
				Expect(fs.WriteFile("/registry-auth.yaml", []byte("username: registry-user\npassword: registry-password\n"), 0600)).To(Succeed())
				body = strings.Replace(body, "    username: registry-user\n    password: registry-password\n", "    file: /registry-auth.yaml\n", 1)
			}
			opts := &collector.Options{}
			Expect(opts.Apply(collector.Readers(strings.NewReader(body)), collector.NoLogs)).To(Succeed())
			collected, err := collector.Scan(opts, FilterKeys)
			Expect(err).ToNot(HaveOccurred())
			cfg.Collector = *collected

			switch {
			case operation == "install" && uki:
				_, err = NewUkiInstallSpec(cfg)
			case operation == "install":
				_, err = NewInstallSpec(cfg)
			case uki:
				_, err = NewUkiUpgradeSpec(cfg)
			default:
				_, err = NewUpgradeSpec(cfg)
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(manifestRequests.Load()).To(BeNumerically(">", 0), "size lookup must authenticate before accessing the image")
			extractor := cfg.ImageExtractor.(imageextractor.OCIImageExtractor)
			Expect(extractor.Insecure).To(BeTrue())
			Expect(extractor.Auth).ToNot(BeNil())
			Expect(extractor.Auth.Username).To(Equal("registry-user"))
		},
		Entry("install", "install", false, false),
		Entry("UKI install", "install", true, false),
		Entry("upgrade", "upgrade", false, false),
		Entry("UKI upgrade", "upgrade", true, false),
		Entry("install from file", "install", false, true),
		Entry("UKI install from file", "install", true, true),
		Entry("upgrade from file", "upgrade", false, true),
		Entry("UKI upgrade from file", "upgrade", true, true),
	)
})
