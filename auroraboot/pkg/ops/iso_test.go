package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	sdkutils "github.com/kairos-io/kairos/v4/sdk/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
	"gopkg.in/yaml.v3"
)

var _ = Describe("materializeISOExtensions", func() {
	It("does nothing when no extensions are configured", func() {
		called := false
		materializeExtensionArtifacts = func(context.Context, []string, []extensions.Request, string, string, bool) ([]string, error) {
			called = true
			return nil, nil
		}
		DeferCleanup(func() { materializeExtensionArtifacts = extensions.Materialize })

		Expect(materializeISOExtensions(context.Background(), schema.ISO{}, "amd64", "/unused", false)).To(Succeed())
		Expect(called).To(BeFalse())
	})

	It("uses the target architecture and ISO root directory", func() {
		tmp := GinkgoT().TempDir()
		requests := []extensions.Request{{Name: "foo", Version: "v1"}}
		materializeExtensionArtifacts = func(_ context.Context, catalogs []string, got []extensions.Request, arch, destination string, insecure bool) ([]string, error) {
			Expect(catalogs).To(Equal([]string{"catalog.yaml"}))
			Expect(got).To(Equal(requests))
			Expect(arch).To(Equal("arm64"))
			Expect(destination).To(Equal(tmp))
			Expect(insecure).To(BeTrue())
			path := filepath.Join(destination, "foo.sysext.raw")
			return []string{path}, os.WriteFile(path, []byte("raw"), 0o644)
		}
		DeferCleanup(func() { materializeExtensionArtifacts = extensions.Materialize })

		iso := schema.ISO{ExtensionsCatalogs: []string{"catalog.yaml"}, Extensions: requests}
		Expect(materializeISOExtensions(context.Background(), iso, "arm64", tmp, true)).To(Succeed())
		Expect(filepath.Join(tmp, "foo.sysext.raw")).To(BeAnExistingFile())
	})

	// Regression: a classic (non-UKI) install ignores a raw image that only
	// sits on the live media. The installer stages only what install.extensions
	// declares, so without this declaration the node boots with no extension.
	It("declares every materialized extension under install.extensions", func() {
		tmp := GinkgoT().TempDir()
		materializeExtensionArtifacts = func(_ context.Context, _ []string, _ []extensions.Request, _, destination string, _ bool) ([]string, error) {
			return []string{
				filepath.Join(destination, "tailscale.sysext.raw"),
				filepath.Join(destination, "drbd.sysext.raw"),
			}, nil
		}
		DeferCleanup(func() { materializeExtensionArtifacts = extensions.Materialize })

		iso := schema.ISO{Extensions: []extensions.Request{{Name: "tailscale"}, {Name: "drbd"}}}
		Expect(materializeISOExtensions(context.Background(), iso, "amd64", tmp, false)).To(Succeed())

		declaration, err := os.ReadFile(filepath.Join(tmp, isoExtensionsConfig))
		Expect(err).ToNot(HaveOccurred())
		// The collector skips a config without the header, which would drop
		// the declaration without a word.
		Expect(collector.HasValidHeader(string(declaration))).To(BeTrue())

		var parsed struct {
			Install struct {
				Extensions extensiontypes.Extensions `yaml:"extensions"`
			} `yaml:"install"`
		}
		Expect(yaml.Unmarshal(declaration, &parsed)).To(Succeed())
		Expect(parsed.Install.Extensions).To(Equal(extensiontypes.Extensions{
			{Name: "/run/initramfs/live/tailscale.sysext.raw"},
			{Name: "/run/initramfs/live/drbd.sysext.raw"},
		}))
	})

	// What the installer sees is the merge of every config in the live media
	// root, so check the declaration through the agent's own collector: it
	// has to add to what the user declared in config.yaml, not replace it.
	It("adds to the install.extensions of the user's config.yaml", func() {
		tmp := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(tmp, "config.yaml"),
			[]byte("#cloud-config\ninstall:\n  auto: true\n  extensions:\n    - name: fwupd\n"), 0o644)).To(Succeed())
		materializeExtensionArtifacts = func(_ context.Context, _ []string, _ []extensions.Request, _, destination string, _ bool) ([]string, error) {
			return []string{filepath.Join(destination, "tailscale.sysext.raw")}, nil
		}
		DeferCleanup(func() { materializeExtensionArtifacts = extensions.Materialize })

		iso := schema.ISO{Extensions: []extensions.Request{{Name: "tailscale"}}}
		Expect(materializeISOExtensions(context.Background(), iso, "amd64", tmp, false)).To(Succeed())

		merged, err := collector.Scan(&collector.Options{ScanDir: []string{tmp}, NoLogs: true}, nil)
		Expect(err).ToNot(HaveOccurred())
		rendered, err := merged.String()
		Expect(err).ToNot(HaveOccurred())
		var parsed struct {
			Install struct {
				Auto       bool                      `yaml:"auto"`
				Extensions extensiontypes.Extensions `yaml:"extensions"`
			} `yaml:"install"`
		}
		Expect(yaml.Unmarshal([]byte(rendered), &parsed)).To(Succeed())
		Expect(parsed.Install.Auto).To(BeTrue())
		Expect(parsed.Install.Extensions).To(ConsistOf(
			extensiontypes.Extension{Name: "fwupd"},
			extensiontypes.Extension{Name: "/run/initramfs/live/tailscale.sysext.raw"},
		))
	})

	It("writes no declaration when no extensions are configured", func() {
		tmp := GinkgoT().TempDir()
		Expect(materializeISOExtensions(context.Background(), schema.ISO{}, "amd64", tmp, false)).To(Succeed())
		Expect(filepath.Join(tmp, isoExtensionsConfig)).ToNot(BeAnExistingFile())
	})
})

