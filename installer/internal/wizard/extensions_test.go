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

// catalogJSON is the shape hadron-layers publishes at releases.json. The
// artifact references are pinned by digest, which is what the generator writes
// and what the resolver requires; a tag reference here would make the fixture
// something the agent would refuse to install.
const catalogJSON = `{
  "repo": "ghcr.io/kairos-io/hadron-layers",
  "layers": [
    {"name": "tailscale", "latest": "1.80.0", "tags": [{"tag": "1.80.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/tailscale@sha256:1111111111111111111111111111111111111111111111111111111111111111"}}}]},
    {"name": "nvidia", "latest": "570.0", "tags": [{"tag": "570.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/sysext/nvidia@sha256:2222222222222222222222222222222222222222222222222222222222222222"}}}]}
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
		found, err := catalogExtensions(context.Background(), client, []string{server.URL}, "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(HaveLen(2))
		Expect(found[0].Name).To(Equal("tailscale"))
		Expect(found[0].Latest).To(Equal("1.80.0"))
		Expect(found[0].Version).To(BeEmpty(), "an unpinned pick means the newest version the catalog publishes")
		Expect(found[0].Origin).To(Equal("ghcr.io/kairos-io/hadron-layers"))
	})

	It("keeps the readable catalogs when another one is unreachable", func() {
		found, err := catalogExtensions(context.Background(), client, []string{"http://127.0.0.1:1/nope", server.URL}, "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(HaveLen(2))
	})

	It("merges the live media and the catalog, sorted by name", func() {
		writeLiveMedia(root, "tools.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{server.URL}, "amd64")
		Expect(err).ToNot(HaveOccurred())

		var labels []string
		for _, choice := range found {
			labels = append(labels, choice.Label)
		}
		Expect(labels).To(Equal([]string{"nvidia", "tailscale", "tools"}))
	})

	It("lets the live media win a name the catalog also publishes", func() {
		writeLiveMedia(root, "tailscale.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{server.URL}, "amd64")
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

		found, err := discoverExtensions(context.Background(), client, root, []string{"http://127.0.0.1:1/nope"}, "amd64")
		Expect(err).To(HaveOccurred(), "the screen says so, rather than pretending the catalog was empty")
		Expect(found).To(HaveLen(1))
		Expect(found[0].Label).To(Equal("tools"))
	})

	It("lists the live media images on the step instead of offering them", func() {
		writeLiveMedia(root, "tailscale.sysext.raw", "tools.sysext.raw")
		env := &SystemEnv{LiveMediaDir: root, Catalogs: []string{server.URL}, Client: client, Architecture: "amd64"}

		s := ExtensionsStep(context.Background(), env)
		Expect(s.Notice).To(Equal("Always installed from the live media: tailscale, tools."))
		Expect(s.Fields[0].Choices).To(HaveLen(1))
		Expect(s.Fields[0].Choices[0].Value).To(Equal("nvidia"))
	})
})

// unpublishedCatalogJSON is the shape of a real catalog that indexes a layer
// whose system extension build is switched off, and one that publishes an
// image for another architecture only. hadron-layers does the first for `git`
// through publishing.yaml, and both shapes also occur while a newly added
// layer waits for its first extension push.
const unpublishedCatalogJSON = `{
  "repo": "ghcr.io/kairos-io/hadron-layers",
  "layers": [
    {"name": "tailscale", "latest": "1.80.0", "tags": [{"tag": "1.80.0", "sysext": {"amd64": {"oci": "ghcr.io/kairos-io/hadron-layers/tailscale@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}]},
    {"name": "git", "latest": "2.56.0", "tags": [{"tag": "2.56.0", "sysext": {}}, {"tag": "2.55.0", "sysext": {}}]},
    {"name": "drbd", "latest": "9.3.4", "tags": [{"tag": "9.3.4", "sysext": {"arm64": {"oci": "ghcr.io/kairos-io/hadron-layers/drbd@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}]}
  ]
}`

var _ = Describe("a catalog layer that publishes no image this node can install", func() {
	var (
		server *httptest.Server
		client *http.Client
	)

	BeforeEach(func() {
		client = &http.Client{}
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(unpublishedCatalogJSON))
		}))
		DeferCleanup(server.Close)
	})

	It("is not offered, so the pick cannot fail the install it was made for", func() {
		found, err := catalogExtensions(context.Background(), client, []string{server.URL}, "amd64")
		Expect(err).ToNot(HaveOccurred())

		var labels []string
		for _, choice := range found {
			labels = append(labels, choice.Label)
		}
		Expect(labels).To(Equal([]string{"tailscale"}))
		Expect(labels).ToNot(ContainElement("git"), "git is published as a base image only")
		Expect(labels).ToNot(ContainElement("drbd"), "drbd publishes arm64 only")
	})

	It("is offered on the architecture that does have it", func() {
		found, err := catalogExtensions(context.Background(), client, []string{server.URL}, "arm64")
		Expect(err).ToNot(HaveOccurred())

		var labels []string
		for _, choice := range found {
			labels = append(labels, choice.Label)
		}
		Expect(labels).To(Equal([]string{"drbd"}))
	})

	It("does not hide the live media copy of the same name", func() {
		root := GinkgoT().TempDir()
		writeLiveMedia(root, "git.sysext.raw")

		found, err := discoverExtensions(context.Background(), client, root, []string{server.URL}, "amd64")
		Expect(err).ToNot(HaveOccurred())

		var git []extensionChoice
		for _, choice := range found {
			if choice.Label == "git" {
				git = append(git, choice)
			}
		}
		Expect(git).To(HaveLen(1))
		Expect(git[0].Origin).To(Equal(originLiveMedia))
	})
})
