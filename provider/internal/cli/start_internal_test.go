package cli_test

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ipfs/go-log"
	. "github.com/kairos-io/kairos/v4/provider/internal/cli"
	"github.com/kairos-io/kairos/v4/provider/internal/provider"
	edgevpnapi "github.com/mudler/edgevpn/api"
	edgevpnclient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/edgevpn/pkg/blockchain"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("EdgeVPN API CLI wiring", func() {
	It("defaults role commands to the EdgeVPN Unix socket", func() {
		apiFlag, ok := RoleCMD.Subcommands[0].Flags[0].(*cli.StringFlag)
		Expect(ok).To(BeTrue())
		Expect(apiFlag.Value).To(Equal(provider.DefaultEdgeVPNAPIAddress))
		Expect(apiFlag.EnvVars).To(Equal([]string{"EDGEVPN_API"}))
	})

	It("registers rotate-token with the documented defaults", func() {
		app := NewApp()
		var cmd *cli.Command
		for _, c := range app.Commands {
			if c.Name == "rotate-token" {
				cmd = c
				break
			}
		}
		Expect(cmd).NotTo(BeNil(), "rotate-token should be reachable from the provider CLI")

		flags := map[string]cli.Flag{}
		for _, f := range cmd.Flags {
			for _, n := range f.Names() {
				flags[n] = f
			}
		}
		dirs, ok := flags["config-dir"].(*cli.StringSliceFlag)
		Expect(ok).To(BeTrue())
		Expect(dirs.Value.Value()).To(Equal([]string{"/etc/kairos", "/oem"}))

		rootDir, ok := flags["root-dir"].(*cli.StringFlag)
		Expect(ok).To(BeTrue())
		Expect(rootDir.Value).To(Equal("/"))

		restart, ok := flags["restart"].(*cli.BoolFlag)
		Expect(ok).To(BeTrue())
		Expect(restart.Value).To(BeFalse())

		Expect(flags).To(HaveKey("api"))
	})

	It("rejects rotate-token when no new token is supplied", func() {
		app := NewApp()
		err := app.Run([]string{"kairos", "rotate-token"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("token is required"))
	})

	It("dials a running EdgeVPN API through a Unix socket", func() {
		socket := filepath.Join(GinkgoT().TempDir(), "edgevpn.sock")
		address := "unix://" + socket
		token := node.GenerateNewConnectionData().Base64()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		edgeNode, err := node.New(
			node.FromBase64(true, true, token, nil, nil),
			node.WithStore(&blockchain.MemoryStore{}),
			node.Logger(logger.New(log.LevelFatal)),
		)
		Expect(err).NotTo(HaveOccurred())
		edgeNode.Start(ctx)

		go func() {
			defer GinkgoRecover()
			err := edgevpnapi.API(ctx, address, 10*time.Second, 20*time.Second, edgeNode, nil, false)
			Expect(err).NotTo(HaveOccurred())
		}()

		client := edgevpnclient.NewClient(edgevpnclient.WithHost(address))
		Eventually(func() error {
			return client.Put("socket-verification", "readiness", "ready")
		}, 10*time.Second, 100*time.Millisecond).ShouldNot(HaveOccurred())

		app := cli.NewApp()
		app.Commands = []*cli.Command{&RoleCMD}
		err = app.Run([]string{
			"kairos", "role", "set",
			"--api", address,
			"--network-id", "socket-verification",
			"node-1", "master",
		})
		Expect(err).NotTo(HaveOccurred())

		serviceClient := service.NewClient("socket-verification", client)
		Eventually(func() (string, error) {
			return serviceClient.Get("role", "node-1")
		}, 10*time.Second, 100*time.Millisecond).Should(Equal("master"))
		fmt.Fprint(GinkgoWriter, "verified role command over ", address)
	})
})