var _ = Describe("applyGrubTemplate", Label("iso"), func() {
	const templateWithPlaceholders = "linux ($root)/boot/kernel cdroot root=live:CDLABEL=COS_LIVE {{LIVE_CONSOLE}}{{NOMODESET}} install-mode\nlinux ($root)/boot/kernel cdroot{{EXTEND_CMDLINE}}\nmenuentry debug { linux console=tty0 }\n"

	It("replaces NOMODESET and EXTEND_CMDLINE with provided values", func() {
		result := mustApplyGrubTemplate([]byte(templateWithPlaceholders), " nomodeset", " rd.debug rd.shell", "", "")
		Expect(string(result)).To(ContainSubstring(" nomodeset"))
		Expect(string(result)).To(ContainSubstring(" rd.debug rd.shell"))
		Expect(string(result)).ToNot(ContainSubstring("{{NOMODESET}}"))
		Expect(string(result)).ToNot(ContainSubstring("{{EXTEND_CMDLINE}}"))
	})

	It("replaces EXTEND_CMDLINE with empty string when not provided", func() {
		result := mustApplyGrubTemplate([]byte(templateWithPlaceholders), "", "", "", "")
		Expect(string(result)).ToNot(ContainSubstring("{{EXTEND_CMDLINE}}"))
		Expect(string(result)).To(ContainSubstring("install-mode\nlinux ($root)/boot/kernel cdroot\n"))
	})

	It("replaces NOMODESET with empty string when not provided", func() {
		result := mustApplyGrubTemplate([]byte(templateWithPlaceholders), "", " rd.debug", "", "")
		Expect(string(result)).ToNot(ContainSubstring("{{NOMODESET}}"))
		Expect(string(result)).To(ContainSubstring(" rd.debug"))
	})

	It("uses the default live consoles when no override is provided", func() {
		result := mustApplyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", "")
		Expect(string(result)).To(ContainSubstring("console=ttyS0 console=tty1"))
		Expect(string(result)).ToNot(ContainSubstring("{{LIVE_CONSOLE}}"))
	})

	// One per {{LIVE_CONSOLE}} in the shipped menu, which is every entry
	// except the debug one: that entry pins console=tty0 so a broken serial
	// console cannot take the debug boot down with it.
	It("replaces live consoles while preserving the debug console", func() {
		result := mustApplyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "console=ttyUSB0,115200", "")
		Expect(string(result)).ToNot(ContainSubstring("console=ttyS0 console=tty1"))
		Expect(strings.Count(string(result), "console=ttyUSB0,115200")).
			To(Equal(strings.Count(string(constants.GrubLiveBiosCfg), "{{LIVE_CONSOLE}}")))
		Expect(string(result)).To(ContainSubstring("console=tty0 rd.debug"))
	})

	// Remote recovery is reached from the interactive installer's welcome
	// page now, not from a live menu entry of its own
	// (kairos-io/kairos#5064). The menu has to shrink towards one entry that
	// lands in the installer, so an entry defined only by a cmdline keyword
	// that the installer already offers cannot come back.
	It("offers no remote recovery entry of its own", func() {
		Expect(string(constants.GrubLiveBiosCfg)).ToNot(ContainSubstring("kairos.remote_recovery_mode"))
		Expect(string(constants.GrubLiveBiosCfg)).ToNot(ContainSubstring("remote recovery"))
	})

	It("strips carriage returns and newlines from a live console override", func() {
		result := mustApplyGrubTemplate([]byte("linux {{LIVE_CONSOLE}} end"), "", "", "console=ttyS1\r\nconsole=tty1", "")
		Expect(string(result)).To(Equal("linux console=ttyS1console=tty1 end"))
	})

	It("defaults to the installer entry", func() {
		result := mustApplyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", "")
		Expect(string(result)).To(ContainSubstring(
			fmt.Sprintf("set default=%q", constants.LiveGrubEntryInstall)))
		Expect(string(result)).ToNot(ContainSubstring("{{DEFAULT_ENTRY}}"))
	})

	It("boots the entry the build names instead", func() {
		result := mustApplyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", constants.LiveGrubEntryBootLocal)
		Expect(string(result)).To(ContainSubstring(
			fmt.Sprintf("set default=%q", constants.LiveGrubEntryBootLocal)))
	})

	It("accepts every id the template defines", func() {
		for _, id := range constants.LiveGrubEntries {
			result, err := applyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", id)
			Expect(err).ToNot(HaveOccurred(), "rejected %s", id)
			Expect(string(result)).To(ContainSubstring(fmt.Sprintf("set default=%q", id)))
		}
	})

	// Grub falls back to the first entry, the unattended installer, when
	// `set default` matches no id. So an id the template does not define
	// does not fail the boot: the ISO installs the disk without asking.
	// The build has to refuse it instead.
	It("refuses an id the template does not define", func() {
		_, err := applyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", "kairos-interactive")
		Expect(err).To(MatchError(ContainSubstring(`unknown default live grub entry "kairos-interactive"`)))
		// The message has to name the ids, since the id is not guessable
		// from the menu titles a user sees.
		for _, id := range constants.LiveGrubEntries {
			Expect(err.Error()).To(ContainSubstring(id))
		}
	})

	// The id lands inside a quoted grub assignment, so a value ending in a
	// backslash escapes the closing quote and the assignment swallows the
	// lines that follow it. Rejecting unknown ids covers that too.
	It("refuses an id that would escape the quoted assignment", func() {
		for _, id := range []string{
			constants.LiveGrubEntryInstall + `\`,
			"Kairos\"\nset timeout=0",
			"Kairos (install)",
		} {
			_, err := applyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", id)
			Expect(err).To(HaveOccurred(), "accepted %q", id)
		}
	})

	// hiddenentry is not a GRUB command. GRUB prints "can't find command
	// 'hiddenentry'" before the menu and the hotkey does not exist. The
	// text mode switch has to be a menuentry.
	It("defines the text mode hotkey with a menuentry", func() {
		result := string(mustApplyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "", ""))
		Expect(result).ToNot(ContainSubstring("hiddenentry"))
		Expect(result).To(MatchRegexp(`(?m)^\s*menuentry "Text mode" --hotkey "t" \{$`))
	})

	It("takes a padded id, since a shell or a yaml quote leaves whitespace", func() {
		result, err := applyGrubTemplate(constants.GrubLiveBiosCfg, "", "", "",
			"  "+constants.LiveGrubEntryBootLocal+"\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(result)).To(ContainSubstring(
			fmt.Sprintf("set default=%q", constants.LiveGrubEntryBootLocal)))
	})
})

// mustApplyGrubTemplate is applyGrubTemplate for the cases that assert on the
// rendered config rather than on the entry id check.
func mustApplyGrubTemplate(cfg []byte, nomodeset, extendCmdline, liveConsole, defaultEntry string) []byte {
	GinkgoHelper()
	out, err := applyGrubTemplate(cfg, nomodeset, extendCmdline, liveConsole, defaultEntry)
	Expect(err).ToNot(HaveOccurred())
	return out
}

// The template and the constants are two halves of one fact: grub matches
// `set default` against an entry id, and silently falls back to the first
// entry when it matches nothing. Renaming an id in the .cfg without renaming
// the constant is exactly the regression kairos-io/kairos#4960 is about, and
// it is invisible to every other test here.
var _ = Describe("the shipped live grub config", Label("iso"), func() {
	cfg := string(constants.GrubLiveBiosCfg)

	// Both directions matter. An id in the template with no constant is
	// unreachable from a build; a constant with no entry in the template is
	// worse, because the build accepts it and grub then falls back to the
	// unattended installer.
	It("defines exactly the ids a build may select", func() {
		var inTemplate []string
		for _, line := range strings.Split(cfg, "\n") {
			if !strings.HasPrefix(line, "menuentry ") {
				continue
			}
			id := regexp.MustCompile(`--id ([^" ]+)`).FindStringSubmatch(line)
			Expect(id).ToNot(BeNil(), "menuentry without an id: %s", line)
			inTemplate = append(inTemplate, id[1])
		}
		Expect(inTemplate).To(ConsistOf(constants.LiveGrubEntries))
	})

	// grub truncates the value of `default` at the first space before
	// matching it against an id, so an id containing one would match a
	// different entry, or none.
	It("gives every top-level entry a space-free id", func() {
		entries := 0
		for _, line := range strings.Split(cfg, "\n") {
			if !strings.HasPrefix(line, "menuentry ") {
				continue
			}
			entries++
			Expect(line).To(MatchRegexp(`--id [^" ]+ `), "menuentry without an id: %s", line)
		}
		Expect(entries).To(BeNumerically(">=", 3))
	})

	It("points the default at the entry that runs the installer", func() {
		Expect(cfg).To(ContainSubstring(`set default="{{DEFAULT_ENTRY}}"`))
		Expect(entryCmdline(cfg, constants.LiveGrubEntryInstall)).
			To(ContainSubstring(" install-mode-interactive "))
	})

	// Unattended, interactive and manual are one entry, because the
	// installer decides between them from the config and the welcome page.
	// A second install entry would reintroduce the choice in the menu, and
	// a menu cannot know what the config says.
	It("offers exactly one entry that installs", func() {
		installing := []string{}
		for _, id := range constants.LiveGrubEntries {
			if strings.Contains(entryCmdline(cfg, id), "install-mode") {
				installing = append(installing, id)
			}
		}
		Expect(installing).To(ConsistOf(constants.LiveGrubEntryInstall))
	})

	// `install-mode` starts kairos-installer.service, which installs with no
	// prompt and no way back. It was the unattended entry's keyword, and
	// that entry is gone, so no entry may carry it: `install-mode` is a
	// prefix of `install-mode-interactive`, so the assertion is
	// space-delimited the same way the kairos stage guard is.
	It("leaves the unattended installer keyword on no entry", func() {
		Expect(cfg).ToNot(MatchRegexp(`(^| )install-mode( |$)`))
	})
})

var _ = Describe("newLiveISOSpec", Label("iso"), func() {
	It("carries every live boot option from the ISO config", func() {
		spec := newLiveISOSpec("/rootfs", "/isoroot", schema.ISO{
			ExtendLiveCmdline: "rd.debug",
			LiveConsole:       "console=ttyUSB0,115200",
			DefaultGrubEntry:  constants.LiveGrubEntryBootLocal,
		})

		Expect(spec.ExtendLiveCmdline).To(Equal("rd.debug"))
		Expect(spec.LiveConsole).To(Equal("console=ttyUSB0,115200"))
		Expect(spec.DefaultGrubEntry).To(Equal(constants.LiveGrubEntryBootLocal))
	})

	It("leaves the default entry to the template when the config names none", func() {
		Expect(newLiveISOSpec("/rootfs", "/isoroot", schema.ISO{}).DefaultGrubEntry).To(BeEmpty())
	})
})

// applyGrubTemplate is only right if the spec reaches it. Without this, the
// helper keeps its default and the build silently ignores the field.
var _ = Describe("prepareBootArtifacts", Label("iso"), func() {
	writeGrubCfg := func(spec *LiveISO) string {
		isoDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(isoDir, "boot"), 0o755)).To(Succeed())

		cfg, err := NewBuildConfig(WithLogger(logger.NewKairosLogger("test", "error", false)))
		Expect(err).ToNot(HaveOccurred())
		Expect(NewBuildISOAction(cfg, spec).prepareBootArtifacts(isoDir)).To(Succeed())

		written, err := os.ReadFile(filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg))
		Expect(err).ToNot(HaveOccurred())
		return string(written)
	}

	It("boots the installer when the spec names no entry", func() {
		Expect(writeGrubCfg(&LiveISO{})).To(ContainSubstring(
			fmt.Sprintf("set default=%q", constants.LiveGrubEntryInstall)))
	})

	It("boots the entry the spec names", func() {
		Expect(writeGrubCfg(&LiveISO{DefaultGrubEntry: constants.LiveGrubEntryBootLocal})).
			To(ContainSubstring(fmt.Sprintf("set default=%q", constants.LiveGrubEntryBootLocal)))
	})

	// The build has to stop here rather than burn an ISO whose default
	// entry silently resolves to the unattended installer.
	It("fails the build on an entry id the template does not define", func() {
		isoDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(isoDir, "boot"), 0o755)).To(Succeed())
		cfg, err := NewBuildConfig(WithLogger(logger.NewKairosLogger("test", "error", false)))
		Expect(err).ToNot(HaveOccurred())

		err = NewBuildISOAction(cfg, &LiveISO{DefaultGrubEntry: "kairos-interactive"}).
			prepareBootArtifacts(isoDir)
		Expect(err).To(MatchError(ContainSubstring("unknown default live grub entry")))
		Expect(filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg)).ToNot(BeAnExistingFile())
	})

	// A rootfs that ships its own grub.cfg never reaches the template, so
	// the id check must not reject that build.
	It("leaves a rootfs-provided grub config alone without checking the id", func() {
		isoDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(isoDir, constants.GrubPrefixDir), 0o755)).To(Succeed())
		own := filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg)
		Expect(os.WriteFile(own, []byte("# shipped by the rootfs\n"), 0o644)).To(Succeed())
		cfg, err := NewBuildConfig(WithLogger(logger.NewKairosLogger("test", "error", false)))
		Expect(err).ToNot(HaveOccurred())

		Expect(NewBuildISOAction(cfg, &LiveISO{DefaultGrubEntry: "kairos-interactive"}).
			prepareBootArtifacts(isoDir)).To(Succeed())
		Expect(os.ReadFile(own)).To(Equal([]byte("# shipped by the rootfs\n")))
	})
})

