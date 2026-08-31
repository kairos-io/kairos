package cli_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/ipfs/go-log"
	. "github.com/kairos-io/provider-kairos/v2/internal/cli"
	edgevpnapi "github.com/mudler/edgevpn/api"
	edgevpnclient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/pkg/blockchain"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

func runCommand(cmd *cli.Command, args ...string) error {
	app := cli.NewApp()
	app.Commands = []*cli.Command{cmd}
	return app.Run(append([]string{"kairos"}, args...))
}

// startAPIOn brings up a real edgevpn API on a unix socket with an empty
// ledger, so the commands below reach a live daemon that simply has nothing
// stored yet.
func startAPIOn(networkID string) string {
	address := "unix://" + filepath.Join(GinkgoT().TempDir(), "edgevpn.sock")
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	edgeNode, err := node.New(
		node.FromBase64(true, true, node.GenerateNewConnectionData().Base64(), nil, nil),
		node.WithStore(&blockchain.MemoryStore{}),
		node.Logger(logger.New(log.LevelFatal)),
	)
	Expect(err).NotTo(HaveOccurred())
	edgeNode.Start(ctx) //nolint:errcheck

	go func() {
		defer GinkgoRecover()
		_ = edgevpnapi.API(ctx, address, 10*time.Second, 20*time.Second, edgeNode, nil, false)
	}()

	client := edgevpnclient.NewClient(edgevpnclient.WithHost(address))
	Eventually(func() error {
		return client.Put(networkID, "readiness", "ready")
	}, 10*time.Second, 100*time.Millisecond).ShouldNot(HaveOccurred())

	return address
}

var _ = Describe("Reporting API failures", func() {
	// These commands used to discard the client error and print an empty
	// answer with a success exit code, so an unreachable daemon was
	// indistinguishable from a cluster that had not published anything yet.
	// The two need different things from whoever is looking, so they have to
	// read differently.

	Context("when the daemon cannot be reached", func() {
		var unreachable string

		BeforeEach(func() {
			unreachable = "unix://" + filepath.Join(GinkgoT().TempDir(), "absent.sock")
		})

		It("get-kubeconfig says so, and names the address it tried", func() {
			err := runCommand(&GetKubeConfigCMD, "get-kubeconfig", "--api", unreachable)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot reach the edgevpn API"))
			Expect(err.Error()).To(ContainSubstring(unreachable))
		})

		It("role list says so, and names the address it tried", func() {
			err := runCommand(&RoleCMD, "role", "list", "--api", unreachable)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot reach the edgevpn API"))
			Expect(err.Error()).To(ContainSubstring(unreachable))
		})
	})

	Context("when the daemon is reachable but has nothing stored", func() {
		It("get-kubeconfig reports the missing kubeconfig, not a connection problem", func() {
			address := startAPIOn("empty-network")
			err := runCommand(&GetKubeConfigCMD,
				"get-kubeconfig", "--api", address, "--network-id", "empty-network")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("kubeconfig"))
			Expect(err.Error()).NotTo(ContainSubstring("cannot reach"))
		})
	})
})
