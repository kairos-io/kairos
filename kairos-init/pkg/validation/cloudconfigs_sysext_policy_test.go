package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	sysextPathRe  = regexp.MustCompile(`^\s+- path: (\S+)`)
	imagePolicyRe = regexp.MustCompile(`--image-policy="([^"]+)"`)
)

var _ = Describe("Bundled sysext cloudconfig image policy", func() {
	It("requires a valid signature under Trusted Boot (UKI) instead of falling back to unsigned verity", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "99_sysext.yaml"))
		Expect(err).NotTo(HaveOccurred())

		// Track which drop-in file each --image-policy value belongs to, since
		// the UKI and non-UKI drop-ins intentionally use different policies.
		var currentPath string
		found := map[string][]string{}
		for _, line := range strings.Split(string(content), "\n") {
			if m := sysextPathRe.FindStringSubmatch(line); m != nil {
				currentPath = m[1]
			}
			if m := imagePolicyRe.FindStringSubmatch(line); m != nil {
				found[currentPath] = append(found[currentPath], m[1])
			}
		}

		ukiDropIns := []string{
			"/etc/systemd/system/systemd-sysext.service.d/kairos-uki.conf",
			"/etc/systemd/system/systemd-confext.service.d/kairos-uki.conf",
		}
		for _, path := range ukiDropIns {
			Expect(found[path]).NotTo(BeEmpty(), "expected --image-policy occurrences in %s", path)
			for _, policy := range found[path] {
				// A bare "verity" alternative (without "signed") lets systemd
				// fall back to unsigned dm-verity when signature checking
				// fails, contradicting the Trusted Boot docs which promise
				// only signed+verity sysexts are accepted.
				Expect(policy).To(Equal(`root=signed+absent:usr=signed+absent`), path)
			}
		}

		nonUkiDropIns := []string{
			"/etc/systemd/system/systemd-sysext.service.d/kairos.conf",
			"/etc/systemd/system/systemd-confext.service.d/kairos.conf",
		}
		for _, path := range nonUkiDropIns {
			Expect(found[path]).NotTo(BeEmpty(), "expected --image-policy occurrences in %s", path)
			for _, policy := range found[path] {
				Expect(policy).To(Equal(`root=verity+absent:usr=verity+absent`), path)
			}
		}
	})
})
