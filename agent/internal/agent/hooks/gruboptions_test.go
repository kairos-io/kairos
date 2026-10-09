package hook_test

import (
	"bytes"
	"os"
	"path/filepath"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	install "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("SelinuxGrubOpts", func() {
	DescribeTable(
		"builds the dedicated grubenv var pair",
		func(selinux install.SelinuxOptions, expected map[string]string) {
			Expect(hook.SelinuxGrubOpts(selinux)).To(Equal(expected))
		},
		Entry("field off (zero value) writes nothing",
			install.SelinuxOptions{},
			nil),
		Entry("enabled with no mode defaults to permissive",
			install.SelinuxOptions{Enabled: true},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "permissive",
			}),
		Entry("enabled with explicit permissive mode",
			install.SelinuxOptions{
				Enabled: true,
				Mode:    "permissive",
			},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "permissive",
			}),
		Entry("enabled with enforcing mode",
			install.SelinuxOptions{
				Enabled: true,
				Mode:    "enforcing",
			},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "enforcing",
			}),
	)
})

var _ = Describe("GrubFirstBootOptions", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var cleanup func()
	var memLog *bytes.Buffer
	var grubenv string

	// newConfig builds a first-boot config whose /proc/cmdline holds cmdline,
	// with a top-level grub_options key set and /oem already there, so the
	// hook's only remaining decision is whether to write the grubenv.
	newConfig := func(cmdline string) {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": cmdline})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fsutils.MkdirAll(fs, cnst.OEMPath, os.ModeDir|os.ModePerm)).To(Succeed())

		memLog = &bytes.Buffer{}
		log := sdkLogger.NewBufferLogger(memLog)
		log.SetLevel("debug")
		cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(log))
		cfg.Collector = collector.Config{}
		cfg.GrubOptions = map[string]string{"extra_cmdline": "console=ttyS0"}
	}

	BeforeEach(func() {
		grubenv = filepath.Join(cnst.OEMPath, cnst.GrubEnv)
	})

	AfterEach(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	// The bug: GrubPostInstallOptions returns early under UKI but this hook
	// did not, so a UKI node with a top-level grub_options key wrote a
	// grubenv that nothing on a UKI boot reads, and logged success for it.
	It("writes no grubenv on a UKI boot, and says why", func() {
		newConfig("rd.immucore.uki root=LABEL=COS_STATE")

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		_, err := fs.Stat(grubenv)
		Expect(os.IsNotExist(err)).To(BeTrue(), "a UKI boot must not get a grubenv written under /oem")
		Expect(memLog.String()).To(ContainSubstring("Skipping GrubFirstBootOptions hook in uki mode"))
		Expect(memLog.String()).ToNot(ContainSubstring("Successfully set grub options in OEM"))
	})

	It("still writes the grubenv on a grub boot", func() {
		newConfig("root=LABEL=COS_STATE")

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		content, err := fs.ReadFile(grubenv)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("extra_cmdline=console=ttyS0"))
		Expect(memLog.String()).To(ContainSubstring("Successfully set grub options in OEM"))
	})

	// Without a grub_options key there is nothing to apply either way, so the
	// UKI warning must not fire and nag a user who set nothing.
	It("stays quiet when no grub options are set", func() {
		newConfig("rd.immucore.uki root=LABEL=COS_STATE")
		cfg.GrubOptions = nil

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		Expect(memLog.String()).ToNot(ContainSubstring("uki mode"))
	})
})

var _ = Describe("GrubPostInstallOptions", func() {
	// Its sibling reads the same cmdline through the same seam, so the two
	// hooks cannot drift apart again on what counts as a UKI boot.
	It("skips a UKI boot read through the config filesystem", func() {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": "rd.immucore.uki"})
		Expect(err).ShouldNot(HaveOccurred())
		defer cleanup()
		Expect(fsutils.MkdirAll(fs, cnst.OEMPath, os.ModeDir|os.ModePerm)).To(Succeed())

		memLog := &bytes.Buffer{}
		log := sdkLogger.NewBufferLogger(memLog)
		log.SetLevel("debug")
		cfg := config.NewConfig(config.WithFs(fs), config.WithLogger(log))
		cfg.Collector = collector.Config{}
		cfg.Install.GrubOptions = map[string]string{"extra_cmdline": "console=ttyS0"}

		Expect(hook.GrubPostInstallOptions{}.Run(*cfg, nil)).To(Succeed())

		_, err = fs.Stat(filepath.Join(cnst.OEMPath, cnst.GrubEnv))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Expect(memLog.String()).To(ContainSubstring("Skipping GrubPostInstallOptions hook in uki mode"))
	})
})
