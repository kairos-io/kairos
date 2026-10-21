package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sshdDirective returns the comma separated values of a single-line directive
// in the hardening drop-in.
func sshdDirective(name string) []string {
	for _, line := range strings.Split(bundled.SshdHardeningConfig, "\n") {
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

	// Documents the CURRENT (buggy) behaviour: the drop-in is static, so it
	// lists KEX names an older sshd does not know and sshd -t rejects the file.
	It("writes KexAlgorithms entries unknown to OpenSSH 9.6", func() {
		var unknown []string
		for _, alg := range sshdDirective("KexAlgorithms") {
			found := false
			for _, s := range openssh96Kex {
				if s == alg {
					found = true
				}
			}
			if !found {
				unknown = append(unknown, alg)
			}
		}
		Expect(unknown).To(ConsistOf("mlkem768x25519-sha256", "sntrup761x25519-sha512"))
	})
})
