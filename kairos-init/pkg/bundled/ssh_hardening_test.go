package bundled_test

import (
	"errors"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sshdDirective returns the comma separated values of a single-line directive
// in an sshd drop-in.
func sshdDirective(config, name string) []string {
	for _, line := range strings.Split(config, "\n") {
		if strings.HasPrefix(line, name+" ") {
			return strings.Split(strings.TrimPrefix(line, name+" "), ",")
		}
	}
	return nil
}

var _ = Describe("SshdHardeningConfig (kairos-io/kairos#5545)", func() {
	// `ssh -Q kex` on Ubuntu 24.04 (OpenSSH 9.6p1): no mlkem768x25519-sha256,
	// which only appeared in OpenSSH 9.9.
	openssh96Kex := []string{
		"sntrup761x25519-sha512@openssh.com",
		"curve25519-sha256", "curve25519-sha256@libssh.org",
		"ecdh-sha2-nistp256", "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
		"diffie-hellman-group-exchange-sha256",
		"diffie-hellman-group16-sha512", "diffie-hellman-group18-sha512",
		"diffie-hellman-group14-sha256",
	}

	// everything reports every name the drop-in lists as supported, with
	// kex overridden when given.
	everything := func(kex []string) func(string) ([]string, error) {
		return func(query string) ([]string, error) {
			if query == "kex" && kex != nil {
				return kex, nil
			}
			var all []string
			for _, d := range []string{"KexAlgorithms", "Ciphers", "MACs", "HostKeyAlgorithms"} {
				all = append(all, sshdDirective(bundled.SshdHardeningConfig, d)...)
			}
			return all, nil
		}
	}

	It("drops KexAlgorithms entries unknown to OpenSSH 9.6", func() {
		filtered := bundled.FilterSshdAlgorithms(bundled.SshdHardeningConfig, everything(openssh96Kex))
		Expect(sshdDirective(filtered, "KexAlgorithms")).To(Equal([]string{
			"sntrup761x25519-sha512@openssh.com",
			"curve25519-sha256", "curve25519-sha256@libssh.org",
			"ecdh-sha2-nistp256", "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
			"diffie-hellman-group16-sha512", "diffie-hellman-group18-sha512",
			"diffie-hellman-group14-sha256",
		}))
		Expect(sshdDirective(filtered, "Ciphers")).To(Equal(sshdDirective(bundled.SshdHardeningConfig, "Ciphers")))
	})

	It("leaves the drop-in unchanged when sshd knows every name", func() {
		Expect(bundled.FilterSshdAlgorithms(bundled.SshdHardeningConfig, everything(nil))).To(Equal(bundled.SshdHardeningConfig))
	})

	It("keeps the original lists when ssh -Q fails", func() {
		failing := func(string) ([]string, error) { return nil, errors.New("ssh: not found") }
		Expect(bundled.FilterSshdAlgorithms(bundled.SshdHardeningConfig, failing)).To(Equal(bundled.SshdHardeningConfig))
	})

	It("keeps a list as is when none of its entries are supported", func() {
		filtered := bundled.FilterSshdAlgorithms(bundled.SshdHardeningConfig, everything([]string{"some-other-kex"}))
		Expect(filtered).To(Equal(bundled.SshdHardeningConfig))
	})
})
