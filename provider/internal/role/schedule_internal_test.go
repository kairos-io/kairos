package role

import (
	"context"
	"path/filepath"
	"time"

	"github.com/ipfs/go-log"
	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	edgevpnapi "github.com/mudler/edgevpn/api"
	edgevpnclient "github.com/mudler/edgevpn/api/client"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/edgevpn/pkg/blockchain"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testNetworkID = "role-scheduling"

// startLedger brings up a real edgevpn API over a unix socket, backed by an
// in-memory ledger. The scheduler reads and writes roles through the same
// client the provider uses at runtime, so the encoding of a ledger entry is
// the real one rather than a fake's idea of it.
func startLedger() *service.Client {
	address := "unix://" + filepath.Join(GinkgoT().TempDir(), "edgevpn.sock")
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	edgeNode, err := node.New(
		node.FromBase64(true, true, node.GenerateNewConnectionData().Base64(), nil, nil),
		node.WithStore(&blockchain.MemoryStore{}),
		node.Logger(logger.New(log.LevelFatal)),
	)
	Expect(err).NotTo(HaveOccurred())
	Expect(edgeNode.Start(ctx)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		_ = edgevpnapi.API(ctx, address, 10*time.Second, 20*time.Second, edgeNode, nil, false)
	}()

	client := service.NewClient(testNetworkID, edgevpnclient.NewClient(edgevpnclient.WithHost(address)))
	Eventually(func() error {
		return client.Set("readiness", "probe", "ready")
	}, 10*time.Second, 100*time.Millisecond).ShouldNot(HaveOccurred())

	return client
}

func roleOf(client *service.Client, uuid string) string {
	role, _ := client.Get("role", uuid)
	return role
}

var _ = Describe("Role scheduling", func() {
	var (
		client  *service.Client
		leader  *service.RoleConfig
		pconfig *providerConfig.Config
		cc      *sdkConfig.Config
	)

	BeforeEach(func() {
		client = startLedger()
		leader = &service.RoleConfig{
			Client: client,
			UUID:   "leader-uuid",
			Logger: log.Logger("role-test"),
		}
		pconfig = &providerConfig.Config{P2P: &providerConfig.P2P{}}
		cc = &sdkConfig.Config{}
	})

	// A node running a binary from before the fix for #4670 wrote the
	// configured `p2p.role: none` straight into the ledger, and only then did
	// NewK8sNode refuse it. Every cluster that hit that bug therefore has
	// `role/<UUID> = none` entries which were never an assignment. Until the
	// leader reads them as unassigned, upgrading the binary is not enough:
	// nothing ever writes a real role over them and the node stays wedged.
	It("assigns a real role over a stale none left by an older binary", func() {
		Expect(client.Set("role", "stuck-uuid", providerConfig.RoleNone)).To(Succeed())
		Expect(roleOf(client, "stuck-uuid")).To(Equal(providerConfig.RoleNone))

		Expect(scheduleRoles([]string{"stuck-uuid"}, leader, cc, pconfig)).To(Succeed())

		Expect(roleOf(client, "stuck-uuid")).To(Equal("master"),
			"the only unassigned node has to become the master")
	})

	It("gives a second stale-none node a worker role once a master exists", func() {
		Expect(client.Set("role", "master-uuid", "master")).To(Succeed())
		Expect(client.Set("role", "stuck-uuid", providerConfig.RoleNone)).To(Succeed())

		Expect(scheduleRoles([]string{"master-uuid", "stuck-uuid"}, leader, cc, pconfig)).To(Succeed())

		Expect(roleOf(client, "master-uuid")).To(Equal("master"))
		Expect(roleOf(client, "stuck-uuid")).To(Equal("worker"))
	})

	It("leaves an already-assigned worker alone", func() {
		Expect(client.Set("role", "master-uuid", "master")).To(Succeed())
		Expect(client.Set("role", "worker-uuid", "worker")).To(Succeed())

		Expect(scheduleRoles([]string{"master-uuid", "worker-uuid"}, leader, cc, pconfig)).To(Succeed())

		Expect(roleOf(client, "master-uuid")).To(Equal("master"))
		Expect(roleOf(client, "worker-uuid")).To(Equal("worker"))
	})

	It("never re-publishes none back into the ledger", func() {
		// The re-publish loop exists so a lost gossipsub message cannot strand
		// a worker. It must not keep a pre-#4670 value alive: whatever a tick
		// leaves behind for a stale-none node, it cannot be `none`.
		Expect(client.Set("role", "master-uuid", "master")).To(Succeed())
		Expect(client.Set("role", "stuck-uuid", providerConfig.RoleNone)).To(Succeed())

		for i := 0; i < 3; i++ {
			Expect(scheduleRoles([]string{"master-uuid", "stuck-uuid"}, leader, cc, pconfig)).To(Succeed())
			Expect(roleOf(client, "stuck-uuid")).NotTo(Equal(providerConfig.RoleNone))
		}
	})

	Describe("unassignedRole", func() {
		It("treats the empty string and none as needing an assignment", func() {
			Expect(unassignedRole("")).To(BeTrue())
			Expect(unassignedRole(providerConfig.RoleNone)).To(BeTrue())
		})

		It("treats every real role as assigned", func() {
			for _, r := range []string{"master", "master/ha", "master/clusterinit", "worker"} {
				Expect(unassignedRole(r)).To(BeFalse(), r)
			}
		})
	})
})
