package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/twpayne/go-vfs/v5/vfst"
	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("prepareConfiguration", func() {
	It("loads the content from a file path", func() {
		temp, err := os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(temp)

		content, err := yaml.Marshal(sdkConfig.Config{
			Debug: true,
			Install: &sdkInstall.Install{
				Device: "fake",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		err = os.WriteFile(filepath.Join(temp, "config.yaml"), content, 0644)
		Expect(err).ToNot(HaveOccurred())

		source, err := prepareConfiguration(filepath.Join(temp, "config.yaml"))
		Expect(err).ToNot(HaveOccurred())

		var cfg sdkConfig.Config
		err = yaml.NewDecoder(source).Decode(&cfg)
		Expect(cfg.ConfigURL).To(BeEmpty())
		Expect(cfg.Debug).To(BeTrue())
		Expect(cfg.Install.Device).To(Equal("fake"))
	})

	It("creates a configuration file containing the given url", func() {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer ts.Close()

		source, err := prepareConfiguration(ts.URL)
		Expect(err).ToNot(HaveOccurred())

		var cfg sdkConfig.Config
		err = yaml.NewDecoder(source).Decode(&cfg)
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.ConfigURL).To(Equal(ts.URL))
	})
})

var _ = Describe("generateInstallConfForCLIArgs", func() {
	It("returns empty when nothing is set", func() {
		Expect(generateInstallConfForCLIArgs("", false)).To(Equal(""))
	})
	It("emits the source", func() {
		Expect(generateInstallConfForCLIArgs("oci:foo", false)).To(ContainSubstring("source: oci:foo"))
	})
	It("emits insecure when requested", func() {
		out := generateInstallConfForCLIArgs("oci:foo", true)
		Expect(out).To(ContainSubstring("source: oci:foo"))
		Expect(out).To(ContainSubstring("allow-insecure-registries: true"))
	})
	It("emits insecure even without a source", func() {
		out := generateInstallConfForCLIArgs("", true)
		Expect(out).To(ContainSubstring("allow-insecure-registries: true"))
	})
})

var _ = Describe("generateUpgradeConfForCLIArgs", func() {
	It("emits allow-insecure-registries when requested", func() {
		out, err := generateUpgradeConfForCLIArgs("oci:foo", "", true)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring(`"allow-insecure-registries":true`))
	})
	It("omits allow-insecure-registries by default", func() {
		out, err := generateUpgradeConfForCLIArgs("oci:foo", "", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(ContainSubstring("allow-insecure-registries"))
	})
})

var _ = Describe("RunInstall", func() {
	var options *sdkConfig.Config
	var err error
	var fs sdkFS.KairosFS
	var cleanup func()
	var ghwTest ghwMock.GhwMock
	var cmdline func() ([]byte, error)

	BeforeEach(func() {
		// Default mock objects
		runner := v1mock.NewFakeRunner()
		//logger.SetLevel(v1.DebugLevel())
		// Set default cmdline function so we dont panic :o
		cmdline = func() ([]byte, error) {
			return []byte{}, nil
		}

		// Init test fs
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": ""})
		Expect(err).Should(BeNil())
		// Create tmp dir
		fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)
		// Create grub confg
		grubCfg := filepath.Join(constants.ActiveDir, constants.GrubConf)
		err = fsutils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
		Expect(err).To(BeNil())
		_, err = fs.Create(grubCfg)
		Expect(err).To(BeNil())

		// Side effect of runners, hijack calls to commands and return our stuff
		runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
			switch cmd {
			case "lsblk":
				return []byte(`{
"blockdevices":
    [
        {"label": "COS_ACTIVE", "type": "loop", "path": "/some/loop0"},
        {"label": "COS_OEM", "type": "part", "path": "/some/device1"},
        {"label": "COS_RECOVERY", "type": "part", "path": "/some/device2"},
        {"label": "COS_STATE", "type": "part", "path": "/some/device3"},
        {"label": "COS_PERSISTENT", "type": "part", "path": "/some/device4"}
    ]
}`), nil
			case "cat":
				if args[0] == "/proc/cmdline" {
					return cmdline()
				}
				return []byte{}, nil
			default:
				return []byte{}, nil
			}
		}

		device := "/some/device"
		err = fsutils.MkdirAll(fs, filepath.Dir(device), constants.DirPerm)
		Expect(err).To(BeNil())
		_, err = fs.Create(device)
		Expect(err).ShouldNot(HaveOccurred())

		options = &sdkConfig.Config{
			Install: &sdkInstall.Install{
				Device: "/some/device",
				Source: "test",
			},
		}

		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					FS:              "ext4",
				},
				{
					Name:            "device2",
					FilesystemLabel: "COS_STATE",
					FS:              "ext4",
				},
				{
					Name:            "device3",
					FilesystemLabel: "COS_PERSISTENT",
					FS:              "ext4",
				},
				{
					Name:            "device4",
					FilesystemLabel: "COS_ACTIVE",
					FS:              "ext4",
				},
				{
					Name:            "device5",
					FilesystemLabel: "COS_PASSIVE",
					FS:              "ext4",
				},
				{
					Name:            "device5",
					FilesystemLabel: "COS_RECOVERY",
					FS:              "ext4",
				},
				{
					Name:            "device6",
					FilesystemLabel: "COS_OEM",
					FS:              "ext4",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		cleanup()
	})

	It("runs the install", func() {
		Skip("Not ready yet")
		err = RunInstall(options)
		Expect(err).ToNot(HaveOccurred())
	})
})
