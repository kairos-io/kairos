package cli_test

import (
	"path/filepath"

	. "github.com/kairos-io/kairos/v4/provider/internal/cli"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("role set", func() {
	// The API address points at a socket nobody listens on, so a call that
	// gets past argument checking fails with a dial error rather than
	// reaching a real network.
	runSet := func(args ...string) error {
		address := "unix://" + filepath.Join(GinkgoT().TempDir(), "absent.sock")
		app := cli.NewApp()
		app.Commands = []*cli.Command{&RoleCMD}
		return app.Run(append([]string{
			"kairos", "role", "set", "--api", address, "--network-id", "role-set-test",
		}, args...))
	}

	DescribeTable("rejects a call without exactly a UUID and a role",
		func(args ...string) {
			err := runSet(args...)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("usage: kairos provider role set <UUID> <role>"))
		},
		Entry("no arguments"),
		Entry("only a UUID", "node-1"),
		Entry("an extra argument", "node-1", "master", "worker"),
	)

	It("rejects a role the provider does not register", func() {
		err := runSet("node-1", "mastr")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`unknown role "mastr"`))
		Expect(err.Error()).To(ContainSubstring("worker, master, master/ha, master/clusterinit, auto, none"))
	})

	DescribeTable("passes a known role on to the network",
		func(role string) {
			err := runSet("node-1", role)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("usage:"))
			Expect(err.Error()).NotTo(ContainSubstring("unknown role"))
			Expect(err.Error()).To(ContainSubstring("absent.sock"))
		},
		Entry("worker", "worker"),
		Entry("master", "master"),
		Entry("master/ha", "master/ha"),
		Entry("master/clusterinit", "master/clusterinit"),
		Entry("auto", "auto"),
		Entry("none", "none"),
	)
})
