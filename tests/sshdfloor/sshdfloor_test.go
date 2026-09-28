package sshdfloor_test

import (
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"kairos-tests/sshdfloor"
)

// hadronEffective is what `sshd -T` reports on a kairos-hadron v0.5.3
// image: /etc/ssh/sshd_config.d/01-hadron-stig.conf and
// 02-hadron-crypto.conf sort below kairos-init's 05-kairos-hardening.conf,
// so the base image wins every directive both set. The crypto lists and
// the login policy below are copied from those two files in
// ghcr.io/kairos-io/hadron:v0.5.3, and the six values here are the ones
// the DevSec profile reported on the red master runs of kairos-io/kairos#5041.
const hadronEffective = `
ciphers aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes256-ctr,aes128-gcm@openssh.com,aes128-ctr
macs hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256,hmac-sha2-512,umac-128@openssh.com
kexalgorithms mlkem768x25519-sha256,sntrup761x25519-sha512,sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256
hostkeyalgorithms ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,rsa-sha2-512,rsa-sha2-256
pubkeyacceptedalgorithms ssh-ed25519,rsa-sha2-512,rsa-sha2-256
hostkey /etc/ssh/ssh_host_rsa_key
hostkey /etc/ssh/ssh_host_ecdsa_key
hostkey /etc/ssh/ssh_host_ed25519_key
hostkey /etc/ssh/ssh_host_mldsa44_ed25519_key
maxauthtries 4
clientaliveinterval 600
clientalivecountmax 1
permitrootlogin prohibit-password
`

// kairosInitEffective is what `sshd -T` reports on a base image that
// ships no sshd drop-in of its own, so kairos-init's
// 05-kairos-hardening.conf is the effective policy. Values copied from
// bundled.SshdHardeningConfig.
const kairosInitEffective = `
ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com,aes256-ctr,aes192-ctr,aes128-ctr
macs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-512,hmac-sha2-256
kexalgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256@libssh.org,diffie-hellman-group-exchange-sha256
hostkeyalgorithms ssh-ed25519,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-256,rsa-sha2-512,rsa-sha2-256-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com
pubkeyacceptedalgorithms ssh-ed25519,rsa-sha2-256,rsa-sha2-512
hostkey /etc/ssh/ssh_host_rsa_key
hostkey /etc/ssh/ssh_host_ed25519_key
maxauthtries 2
clientaliveinterval 300
clientalivecountmax 3
permitrootlogin no
`

// opensshDefaults is an image that received no hardening at all: the
// values are OpenSSH's own compiled-in defaults. The floor has to reject
// it, otherwise waiving the six controls really would be a blind spot.
const opensshDefaults = `
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
kexalgorithms curve25519-sha256,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1
hostkeyalgorithms ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
pubkeyacceptedalgorithms ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
hostkey /etc/ssh/ssh_host_rsa_key
hostkey /etc/ssh/ssh_host_ed25519_key
maxauthtries 6
clientaliveinterval 0
clientalivecountmax 3
`

