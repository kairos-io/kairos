package uki

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("catalog extensions", func() {
	It("rejects extension requests for non-ISO output", func() {
		opts := validTestOptions()
		opts.Extensions = []extensions.Request{{Name: "tool"}}
		opts.ExtensionsCatalogs = []string{"catalog.yaml"}
		Expect(opts.validate()).To(MatchError("extensions are only supported for iso artifacts"))
	})

	It("accepts extensions with no catalog, which reads the default one", func() {
		opts := validTestOptions()
		opts.OutputType = "iso"
		opts.Extensions = []extensions.Request{{Name: "tool"}}
		Expect(opts.validate()).To(Succeed())
	})

	It("passes an empty catalog list through, so the default one is what is read", func() {
		original := materializeExtensions
		DeferCleanup(func() { materializeExtensions = original })
		var got []string
		called := false
		materializeExtensions = func(_ context.Context, catalogs []string, _ []extensions.Request, _, _ string, _ bool) ([]string, error) {
			called = true
			got = catalogs
			return nil, nil
		}
		Expect(stageExtensions(context.Background(), nil, []extensions.Request{{Name: "tool"}}, "amd64", tTempDir(), false)).To(Succeed())
		Expect(called).To(BeTrue())
		Expect(got).To(BeEmpty())
	})

	It("materializes extensions into the staged ISO root with the target architecture", func() {
		original := materializeExtensions
		DeferCleanup(func() { materializeExtensions = original })
		stagedRoot := tTempDir()
		called := false
		materializeExtensions = func(_ context.Context, catalogs []string, requests []extensions.Request, arch, destination string, insecure bool) ([]string, error) {
			called = true
			Expect(catalogs).To(Equal([]string{"catalog.yaml"}))
			Expect(requests).To(Equal([]extensions.Request{{Name: "tool", Version: "v2"}}))
			Expect(arch).To(Equal("arm64"))
			Expect(destination).To(Equal(stagedRoot))
			Expect(insecure).To(BeTrue())
			return []string{filepath.Join(destination, "tool.sysext.raw")}, nil
		}

		Expect(stageExtensions(context.Background(), []string{"catalog.yaml"}, []extensions.Request{{Name: "tool", Version: "v2"}}, "arm64", stagedRoot, true)).To(Succeed())
		Expect(called).To(BeTrue())
	})
})

func validTestOptions() Options {
	dir := tTempDir()
	for _, name := range []string{"sb.key", "sb.pem", "pcr.key"} {
		Expect(os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600)).To(Succeed())
	}
	return Options{
		Source:           "dir:rootfs",
		SBKey:            filepath.Join(dir, "sb.key"),
		SBCert:           filepath.Join(dir, "sb.pem"),
		TPMPCRPrivateKey: filepath.Join(dir, "pcr.key"),
	}
}

