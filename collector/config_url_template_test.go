package collector_test

import (
	"os"

	. "github.com/kairos-io/kairos-sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RenderConfigURL", func() {
	// mockContext swaps the package-level buildContext seam so specs can
	// exercise deterministic values. When escape is true the map is walked
	// through the same URL-escaping the real builder applies, so specs can
	// assert on the resulting escaped output.
	mockContext := func(raw map[string]interface{}, escape bool) {
		DeferCleanup(SetBuildContextForTest(func() (map[string]interface{}, error) {
			if escape {
				return WalkAndEscape(raw), nil
			}
			return raw, nil
		}))
	}

	Describe("fast path", func() {
		It("returns non-templated URLs unchanged without building context", func() {
			DeferCleanup(SetBuildContextForTest(func() (map[string]interface{}, error) {
				Fail("buildContext must not be called on the passthrough fast path")
				return nil, nil
			}))

			in := "http://x/"
			out, err := RenderConfigURL(in)
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(in))
		})
	})

	Describe("substitution", func() {
		It("resolves a single template expression", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": "abc-123"},
				},
			}, false)

			out, err := RenderConfigURL("?uuid={{ .Values.product.uuid }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("?uuid=abc-123"))
		})

		It("URL-escapes scalar values before substitution", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"serial": "AB CD+EF"},
				},
			}, true)

			out, err := RenderConfigURL("?serial={{ .Values.product.serial }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("?serial=AB+CD%2BEF"))
		})

		It("resolves multiple variables in one URL", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{
						"uuid":   "abc-123",
						"serial": "SN42",
					},
					"node": map[string]interface{}{"hostname": "boxen"},
				},
			}, false)

			out, err := RenderConfigURL("http://d/?uuid={{ .Values.product.uuid }}&serial={{ .Values.product.serial }}&h={{ .Values.node.hostname }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("http://d/?uuid=abc-123&serial=SN42&h=boxen"))
		})

		It("supports Sprig template functions", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": "abc-123"},
				},
			}, false)

			out, err := RenderConfigURL("?u={{ .Values.product.uuid | upper }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("?u=ABC-123"))
		})
	})

	Describe("errors", func() {
		It("fails when the template references an undefined field", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": "abc-123"},
				},
			}, false)

			_, err := RenderConfigURL("?x={{ .Values.nope }}")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("config_url"))
		})

		It("fails when a substitution resolves to an empty string", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": ""},
				},
			}, false)

			_, err := RenderConfigURL("?uuid={{ .Values.product.uuid }}")
			Expect(err).To(HaveOccurred())
		})

		It("fails on a malformed template", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": "abc-123"},
				},
			}, false)

			_, err := RenderConfigURL("?u={{ .Values.product.uuid")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("config_url"))
		})
	})

	Describe("default context", func() {
		It("resolves Random and ProtectedID to non-empty values", func() {
			out, err := RenderConfigURL("?r={{ .Values.Random }}&p={{ .Values.ProtectedID }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(HavePrefix("?r="))
			Expect(out).To(ContainSubstring("&p="))
			Expect(out).ToNot(ContainSubstring("?r=&"))
			Expect(out).ToNot(HaveSuffix("&p="))
		})

		It("resolves the host hostname when the system reports one", func() {
			hn, err := os.Hostname()
			if err != nil || hn == "" {
				Skip("hostname unavailable on this host")
			}
			out, err := RenderConfigURL("?u={{ .Values.node.hostname }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(HavePrefix("?u="))
			Expect(len(out)).To(BeNumerically(">", len("?u=")))
		})
	})
})
