package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
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

var _ = Describe("the extensions page", func() {
	var (
		root   string
		server *httptest.Server
		page   *extensionsPage
	)

	BeforeEach(func() {
		logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
		mainModel = InitialModel(&logger, "")

		root = GinkgoT().TempDir()
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(catalogJSON))
		}))
		DeferCleanup(server.Close)

		page = newExtensionsPage()
		page.root = root
		page.catalogs = []string{server.URL}
	})

	// load runs the discovery command the way bubbletea would, and feeds the
	// resulting message back into the page.
	load := func(p *extensionsPage) {
		cmd := p.Init()
		Expect(cmd).ToNot(BeNil())
		_, _ = p.Update(cmd())
	}

	It("shows what it found, and which entries need no network", func() {
		writeLiveMedia(root, "tools.sysext.raw")
		load(page)

		view := page.View()
		Expect(view).To(ContainSubstring("tools (live media)"))
		Expect(view).To(ContainSubstring("tailscale (ghcr.io/kairos-io/hadron-layers, latest 1.80.0)"))
	})

	It("says so when nothing is on the media and no catalog answers", func() {
		page.catalogs = []string{"http://127.0.0.1:1/nope"}
		load(page)

		Expect(page.View()).To(ContainSubstring("No catalog could be read"))
		Expect(page.View()).To(ContainSubstring("No extension was found"))
	})

	It("writes the selection into install.extensions of the rendered config", func() {
		writeLiveMedia(root, "tools.sysext.raw")
		load(page)

		// nvidia, tailscale, tools: pick the catalog entry and the local image.
		Expect(page.choices).To(HaveLen(3))
		page.cursor = 1
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		page.cursor = 2
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		_, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(cmd).ToNot(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "customization"}))

		mainModel.disk = "/dev/sda"
		out, err := RenderCloudConfig(&mainModel)
		Expect(err).ToNot(HaveOccurred())
		// The shorthand string form is what install.extensions round-trips
		// through, and an unpinned catalog pick carries no `@version`: the same
		// install.extensions an AuroraBoot build writes when it leaves an
		// extension on latest.
		Expect(out).To(ContainSubstring("extensions:"))
		Expect(out).To(ContainSubstring("- tailscale\n"))
		Expect(out).To(ContainSubstring("- " + filepath.Join(root, "tools.sysext.raw") + "\n"))
		Expect(out).ToNot(ContainSubstring("@"))

		// The agent reads the file back as a Config, so decode it the same way
		// rather than trusting the rendered text alone.
		var decoded sdkConfig.Config
		Expect(yaml.Unmarshal([]byte(out), &decoded)).To(Succeed())
		Expect(decoded.Install).ToNot(BeNil())
		Expect(decoded.Install.Extensions).To(Equal(mainModel.extensions))
	})

	It("leaves install.extensions out when nothing is picked", func() {
		writeLiveMedia(root, "tools.sysext.raw")
		load(page)
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})

		mainModel.disk = "/dev/sda"
		out, err := RenderCloudConfig(&mainModel)
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(ContainSubstring("extensions:"))
	})

	It("keeps the selection when the user walks back into the page", func() {
		writeLiveMedia(root, "tools.sysext.raw")
		load(page)
		page.cursor = 2
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

		Expect(page.Init()).To(BeNil(), "a second discovery would drop what the user already ticked")
		Expect(page.selected[2]).To(BeTrue())
	})

	It("is reachable from the customization menu and ticks once something is picked", func() {
		customization := newCustomizationPage()
		Expect(customization.cursorWithIDs).To(HaveKeyWithValue(2, extensionsPageID))

		// The page has to be registered, or the menu entry goes nowhere.
		var registered bool
		for _, p := range mainModel.pages {
			if p.ID() == extensionsPageID {
				registered = true
			}
		}
		Expect(registered).To(BeTrue())

		Expect(customization.isConfigured(extensionsPageID)).To(BeFalse())
		writeLiveMedia(root, "tools.sysext.raw")
		load(page)
		page.cursor = 2
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
		_, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(customization.isConfigured(extensionsPageID)).To(BeTrue())
	})
})

var _ = Describe("the extensions list window", func() {
	It("keeps the cursor on screen without scrolling past either end", func() {
		// Head of the list.
		first, last := visibleWindow(0, 20, 8)
		Expect([]int{first, last}).To(Equal([]int{0, 8}))
		// Middle: the cursor sits inside the window.
		first, last = visibleWindow(10, 20, 8)
		Expect(first).To(BeNumerically("<=", 10))
		Expect(last).To(BeNumerically(">", 10))
		// Tail: the window stops at the end rather than running past it.
		first, last = visibleWindow(19, 20, 8)
		Expect([]int{first, last}).To(Equal([]int{12, 20}))
		// A short list is shown whole.
		first, last = visibleWindow(2, 3, 8)
		Expect([]int{first, last}).To(Equal([]int{0, 3}))
	})

	It("fits the body an 80x24 console leaves for a page", func() {
		logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
		mainModel = InitialModel(&logger, "")

		root := GinkgoT().TempDir()
		var many []string
		for i := 0; i < 30; i++ {
			many = append(many, fmt.Sprintf("layer%02d.sysext.raw", i))
		}
		writeLiveMedia(root, many...)

		page := newExtensionsPage()
		page.root = root
		page.catalogs = nil
		cmd := page.Init()
		Expect(cmd).ToNot(BeNil())
		_, _ = page.Update(cmd())

		// The model keeps this many lines of a page body before it truncates,
		// on the 80x24 fallback a serial console gets.
		budget := defaultTermHeight - 8 - 2

		// Mid-list is the widest the page ever gets: both markers are drawn.
		page.cursor = 15
		view := page.View()
		Expect(strings.Count(view, "\n")).To(BeNumerically("<=", budget),
			"a body the model truncates loses rows with nothing on screen to say so")
		Expect(view).To(ContainSubstring("more above"))
		Expect(view).To(ContainSubstring("more below"))
		Expect(view).To(ContainSubstring("layer15"))

		// The last entry is reachable, rather than sitting below the cut.
		page.cursor = 29
		Expect(page.View()).To(ContainSubstring("layer29"))
	})
})
