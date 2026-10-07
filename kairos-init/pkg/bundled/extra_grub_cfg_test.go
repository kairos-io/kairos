package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ExtraGrubCfg is the user override file, copied to the state partition as
// /grubmenu and sourced by grub.cfg. Kairos ships it with no entries of its
// own: the `remoterecovery` entry it used to carry was a second way into
// kairos.remote_recovery_mode next to the interactive installer's welcome
// page, and the boot-menu reduction in #4960/#4962 needs every
// cmdline-keyword-only entry accounted for first. See #5064.
var _ = Describe("ExtraGrubCfg", func() {
	// directives strips comment lines, so an assertion about what the file
	// declares cannot be satisfied by prose explaining what it used to.
	directives := func() string {
		var out []string
		for _, line := range strings.Split(bundled.ExtraGrubCfg, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}

	It("declares no boot entry of its own", func() {
		Expect(directives()).ToNot(MatchRegexp(`\bmenuentry\b`))
		Expect(directives()).ToNot(ContainSubstring("--id"))
	})

	// The entry was the only route to this keyword from the boot menu. If it
	// comes back here, the installer's welcome page and a menu entry are
	// offering the same flow again, which is what #5064 removed.
	It("does not reintroduce the remote recovery route", func() {
		Expect(directives()).ToNot(ContainSubstring("remote_recovery_mode"))
		Expect(directives()).ToNot(ContainSubstring("remoterecovery"))
	})

	// The file is still shipped and still has to be a readable grub fragment,
	// because 08_grub.yaml copies it to the state partition unconditionally
	// and grub.cfg sources whatever is there. An empty string would work, but
	// then nothing tells the next reader that dropping entries in is the
	// supported thing to do.
	It("still documents itself as the override point", func() {
		Expect(bundled.ExtraGrubCfg).ToNot(BeEmpty())
		Expect(bundled.ExtraGrubCfg).To(ContainSubstring("menuentry"))
		Expect(strings.TrimSpace(directives())).To(BeEmpty())
	})

	// grub treats an unterminated last line as a line all the same, but a
	// fragment that is concatenated or sourced is easier to reason about when
	// every line, including the last, is terminated.
	It("is a well-formed fragment", func() {
		Expect(bundled.ExtraGrubCfg).To(HaveSuffix("\n"))
		for _, line := range strings.Split(strings.TrimSuffix(bundled.ExtraGrubCfg, "\n"), "\n") {
			Expect(line).To(MatchRegexp(`^(#.*)?$`), "unexpected non-comment line")
		}
	})

})
