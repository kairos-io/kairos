package builder_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

var _ = Describe("RedactLine", func() {
	const (
		regToken = "2665b0f59f6418afbd769be173557482"
		password = "supersecretpassword"
	)

	It("returns the line unchanged when there are no secrets", func() {
		line := "cloudConfig=registration_token: " + regToken
		Expect(builder.RedactLine(line, nil)).To(Equal(line))
	})

	It("redacts a verbatim registration token", func() {
		line := "cloudConfig=registration_token: " + regToken + " ..."
		Expect(builder.RedactLine(line, []string{regToken, password})).
			To(Equal("cloudConfig=registration_token: <redacted> ..."))
	})

	It("redacts multiple secrets in a single line", func() {
		line := "token=" + regToken + " passwd=" + password
		Expect(builder.RedactLine(line, []string{regToken, password})).
			To(Equal("token=<redacted> passwd=<redacted>"))
	})

	It("leaves values shorter than 8 characters alone", func() {
		// A short password would match unrelated text that happens to
		// contain the same letters, so we skip it.
		line := "user=admin passwd=a"
		Expect(builder.RedactLine(line, []string{"a", "admin"})).
			To(Equal("user=admin passwd=a"))
	})

	It("ignores empty secret entries", func() {
		line := "hello world"
		Expect(builder.RedactLine(line, []string{"", " ", ""})).To(Equal(line))
	})
})
