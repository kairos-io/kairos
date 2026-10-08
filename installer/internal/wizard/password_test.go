package wizard

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
)

var _ = Describe("installer passwords", func() {
	It("creates distinct SHA-512 crypt hashes that verify", func() {
		first, err := hashPassword("correct horse battery staple")
		Expect(err).NotTo(HaveOccurred())
		second, err := hashPassword("correct horse battery staple")
		Expect(err).NotTo(HaveOccurred())

		Expect(first).To(HavePrefix("$6$"))
		Expect(second).To(HavePrefix("$6$"))
		Expect(second).NotTo(Equal(first))
		Expect(sha512_crypt.New().Verify(first, []byte("correct horse battery staple"))).To(Succeed())
	})
})
