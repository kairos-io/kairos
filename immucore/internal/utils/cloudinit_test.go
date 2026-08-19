package utils_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"

	"github.com/kairos-io/immucore/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4"
	"github.com/twpayne/go-vfs/v4/vfst"
	"gopkg.in/yaml.v3"
)

// These specs exercise the cmdline-parsing helpers that immucore inherits from
// kairos-sdk (PR #812). The goal is to guarantee immucore stays wired to the
// SDK parser so the modern kairos.config= / kairos.config_url= stanzas and the
// legacy cos.setup= alias all resolve consistently.
var _ = Describe("Kairos cmdline parsing (kairos-sdk integration)", func() {
	var fs vfs.FS
	var cleanup func()

	writeCmdline := func(s string) {
		Expect(fs.WriteFile("/proc/cmdline", []byte(s), os.ModePerm)).To(Succeed())
	}

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{ "/proc/cmdline": "" })
		Expect(err).ToNot(HaveOccurred())
		path, err := fs.RawPath("/proc/cmdline")
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Setenv("HOST_PROC_CMDLINE", path)).To(Succeed())
	})
	AfterEach(func() {
		_ = os.Unsetenv("HOST_PROC_CMDLINE")
		cleanup()
	})

	Context("KairosConfigURIFromCmdline (file-backed)", func() {
		It("extracts URI from kairos.config_url=", func() {
			writeCmdline("root=LABEL=X kairos.config_url=https://example.com/cfg.yaml quiet")
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal("https://example.com/cfg.yaml"))
		})
		It("extracts URI from legacy bare cos.setup=", func() {
			writeCmdline("root=LABEL=X cos.setup=https://example.com/legacy.yaml quiet")
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal("https://example.com/legacy.yaml"))
		})
		It("returns empty when only nested kairos.config= stanzas are present", func() {
			// kairos.config=key=value is consumed by kairos-agent, not immucore, so
			// there is no yip source URI to hand off — expect empty.
			writeCmdline("kairos.config=install.auto=true kairos.config=users.0.name=kairos")
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal(""))
		})
		It("returns empty when cmdline contains no Kairos-owned stanzas", func() {
			writeCmdline("root=LABEL=X rd.immucore.debug quiet console=ttyS0")
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal(""))
		})
		It("respects HOST_PROC_CMDLINE and returns empty on missing cmdline file", func() {
			path, err := fs.RawPath("/proc/cmdline")
			Expect(err).ToNot(HaveOccurred())
			Expect(os.Setenv("HOST_PROC_CMDLINE", path)).To(Succeed())

			writeCmdline("root=LABEL=X kairos.config_url=https://example.com/from-env.yaml quiet")
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal("https://example.com/from-env.yaml"))

			Expect(os.Setenv("HOST_PROC_CMDLINE", "/definitely/not/a/real/path/proc/cmdline")).To(Succeed())
			Expect(utils.KairosConfigURIFromCmdline()).To(Equal(""))
		})
	})

	Context("KairosConfigURIFromString (all variants)", func() {
		DescribeTable("resolves the config URI",
			func(cmdline, expected string) {
				Expect(utils.KairosConfigURIFromString(cmdline)).To(Equal(expected))
			},
			Entry("modern kairos.config_url=URL",
				"kairos.config_url=https://x.example/cfg.yaml", "https://x.example/cfg.yaml"),
			Entry("legacy bare cos.setup=URL",
				"cos.setup=https://x.example/legacy.yaml", "https://x.example/legacy.yaml"),
			Entry("legacy bare cos.setup=file path",
				"cos.setup=/oem/99_custom.yaml", "/oem/99_custom.yaml"),
			Entry("cos.setup= with key=value routes into nested config, not URI",
				"cos.setup=install.auto=true", ""),
			Entry("kairos.config_url wins on collision with cos.setup",
				// Later occurrence wins per SDK contract; kairos.config_url and
				// bare cos.setup both write to config_url so order matters.
				"cos.setup=/first.yaml kairos.config_url=/second.yaml", "/second.yaml"),
			Entry("multiple cos.setup= — last one wins",
				"cos.setup=/first.yaml cos.setup=/second.yaml", "/second.yaml"),
			Entry("quoted URL with spaces",
				`kairos.config_url="https://x.example/path with space.yaml"`, "https://x.example/path with space.yaml"),
			Entry("empty payload is ignored",
				"kairos.config_url= root=LABEL=X", ""),
			Entry("no Kairos-owned tokens",
				"root=LABEL=X rd.immucore.debug console=ttyS0", ""),
			Entry("kairos.config=key=value alone yields no URI",
				"kairos.config=install.auto=true kairos.config=users.0.name=kairos", ""),
			Entry("mix of nested kairos.config= and kairos.config_url= — URI still resolved",
				"kairos.config=install.auto=true kairos.config_url=https://x.example/cfg.yaml",
				"https://x.example/cfg.yaml"),
		)
	})

	// RunStage is the actual initramfs entrypoint. Since kairos-sdk PR #820 the
	// kairos.config_url= URI is templated through collector.RenderConfigURL
	// before yip fetches it, so operators can inject per-machine values
	// (SMBIOS UUID, MAC, hostname, ...) into the discovery URL. These specs
	// drive RunStage end-to-end via an httptest.Server so the assertion sees
	// the exact URL yip fetched.
	Context("RunStage config_url template rendering", func() {
		type serverState struct {
			mu   sync.Mutex
			hits []string
		}
		var (
			state *serverState
			srv   *httptest.Server
		)

		BeforeEach(func() {
			state = &serverState{}
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state.mu.Lock()
				state.hits = append(state.hits, r.URL.RequestURI())
				state.mu.Unlock()
				// Minimal valid yip config so the DAG runs without producing
				// noisy load errors. The stage body itself is a no-op.
				_, _ = io.WriteString(w, "stages:\n  initramfs:\n    - name: noop\n")
			}))
		})
		AfterEach(func() {
			srv.Close()
		})

		snapshotHits := func() []string {
			state.mu.Lock()
			defer state.mu.Unlock()
			out := make([]string, len(state.hits))
			copy(out, state.hits)
			return out
		}

		It("renders template variables before the URL reaches yip", func() {
			// A bare string literal piped through Sprig's upper keeps the
			// spec fully deterministic on any host (the sysinfo-context
			// path is covered at the SDK level with a mocked context).
			// The pipe also makes it obvious to a skeptical reader that
			// templating actually ran: if RenderConfigURL was bypassed,
			// yip would receive the raw {{ ... }} markers and this
			// assertion would fail with an unmistakable diff.
			writeCmdline(fmt.Sprintf(
				`root=LABEL=X kairos.config_url=%q`,
				srv.URL+`/?h={{ "hello" | upper }}`,
			))
			Expect(utils.RunStage("initramfs")).To(BeNil())

			// RunStage exercises the URI once each for stageBefore, stage,
			// and stageAfter, so the server must see exactly three identical
			// hits. Asserting the count catches regressions that drop one
			// or two of the sub-stages.
			hits := snapshotHits()
			Expect(hits).To(HaveLen(3), "expected one fetch per stageBefore/stage/stageAfter")
			Expect(hits).To(HaveEach("/?h=HELLO"))
		})

		It("skips the fetch when the template references an unknown value", func() {
			writeCmdline(fmt.Sprintf(
				`root=LABEL=X kairos.config_url="%s/should-not-be-hit?u={{ .Values.definitely_not_a_key }}"`,
				srv.URL,
			))
			// RunStage currently returns nil unconditionally (see the comment
			// in the source about not deciding yet which errors are
			// permissible). We assert on the side effect that matters: no
			// HTTP fetch happens.
			Expect(utils.RunStage("initramfs")).To(BeNil())
			Expect(snapshotHits()).To(BeEmpty(),
				"yip must not fetch a URL whose template failed to render")
		})

		It("passes a non-templated URL through unchanged (regression)", func() {
			writeCmdline(fmt.Sprintf(
				`root=LABEL=X kairos.config_url=%s/plain`, srv.URL,
			))
			Expect(utils.RunStage("initramfs")).To(BeNil())

			hits := snapshotHits()
			Expect(hits).ToNot(BeEmpty(), "plain URLs must still reach yip")
			for _, uri := range hits {
				Expect(uri).To(Equal("/plain"))
			}
		})
	})

	Context("SDKDotNotationModifier", func() {
		It("converts generic dot-nested tokens to YAML", func() {
			out, err := utils.SDKDotNotationModifier([]byte("foo.bar=baz nested.deep.key=1"))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
			Expect(m).To(HaveKey("foo"))
			Expect(m["foo"]).To(HaveKeyWithValue("bar", "baz"))
			Expect(m).To(HaveKey("nested"))
		})
		It("skips Kairos-owned prefixes so they are not double-processed", func() {
			// The whole point of routing through the SDK: kairos.config=,
			// kairos.config_url= and cos.setup= are handled elsewhere and MUST
			// NOT leak into the generic dot-nested output as spurious top-level
			// keys.
			out, err := utils.SDKDotNotationModifier([]byte(
				"foo.bar=baz kairos.config=install.auto=true kairos.config_url=http://x/y cos.setup=/z",
			))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
			Expect(m).To(HaveKey("foo"))
			Expect(m).ToNot(HaveKey("kairos"))
			Expect(m).ToNot(HaveKey("cos"))
			Expect(m).ToNot(HaveKey("config_url"))
		})
		It("returns valid YAML for empty input", func() {
			out, err := utils.SDKDotNotationModifier([]byte(""))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
		})
	})
})
