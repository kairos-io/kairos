package cmd_test

import (
	"github.com/kairos-io/AuroraBoot/internal/cmd"
	"github.com/urfave/cli/v2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("internal/cmd.SysextCmd flags", func() {
	findFlag := func(name string) cli.Flag {
		for _, f := range cmd.SysextCmd.Flags {
			for _, n := range f.Names() {
				if n == name {
					return f
				}
			}
		}
		return nil
	}

	It("declares --include-path as a repeatable string-slice flag", func() {
		f := findFlag("include-path")
		Expect(f).ToNot(BeNil(), "expected --include-path flag")
		_, ok := f.(*cli.StringSliceFlag)
		Expect(ok).To(BeTrue(), "--include-path should be a StringSliceFlag")
	})

	It("keeps --with-opt as a (now-deprecated) bool flag", func() {
		f := findFlag("with-opt")
		Expect(f).ToNot(BeNil(), "expected --with-opt to remain for backward compat")
		_, ok := f.(*cli.BoolFlag)
		Expect(ok).To(BeTrue(), "--with-opt should remain a BoolFlag")
	})
})
