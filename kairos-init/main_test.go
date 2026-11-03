package main

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = Describe("provider flags", func() {
	DescribeTable("getProvidersFromArgs reads every form --provider accepts",
		func(args []string, expected []string) {
			Expect(getProvidersFromArgs(args)).To(Equal(expected))
		},
		Entry("separate token", []string{"--provider", "k3s"}, []string{"k3s"}),
		Entry("short separate token", []string{"-p", "k3s"}, []string{"k3s"}),
		Entry("equals", []string{"--provider=k3s"}, []string{"k3s"}),
		Entry("short equals", []string{"-p=k3s"}, []string{"k3s"}),
		Entry("attached shorthand", []string{"-pk3s"}, []string{"k3s"}),
		Entry("comma list", []string{"--provider", "k3s,k0s"}, []string{"k3s", "k0s"}),
		Entry("comma list with equals", []string{"--provider=k3s,k0s"}, []string{"k3s", "k0s"}),
		Entry("repeated and duplicated", []string{"-p", "k3s", "--provider", "k0s,k3s"}, []string{"k3s", "k0s"}),
		Entry("stops at --", []string{"-p", "k3s", "--", "-pk0s"}, []string{"k3s"}),
		Entry("no provider", []string{"--version", "v4.0.0", "--provider-k3s-version", "v1"}, nil),
	)

	DescribeTable("the per-provider flags parse for each provider",
		func(args ...string) {
			cmd := &cobra.Command{Use: "test"}
			var provs []string
			cmd.Flags().StringSliceVarP(&provs, "provider", "p", []string{}, "")
			registerProviderFlags(cmd, args)

			Expect(cmd.ParseFlags(args)).To(Succeed())
			Expect(provs).To(ContainElements("k3s", "k0s"))
			Expect(cmd.Flags().Lookup("provider-k3s-version").Value.String()).To(Equal("v1.30.0"))
			Expect(cmd.Flags().Lookup("provider-k0s-config").Value.String()).To(Equal("/x.yaml"))
		},
		Entry("comma list", "--provider", "k3s,k0s", "--provider-k3s-version", "v1.30.0", "--provider-k0s-config", "/x.yaml"),
		Entry("comma list with equals", "--provider=k3s,k0s", "--provider-k3s-version", "v1.30.0", "--provider-k0s-config", "/x.yaml"),
		Entry("attached shorthand", "-pk3s", "-pk0s", "--provider-k3s-version", "v1.30.0", "--provider-k0s-config", "/x.yaml"),
		Entry("duplicated provider", "-p", "k3s", "-p", "k0s,k3s", "--provider-k3s-version", "v1.30.0", "--provider-k0s-config", "/x.yaml"),
	)
})
