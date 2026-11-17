package uki

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// installKeywords are the keywords that ask a Kairos boot to run an installer.
// None of them may reach a UKI cmdline: on Trusted Boot the cmdline lives in a
// signed section of the EFI and the installer copies norole.efi byte for byte
// into the active, passive, recovery and statereset roles, so a keyword set for
// the live medium stays on the installed machine's kernel command line for the
// life of the machine. kairos-io/kairos#5000.
var installKeywords = []string{"install-mode", "install-mode-interactive", "interactive-install"}

func cmdlineHasKeyword(cmdline, keyword string) bool {
	for _, arg := range strings.Fields(cmdline) {
		if arg == keyword {
			return true
		}
	}
	return false
}

var _ = Describe("UKI cmdline", func() {
	Describe("GetUkiCmdline", func() {
		It("builds the default entry from the base cmdline alone", func() {
			entries := GetUkiCmdline("", "Kairos", nil, false)
			Expect(entries).To(HaveLen(1))
			Expect(entries[0].Cmdline).To(Equal(constants.UkiCmdline))
			Expect(entries[0].FileName).To(Equal(constants.ArtifactBaseName))
		})

		It("puts no install keyword on any entry it builds", func() {
			entries := GetUkiCmdline("extended=1", "Kairos", []string{"extra=1"}, false)
			entries = append(entries, GetUkiCmdline("", "Kairos", []string{"extra=1"}, false)...)
			entries = append(entries, GetUkiSingleCmdlines("Kairos", []string{"My Entry: single=1", "bare=1"}, sdkLogger.NewNullLogger())...)

			Expect(entries).ToNot(BeEmpty())
			for _, entry := range entries {
				for _, keyword := range installKeywords {
					Expect(cmdlineHasKeyword(entry.Cmdline, keyword)).To(BeFalse(),
						"entry %q must not carry %q, got %q", entry.FileName, keyword, entry.Cmdline)
				}
			}
		})

		// An entry whose whole addition is "install-mode" used to be renamed to
		// the bare basename, which is the default entry's name. Two entries with
		// one name overwrite each other's .efi and .conf.
		It("gives every entry its own file name, install keyword or not", func() {
			entries := GetUkiCmdline("", "Kairos", []string{"install-mode", "install-mode-interactive"}, false)

			names := []string{}
			for _, entry := range entries {
				names = append(names, entry.FileName)
			}
			// ConsistOf compares as a multiset, so a name claimed twice fails
			// here rather than silently overwriting an .efi on disk.
			Expect(names).To(ConsistOf(
				constants.ArtifactBaseName,
				constants.ArtifactBaseName+"_install-mode",
				constants.ArtifactBaseName+"_install-mode-interactive",
			))
		})
	})

	Describe("createConfFiles", func() {
		var sourceDir, selinuxBase string

		BeforeEach(func() {
			sourceDir = GinkgoT().TempDir()

			selinuxBase = strings.Replace(constants.UkiCmdline, " selinux=0", " security=selinux selinux=1 enforcing=0 rd.cos.selinux=permissive", 1)
			Expect(selinuxBase).NotTo(Equal(constants.UkiCmdline))
		})

		readConf := func(name string) string {
			content, err := os.ReadFile(filepath.Join(sourceDir, "entries", name))
			Expect(err).ToNot(HaveOccurred())
			return string(content)
		}

		It("writes an empty cmdline line for the default entry, which adds nothing", func() {
			Expect(createConfFiles(selinuxBase, sourceDir, constants.UkiCmdline, "Kairos", constants.ArtifactBaseName, "v1", "0", false, true)).To(Succeed())
			Expect(readConf(constants.ArtifactBaseName + ".conf")).To(ContainSubstring("cmdline"))
		})
	})
})

// splashKeyword is the token kairos-splash.service gates on.
//
// kairos-init installs and enables that unit on every systemd image,
// Trusted Boot included, with ConditionKernelCommandLine=splash
// (kairos-init/pkg/bundled/bundled.go). On a GRUB system the token comes from
// BootArgsCfg. Trusted Boot does not read that file and its cmdline is in a
// signed section, so the only place the token can come from is the base
// cmdline here. Without it the unit is skipped on every boot of every UKI
// image. kairos-io/kairos#5285.
const splashKeyword = "splash"

var _ = Describe("UKI cmdline splash", func() {
	It("puts the splash token on every entry it builds", func() {
		entries := GetUkiCmdline("", "Kairos", nil, false)
		entries = append(entries, GetUkiCmdline("extended=1", "Kairos", nil, false)...)
		entries = append(entries, GetUkiCmdline("", "Kairos", []string{"extra=1"}, false)...)
		entries = append(entries, GetUkiSingleCmdlines("Kairos", []string{"My Entry: single=1", "bare=1"}, sdkLogger.NewNullLogger())...)

		Expect(entries).ToNot(BeEmpty())
		for _, entry := range entries {
			Expect(cmdlineHasKeyword(entry.Cmdline, splashKeyword)).To(BeTrue(),
				"entry %q must carry %q, got %q", entry.FileName, splashKeyword, entry.Cmdline)
		}
	})

	// The token is a whole word, not a substring: ConditionKernelCommandLine
	// matches an argument, so "nosplash" or "splash=0" would not satisfy it.
	It("carries the token as its own argument", func() {
		Expect(strings.Fields(constants.UkiCmdline)).To(ContainElement(splashKeyword))
	})

	// Adding a token to the base cmdline must not rename any artifact.
	// nameFromCmdline names an entry after what it adds to the base, so the
	// default entry stays norole.efi and an extra entry stays named after its
	// own addition. A rename here would break every tool that knows those
	// names, and would silently change what the installer copies.
	It("leaves the entry file names alone", func() {
		entries := GetUkiCmdline("", "Kairos", []string{"extra=1", "rd.immucore.debug"}, false)

		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.FileName)
		}
		Expect(names).To(ConsistOf(
			constants.ArtifactBaseName,
			constants.ArtifactBaseName+"_extra_1",
			constants.ArtifactBaseName+"_rd.immucore.debug",
		))
	})

	// install.selinux rewrites the base cmdline in place before the entries
	// are built, so the token has to survive that rewrite too.
	It("keeps the token when selinux is spliced into the base cmdline", func() {
		patched, err := spliceSelinuxCmdline(constants.UkiCmdline, true, "enforcing")
		Expect(err).ToNot(HaveOccurred())
		Expect(patched).ToNot(Equal(constants.UkiCmdline))
		Expect(strings.Fields(patched)).To(ContainElement(splashKeyword))
	})
})
