package action_test

import (
	"bytes"
	"fmt"
	"os"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Sysext Actions test", Label("sysext"), func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var httpClient *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var err error

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		httpClient = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		extractor = v1mock.NewFakeImageExtractor(logger)
		cloudInit = &v1mock.FakeCloudInitRunner{}
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		err := vfs.MkdirAll(fs, "/var/lib/kairos/extensions", 0755)
		Expect(err).ToNot(HaveOccurred())
		err = vfs.MkdirAll(fs, "/run/extensions", 0755)
		Expect(err).ToNot(HaveOccurred())

		// Config object with all of our fakes on it
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(httpClient),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
			agentConfig.WithPlatform("linux/amd64"),
		)
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Extension type", func() {
		It("returns the name as string", func() {
			ext := &action.Extension{Name: "valid.raw", Location: "/var/lib/kairos/extensions/valid.raw"}
			Expect(ext.String()).To(Equal("valid.raw"))
		})
	})

	Describe("Invalid extension type", func() {
		It("should fail to list extensions with an invalid extension type", func() {
			_, err := action.ListExtensions(config, "active", "invalid")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Getting extensions", func() {
		It("should fail with an invalid regex", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			_, err := action.GetExtension(config, "[invalid", "", "sysext")
			Expect(err).To(HaveOccurred())
		})
		It("should fail when listing the extensions fails", func() {
			_, err := action.GetExtension(config, "valid.raw", "", "invalid")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Listing extensions", func() {
		It("should NOT fail if the bootstate is not valid", func() {
			extensions, err := action.ListExtensions(config, "invalid", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		Describe("With no dir", func() {
			BeforeEach(func() {
				err = config.Fs.RemoveAll("/var/lib/kairos/extensions")
				Expect(err).ToNot(HaveOccurred())
			})
			AfterEach(func() {
				cleanup()
			})
			It("should return no extensions for installed extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for active enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "active", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for passive enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "passive", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for recovery enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "recovery", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for common enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "common", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("With empty dir", func() {
			It("should return no extensions", func() {
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for active enabled extensions", func() {
				extensions, err := action.ListExtensions(config, "active", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for passive enabled extensions", func() {
				extensions, err := action.ListExtensions(config, "passive", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for recovery enabled extensions", func() {
				extensions, err := action.ListExtensions(config, "recovery", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for common enabled extensions", func() {
				extensions, err := action.ListExtensions(config, "common", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("With dir with files", func() {
			It("should not return files that are not valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/invalid", []byte("invalid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return files that are valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(Equal([]action.Extension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
			It("should ONLY return files that are valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/invalid", []byte("invalid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(len(extensions)).To(Equal(1))
				Expect(extensions).To(Equal([]action.Extension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
		})
	})
	Describe("Enabling extensions", func() {
		It("should enable an installed extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			extensions, err = action.ListExtensions(config, "recovery", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			extensions, err = action.ListExtensions(config, "common", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for passive
			err = action.EnableExtension(config, "valid.raw", "passive", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			// Passive should have the extension
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/passive/valid.raw",
				},
			}))
			// Check active again to see if it is still there
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Enable it for recovery
			err = action.EnableExtension(config, "valid.raw", "recovery", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			// Passive should have the extension
			extensions, err = action.ListExtensions(config, "recovery", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/recovery/valid.raw",
				},
			}))

		})
		It("should enable an installed extension and reload the system with it", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			// Fake the boot state
			Expect(config.Fs.Mkdir("/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			// This basically returns an error if the command is not executed
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Symlink should be created in /run/extensions
			_, err = config.Fs.Stat("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			readlink, err := config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			// Get the raw path as the readlink will return the real path, not the one in our fake fs
			realPath, err := config.Fs.RawPath("/var/lib/kairos/extensions/active/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			Expect(readlink).To(Equal(realPath))
		})
		It("should enable an installed extension and reload the system with it if its a common one", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "common", "sysext")
			Expect(err).ToNot(HaveOccurred())
			// This basically returns an error if the command is not executed
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for common
			err = action.EnableExtension(config, "valid.raw", "common", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			// Should have refreshed the systemd-sysext
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).ToNot(HaveOccurred())
			// Should be enabled
			extensions, err = action.ListExtensions(config, "common", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/common/valid.raw",
				},
			}))
			// Active and Passive should be empty
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Symlink should be created in /run/extensions
			_, err = config.Fs.Stat("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
		})
		It("should enable an installed extension and NOT reload the system with it if we are on the wrong boot state", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			// This basically returns an error if the command is not executed
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Symlink should be created in /run/extensions
			_, err = config.Fs.Stat("/run/extensions/valid.raw")
			Expect(err).To(HaveOccurred())
		})
		It("should fail to enable a missing extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "invalid.raw", "active", "sysext", false)
			Expect(err).To(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Passive should be empty
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should not fail if the extension is already enabled", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
		})

	})
	Describe("Disabling extensions", func() {
		It("should disable an enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Disable it
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should not fail to disable a not enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Disable a non enabled extension
			err = action.DisableExtension(config, "invalid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
		})
	})
	Describe("Installing extensions", func() {
		Describe("With a file source", func() {
			It("should install a extension", func() {
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
				err = config.Fs.WriteFile("/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				err = action.InstallExtension(config, "file:///valid.raw", "sysext")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				// Check if the extension is installed
				extensions, err = action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(Equal([]action.Extension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
			It("should preserve file permissions when streaming", func() {
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
				// Create a file with specific permissions (0755 survives default umask 022)
				err = config.Fs.WriteFile("/perms.raw", []byte("valid"), 0640)
				Expect(err).ToNot(HaveOccurred())
				err = action.InstallExtension(config, "file:///perms.raw", "sysext")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				// Check if the extension is installed
				extensions, err = action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(len(extensions)).To(Equal(1))
				// Check that file permissions are preserved
				stat, err := config.Fs.Stat("/var/lib/kairos/extensions/perms.raw")
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.Mode() & 0777).To(Equal(os.FileMode(0640)))
			})
			It("should fail to install a missing extension", func() {
				extensions, err := action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
				err = action.InstallExtension(config, "file:///invalid.raw", "sysext")
				Expect(err).To(HaveOccurred(), memLog.String())
				// Check if the extension is installed
				extensions, err = action.ListExtensions(config, "", "sysext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("with a docker source", func() {
			It("should install a extension", func() {
				err = action.InstallExtension(config, "docker://quay.io/valid:v1.0.0", "sysext")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				expectedCall := v1mock.ExtractCall{ImageRef: "quay.io/valid:v1.0.0", Destination: "/var/lib/kairos/extensions/", PlatformRef: ""}
				Expect(extractor.WasCalledWithExtractCall(expectedCall)).To(BeTrue())
			})
			It("should fail to install a missing extension", func() {
				extractor.SideEffect = func(imageRef, destination, platformRef string) error {
					return fmt.Errorf("error")
				}
				err = action.InstallExtension(config, "docker://quay.io/invalid:v1.0.0", "sysext")
				Expect(err).To(HaveOccurred(), memLog.String())
				expectedCall := v1mock.ExtractCall{ImageRef: "quay.io/invalid:v1.0.0", Destination: "/var/lib/kairos/extensions/", PlatformRef: ""}
				Expect(extractor.WasCalledWithExtractCall(expectedCall)).To(BeTrue())
			})
		})
		Describe("with a http source", func() {
			It("should install a extension", func() {
				err = action.InstallExtension(config, "http://localhost:8080/valid.raw", "sysext")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				Expect(httpClient.WasGetCalledWith("http://localhost:8080/valid.raw")).To(BeTrue())
			})
			It("should fail to install a missing extension", func() {
				httpClient.Error = true
				err = action.InstallExtension(config, "http://localhost:8080/invalid.raw", "sysext")
				Expect(err).To(HaveOccurred())
				Expect(httpClient.WasGetCalledWith("http://localhost:8080/invalid.raw")).To(BeTrue())
			})
		})
	})
	Describe("Removing extensions", func() {
		It("should remove an installed extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/valid.raw",
				},
			}))
			err = action.RemoveExtension(config, "valid.raw", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should disable and remove an enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			err = action.RemoveExtension(config, "valid.raw", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			// Check if it is removed from active
			extensions, err = action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Check if it is removed from passive
			extensions, err = action.ListExtensions(config, "passive", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Check if it is removed from installed
			extensions, err = action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should fail to remove a missing extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/valid.raw",
				},
			}))
			err = action.RemoveExtension(config, "invalid.raw", "sysext", false)
			Expect(err).To(HaveOccurred())
		})
		It("should remove an extension and refresh the system if it was active", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			// Fake the boot state
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			// Enable it for active with now, so the symlink in /run/extensions is created
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			_, err = config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			runner.ClearCmds()
			// Remove it with now
			err = action.RemoveExtension(config, "valid.raw", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).ToNot(HaveOccurred())
			// Symlink should be gone
			_, err = config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).To(HaveOccurred())
			// Extension should be gone
			extensions, err := action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should fail to remove an active extension if the refresh fails", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			// Fake the boot state
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == "systemctl" {
					return []byte{}, fmt.Errorf("systemctl failure")
				}
				return []byte{}, nil
			}
			err = action.RemoveExtension(config, "valid.raw", "sysext", true)
			Expect(err).To(HaveOccurred())
		})
		It("should remove an extension without refreshing if it was not merged", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			err = action.RemoveExtension(config, "valid.raw", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			// No refresh should have happened as there was no symlink in /run/extensions
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
	})
	Describe("Disabling extensions with immediate refresh", func() {
		BeforeEach(func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should fail to disable if the target dir does not exist", func() {
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})
		It("should disable and refresh an enabled and merged extension", func() {
			// Fake the boot state
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			_, err = config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			runner.ClearCmds()

			err = action.DisableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).ToNot(HaveOccurred())
			// Symlink should be gone
			_, err = config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).To(HaveOccurred())
			extensions, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should fail to disable a merged extension if the refresh fails", func() {
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == "systemctl" {
					return []byte{}, fmt.Errorf("systemctl failure")
				}
				return []byte{}, nil
			}
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).To(HaveOccurred())
		})
		It("should disable without refreshing if the extension was not merged", func() {
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
		})
		It("should disable but not refresh if booted in a different boot state", func() {
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("not refreshed as we are currently not booted in"))
		})
	})
	Describe("Enabling extensions with immediate refresh failures", func() {
		BeforeEach(func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should fail to enable an extension if the refresh fails", func() {
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == "systemctl" {
					return []byte{}, fmt.Errorf("systemctl failure")
				}
				return []byte{}, nil
			}
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).To(HaveOccurred())
		})
		It("should fail to enable an extension if the symlink cannot be created", func() {
			// Block the target dir with a file so the symlink fails
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/active", []byte("blocking"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create symlink"))
		})
	})
	Describe("Confext extensions", func() {
		BeforeEach(func() {
			err := vfs.MkdirAll(fs, "/var/lib/kairos/confexts", 0755)
			Expect(err).ToNot(HaveOccurred())
			err = vfs.MkdirAll(fs, "/run/confexts", 0755)
			Expect(err).ToNot(HaveOccurred())
			err = config.Fs.WriteFile("/var/lib/kairos/confexts/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should list and enable confexts in all boot states", func() {
			extensions, err := action.ListExtensions(config, "", "confext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/confexts/valid.raw",
				},
			}))
			for _, state := range []string{"active", "passive", "recovery", "common"} {
				err = action.EnableExtension(config, "valid.raw", state, "confext", false)
				Expect(err).ToNot(HaveOccurred())
				extensions, err = action.ListExtensions(config, state, "confext")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(Equal([]action.Extension{
					{
						Name:     "valid.raw",
						Location: fmt.Sprintf("/var/lib/kairos/confexts/%s/valid.raw", state),
					},
				}))
			}
		})
		It("should enable and merge a common confext immediately", func() {
			err = action.EnableExtension(config, "valid.raw", "common", "confext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-confext"},
			})).ToNot(HaveOccurred())
			_, err = config.Fs.Readlink("/run/confexts/valid.raw")
			Expect(err).ToNot(HaveOccurred())
		})
		It("should disable and refresh a merged confext", func() {
			err = action.EnableExtension(config, "valid.raw", "common", "confext", true)
			Expect(err).ToNot(HaveOccurred())
			runner.ClearCmds()
			err = action.DisableExtension(config, "valid.raw", "common", "confext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-confext"},
			})).ToNot(HaveOccurred())
			_, err = config.Fs.Readlink("/run/confexts/valid.raw")
			Expect(err).To(HaveOccurred())
		})
		It("should remove a merged confext and refresh", func() {
			err = action.EnableExtension(config, "valid.raw", "common", "confext", true)
			Expect(err).ToNot(HaveOccurred())
			runner.ClearCmds()
			err = action.RemoveExtension(config, "valid.raw", "confext", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-confext"},
			})).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "confext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should install a confext from a file source", func() {
			err = config.Fs.WriteFile("/newconf.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.InstallExtension(config, "file:///newconf.raw", "confext")
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "confext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(ContainElement(action.Extension{
				Name:     "newconf.raw",
				Location: "/var/lib/kairos/confexts/newconf.raw",
			}))
		})
	})
	Describe("Installing extensions URI parsing", func() {
		It("should fail with an invalid URI", func() {
			err = action.InstallExtension(config, ":invalid", "sysext")
			Expect(err).To(HaveOccurred())
		})
		It("should fail with an unsupported scheme", func() {
			err = action.InstallExtension(config, "ftp://example.com/valid.raw", "sysext")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid URI reference"))
		})
		It("should fail with an invalid image reference", func() {
			err = action.InstallExtension(config, "docker://repo/Invalid_Image", "sysext")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid image reference"))
		})
		It("should append the latest tag to name-only image references", func() {
			err = action.InstallExtension(config, "oci://test/valid", "sysext")
			Expect(err).ToNot(HaveOccurred())
			expectedCall := v1mock.ExtractCall{ImageRef: "test/valid:latest", Destination: "/var/lib/kairos/extensions/", PlatformRef: ""}
			Expect(extractor.WasCalledWithExtractCall(expectedCall)).To(BeTrue())
		})
		It("should create the target dir if it does not exist", func() {
			Expect(config.Fs.RemoveAll("/var/lib/kairos/extensions")).ToNot(HaveOccurred())
			err = config.Fs.WriteFile("/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.InstallExtension(config, "file:///valid.raw", "sysext")
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListExtensions(config, "", "sysext")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.Extension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/valid.raw",
				},
			}))
		})
		It("should fail if the target dir cannot be created", func() {
			Expect(config.Fs.RemoveAll("/var/lib/kairos/extensions")).ToNot(HaveOccurred())
			// Block the path with a file so MkdirAll fails
			err = config.Fs.WriteFile("/var/lib/kairos/extensions", []byte("blocking"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = config.Fs.WriteFile("/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.InstallExtension(config, "file:///valid.raw", "sysext")
			Expect(err).To(HaveOccurred())
		})
		It("should fail if the file cannot be written to the target dir", func() {
			err = config.Fs.WriteFile("/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.InstallExtension(config, "file:///valid.raw", "sysext")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create file"))
		})
	})
	Describe("Read only filesystem failures", func() {
		BeforeEach(func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should fail to install when the target dir cannot be created", func() {
			Expect(config.Fs.RemoveAll("/var/lib/kairos/extensions")).ToNot(HaveOccurred())
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.InstallExtension(config, "file:///valid.raw", "sysext")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create target dir"))
		})
		It("should fail to list extensions when the dir cannot be created", func() {
			config.Fs = vfs.NewReadOnlyFS(fs)
			_, err := action.ListExtensions(config, "active", "sysext")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create target dir"))
		})
		It("should fail to enable an extension when the target dir cannot be created", func() {
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create target dir"))
		})
		It("should fail to disable an extension when the symlink cannot be removed", func() {
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.DisableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to remove symlink"))
		})
		It("should fail to remove an enabled extension when the symlink cannot be removed", func() {
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", false)
			Expect(err).ToNot(HaveOccurred())
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.RemoveExtension(config, "valid.raw", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to remove symlink"))
		})
		It("should fail to remove an extension when the file cannot be removed", func() {
			config.Fs = vfs.NewReadOnlyFS(fs)
			err = action.RemoveExtension(config, "valid.raw", "sysext", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to remove extension"))
		})
	})
	Describe("Enabling extensions when the run dir is missing", func() {
		It("should fail to enable with immediate refresh if the run dir does not exist", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			Expect(vfs.MkdirAll(config.Fs, "/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			Expect(config.Fs.RemoveAll("/run/extensions")).ToNot(HaveOccurred())
			err = action.EnableExtension(config, "valid.raw", "active", "sysext", true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create symlink"))
		})
	})
})
