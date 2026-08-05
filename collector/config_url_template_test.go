package collector_test

import (
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
			Expect(err.Error()).To(ContainSubstring("rendering config_url: execute"))
			Expect(err.Error()).To(ContainSubstring("nope"))
		})

		It("fails on a malformed template", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": "abc-123"},
				},
			}, false)

			_, err := RenderConfigURL("?u={{ .Values.product.uuid")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rendering config_url: parse"))
		})
	})

	Describe("empty values", func() {
		// Design choice: RenderConfigURL does not reject empty renders on its
		// own. A user piping through Sprig's required opts into the check;
		// otherwise a defined-but-empty field renders empty.

		It("renders empty when a bare substitution resolves to empty", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": ""},
				},
			}, false)

			out, err := RenderConfigURL("?uuid={{ .Values.product.uuid }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("?uuid="))
		})

		It("fails when a Sprig required field is empty", func() {
			mockContext(map[string]interface{}{
				"Values": map[string]interface{}{
					"product": map[string]interface{}{"uuid": ""},
				},
			}, false)

			_, err := RenderConfigURL(`?uuid={{ required "uuid is required for discovery" .Values.product.uuid }}`)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("uuid is required for discovery"))
		})
	})

	Describe("context values", func() {
		Describe("passthrough of context-provided values", func() {
			It("passes Random and ProtectedID through unchanged", func() {
				mockContext(map[string]interface{}{
					"Values": map[string]interface{}{
						"Random":      "deadbeefcafebabefeedfacefacefeed",
						"ProtectedID": "protected-id-value",
					},
				}, false)

				out, err := RenderConfigURL("?r={{ .Values.Random }}&p={{ .Values.ProtectedID }}")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(Equal("?r=deadbeefcafebabefeedfacefacefeed&p=protected-id-value"))
			})

			It("passes node.hostname through unchanged (after URL-escape)", func() {
				mockContext(map[string]interface{}{
					"Values": map[string]interface{}{
						"node": map[string]interface{}{"hostname": "boxen"},
					},
				}, true)

				out, err := RenderConfigURL("?u={{ .Values.node.hostname }}")
				Expect(err).ToNot(HaveOccurred())
				Expect(out).To(Equal("?u=boxen"))
			})
		})

		// Smoke check that the real defaultBuildContext is still callable.
		// We do not assert on any specific value: /etc/machine-id and
		// sysinfo output vary (or are unavailable) on some hosts. Random
		// is populated unconditionally so the substitution check passes.
		It("defaultBuildContext returns without error", func() {
			out, err := RenderConfigURL("?r={{ .Values.Random }}")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(HavePrefix("?r="))
			Expect(len(out)).To(BeNumerically(">", len("?r=")))
		})
	})
})
