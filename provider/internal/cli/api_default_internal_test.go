package cli

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/provider-kairos/v2/internal/provider"
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

	// A node that has not bootstrapped yet has no env file. The default is
	// still the best guess, and is what the daemon would be given anyway.
	It("keeps the default when the daemon has no env file yet", func() {
		applyAPIDefault(filepath.Join(GinkgoT().TempDir(), "absent.env"))
		Expect(apiFlagValue()).To(Equal(provider.DefaultEdgeVPNAPIAddress))
	})
})
