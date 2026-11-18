package cmd_test

import (
	"bytes"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("root command", Label("cmd"), func() {
	It("rejects an invocation without an image or release source", func() {
		app := cmdpkg.GetApp("v0.0.0")
		app.Writer = new(bytes.Buffer)

		err := app.Run([]string{"auroraboot"})

		Expect(err).To(MatchError("no source defined: provide container_image or a complete release artifact configuration"))
	})

	It("rejects an incomplete release source", func() {
		app := cmdpkg.GetApp("v0.0.0")
		app.Writer = new(bytes.Buffer)

		err := app.Run([]string{"auroraboot", "--set", "repository=kairos-io/kairos"})

		Expect(err).To(MatchError("no source defined: provide container_image or a complete release artifact configuration"))
	})
})