// entryCmdline returns the `linux` line of the menuentry carrying the given
// --id, padded with a space at both ends so callers can match whole cmdline
// tokens.
func entryCmdline(cfg, id string) string {
	lines := strings.Split(cfg, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "menuentry ") || !strings.Contains(line, fmt.Sprintf("--id %s ", id)) {
			continue
		}
		for _, body := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(body), "linux ") {
				return " " + strings.TrimSpace(body) + " "
			}
			if strings.TrimSpace(body) == "}" {
				break
			}
		}
	}
	Fail(fmt.Sprintf("no linux line under the menuentry with --id %s", id))
	return ""
}

var _ = Describe("getEfiGrubFilesForArch", Label("iso"), func() {
	It("prepends the openSUSE riscv64 path before SDK paths", func() {
		paths := getEfiGrubFilesForArch("riscv64")
		sdkPaths := sdkutils.GetEfiGrubFiles("riscv64")

		Expect(paths[0]).To(Equal("/usr/share/efi/riscv64/grub.efi"))
		Expect(paths).To(Equal(append([]string{"/usr/share/efi/riscv64/grub.efi"}, sdkPaths...)))
	})

	It("prepends gcdx64.efi.signed on amd64 so the ISO uses the CD grub, not the disk one", func() {
		// gcdx64.efi.signed has iso9660 baked in and its prefix set to
		// /boot/grub, which is what a live ISO needs. grubx64.efi.signed
		// is built for disk installs and its /EFI/ubuntu prefix breaks on
		// firmware that sets $root to the ISO9660 partition.
		paths := getEfiGrubFilesForArch("amd64")
		Expect(paths[0]).To(Equal("/usr/lib/grub/x86_64-efi-signed/gcdx64.efi.signed"))
		Expect(paths).To(ContainElement("/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed"))
		Expect(indexOf(paths, "/usr/lib/grub/x86_64-efi-signed/gcdx64.efi.signed")).To(
			BeNumerically("<", indexOf(paths, "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed")),
		)
	})

	It("prepends gcdaa64.efi.signed on arm64 for the same reason", func() {
		paths := getEfiGrubFilesForArch("arm64")
		Expect(paths[0]).To(Equal("/usr/lib/grub/arm64-efi-signed/gcdaa64.efi.signed"))
		Expect(paths).To(ContainElement("/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed"))
		Expect(indexOf(paths, "/usr/lib/grub/arm64-efi-signed/gcdaa64.efi.signed")).To(
			BeNumerically("<", indexOf(paths, "/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed")),
		)
	})

	It("keeps the full SDK path list after the CD-grub prepends", func() {
		amd64Paths := getEfiGrubFilesForArch("amd64")
		for _, p := range sdkutils.GetEfiGrubFiles("amd64") {
			Expect(amd64Paths).To(ContainElement(p))
		}
	})
})