// replace swaps one directive line for another so a spec can regress a
// single value away from a configuration that otherwise passes.
func replace(cfg, directive, newLine string) string {
	var out []string
	for _, line := range strings.Split(cfg, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), directive+" ") {
			if newLine != "" {
				out = append(out, newLine)
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

var _ = Describe("sshd floor", func() {
	Describe("Parse", func() {
		It("lowercases names and collects repeated directives in order", func() {
			cfg := sshdfloor.Parse("HostKey /a\nhostkey /b\nMaxAuthTries 2\n")
			Expect(cfg["hostkey"]).To(Equal([]string{"/a", "/b"}))
			Expect(cfg["maxauthtries"]).To(Equal([]string{"2"}))
		})

		It("ignores blank lines and comments", func() {
			cfg := sshdfloor.Parse("\n# a comment\nmaxauthtries 2\n")
			Expect(cfg).To(HaveLen(1))
		})

		It("records a directive that sshd printed with no value", func() {
			cfg := sshdfloor.Parse("permitlisten\n")
			Expect(cfg["permitlisten"]).To(Equal([]string{""}))
		})
	})

	Describe("Check", func() {
		// The point of the floor: both policies in kairos-io/kairos#5041
		// clear it, so waiving the six exact-list controls does not waive
		// the security property they were there for.
		It("passes the effective config of a hadron v0.5.3 image", func() {
			Expect(sshdfloor.Check(sshdfloor.Parse(hadronEffective))).To(BeEmpty())
		})

		It("passes the effective config of a base image with no sshd policy of its own", func() {
			Expect(sshdfloor.Check(sshdfloor.Parse(kairosInitEffective))).To(BeEmpty())
		})

		It("rejects an image that received no hardening at all", func() {
			problems := sshdfloor.Check(sshdfloor.Parse(opensshDefaults))
			Expect(problems).To(ContainElement(ContainSubstring("hmac-sha1")))
			Expect(problems).To(ContainElement(ContainSubstring("umac-64")))
			Expect(problems).To(ContainElement(ContainSubstring("diffie-hellman-group14-sha1")))
			Expect(problems).To(ContainElement(ContainSubstring("ssh-rsa")))
			Expect(problems).To(ContainElement(ContainSubstring("maxauthtries is 6")))
			Expect(problems).To(ContainElement(ContainSubstring("never disconnected")))
		})

		DescribeTable("rejects one regressed directive at a time",
			func(directive, line, wantSubstring string) {
				problems := sshdfloor.Check(sshdfloor.Parse(replace(hadronEffective, directive, line)))
				Expect(problems).To(ContainElement(ContainSubstring(wantSubstring)))
			},
			Entry("a CBC cipher", "ciphers", "ciphers aes256-ctr,aes128-cbc", "aes128-cbc"),
			Entry("3DES", "ciphers", "ciphers aes256-ctr,3des-cbc", "3des-cbc"),
			Entry("an HMAC over MD5", "macs", "macs hmac-md5", "hmac-md5"),
			Entry("an HMAC over SHA-1", "macs", "macs hmac-sha1-etm@openssh.com", "hmac-sha1-etm@openssh.com"),
			Entry("a SHA-1 key exchange", "kexalgorithms", "kexalgorithms diffie-hellman-group14-sha1", "diffie-hellman-group14-sha1"),
			Entry("the 1024-bit DH group", "kexalgorithms", "kexalgorithms diffie-hellman-group1-sha256", "diffie-hellman-group1-"),
			Entry("the SHA-1 RSA signature algorithm", "hostkeyalgorithms", "hostkeyalgorithms ssh-ed25519,ssh-rsa", "ssh-rsa"),
			Entry("DSA public keys", "pubkeyacceptedalgorithms", "pubkeyacceptedalgorithms ssh-ed25519,ssh-dss", "ssh-dss"),
			Entry("MaxAuthTries above the bound", "maxauthtries", "maxauthtries 5", "maxauthtries is 5"),
			Entry("MaxAuthTries of zero", "maxauthtries", "maxauthtries 0", "maxauthtries is 0"),
			Entry("an idle window over 900s", "clientaliveinterval", "clientaliveinterval 901", "901 times clientalivecountmax 1"),
		)

		It("rejects a DSA host key", func() {
			cfg := hadronEffective + "hostkey /etc/ssh/ssh_host_dsa_key\n"
			Expect(sshdfloor.Check(sshdfloor.Parse(cfg))).To(
				ContainElement(ContainSubstring(`host key "/etc/ssh/ssh_host_dsa_key" is DSA`)))
		})

		// The match is on "_dsa" rather than on the full "_dsa_key" file
		// name, because HostKey takes any path and a DSA key does not stop
		// being one when it is not called _key.
		// Check walks bannedAlgorithms, which is a map, so without an
		// explicit sort the problem list comes out in a different order on
		// every call. A red leg is read by a human comparing two runs, so
		// the order has to be stable. One call cannot show this: the
		// unsorted order is right by luck often enough, so repeat it.
		It("returns the problems in the same order every time", func() {
			first := sshdfloor.Check(sshdfloor.Parse(opensshDefaults))
			Expect(len(first)).To(BeNumerically(">", 1), "needs several problems to have an order at all")
			Expect(sort.StringsAreSorted(first)).To(BeTrue(), "want the list sorted, got %v", first)
			for i := 0; i < 50; i++ {
				Expect(sshdfloor.Check(sshdfloor.Parse(opensshDefaults))).To(Equal(first))
			}
		})

		It("rejects a DSA host key whose path does not end in _key", func() {
			cfg := hadronEffective + "hostkey /etc/ssh/ssh_host_dsa\n"
			Expect(sshdfloor.Check(sshdfloor.Parse(cfg))).To(
				ContainElement(ContainSubstring(`host key "/etc/ssh/ssh_host_dsa" is DSA`)))
		})

		// Negative control for the widened match above. Both names contain
		// "dsa", and the ML-DSA one is in the real kairos-init host key
		// set, so reading either as DSA would fail the leg on an image
		// that is doing the right thing.
		DescribeTable("does not read a non-DSA host key as DSA",
			func(path string) {
				cfg := hadronEffective + "hostkey " + path + "\n"
				for _, p := range sshdfloor.Check(sshdfloor.Parse(cfg)) {
					Expect(p).NotTo(ContainSubstring("is DSA"))
				}
			},
			Entry("an ECDSA key", "/etc/ssh/ssh_host_ecdsa_key"),
			Entry("an ML-DSA key", "/etc/ssh/ssh_host_mldsa44_ed25519_key"),
		)

		It("rejects a host key set with no ed25519 key", func() {
			cfg := replace(hadronEffective, "hostkey", "")
			cfg += "hostkey /etc/ssh/ssh_host_rsa_key\n"
			Expect(sshdfloor.Check(sshdfloor.Parse(cfg))).To(
				ContainElement(ContainSubstring("no ed25519 host key")))
		})

		// sshd -T prints every directive the build supports, so a missing
		// one means the value could not be read rather than that it is
		// acceptable. Silence here would let a parse change pass the leg.
		DescribeTable("reports a directive it cannot read",
			func(directive, want string) {
				problems := sshdfloor.Check(sshdfloor.Parse(replace(hadronEffective, directive, "")))
				Expect(problems).To(ContainElement(ContainSubstring(want)))
			},
			Entry("ciphers", "ciphers", "ciphers is not reported by sshd -T"),
			Entry("macs", "macs", "macs is not reported by sshd -T"),
			Entry("kexalgorithms", "kexalgorithms", "kexalgorithms is not reported by sshd -T"),
			Entry("hostkeyalgorithms", "hostkeyalgorithms", "hostkeyalgorithms is not reported by sshd -T"),
			Entry("maxauthtries", "maxauthtries", "maxauthtries is not reported by sshd -T"),
			Entry("clientaliveinterval", "clientaliveinterval", "clientaliveinterval is not reported by sshd -T"),
			Entry("hostkey", "hostkey", "no hostkey"),
		)

		It("reports a non-numeric value rather than treating it as zero", func() {
			Expect(sshdfloor.Check(sshdfloor.Parse(replace(hadronEffective, "maxauthtries", "maxauthtries lots")))).To(
				ContainElement(ContainSubstring(`maxauthtries is "lots", which is not a number`)))
		})

		// pubkeyacceptedalgorithms is the one directive whose absence is
		// tolerated: OpenSSH below 8.2 spells it pubkeyacceptedkeytypes.
		It("tolerates an OpenSSH build that does not report pubkeyacceptedalgorithms", func() {
			Expect(sshdfloor.Check(sshdfloor.Parse(replace(hadronEffective, "pubkeyacceptedalgorithms", "")))).To(BeEmpty())
		})
	})
})
