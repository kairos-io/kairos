package agent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The plain installer used to run next to a kairos-webui service listening on
// 8080, and this line was where an operator read the address off. That service
// is retired: the web UI is served in-process by the interactive installer,
// and on this path nothing listens. An address printed here would send an
// operator to a closed port, which is what QA found on the GRUB live ISO.
var _ = Describe("installerInfoLine", func() {
	It("names the interfaces the node holds", func() {
		Expect(installerInfoLine([]string{"lo", "eth0"})).To(Equal("Interfaces: lo eth0"))
	})

	It("advertises no web UI address", func() {
		line := installerInfoLine([]string{"eth0"})
		Expect(line).ToNot(ContainSubstring("WebUI"))
		Expect(line).ToNot(ContainSubstring("8080"))
	})

	It("says nothing more when there is no interface to name", func() {
		Expect(installerInfoLine(nil)).To(Equal("Interfaces: "))
	})
})
