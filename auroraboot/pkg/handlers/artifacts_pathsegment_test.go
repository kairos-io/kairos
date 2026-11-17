package handlers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("safePathSegment", func() {
	It("accepts values that URL params legitimately produce", func() {
		for _, s := range []string{
			"dc7d2174-8cc1-4d9a-96e1-23f2e87ac8b4",
			"kairos.iso",
			"kairos.iso.sha256",
			"container.tar",
			"a.b.c-1",
		} {
			Expect(safePathSegment(s)).To(Succeed(), "value %q should be accepted", s)
		}
	})

	It("rejects empties and traversal-shaped inputs", func() {
		for _, s := range []string{
			"",
			".",
			"..",
			"../other",
			"a/b",
			`a\b`,
			"/abs",
			"foo/../bar",
			`C:\Windows`,
		} {
			Expect(safePathSegment(s)).To(HaveOccurred(), "value %q should be rejected", s)
		}
	})
})
