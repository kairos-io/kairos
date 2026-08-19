package hook_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Hooks Suite")
}

var _ = Describe("Hooks", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var runner *v1mock.FakeRunner
	var mounter *v1mock.ErrorMounter
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var ghwTest ghwMock.GhwMock
	var err error

	Context("SysExtPostInstall", func() {
		BeforeEach(func() {
			runner = v1mock.NewFakeRunner()
			syscallMock = &v1mock.FakeSyscall{}
			mounter = v1mock.NewErrorMounter()
			client = &v1mock.FakeHTTPClient{}
			memLog = &bytes.Buffer{}
			logger = sdkLogger.NewBufferLogger(memLog)
			extractor = v1mock.NewFakeImageExtractor(logger)
			logger.SetLevel("debug")
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
			// Create proper dir structure for our EFI partition contents
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/efi/EFI/BOOT", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/efi/EFI/kairos", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/etc/cos/", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/run/initramfs/cos-state/grub/", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/etc/kairos/branding/", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())

			cloudInit = &v1mock.FakeCloudInitRunner{}
			cfg = config.NewConfig(
				config.WithFs(fs),
				config.WithRunner(runner),
				config.WithLogger(logger),
				config.WithMounter(mounter),
				config.WithSyscall(syscallMock),
				config.WithClient(client),
				config.WithCloudInitRunner(cloudInit),
				config.WithImageExtractor(extractor),
			)
			cfg.Collector = collector.Config{}

			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						FS:              "ext4",
						MountPoint:      "/efi",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
			cleanup()
		})
		It("should copy all files with .sysext.raw extension", func() {
			err = fsutils.MkdirAll(fs, cnst.LiveDir, os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "test1.sysext.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "test2.sysext.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).Should(BeNil())
			// we expect them to be here as its where we mount the efi partition but then we fake unmount
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "test1.sysext.raw"))
			Expect(err).Should(BeNil())
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "test2.sysext.raw"))
			Expect(err).Should(BeNil())
		})
		It("should ignore files without .sysext.raw extension", func() {
			err = fsutils.MkdirAll(fs, cnst.LiveDir, os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "test1.sysext.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "test2.sysext.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "hello.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "hello.sysext.what.raw"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			err = fs.WriteFile(filepath.Join(cnst.LiveDir, "hello.sysext"), []byte("test"), os.ModePerm)
			Expect(err).Should(BeNil())
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).Should(BeNil())
			// we expect them to be here as its where we mount the efi partition but then we fake unmount
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "test1.sysext.raw"))
			Expect(err).Should(BeNil())
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "test2.sysext.raw"))
			Expect(err).Should(BeNil())
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "hello.raw"))
			Expect(err).ShouldNot(BeNil())
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "hello.sysext.what.raw"))
			Expect(err).ShouldNot(BeNil())
			_, err = fs.Stat(filepath.Join(cnst.EfiDir, "EFI/kairos/active.efi.extra.d/", "hello.sysext"))
			Expect(err).ShouldNot(BeNil())
		})
		It("doesn't error if it cant find the efi partition", func() {
			ghwTest.Clean()
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).Should(BeNil())
		})
		It("errors if it cant mount the efi partition and strict is set", func() {
			ghwTest.Clean()
			cfg.FailOnBundleErrors = true
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).ShouldNot(BeNil())
		})
		It("doesn't error if it cant mount the efi partition", func() {
			mounter.ErrorOnMount = true
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).Should(BeNil())
		})
		It("errors if it cant mount the efi partition and strict is set", func() {
			mounter.ErrorOnMount = true
			cfg.FailOnBundleErrors = true
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).ShouldNot(BeNil())
		})
		It("doesn't error if it cant create the dirs", func() {
			ROfs := vfs.NewReadOnlyFS(fs)
			cfg.Fs = ROfs
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).Should(BeNil())
		})
		It("errors if it cant create the dirs and strict is set", func() {
			cfg.FailOnBundleErrors = true
			ROfs := vfs.NewReadOnlyFS(fs)
			cfg.Fs = ROfs
			postInstall := hook.SysExtPostInstall{}
			err = postInstall.Run(*cfg, nil)
			Expect(err).ShouldNot(BeNil())
		})

	})
})
