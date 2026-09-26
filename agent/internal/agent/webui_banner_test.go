package agent

import (
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/branding"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// captureStdout swaps os.Stdout for the duration of f and returns everything
// written to it. displayInfo prints the banner rather than returning it.
func captureStdout(f func()) string {
	r, w, err := os.Pipe()
	Expect(err).ToNot(HaveOccurred())
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	f()

	Expect(w.Close()).To(Succeed())
	out, err := io.ReadAll(r)
	Expect(err).ToNot(HaveOccurred())
	Expect(r.Close()).To(Succeed())
	return string(out)
}

// offeredURLs returns the addresses the banner advertises, or nil when it
// advertises none.
func offeredURLs(banner string) []string {
	const marker = " - WebUI installer: "
	_, rest, found := strings.Cut(banner, marker)
	if !found {
		return nil
	}
	return strings.Fields(rest)
}

var _ = Describe("webUIBanner", func() {
	It("names the interfaces and the addresses the web UI answers on", func() {
		Expect(webUIBanner([]string{"eth0", "eth1"}, []string{"http://192.168.1.50:8080"})).
			To(Equal("Interfaces: eth0 eth1 - WebUI installer: http://192.168.1.50:8080"))
	})

	It("lists every address, because any of them reaches the same server", func() {
		Expect(webUIBanner([]string{"eth0"}, []string{"http://192.168.1.50:8080", "http://[fd00::5]:8080"})).
			To(Equal("Interfaces: eth0 - WebUI installer: http://192.168.1.50:8080 http://[fd00::5]:8080"))
	})

	It("still names the interfaces when there is no address to offer", func() {
		Expect(webUIBanner([]string{"eth0"}, nil)).To(Equal("Interfaces: eth0"))
	})
})

var _ = Describe("displayInfo", func() {
	It("says nothing at all when the image turned the web UI off", func() {
		out := captureStdout(func() {
			displayInfo(&branding.Config{WebUI: branding.WebUI{
				Disable:       true,
				ListenAddress: "192.168.1.50:9000",
			}})
		})
		Expect(out).To(BeEmpty())
	})

	It("offers a pinned listen address as a URL, not as the bind address", func() {
		out := captureStdout(func() {
			displayInfo(&branding.Config{WebUI: branding.WebUI{ListenAddress: "192.168.1.50:9000"}})
		})
		Expect(offeredURLs(out)).To(Equal([]string{"http://192.168.1.50:9000"}))
	})

	// A listen address is where the server binds. ":9000", "0.0.0.0:9000" and
	// "[::]:9000" all mean "every interface", so printing one of them verbatim
	// hands the operator somewhere a browser cannot go. What the banner offers
	// has to be an address this node actually holds, carrying the pinned port.
	for _, wildcard := range []string{":9000", "0.0.0.0:9000", "[::]:9000"} {
		It("expands the wildcard listen address "+wildcard+" to addresses of this node", func() {
			out := captureStdout(func() {
				displayInfo(&branding.Config{WebUI: branding.WebUI{ListenAddress: wildcard}})
			})

			// A host with no offerable address offers nothing, and an empty
			// list must not read as a pass: the bind address must be absent
			// from the line either way.
			Expect(out).ToNot(ContainSubstring(" - WebUI installer: " + wildcard))

			for _, offered := range offeredURLs(out) {
				u, err := url.Parse(offered)
				Expect(err).ToNot(HaveOccurred(), "banner offered %q", offered)
				Expect(u.Scheme).To(Equal("http"))
				Expect(u.Hostname()).ToNot(BeEmpty(), "banner offered a bind address: %q", offered)
				Expect(u.Port()).To(Equal("9000"))
			}
		})
	}
})
