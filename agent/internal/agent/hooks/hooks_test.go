package hook_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	implSpec "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
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

	Context("FirstBootStage", func() {
		BeforeEach(func() {
			// An empty /proc/cmdline keeps RunStage's cmdline read from
			// manufacturing an error of its own on every call, which would
			// otherwise mask what the strict specs below assert on.
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": ""})
			Expect(err).Should(BeNil())
			memLog = &bytes.Buffer{}
			logger = sdkLogger.NewBufferLogger(memLog)
			logger.SetLevel("debug")
			cloudInit = &v1mock.FakeCloudInitRunner{}
			cfg = config.NewConfig(
				config.WithFs(fs),
				config.WithLogger(logger),
				config.WithCloudInitRunner(cloudInit),
			)
			cfg.Collector = collector.Config{}
		})
		AfterEach(func() {
			cleanup()
		})

		It("drives the cloud-init runner through the first-boot stage family", func() {
			stage := hook.FirstBootStage{}
			err = stage.Run(*cfg, nil)
			Expect(err).Should(BeNil())

			// RunStage (already covered end-to-end in agent/pkg/utils) fans a
			// single stage name out into before/main/after, and may revisit
			// them again for the dot-notation cmdline pass. What belongs to
			// this hook's own contract is: every stage name it produces is
			// part of the first-boot family, and before/main/after each show
			// up at least once -- i.e. FirstBootStage handed "first-boot",
			// not some other stage name, to RunStage.
			Expect(cloudInit.ExecStages).To(ContainElements(
				cnst.FirstBootHook+".before",
				cnst.FirstBootHook,
				cnst.FirstBootHook+".after",
			))
			for _, s := range cloudInit.ExecStages {
				Expect(s).To(HavePrefix(cnst.FirstBootHook))
			}
			Expect(memLog.String()).To(ContainSubstring("Running first-boot hook"))
			Expect(memLog.String()).To(ContainSubstring("Finish first-boot hook"))
		})

		It("logs a cloud-init failure in strict mode instead of failing the hook", func() {
			cloudInit.Error = true
			cfg.Strict = true
			stage := hook.FirstBootStage{}
			err = stage.Run(*cfg, nil)
			Expect(err).Should(BeNil())
			// Swallowing the error is what keeps
			// machine.CreateSentinel("firstboot") reachable in agent.Run (not
			// exercised by this suite), so the node is not stuck re-running the
			// first boot block. The log line is the operator's only trace.
			Expect(memLog.String()).To(ContainSubstring("continuing so the firstboot sentinel still gets written"))
			Expect(memLog.String()).To(ContainSubstring("cloud init failure"))
			Expect(memLog.String()).To(ContainSubstring("Finish first-boot hook"))
		})

		It("does not fail the hook when strict mode is off, even if the runner errors", func() {
			cloudInit.Error = true
			cfg.Strict = false
			stage := hook.FirstBootStage{}
			err = stage.Run(*cfg, nil)
			Expect(err).Should(BeNil())
			// Non-strict runs never reach the hook's own error branch: RunStage
			// absorbs the failure itself and hands back nil.
			Expect(memLog.String()).ToNot(ContainSubstring("continuing so the firstboot sentinel still gets written"))
			Expect(memLog.String()).To(ContainSubstring("Finish first-boot hook"))
		})

		It("keeps the FirstBoot hook chain alive when the stage fails in strict mode", func() {
			// This is the actual failure mode from the bug report: agent.Run
			// walks hook.FirstBoot as a single chain and only reaches
			// machine.CreateSentinel("firstboot") if that chain returns nil.
			// The specs above only exercise FirstBootStage in isolation, so
			// they can't tell us whether hook.Run -- which returns early on
			// the first error, see hook.go -- still makes it past this stage
			// when it's last in line and the node is strict. Drive the real
			// chain to be sure nothing upstream of FirstBootStage regresses
			// that guarantee.
			cloudInit.Error = true
			cfg.Strict = true
			err = hook.Run(*cfg, nil, hook.FirstBoot...)
			Expect(err).Should(BeNil())
			Expect(cloudInit.ExecStages).To(ContainElement(cnst.FirstBootHook))
			Expect(memLog.String()).To(ContainSubstring("continuing so the firstboot sentinel still gets written"))
		})

		It("runs last in the FirstBoot hook list, after bundles and grub options", func() {
			Expect(hook.FirstBoot).To(HaveLen(3))
			Expect(hook.FirstBoot[0]).To(BeAssignableToTypeOf(&hook.BundleFirstBoot{}))
			Expect(hook.FirstBoot[1]).To(BeAssignableToTypeOf(&hook.GrubFirstBootOptions{}))
			Expect(hook.FirstBoot[2]).To(BeAssignableToTypeOf(&hook.FirstBootStage{}))
		})

		It("names the stage first-boot", func() {
			Expect(cnst.FirstBootHook).To(Equal("first-boot"))
		})
	})

	Context("OEMFiles", func() {
		var installSpec *implSpec.InstallSpec

		BeforeEach(func() {
			runner = v1mock.NewFakeRunner()
			syscallMock = &v1mock.FakeSyscall{}
			mounter = v1mock.NewErrorMounter()
			client = &v1mock.FakeHTTPClient{}
			memLog = &bytes.Buffer{}
			logger = sdkLogger.NewBufferLogger(memLog)
			logger.SetLevel("debug")
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
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
			)
			cfg.Collector = collector.Config{}
			cfg.Install = &sdkInstall.Install{}

			// The state the installer is in when it runs the PostInstall
			// hooks: every partition of the spec is mounted, OEM included.
			installSpec = &implSpec.InstallSpec{
				Partitions: sdkPartitions.ElementalPartitions{
					OEM: &sdkPartitions.Partition{
						FilesystemLabel: cnst.OEMLabel,
						Path:            "/dev/device1",
						MountPoint:      cnst.OEMDir,
					},
				},
			}
			err = fsutils.MkdirAll(fs, cnst.OEMDir, os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			err = mounter.Mount("/dev/device1", cnst.OEMDir, "auto", []string{})
			Expect(err).Should(BeNil())
		})
		AfterEach(func() {
			cleanup()
		})

		It("does nothing when install.oem_files is empty", func() {
			oemFiles := hook.OEMFiles{}
			err = oemFiles.Run(*cfg, installSpec)
			Expect(err).Should(BeNil())
		})

		It("writes the configured files into the mounted OEM partition", func() {
			cfg.Install.OEMFiles = []sdkInstall.OEMFile{
				{Name: "foo", Content: "#cloud-config\nfoo: bar\n"},
				{Name: "bar.yaml", Content: "#cloud-config\nbar: baz\n"},
			}

			oemFiles := hook.OEMFiles{}
			err = oemFiles.Run(*cfg, installSpec)
			Expect(err).Should(BeNil())

			content, err := fs.ReadFile(filepath.Join(cnst.OEMDir, "foo.yaml"))
			Expect(err).Should(BeNil())
			Expect(string(content)).Should(Equal("#cloud-config\nfoo: bar\n"))

			info, err := fs.Stat(filepath.Join(cnst.OEMDir, "foo.yaml"))
			Expect(err).Should(BeNil())
			Expect(info.Mode().Perm()).Should(Equal(os.FileMode(0400)))

			content, err = fs.ReadFile(filepath.Join(cnst.OEMDir, "bar.yaml"))
			Expect(err).Should(BeNil())
			Expect(string(content)).Should(Equal("#cloud-config\nbar: baz\n"))
		})

		It("errors instead of writing anywhere else when OEM is not mounted", func() {
			err = mounter.Unmount(cnst.OEMDir)
			Expect(err).Should(BeNil())
			err = fsutils.MkdirAll(fs, "/usr/local/cloud-config", os.ModeDir|os.ModePerm)
			Expect(err).Should(BeNil())
			cfg.Install.OEMFiles = []sdkInstall.OEMFile{{Name: "foo", Content: "hello"}}

			oemFiles := hook.OEMFiles{}
			err = oemFiles.Run(*cfg, installSpec)
			Expect(err).ShouldNot(BeNil())

			_, err = fs.Stat(filepath.Join(cnst.OEMDir, "foo.yaml"))
			Expect(err).ShouldNot(BeNil())
			_, err = fs.Stat("/usr/local/cloud-config/foo.yaml")
			Expect(err).ShouldNot(BeNil())
		})

		It("errors when the spec has no OEM partition to write to", func() {
			cfg.Install.OEMFiles = []sdkInstall.OEMFile{{Name: "foo", Content: "hello"}}

			oemFiles := hook.OEMFiles{}
			err = oemFiles.Run(*cfg, &implSpec.InstallSpec{})
			Expect(err).ShouldNot(BeNil())
		})

		It("rejects a bad name before writing anything, leaving earlier entries unwritten", func() {
			cfg.Install.OEMFiles = []sdkInstall.OEMFile{
				{Name: "good", Content: "hello"},
				{Name: "../evil", Content: "hello"},
			}

			oemFiles := hook.OEMFiles{}
			err = oemFiles.Run(*cfg, installSpec)
			Expect(err).ShouldNot(BeNil())

			_, err = fs.Stat(filepath.Join(cnst.OEMDir, "good.yaml"))
			Expect(err).ShouldNot(BeNil())
		})
	})
})
