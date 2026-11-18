//go:build ui

package ui_test

import (
	"io/fs"

	"github.com/kairos-io/AuroraBoot/internal/ui"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("a build with the ui tag", func() {
	// A build that runs the UI build but forgets the tag ships without the
	// web UI. CI runs the tests with the tag, so this catches the reverse:
	// the tag set but no bundle built.
	It("embeds the built index.html", func() {
		_, err := fs.Stat(ui.Assets, "dist/index.html")
		Expect(err).ToNot(HaveOccurred())
	})
})
