package webui

import (
	"io"
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
)

// pageStep pulls the step ids out of the STEPS table in progress.html, which
// is what the checklist is built from.
var pageStep = regexp.MustCompile(`\['([a-z-]+)',\s*'[^']*'\],`)

var _ = Describe("progress.html", func() {
	var page string

	BeforeEach(func() {
		f, err := getFS().Open("progress.html")
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		b, err := io.ReadAll(f)
		Expect(err).ToNot(HaveOccurred())
		page = string(b)
	})

	It("lists exactly the agent's steps, in the order the agent emits them", func() {
		// The page hard-codes the step ids, because it is static HTML with
		// no build step. This is what stops a rename in sdk/agentrun from
		// silently leaving the checklist stuck on the first row.
		var ids []string
		for _, m := range pageStep.FindAllStringSubmatch(page, -1) {
			ids = append(ids, m[1])
		}
		Expect(ids).To(Equal(agentrun.Steps))
	})

	It("reads the message types this server sends", func() {
		for _, t := range []string{MessageStep, MessageLog, MessageError, MessageDone} {
			Expect(page).To(ContainSubstring("case '" + t + "':"))
		}
	})

	It("renders agent output as text, never as markup", func() {
		// The stream used to carry HTML the server had built out of the
		// agent's ANSI codes, and the page wrote it with innerHTML. It is
		// plain text now, so it has to stay out of any markup sink.
		Expect(page).To(ContainSubstring("div.textContent = text;"))
		Expect(page).ToNot(ContainSubstring("innerHTML"))
	})
})

var _ = Describe("the wizard page", func() {
	read := func(name string) string {
		f, err := getFS().Open(name)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		b, err := io.ReadAll(f)
		Expect(err).ToNot(HaveOccurred())
		return string(b)
	}

	It("has a renderer for every field kind the wizard defines", func() {
		js := read("wizard.js")
		for _, k := range wizard.Kinds {
			Expect(js).To(MatchRegexp(`\n\s+`+string(k)+`: \(`), "no web renderer for kind %q", k)
		}
	})

	It("loads the wizard script and nothing from the network", func() {
		html := read("index.html")
		Expect(html).To(ContainSubstring(`<script src="/wizard.js"></script>`))
		Expect(html).ToNot(MatchRegexp(`(src|href)="https?://`))
	})

	It("writes operator and server text as text, never as markup", func() {
		js := read("wizard.js")
		Expect(js).ToNot(ContainSubstring("innerHTML"))
		Expect(js).ToNot(ContainSubstring("insertAdjacentHTML"))
	})

	It("posts the install as JSON with the confirmed device and finish action", func() {
		js := read("wizard.js")
		Expect(js).To(ContainSubstring("fetch('/install'"))
		Expect(js).To(ContainSubstring("device: state.answers.disk"))
		Expect(js).To(ContainSubstring("finish_action: state.answers.finish_action"))
	})

	It("offers an optional single choice a first row that leaves it unset", func() {
		js := read("wizard.js")
		Expect(js).To(ContainSubstring("Leave unset"))
	})
})
