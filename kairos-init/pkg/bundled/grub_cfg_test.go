package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// GrubCfg is /etc/cos/grub.cfg. Some GRUB builds, such as the signed ones in
// Ubuntu, compile the video drivers into the core image and ship no
// all_video.mod. An unguarded `insmod all_video` then prints an error at the
// start of every boot.
var _ = Describe("GrubCfg", func() {
	// directives drops comment lines so that prose cannot satisfy or break
	// an assertion about what the script runs.
	directives := func() []string {
		var out []string
		for _, line := range strings.Split(bundled.GrubCfg, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				out = append(out, line)
			}
		}
		return out
	}

	It("does not load all_video unconditionally", func() {
		for _, line := range directives() {
			if strings.TrimSpace(line) == "insmod all_video" {
				// The only allowed form is inside the file test.
				Expect(line).To(HavePrefix("  "), "insmod all_video must be indented under a guard")
			}
		}
		Expect(strings.Join(directives(), "\n")).ToNot(MatchRegexp(`(?m)^insmod all_video$`))
	})

	It("loads all_video only when the module file exists", func() {
		guard := regexp.MustCompile(
			`(?m)^if \[ -f "\$\{prefix\}/\$\{grub_cpu\}-\$\{grub_platform\}/all_video\.mod" \]; then\n  insmod all_video\nfi$`)
		Expect(bundled.GrubCfg).To(MatchRegexp(guard.String()))
	})

	It("still loads the other modules unconditionally", func() {
		for _, mod := range []string{"loopback", "squash4", "serial"} {
			Expect(bundled.GrubCfg).To(ContainSubstring("\ninsmod " + mod + "\n"))
		}
	})

	// rmmod is in the minicmd module, which Hadron's GRUB does not embed. The
	// config defines a no-op rmmod function for that case. GRUB runs a built-in
	// command before a function of the same name, so Ubuntu still runs the
	// real rmmod for its tpm workaround.
	Describe("rmmod tpm", func() {
		fallback := regexp.MustCompile(`(?m)^function rmmod \{\n  true\n\}$`)
		call := regexp.MustCompile(`(?m)^if \[ "\$\{grub_platform\}" = "efi" \]; then\n(?:\s*#.*\n)*\s+rmmod tpm\nfi$`)

		It("still removes tpm on EFI", func() {
			Expect(bundled.GrubCfg).To(MatchRegexp(call.String()))
		})

		It("defines a top-level rmmod fallback before the call", func() {
			// GRUB accepts a function definition only at the top level.
			def := fallback.FindStringIndex(bundled.GrubCfg)
			Expect(def).ToNot(BeNil(), "no rmmod fallback function")
			use := call.FindStringIndex(bundled.GrubCfg)
			Expect(use).ToNot(BeNil())
			Expect(def[0]).To(BeNumerically("<", use[0]), "the fallback must be defined before rmmod tpm runs")
		})

		It("does not call rmmod anywhere else", func() {
			var calls []string
			for _, line := range directives() {
				if strings.HasPrefix(strings.TrimSpace(line), "rmmod ") {
					calls = append(calls, strings.TrimSpace(line))
				}
			}
			Expect(calls).To(Equal([]string{"rmmod tpm"}))
		})
	})

	It("passes grub-script-check", func() {
		check, err := exec.LookPath("grub-script-check")
		if err != nil {
			Skip("grub-script-check is not installed")
		}
		cfg := filepath.Join(GinkgoT().TempDir(), "grub.cfg")
		Expect(os.WriteFile(cfg, []byte(bundled.GrubCfg), 0o600)).To(Succeed())
		out, err := exec.Command(check, cfg).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
	})
})