func indexOf(list []string, target string) int {
	for i, s := range list {
		if s == target {
			return i
		}
	}
	return -1
}

var _ = Describe("writeCdGrubEfiCfg", Label("iso"), func() {
	// gcdx64.efi.signed (and its arm64 equivalent) is compiled with prefix
	// /boot/grub. When the CD-media grub is loaded it looks for its config at
	// ($root)/boot/grub/grub.cfg. Without a file at that path the ISO drops
	// to the grub rescue prompt on firmwares where $root is the ISO device.
	It("writes grub.cfg at /boot/grub/grub.cfg with the chainloader stub", func() {
		fs, cleanup, err := vfst.NewTestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		b := &BuildISOAction{cfg: &BuildConfig{Config: sdkConfig.Config{
			Fs:     fs,
			Logger: logger.NewNullLogger(),
		}}}

		root := "/iso"
		Expect(fs.Mkdir(root, 0o755)).To(Succeed())

		Expect(b.writeCdGrubEfiCfg(root)).To(Succeed())

		out, err := fs.ReadFile(filepath.Join(root, "boot", "grub", constants.GrubCfg))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(out)).To(Equal(constants.GrubEfiCfg))
	})
})

// kairos#5776: the deployer used to find the generated ISO by scanning the
// state dir, which picks an older ISO there. burnISO records the exact file it
// wrote so GenISO can hand that path back.
var _ = Describe("burnISO", Label("iso"), func() {
	DescribeTable("records the ISO it wrote",
		func(date bool, want string) {
			fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{"/out/kairos-old.iso": "old", "/iso": &vfst.Dir{Perm: 0o755}})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cleanup)

			runner := mocks.NewFakeRunner()
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				for i, a := range args {
					if a == "-outdev" {
						return nil, fs.WriteFile(args[i+1], []byte("new"), 0o644)
					}
				}
				return nil, nil
			}
			b := &BuildISOAction{
				cfg:  &BuildConfig{Config: sdkConfig.Config{Fs: fs, Runner: runner, Logger: logger.NewNullLogger()}, Name: "kairos-new", OutDir: "/out", Date: date},
				spec: &LiveISO{Label: "COS_LIVE"},
			}

			Expect(b.burnISO("/iso")).To(Succeed())
			Expect(b.outputFile).To(MatchRegexp(want))
			got, err := fs.ReadFile(b.outputFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(got)).To(Equal("new"))
		},
		Entry("without a date", false, `^/out/kairos-new\.iso$`),
		Entry("with a date", true, `^/out/kairos-new\.[0-9]{8}\.iso$`),
	)
})

