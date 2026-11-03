package integration_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Install Script", func() {
	It("should return a valid shell script with token and URL", func() {
		resp, err := http.Get(testServerURL + "/api/v1/install-agent")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body := readBody(resp)
		Expect(body).To(ContainSubstring("#!/bin/sh"))
		Expect(body).To(ContainSubstring("AURORABOOT_URL"))
		Expect(body).To(ContainSubstring("/oem/phonehome.yaml"))
		Expect(body).To(ContainSubstring("kairos-agent start"))
		Expect(body).To(ContainSubstring("agent_needs_phonehome_execstart_fix"))
		Expect(body).To(ContainSubstring("agent_version_lt"))
		Expect(body).To(ContainSubstring("2.30.1"))
		Expect(body).NotTo(ContainSubstring("sort -V"))
		Expect(body).To(ContainSubstring("sed -i"))
		Expect(body).To(ContainSubstring("systemctl is-active --quiet kairos-agent-phonehome"))
	})

	// The install script bakes allowed_commands into the node's cloud-config
	// from AURORABOOT_ALLOWED_COMMANDS (comma-separated). The snippet must
	// always be present so nodes never inherit an implicit agent default.
	It("should include allowed_commands handling with safe defaults fallback", func() {
		resp, err := http.Get(testServerURL + "/api/v1/install-agent")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		body := readBody(resp)
		Expect(body).To(ContainSubstring("AURORABOOT_ALLOWED_COMMANDS"))
		// The default set must be in the script so an unset env still produces
		// a concrete allowed_commands list rather than a silent fallback.
		Expect(body).To(ContainSubstring("upgrade,upgrade-recovery,reboot,unregister"))
		// The key itself is always emitted by the heredoc.
		Expect(body).To(ContainSubstring("allowed_commands:"))
	})
})
