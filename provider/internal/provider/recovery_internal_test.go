package provider

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The recovery SSH server environment", func() {
	env := recoveryServerEnv("atoken", "a-service", "a-password", "127.0.0.1:2222")

	It("names the token the way the recovery command reads it", func() {
		// The command's token flag comes from EdgeVPN's cmd.CommonFlags, so
		// the name is EDGEVPNTOKEN. Passed as TOKEN the value is dropped and
		// the server starts with no network to join.
		Expect(env).To(ContainElement("EDGEVPNTOKEN=atoken"))
	})

	It("passes the session values the operator was told to use", func() {
		Expect(env).To(ContainElement("SERVICE=a-service"))
		Expect(env).To(ContainElement("PASSWORD=a-password"))
		Expect(env).To(ContainElement("LISTEN=127.0.0.1:2222"))
	})
})