var _ = Describe("cleanupGrubName", Label("iso"), func() {
	DescribeTable("strips signature suffixes",
		func(in, want string) {
			Expect(cleanupGrubName(in)).To(Equal(want))
		},
		Entry("plain grubx64.efi", "grubx64.efi", "grubx64.efi"),
		Entry("Ubuntu grubx64.efi.signed", "grubx64.efi.signed", "grubx64.efi"),
		Entry("Ubuntu grubaa64.efi.signed", "grubaa64.efi.signed", "grubaa64.efi"),
		Entry("Ubuntu grubaa64.efi.dualsigned", "grubaa64.efi.dualsigned", "grubaa64.efi"),
		Entry("Ubuntu grubaa64.efi.signed.latest", "grubaa64.efi.signed.latest", "grubaa64.efi"),
	)

	DescribeTable("renames the CD grub to the name shim chainloads",
		func(in, want string) {
			// Ubuntu's shim is compiled to load grub{x64,aa64}.efi from the
			// same directory. When we ship the CD variant of grub, its
			// filename is gcd*.efi.signed. We must rename it so shim finds
			// it under Secure Boot.
			Expect(cleanupGrubName(in)).To(Equal(want))
		},
		Entry("gcdx64.efi.signed becomes grubx64.efi", "gcdx64.efi.signed", "grubx64.efi"),
		Entry("gcdaa64.efi.signed becomes grubaa64.efi", "gcdaa64.efi.signed", "grubaa64.efi"),
		Entry("plain gcdx64.efi (unsigned build) also renames", "gcdx64.efi", "grubx64.efi"),
	)
})

