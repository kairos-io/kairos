package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/provider/internal/provider"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

func apiFlagValue() string {
	for _, f := range networkAPI {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == "api" {
			return sf.Value
		}
	}
	Fail("networkAPI has no --api flag")
	return ""
}

var _ = Describe("The --api flag default", func() {
	// applyAPIDefault mutates the flag shared by every command that talks to
	// the API, so put it back or whichever spec runs next inherits it.
	BeforeEach(func() {
		original := apiFlagValue()
		DeferCleanup(func() { setAPIFlagDefault(original) })
	})

	writeEnv := func(contents string) string {
		envFile := filepath.Join(GinkgoT().TempDir(), "edgevpn-kairos.env")
		Expect(os.WriteFile(envFile, []byte(contents), 0600)).To(Succeed())
		return envFile
	}

	// The daemon's address is whatever the agent passed at bootstrap, which is
	// not necessarily this package's default. Following the daemon is what
	// stops `get-kubeconfig` and `role list` from quietly querying an address
	// nothing is listening on.
	It("follows a daemon that was moved onto a TCP port", func() {
		applyAPIDefault(writeEnv("APILISTEN=\"127.0.0.1:8080\"\n"))
		Expect(apiFlagValue()).To(Equal("http://127.0.0.1:8080"))
	})

	It("follows a daemon on a non-standard socket", func() {
		applyAPIDefault(writeEnv("APILISTEN=\"unix:///run/custom.sock\"\n"))
		Expect(apiFlagValue()).To(Equal("unix:///run/custom.sock"))
	})

	// No env file and no socket is an operator's machine, not a node: these
	// commands are run there after `bridge`, whose API is the only one within
	// reach. Both are absent in a temp directory, so this is that case.
	It("uses the bridge's API when there is no daemon to follow", func() {
		// applyAPIDefault reads the real socket path, so a machine that is
		// itself a kairos node would be the other case. Nothing to check there.
		if _, err := os.Stat(strings.TrimPrefix(provider.DefaultEdgeVPNAPIAddress, "unix://")); err == nil {
			Skip("this machine has a local edgevpn socket, so it is not the no-daemon case")
		}

		applyAPIDefault(filepath.Join(GinkgoT().TempDir(), "absent.env"))
		Expect(apiFlagValue()).To(Equal("http://" + provider.DefaultBridgeAPIListen))
	})

	// And that default is the address bridge itself listens on, so the two
	// cannot be changed apart.
	It("names the address bridge serves", func() {
		bridgeAPI := ""
		for _, f := range BridgeCMD("kairos provider").Flags {
			if sf, ok := f.(*cli.StringFlag); ok && sf.Name == apiFlagName {
				bridgeAPI = sf.Value
			}
		}
		Expect(bridgeAPI).To(Equal(provider.DefaultBridgeAPIListen))
	})
})
