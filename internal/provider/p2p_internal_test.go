package provider

import (
	"os"
	"path/filepath"

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

var _ = Describe("Resolving the API address the daemon actually listens on", func() {
	var envFile string

	writeEnv := func(contents string) {
		dir := GinkgoT().TempDir()
		envFile = filepath.Join(dir, "edgevpn-kairos.env")
		Expect(os.WriteFile(envFile, []byte(contents), 0600)).To(Succeed())
	}

	// SetupVPN and SetupAPI write APILISTEN into this file for the daemon.
	// Reading it back is what keeps the command line client pointed at the
	// same place, instead of both sides keeping their own default and
	// happening to agree.
	It("reads a unix socket listener back unchanged", func() {
		writeEnv("APILISTEN=\"unix:///run/edgevpn-kairos.sock\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal("unix:///run/edgevpn-kairos.sock"))
	})

	// normalizeAPIAddress strips the scheme before writing APILISTEN, but the
	// API client builds request URLs by concatenating host and path, so a bare
	// host:port would produce "127.0.0.1:8080/api/..." and fail to parse.
	It("puts the scheme back on a TCP listener", func() {
		writeEnv("APILISTEN=\"127.0.0.1:8080\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal("http://127.0.0.1:8080"))
	})

	It("falls back to the default when the file does not exist", func() {
		Expect(ResolveAPIAddress("/nonexistent/edgevpn-kairos.env")).To(Equal(DefaultEdgeVPNAPIAddress))
	})

	It("falls back to the default when the file has no APILISTEN", func() {
		writeEnv("EDGEVPNTOKEN=\"sometoken\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal(DefaultEdgeVPNAPIAddress))
	})

	It("falls back to the default when APILISTEN is empty", func() {
		writeEnv("APILISTEN=\"\"\n")
		Expect(ResolveAPIAddress(envFile)).To(Equal(DefaultEdgeVPNAPIAddress))
	})
})