func tTempDir() string {
	dir, err := os.MkdirTemp("", "uki-extension-test-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

var _ = Describe("sumFileSizes", func() {
	var tempDir string
	var err error

	BeforeEach(func() {
		tempDir, err = os.MkdirTemp("", "sumFileSizes-test-")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	It("should account for filesystem overhead", func() {
		// Create a file that is 1 MB (1048576 bytes)
		file1 := filepath.Join(tempDir, "file1")
		err := os.WriteFile(file1, make([]byte, 1048576), 0o644)
		Expect(err).ToNot(HaveOccurred())

		filesMap := map[string][]string{
			"dir1": {file1},
		}

		sizeMB, err := sumFileSizes(filesMap)
		Expect(err).ToNot(HaveOccurred())
		// Should be more than 1 MB due to filesystem overhead
		Expect(sizeMB).To(BeNumerically(">", int64(1)))
	})

	It("should handle larger files with overhead", func() {
		// Create a file that is exactly 5 MB (5242880 bytes)
		file1 := filepath.Join(tempDir, "file1")
		err := os.WriteFile(file1, make([]byte, 5*1024*1024), 0o644)
		Expect(err).ToNot(HaveOccurred())

		filesMap := map[string][]string{
			"dir1": {file1},
		}

		sizeMB, err := sumFileSizes(filesMap)
		Expect(err).ToNot(HaveOccurred())
		// Should be more than 5 MB due to filesystem overhead
		Expect(sizeMB).To(BeNumerically(">", int64(5)))
	})

	It("should sum multiple files with overhead", func() {
		// Create file1: 1.5 MB
		file1 := filepath.Join(tempDir, "file1")
		err := os.WriteFile(file1, make([]byte, 1536*1024), 0o644) // 1.5 MB
		Expect(err).ToNot(HaveOccurred())

		// Create file2: 2.25 MB
		file2 := filepath.Join(tempDir, "file2")
		err = os.WriteFile(file2, make([]byte, 2355200), 0o644) // ~2.25 MB
		Expect(err).ToNot(HaveOccurred())

		filesMap := map[string][]string{
			"dir1": {file1},
			"dir2": {file2},
		}

		sizeMB, err := sumFileSizes(filesMap)
		Expect(err).ToNot(HaveOccurred())
		// Total: ~3.75 MB + overhead, should be at least 4 MB
		Expect(sizeMB).To(BeNumerically(">=", int64(4)))
	})

	It("should handle fractional megabytes with overhead", func() {
		// Create a file that is 50.5 MB (52953088 bytes)
		file1 := filepath.Join(tempDir, "file1")
		err := os.WriteFile(file1, make([]byte, 52953088), 0o644)
		Expect(err).ToNot(HaveOccurred())

		filesMap := map[string][]string{
			"dir1": {file1},
		}

		sizeMB, err := sumFileSizes(filesMap)
		Expect(err).ToNot(HaveOccurred())
		// Should be more than 50.5 MB due to filesystem overhead
		Expect(sizeMB).To(BeNumerically(">=", int64(51)))
	})

	It("should return error for non-existent file", func() {
		filesMap := map[string][]string{
			"dir1": {"/nonexistent/file"},
		}

		_, err := sumFileSizes(filesMap)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("finding file info"))
	})
})

var _ = Describe("absolutizePaths", func() {
	It("rewrites relative key/cert/splash paths to absolute", func() {
		opts := &Options{
			TPMPCRPrivateKey: "data/keys/production/tpm2-pcr-private.pem",
			SBKey:            "data/keys/db.key",
			SBCert:           "data/keys/db.pem",
			PublicKeysDir:    "data/keys",
			Splash:           "data/splash.bmp",
		}
		Expect(absolutizePaths(opts)).To(Succeed())

		Expect(filepath.IsAbs(opts.TPMPCRPrivateKey)).To(BeTrue())
		Expect(filepath.IsAbs(opts.SBKey)).To(BeTrue())
		Expect(filepath.IsAbs(opts.SBCert)).To(BeTrue())
		Expect(filepath.IsAbs(opts.PublicKeysDir)).To(BeTrue())
		Expect(filepath.IsAbs(opts.Splash)).To(BeTrue())
		Expect(opts.TPMPCRPrivateKey).To(HaveSuffix("/data/keys/production/tpm2-pcr-private.pem"))
	})

	It("rewrites relative overlay and output paths to absolute", func() {
		opts := &Options{
			OverlayRootfs: "data/overlay-rootfs",
			OverlayISO:    "data/overlay-iso",
			OutputDir:     "build/out",
		}
		Expect(absolutizePaths(opts)).To(Succeed())

		Expect(filepath.IsAbs(opts.OverlayRootfs)).To(BeTrue())
		Expect(filepath.IsAbs(opts.OverlayISO)).To(BeTrue())
		Expect(filepath.IsAbs(opts.OutputDir)).To(BeTrue())
		Expect(opts.OutputDir).To(HaveSuffix("/build/out"))
	})

	It("leaves empty values and pkcs11 URIs untouched", func() {
		opts := &Options{
			SBKey:            "pkcs11:token=mytoken;object=mykey",
			TPMPCRPrivateKey: "",
			OverlayISO:       "",
			OutputDir:        "",
		}
		Expect(absolutizePaths(opts)).To(Succeed())
		Expect(opts.SBKey).To(Equal("pkcs11:token=mytoken;object=mykey"))
		Expect(opts.TPMPCRPrivateKey).To(BeEmpty())
		Expect(opts.OverlayISO).To(BeEmpty())
		Expect(opts.OutputDir).To(BeEmpty())
	})

	It("leaves already-absolute paths unchanged", func() {
		opts := &Options{TPMPCRPrivateKey: "/data/keys/production/tpm2-pcr-private.pem"}
		Expect(absolutizePaths(opts)).To(Succeed())
		Expect(opts.TPMPCRPrivateKey).To(Equal("/data/keys/production/tpm2-pcr-private.pem"))
	})
})

var _ = Describe("parseSelinuxOptions", func() {
	var log *logger.KairosLogger

	BeforeEach(func() {
		l := logger.NewKairosLogger("uki-test", "warn", false)
		log = &l
	})

	It("returns disabled for an empty cloud config", func() {
		enabled, mode, err := parseSelinuxOptions(log, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeFalse())
		Expect(mode).To(BeEmpty())
	})

	It("returns disabled when install.selinux is absent", func() {
		cc := "#cloud-config\nusers:\n  - name: kairos\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeFalse())
		Expect(mode).To(BeEmpty())
	})

	It("returns disabled when install.selinux.enabled is false", func() {
		cc := "install:\n  selinux:\n    enabled: false\n    mode: enforcing\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeFalse())
		Expect(mode).To(BeEmpty())
	})

	It("returns enabled with the given mode", func() {
		cc := "install:\n  selinux:\n    enabled: true\n    mode: enforcing\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(mode).To(Equal("enforcing"))
	})

	It("defaults to permissive when mode is missing", func() {
		cc := "install:\n  selinux:\n    enabled: true\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(mode).To(Equal("permissive"))
	})

	It("falls back to permissive for an invalid mode", func() {
		cc := "install:\n  selinux:\n    enabled: true\n    mode: bogus\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(mode).To(Equal("permissive"))
	})

	It("picks up install.selinux from a later document in a multi-doc config", func() {
		cc := "#cloud-config\nusers:\n  - name: kairos\n---\ninstall:\n  selinux:\n    enabled: true\n    mode: enforcing\n"
		enabled, mode, err := parseSelinuxOptions(log, cc)
		Expect(err).NotTo(HaveOccurred())
		Expect(enabled).To(BeTrue())
		Expect(mode).To(Equal("enforcing"))
	})

	It("returns an error for malformed YAML", func() {
		_, _, err := parseSelinuxOptions(log, "install: [unclosed\n  bad: yaml:\n")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("spliceSelinuxCmdline", func() {
	It("leaves the base alone when SELinux is not enabled", func() {
		got, err := spliceSelinuxCmdline(constants.UkiCmdline, false, "enforcing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(constants.UkiCmdline))
	})

	It("splices the fragment in when SELinux is enabled", func() {
		got, err := spliceSelinuxCmdline(constants.UkiCmdline, true, "enforcing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ContainSubstring("security=selinux selinux=1 enforcing=0 rd.cos.selinux=enforcing"))
		Expect(got).NotTo(ContainSubstring(" selinux=0"))
	})

	It("errors when the base carries no selinux=0 to replace", func() {
		_, err := spliceSelinuxCmdline("console=tty1 panic=5", true, "permissive")
		Expect(err).To(MatchError(ContainSubstring("requires the base cmdline to contain")))
	})

	It("errors rather than splicing twice into an already patched base", func() {
		patched, err := spliceSelinuxCmdline(constants.UkiCmdline, true, "permissive")
		Expect(err).NotTo(HaveOccurred())

		_, err = spliceSelinuxCmdline(patched, true, "permissive")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("selinuxBaseCmdline", func() {
	var rootfs string
	var log *logger.KairosLogger

	const enabledCC = "install:\n  selinux:\n    enabled: true\n    mode: enforcing\n"

	writeFamily := func(family string) {
		Expect(os.WriteFile(filepath.Join(rootfs, "etc/kairos-release"),
			[]byte("KAIROS_FAMILY=\""+family+"\"\n"), 0o644)).To(Succeed())
	}

	BeforeEach(func() {
		rootfs = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootfs, "etc"), 0o755)).To(Succeed())
		l := logger.NewKairosLogger("uki-test", "warn", false)
		log = &l
	})

	It("enables SELinux on a family that supports it", func() {
		writeFamily("redhat")

		got, err := selinuxBaseCmdline(log, rootfs, enabledCC)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(ContainSubstring("security=selinux selinux=1 enforcing=0 rd.cos.selinux=enforcing"))
		Expect(got).NotTo(ContainSubstring(" selinux=0"))
	})

	It("keeps selinux=0 on a family that does not support it, as the GRUB path does", func() {
		// The build used to warn about the family and then splice the
		// fragment in anyway, so a debian-family artifact booted with
		// security=selinux and no policy to go with it.
		writeFamily("debian")

		got, err := selinuxBaseCmdline(log, rootfs, enabledCC)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(constants.UkiCmdline))
		Expect(got).To(ContainSubstring(" selinux=0"))
		Expect(got).NotTo(ContainSubstring("security=selinux"))
		Expect(got).NotTo(ContainSubstring("rd.cos.selinux"))
	})

	It("keeps selinux=0 when the family cannot be read at all", func() {
		got, err := selinuxBaseCmdline(log, rootfs, enabledCC)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(constants.UkiCmdline))
	})

	It("keeps selinux=0 for a supported family the cloud-config does not ask about", func() {
		writeFamily("redhat")

		got, err := selinuxBaseCmdline(log, rootfs, "#cloud-config\nusers:\n  - name: kairos\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(constants.UkiCmdline))
	})

	It("keeps selinux=0 with no cloud-config at all", func() {
		writeFamily("redhat")

		got, err := selinuxBaseCmdline(log, rootfs, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(constants.UkiCmdline))
	})

	It("fails the build on a malformed cloud-config", func() {
		writeFamily("redhat")

		_, err := selinuxBaseCmdline(log, rootfs, "install: [unclosed\n  bad: yaml:\n")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("UKI cmdlines with SELinux base", func() {
	var selinuxBase string

	BeforeEach(func() {
		selinuxBase = strings.Replace(constants.UkiCmdline, " selinux=0", " security=selinux selinux=1 enforcing=0 rd.cos.selinux=permissive", 1)
		Expect(selinuxBase).NotTo(Equal(constants.UkiCmdline))
	})

	It("puts the enabled fragment in every entry and never selinux=0", func() {
		entries := getUkiCmdline(selinuxBase, "role=agent", "Kairos", []string{}, false)
		// extend mode → single entry
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Cmdline).To(ContainSubstring("security=selinux selinux=1 enforcing=0 rd.cos.selinux=permissive"))
		Expect(entries[0].Cmdline).To(ContainSubstring("role=agent"))
		Expect(entries[0].Cmdline).ToNot(ContainSubstring("selinux=0"))
	})

	It("adds one entry per extra cmdline with the patched base", func() {
		entries := getUkiCmdline(selinuxBase, "", "Kairos", []string{"role=agent", "role=backup"}, false)
		Expect(entries).To(HaveLen(3))
		for _, e := range entries {
			Expect(e.Cmdline).To(ContainSubstring("rd.cos.selinux=permissive"))
			Expect(e.Cmdline).ToNot(ContainSubstring("selinux=0"))
		}
	})

	It("keeps unpatched base byte-identical to today (regression)", func() {
		entries := GetUkiCmdline("", "Kairos", []string{}, false)
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Cmdline).To(Equal(constants.UkiCmdline))
	})

	It("single-efi entries carry the patched base", func() {
		l := logger.NewKairosLogger("uki-test", "warn", false)
		entries := getUkiSingleCmdlines(selinuxBase, "Kairos", []string{"My Entry: quiet"}, l)
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Title).To(Equal("Kairos (My Entry)"))
		Expect(entries[0].Cmdline).To(ContainSubstring("rd.cos.selinux=permissive"))
		Expect(entries[0].Cmdline).To(ContainSubstring("quiet"))
		Expect(entries[0].Cmdline).ToNot(ContainSubstring("selinux=0"))
	})

	It("EFI names stay short when the base is patched", func() {
		name := nameFromCmdline(selinuxBase, constants.ArtifactBaseName, selinuxBase+" quiet")
		Expect(name).ToNot(ContainSubstring("selinux"))
		Expect(name).To(HavePrefix("norole_"))
		Expect(nameFromCmdline(selinuxBase, constants.ArtifactBaseName, selinuxBase)).To(Equal(constants.ArtifactBaseName))
	})
})

var _ = Describe("isSelinuxSupported", func() {
	var rootfs string
	var log *logger.KairosLogger

	BeforeEach(func() {
		var err error
		rootfs, err = os.MkdirTemp("", "isSelinuxSupported-test-")
		Expect(err).ToNot(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(rootfs, "etc"), 0o755)).To(Succeed())
		l := logger.NewKairosLogger("uki-test", "warn", false)
		log = &l
	})

	AfterEach(func() {
		os.RemoveAll(rootfs)
	})

	DescribeTable(
		"reports support by family, matching the GRUB gate",
		func(kairosRelease, osRelease string, want bool) {
			if kairosRelease != "" {
				Expect(os.WriteFile(filepath.Join(rootfs, "etc/kairos-release"), []byte(kairosRelease), 0o644)).To(Succeed())
			}
			if osRelease != "" {
				Expect(os.WriteFile(filepath.Join(rootfs, "etc/os-release"), []byte(osRelease), 0o644)).To(Succeed())
			}
			ok, _ := isSelinuxSupported(rootfs, log)
			Expect(ok).To(Equal(want))
		},
		Entry("fedora flavor, redhat family",
			`KAIROS_FAMILY="redhat"
KAIROS_FLAVOR="fedora"
`, "", true),
		Entry("rocky flavor, redhat family",
			`KAIROS_FAMILY="redhat"
KAIROS_FLAVOR="rockylinux"
`, "", true),
		Entry("capitalized family",
			`KAIROS_FAMILY="RedHat"
`, "", true),
		Entry("openSUSE, suse family",
			`KAIROS_FAMILY="suse"
KAIROS_FLAVOR="opensuse"
`, "", true),
		Entry("ubuntu, debian family(ships apparmor, not selinux)",
			`KAIROS_FAMILY="debian"
KAIROS_FLAVOR="ubuntu"
`, "", false),
		Entry("hadron kairos-release",
			`KAIROS_FAMILY="hadron"
KAIROS_FLAVOR="hadron"
`, "", false),
		Entry("bare legacy FAMILY key without the KAIROS_ prefix",
			`FAMILY="redhat"
KAIROS_FLAVOR="fedora"
`, "", true),
		Entry("kairos-release wins over a hostile rootfs os-release",
			`KAIROS_FAMILY="redhat"
`, `KAIROS_FAMILY="debian"
ID=debian
`, true),
		Entry("no family in kairos-release: no os-release fallback",
			`KAIROS_FLAVOR="fedora"
`, "", false),
		Entry("no family anywhere",
			`KAIROS_FLAVOR="fedora"
`, `ID=rocky
`, false),
	)
})

var _ = Describe("single-efi-cmdline file names", func() {
	It("rejects titles that only differ by case", func() {
		opts := validTestOptions()
		opts.SingleEfiCmdlines = []string{"Debug: rd.debug", "debug: rd.shell"}
		Expect(opts.validate()).To(MatchError(`uki.Build: boot entries "Kairos (Debug)" and "Kairos (debug)" both produce debug.efi, use distinct titles`))
	})

	It("rejects a title that maps onto the default entry", func() {
		opts := validTestOptions()
		opts.SingleEfiCmdlines = []string{"Norole: rd.debug"}
		Expect(opts.validate()).To(MatchError(`uki.Build: boot entries "Kairos" and "Kairos (Norole)" both produce norole.efi, use distinct titles`))
	})

	It("accepts distinct titles", func() {
		opts := validTestOptions()
		opts.SingleEfiCmdlines = []string{"Debug: rd.debug", "Shell: rd.shell"}
		Expect(opts.validate()).To(Succeed())
	})
})

var _ = Describe("single-efi-cmdline titles", func() {
	DescribeTable(
		"only treats the text before the first colon as a title when it looks like one",
		func(value, wantTitle, wantExtra, wantFileName string) {
			entries := getUkiSingleCmdlines("base", "Kairos", []string{value}, logger.KairosLogger{})
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Title).To(Equal(wantTitle))
			Expect(entries[0].Cmdline).To(Equal("base " + wantExtra))
			Expect(entries[0].FileName).To(Equal(wantFileName))
		},
		Entry("titled", "My Entry: quiet", "Kairos (My Entry)", " quiet", "my_entry"),
		Entry("untitled plain", "quiet", "Kairos", "quiet", "single_entry_quiet"),
		Entry("untitled with colons and '='",
			"ip=10.0.0.2::10.0.0.1:255.255.255.0::eth0:none",
			"Kairos",
			"ip=10.0.0.2::10.0.0.1:255.255.255.0::eth0:none",
			"single_entry_ip_10.0.0.2_10.0.0.1_255.255.255.0_eth0_none"),
		Entry("slash before the colon", "a/b: quiet", "Kairos", "a/b: quiet", "single_entry_a_b_quiet"),
	)
})

var _ = Describe("writeISOCloudConfig", func() {
	It("writes the cloud-config to config.yaml at the ISO root", func() {
		isoDir := tTempDir()
		cc := "#cloud-config\ninstall:\n  auto: true\n"
		Expect(writeISOCloudConfig(isoDir, cc)).To(Succeed())
		got, err := os.ReadFile(filepath.Join(isoDir, "config.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(got)).To(Equal(cc))
	})

	It("writes nothing when there is no cloud-config", func() {
		isoDir := tTempDir()
		Expect(writeISOCloudConfig(isoDir, "  \n")).To(Succeed())
		_, err := os.Stat(filepath.Join(isoDir, "config.yaml"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})

var _ = Describe("host binaries", func() {
	DescribeTable("lists only the binaries the output type runs",
		func(outputType string, expected []string) {
			Expect(hostBinaries(outputType)).To(Equal(expected))
		},
		Entry("iso", string(constants.IsoOutput), []string{"dd", "mformat", "mmd", "mcopy", "xorriso"}),
		Entry("uki", string(constants.DefaultOutput), nil),
		Entry("container", string(constants.ContainerOutput), nil),
	)

	It("fails the iso dependency check when a binary is missing", func() {
		GinkgoT().Setenv("PATH", tTempDir())
		Expect(checkBuildUKIDeps("amd64", string(constants.IsoOutput))).To(MatchError(ContainSubstring("executable file not found")))
	})
})
