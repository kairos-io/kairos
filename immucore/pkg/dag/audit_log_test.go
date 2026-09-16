package dag_test

import (
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/pkg/dag"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

// layerOf returns the index of the layer a step is scheduled in. herd runs one
// layer at a time and waits for it, so a higher index is a hard guarantee that
// the step runs after every step in the layers below it.
func layerOf(g *herd.Graph, name string) int {
	for i, layer := range g.Analyze() {
		for _, op := range layer {
			if op.Name == name {
				return i
			}
		}
	}
	return -1
}

var _ = Describe("The audit log mount in the boot graphs", func() {
	// Every boot flavour that has a persistent partition registers the pair,
	// and it has to be wired the same way in all of them.
	for name, register := range map[string]func(*state.State, *herd.Graph) error{
		"normal boot": dag.RegisterNormalBoot,
		"in-RAM boot": dag.RegisterInRAMBoot,
		"UKI boot":    dag.RegisterUKI,
	} {
		register := register

		Context(name, func() {
			var g *herd.Graph

			BeforeEach(func() {
				g = herd.DAG(herd.EnableInit)
				Expect(register(&state.State{Rootdir: "/sysroot"}, g)).To(Succeed())
			})

			It("registers the audit log bind and its auditd requirement", func() {
				Expect(layerOf(g, cnst.OpMountAuditLog)).To(BeNumerically(">", 0))
				Expect(layerOf(g, cnst.OpAuditdMountRequirement)).To(BeNumerically(">", 0))
			})

			It("binds the audit log only after the generic binds", func() {
				// /var/log is one of the PERSISTENT_STATE_PATHS binds, and
				// mounting the parent after the child would shadow the audit
				// mount. A hard dep is what keeps the order.
				entry := g.State(cnst.OpMountAuditLog)
				Expect(entry.Dependencies).To(ContainElement(cnst.OpMountBind))
				Expect(entry.WeakDependencies).ToNot(ContainElement(cnst.OpMountBind))
				Expect(layerOf(g, cnst.OpMountAuditLog)).To(BeNumerically(">", layerOf(g, cnst.OpMountBind)))
			})

			It("writes the auditd requirement even when the binds fail", func() {
				// The drop-in is what stops auditd from logging to the
				// ephemeral directory, so it must not be downstream of
				// anything that can fail and skip it.
				Expect(g.State(cnst.OpAuditdMountRequirement).Dependencies).
					ToNot(ContainElement(cnst.OpMountBind))
				Expect(g.State(cnst.OpAuditdMountRequirement).Dependencies).
					ToNot(ContainElement(cnst.OpMountAuditLog))
			})

			It("orders fstab after the audit log mount, weakly", func() {
				// systemd-fstab-generator turns the entry into the mount unit
				// auditd requires, so the entry has to be in the file. A weak
				// dep keeps a failed audit mount from leaving the system with
				// no fstab at all.
				entry := g.State(cnst.OpWriteFstab)
				Expect(entry.Dependencies).To(ContainElement(cnst.OpMountAuditLog))
				Expect(entry.WeakDependencies).To(ContainElement(cnst.OpMountAuditLog))
				Expect(layerOf(g, cnst.OpWriteFstab)).To(BeNumerically(">", layerOf(g, cnst.OpMountAuditLog)))
			})

			It("orders the initramfs stage after the audit log mount, weakly", func() {
				entry := g.State(cnst.OpInitramfsHook)
				Expect(entry.Dependencies).To(ContainElement(cnst.OpMountAuditLog))
				Expect(entry.WeakDependencies).To(ContainElement(cnst.OpMountAuditLog))
			})
		})
	}

	Context("live media", func() {
		It("registers no audit log mount", func() {
			// Live media has no persistent partition, so there is nothing to
			// bind and nothing for auditd to wait for.
			g := herd.DAG(herd.EnableInit)
			Expect(dag.RegisterLiveMedia(&state.State{Rootdir: "/sysroot"}, g)).To(Succeed())

			Expect(layerOf(g, cnst.OpMountAuditLog)).To(Equal(-1))
			Expect(layerOf(g, cnst.OpAuditdMountRequirement)).To(Equal(-1))
		})
	})
})
