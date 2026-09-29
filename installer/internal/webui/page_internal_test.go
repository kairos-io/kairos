package webui

import (
	"fmt"
	"io"
	"regexp"
	"strings"

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

	It("never hands a missing child straight to replaceChildren", func() {
		// replaceChildren turns a null into the text "null", and the review
		// step printed exactly that under its title on a real boot. Every
		// redraw goes through fill, which drops the empty slots first.
		js := read("wizard.js")
		Expect(js).To(MatchRegexp(`function fill\(node, \.\.\.children\) \{\n\s+node\.replaceChildren\(\.\.\.children\.flat\(\)\.filter\(c => c != null && c !== false\)\);`))
		Expect(strings.Count(js, ".replaceChildren(")).To(Equal(1), "call fill instead of replaceChildren")
	})

	It("offers an optional single choice a first row that leaves it unset", func() {
		js := read("wizard.js")
		// The row is added by code that checks the field is optional and
		// has no empty choice of its own; a comment cannot satisfy this.
		Expect(js).To(MatchRegexp(`canLeave = !f\.required && !f\.choices\.some\(c => c\.value === ''\)`))
		Expect(js).To(MatchRegexp(`\{ value: '', label: 'Leave unset'`))
	})

	It("reads a provider gate back as yes when its section is set", func() {
		js := read("wizard.js")
		Expect(js).To(ContainSubstring("if (id.endsWith('" + wizard.AskSuffix + "')) return String(providerValue(id.slice(0, -" + fmt.Sprint(len(wizard.AskSuffix)) + ")) !== '');"))
	})

	It("makes the review read only and hides Regenerate when the branding switch is on", func() {
		js := read("wizard.js")
		Expect(js).To(ContainSubstring("state.advancedDisabled = !!r.advanced_disabled;"))
		Expect(js).To(ContainSubstring("area.readOnly = state.advancedDisabled;"))
		Expect(js).To(MatchRegexp(`state\.advancedDisabled \? null : el\('button', \{ type: 'button', class: 'btn ghost', onclick: regenerate \}, 'Regenerate from answers'\)`))
	})
})
