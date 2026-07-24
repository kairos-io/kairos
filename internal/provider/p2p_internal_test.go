package provider

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func listenerOptionsForTest(apiAddress string, userEnv map[string]string) map[string]string {
	opts := map[string]string{"APILISTEN": normalizeAPIAddress(apiAddress)}
	applyAPIListenerEnv(opts, userEnv)
	return opts
}

var _ = Describe("EdgeVPN API listener options", func() {
	It("uses a secure Unix socket by default", func() {
		opts := listenerOptionsForTest("", nil)
		Expect(opts).To(HaveKeyWithValue("APILISTEN", DefaultEdgeVPNAPIAddress))
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", DefaultEdgeVPNAPISocketMode))
	})

	It("adds the secure mode when user configuration changes TCP to Unix", func() {
		opts := listenerOptionsForTest("http://localhost:8080", map[string]string{
			"APILISTEN": "unix:///run/custom-edgevpn.sock",
		})
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", DefaultEdgeVPNAPISocketMode))
	})

	It("does not add a Unix mode when user configuration changes Unix to TCP", func() {
		opts := listenerOptionsForTest(DefaultEdgeVPNAPIAddress, map[string]string{
			"APILISTEN": "127.0.0.1:8080",
		})
		Expect(opts).NotTo(HaveKey("APILISTENUNIXMODE"))
	})

	It("preserves an explicit Unix socket mode", func() {
		opts := listenerOptionsForTest("", map[string]string{"APILISTENUNIXMODE": "0660"})
		Expect(opts).To(HaveKeyWithValue("APILISTENUNIXMODE", "0660"))
	})
})