var _ = Describe("GenISO", func() {
	// The ISO root gets the cloud config from the path the caller hands in,
	// not from <dst>/config.yaml, which may be a file the user owns.
	It("copies the cloud config from the path the getter returns", func() {
		dst := GinkgoT().TempDir()
		src := GinkgoT().TempDir()
		other := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dst, "config.yaml"), []byte("hostname: users-own\n"), 0o600)).To(Succeed())
		ccPath := filepath.Join(other, "cc.yaml")
		Expect(os.WriteFile(ccPath, []byte("hostname: from-flag\n"), 0o600)).To(Succeed())

		var recorded string
		materializeExtensionArtifacts = func(_ context.Context, _ []string, _ []extensions.Request, _, destination string, _ bool) ([]string, error) {
			b, err := os.ReadFile(filepath.Join(destination, "config.yaml"))
			Expect(err).ToNot(HaveOccurred())
			recorded = string(b)
			return nil, errors.New("stop here")
		}
		DeferCleanup(func() { materializeExtensionArtifacts = extensions.Materialize })

		iso := schema.ISO{OverrideName: "probe", Extensions: []extensions.Request{{Name: "x"}}}
		err := GenISO(
			func() string { return src },
			func() string { return dst },
			func() string { return ccPath },
			iso, "amd64", false, nil,
		)(context.Background())

		Expect(err).To(MatchError(ContainSubstring("stop here")))
		Expect(recorded).To(ContainSubstring("from-flag"))
		Expect(recorded).ToNot(ContainSubstring("users-own"))
	})
})
