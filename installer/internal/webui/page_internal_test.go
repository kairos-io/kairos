package webui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

var _ = Describe("the shared look", func() {
	read := func(name string) string {
		f, err := getFS().Open(name)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		b, err := io.ReadAll(f)
		Expect(err).ToNot(HaveOccurred())
		return string(b)
	}
	pages := []string{"index.html", "progress.html", "message.html"}

	// block returns the declarations of the first rule whose selector is
	// exactly sel, so the light and the dark token sets can be read apart.
	block := func(css, sel string) string {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		Expect(m).ToNot(BeNil(), "no rule for %s in style.css", sel)
		return m[1]
	}
	token := func(decls, name string) string {
		m := regexp.MustCompile(`(?:^|[\s;])` + regexp.QuoteMeta(name) + `:\s*([^;]+);`).FindStringSubmatch(decls)
		if m == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(m[1]))
	}

	It("links one stylesheet and the theme script from every page", func() {
		// The three pages are one flow. A page with its own styles is a page
		// that changes look halfway through an install.
		for _, p := range pages {
			html := read(p)
			Expect(html).To(ContainSubstring(`<link rel="stylesheet" href="/style.css">`), p)
			Expect(html).To(ContainSubstring(`<script src="/theme.js"></script>`), p)
			Expect(html).ToNot(ContainSubstring("<style"), "%s keeps styles of its own", p)
		}
	})

	It("defines AuroraBoot's light and dark tokens", func() {
		// The values of AuroraBoot's ui/src/index.css (:root and .dark).
		css := read("style.css")
		light, dark := block(css, ":root"), block(css, ":root.dark")
		for name, want := range map[string][2]string{
			"--background":         {"#ffffff", "#1a1e25"},
			"--foreground":         {"#0f172a", "#f1f5f9"},
			"--card":               {"#ffffff", "#232830"},
			"--primary":            {"#ee5007", "#ff7442"},
			"--primary-hover":      {"#ff7442", "#ff8a5c"},
			"--primary-foreground": {"#ffffff", "#0f172a"},
			"--primary-soft":       {"#fff0e8", "#2a1a14"},
			"--navy":               {"#03153a", "#111419"},
			"--secondary":          {"#f1f5f9", "#2d3341"},
			"--muted":              {"#f1f5f9", "#2d3341"},
			"--muted-foreground":   {"#64748b", "#94a3b8"},
			"--accent":             {"#f1f5f9", "#2d3341"},
			"--accent-foreground":  {"#0f172a", "#f1f5f9"},
			"--border":             {"#e2e8f0", "#334155"},
			"--input":              {"#e2e8f0", "#334155"},
			"--ring":               {"#ff7442", "#ee5007"},
			"--success":            {"#1a9a52", "#3cc97a"},
			"--warning":            {"#c98a04", "#eab308"},
			"--danger":             {"#d6372c", "#ff5f55"},
			"--danger-foreground":  {"#a8261d", "#ff8a82"},
			"--info":               {"#2f6fe0", "#5d93ff"},
			"--sidebar-accent":     {"#ee5007", "#ff7442"},
		} {
			Expect(token(light, name)).To(Equal(want[0]), "light %s", name)
			Expect(token(dark, name)).To(Equal(want[1]), "dark %s", name)
		}
		Expect(token(light, "--radius")).To(Equal("0.625rem"))
		Expect(token(light, "--font-sans")).To(HavePrefix(`"noto sans", `))
	})

	It("follows the system setting when no script runs, unless a theme was chosen", func() {
		// theme.js sets .dark or .light on <html>. Without it, or before it,
		// the page still has to follow prefers-color-scheme, with the same
		// values as the .dark set.
		css := read("style.css")
		Expect(css).To(MatchRegexp(`@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\.light\) \{`))
		fallback := regexp.MustCompile(`:root:not\(\.light\)\s*\{([^}]*)\}`).FindStringSubmatch(css)
		Expect(fallback).ToNot(BeNil())
		Expect(strings.Join(strings.Fields(fallback[1]), " ")).To(Equal(strings.Join(strings.Fields(block(css, ":root.dark")), " ")))
	})

	It("stores the theme where AuroraBoot stores it, and survives blocked storage", func() {
		js := read("theme.js")
		Expect(js).To(ContainSubstring(`var THEME_KEY = 'auroraboot_theme';`))
		Expect(js).To(ContainSubstring(`classList.toggle('dark', dark)`))
		for _, call := range []string{"localStorage.getItem(THEME_KEY)", "localStorage.setItem(THEME_KEY, m)"} {
			Expect(js).To(MatchRegexp(`try \{[^}]*`+regexp.QuoteMeta(call)), "%s outside a try", call)
		}
		for _, m := range []string{"system", "light", "dark"} {
			Expect(js).To(ContainSubstring(`'` + m + `'`))
		}
	})

	It("requests nothing from the network", func() {
		// The live ISO may have no network. Links a person can follow are
		// fine; a script, stylesheet, image or font fetched from outside is not.
		for _, p := range append(pages, "wizard.js", "theme.js") {
			src := read(p)
			Expect(src).ToNot(MatchRegexp(`<(script|link|img|iframe|source)\b[^>]*\b(src|href)="(https?:)?//`), p)
			Expect(src).ToNot(MatchRegexp(`fetch\(['"]https?:`), p)
		}
		css := read("style.css")
		Expect(css).ToNot(ContainSubstring("@import"))
		Expect(css).ToNot(MatchRegexp(`url\(\s*['"]?(https?:)?//`))
	})

	It("lets a step name wrap in the sidebar rather than cut it short", func() {
		// AuroraBoot's sidebar wraps its labels. At 240px a cut name read
		// "Timezone and keyb..." and "When the install fini...".
		css := read("style.css")
		for _, sel := range []string{".rail button", ".rail .step-title"} {
			for _, m := range regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(sel)+`\s*\{([^}]*)\}`).FindAllStringSubmatch(css, -1) {
				Expect(m[1]).ToNot(ContainSubstring("nowrap"), sel)
				Expect(m[1]).ToNot(ContainSubstring("ellipsis"), sel)
			}
		}
		Expect(css).To(MatchRegexp(`(?m)^\.rail \.step-title\s*\{`))
	})

	It("serves the stylesheet, the theme script and the logo", func() {
		srv := newServer(Options{})
		for path, ctype := range map[string]string{"/style.css": "text/css", "/theme.js": "javascript", "/kairos-logo.svg": "image/svg+xml"} {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(rec.Code).To(Equal(http.StatusOK), path)
			Expect(rec.Header().Get("Content-Type")).To(ContainSubstring(ctype), path)
		}
	})
})
