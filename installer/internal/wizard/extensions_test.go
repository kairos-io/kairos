package wizard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// catalogJSON is the shape hadron-layers publishes at releases.json.
const catalogJSON = `{
  "repo": "ghcr.io/kairos-io/hadron-layers",
  "layers": [
    {"name": "tailscale", "latest": "1.80.0", "tags": [{"tag": "1.80.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/tailscale:1.80.0"}}}]},
    {"name": "nvidia", "latest": "570.0", "tags": [{"tag": "570.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/nvidia:570.0"}}}]}
  ]
}`

func writeLiveMedia(root string, names ...string) {
	for _, name := range names {
		Expect(os.WriteFile(filepath.Join(root, name), []byte("image"), 0644)).To(Succeed())
	}
}

var _ = Describe("extension discovery", func() {
	var (
		root   string
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		client = &http.Client{}
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(catalogJSON))
		}))
		DeferCleanup(server.Close)
	})

	It("offers every extension image the live media carries", func() {
		writeLiveMedia(root, "tools.sysext.raw", "config.yaml", "notes.txt")

		found, err := liveMediaExtensions(root)
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(HaveLen(1))
		Expect(found[0].Label).To(Equal("tools"))
		Expect(found[0].Name).To(Equal(filepath.Join(root, "tools.sysext.raw")))
		Expect(found[0].Origin).To(Equal(originLiveMedia))
	})

	It("treats a missing live directory as no extensions, not as an error", func() {
		found, err := liveMediaExtensions(filepath.Join(root, "never-mounted"))
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeEmpty())
	})

	It("lists the layers a catalog publishes", func() {
		found, err := catalogExtensions(context.Background(), client, []string{server.URL})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(HaveLen(2))
		Expect(found[0].Name).To(Equal("tailscale"))
		Expect(found[0].Latest).To(Equal("1.80.0"))
		Expect(found[0].Version).To(BeEmpty(), "an unpinned pick means the newest version the catalog publishes")
		Expect(found[0].Origin).To(Equal("ghcr.io/kairos-io/hadron-layers"))
	})

	It("keeps the readable catalogs when another one is unreachable", func() {
		found, err := catalogExtensions(context.Background(), client, []string{"http://127.0.0.1:1/nope", server.URL})
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(HaveLen(2))
	})

	It("merges the live media and the catalog, sorted by name", func() {
		writeLiveMedia(root, "tools.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{server.URL})
		Expect(err).ToNot(HaveOccurred())

		var labels []string
		for _, choice := range found {
			labels = append(labels, choice.Label)
		}
		Expect(labels).To(Equal([]string{"nvidia", "tailscale", "tools"}))
	})

	It("lets the live media win a name the catalog also publishes", func() {
		writeLiveMedia(root, "tailscale.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{server.URL})
		Expect(err).ToNot(HaveOccurred())

		var tailscale []extensionChoice
		for _, choice := range found {
			if choice.Label == "tailscale" {
				tailscale = append(tailscale, choice)
			}
		}
		Expect(tailscale).To(HaveLen(1), "the same extension must not be offered twice")
		Expect(tailscale[0].Origin).To(Equal(originLiveMedia), "the local copy installs with no network")
		Expect(tailscale[0].Name).To(Equal(filepath.Join(root, "tailscale.sysext.raw")))
	})

	It("still offers the live media when no catalog can be read", func() {
		writeLiveMedia(root, "tools.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{"http://127.0.0.1:1/nope"})
		Expect(err).To(HaveOccurred(), "the screen says so, rather than pretending the catalog was empty")
		Expect(found).To(HaveLen(1))
		Expect(found[0].Label).To(Equal("tools"))
	})
})
