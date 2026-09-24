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

// A write to the ledger is only announced, never committed synchronously: the
// API's PUT handler calls ledger.Persist and answers {"State":"Announcing"},
// and Persist adds the entry from a goroutine driven by a backoff ticker.
// Nothing orders a write before a later read, so every read this file depends
// on has to be an Eventually.
//
// ledgerSettle stays well under the second tick of a Persist that has not
// reconciled yet (the backoff starts at 5s with a 0.5 randomization factor,
// so the earliest retry is around 2.5s). Confirming a seed inside that window
// means the scheduler's own write cannot be undone by the seed re-announcing
// its old value on top of it.
const (
	ledgerSettle = 2 * time.Second
	ledgerPoll   = 20 * time.Millisecond
)

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

// seedRole writes a role and does not return until it is in the ledger. A
// scheduler run started on an uncommitted seed reads "" for that node and
// schedules against a ledger that does not exist yet.
func seedRole(client *service.Client, uuid, role string) {
	GinkgoHelper()
	Expect(client.Set("role", uuid, role)).To(Succeed())
	Eventually(func() string { return roleOf(client, uuid) }, ledgerSettle, ledgerPoll).
		Should(Equal(role), "the %q seed for %s never committed", role, uuid)
}

// expectRole waits for a node to hold the given role. Used after
// scheduleRoles, whose writes go through the same announcing PUT.
func expectRole(client *service.Client, uuid, role string, description string) {
	GinkgoHelper()
	Eventually(func() string { return roleOf(client, uuid) }, ledgerSettle, ledgerPoll).
		Should(Equal(role), description)
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
		seedRole(client, "stuck-uuid", providerConfig.RoleNone)

		Expect(scheduleRoles([]string{"stuck-uuid"}, leader, cc, pconfig)).To(Succeed())

		expectRole(client, "stuck-uuid", "master",
			"the only unassigned node has to become the master")
	})

	It("gives a second stale-none node a worker role once a master exists", func() {
		seedRole(client, "master-uuid", "master")
		seedRole(client, "stuck-uuid", providerConfig.RoleNone)

		Expect(scheduleRoles([]string{"master-uuid", "stuck-uuid"}, leader, cc, pconfig)).To(Succeed())

		expectRole(client, "stuck-uuid", "worker",
			"the stale-none node has to take the free worker slot")
		expectRole(client, "master-uuid", "master",
			"the existing master has to be left where it is")
	})

	It("leaves an already-assigned worker alone", func() {
		seedRole(client, "master-uuid", "master")
		seedRole(client, "worker-uuid", "worker")

		Expect(scheduleRoles([]string{"master-uuid", "worker-uuid"}, leader, cc, pconfig)).To(Succeed())

		expectRole(client, "master-uuid", "master", "")
		expectRole(client, "worker-uuid", "worker", "")
	})

	It("never re-publishes none back into the ledger", func() {
		// The re-publish loop exists so a lost gossipsub message cannot strand
		// a worker. It must not keep a pre-#4670 value alive: whatever a tick
		// leaves behind for a stale-none node, it cannot be `none`.
		seedRole(client, "master-uuid", "master")
		seedRole(client, "stuck-uuid", providerConfig.RoleNone)

		for i := 0; i < 3; i++ {
			Expect(scheduleRoles([]string{"master-uuid", "stuck-uuid"}, leader, cc, pconfig)).To(Succeed())
			// Asserting only "not none" would pass on an empty read, which is
			// what an uncommitted write looks like. Pin the role instead.
			expectRole(client, "stuck-uuid", "worker",
				"a re-publish tick must leave a real role behind, never none")
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
