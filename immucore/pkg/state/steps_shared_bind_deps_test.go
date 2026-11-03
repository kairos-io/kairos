package state_test

import (
	"context"
	"errors"
	"sync"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

// entry finds a node in the analyzed graph by name.
func entry(g *herd.Graph, name string) herd.GraphEntry {
	for _, layer := range g.Analyze() {
		for _, e := range layer {
			if e.Name == name {
				return e
			}
		}
	}

	Fail("no graph entry named " + name)

	return herd.GraphEntry{}
}

var _ = Describe("MountCustomBindsDagStep failure policy", func() {
	var g *herd.Graph
	var order []string
	// herd runs the steps that do not depend on each other on their own
	// goroutines, so recording into order is a concurrent write.
	var orderMu sync.Mutex

	// stub stands in for one of the mount steps OpMountBind is wired to. Each
	// one records that it ran, so the specs can assert the ordering the graph
	// edges have to keep.
	stub := func(name string, err error, deps ...string) {
		opts := []herd.OpOption{herd.WithCallback(func(_ context.Context) error {
			orderMu.Lock()
			order = append(order, name)
			orderMu.Unlock()

			return err
		})}
		if len(deps) > 0 {
			opts = append(opts, herd.WithDeps(deps...))
		}
		Expect(g.Add(name, opts...)).To(Succeed())
	}

	BeforeEach(func() {
		internalUtils.KLog = logger.NewNullLogger()
		g = herd.DAG()
		order = nil
	})

	// The bind list is empty, so the step's loop body never runs and no real
	// mount is attempted. What is under test is the failure policy of the node:
	// whether a mount that did not happen takes the boot down, which the
	// dependency edges and herd.FatalOp decide on their own.
	addBindStep := func() {
		s := &state.State{Rootdir: GinkgoT().TempDir()}
		Expect(s.MountCustomBindsDagStep(g)).To(Succeed())
	}

	// A node that cannot be given its persistent state is a node that would
	// come up writing to the ephemeral overlay. Before this it booted anyway,
	// to a login prompt, and g.Run returned nil because nothing in the chain
	// was fatal.
	It("fails the boot when one custom mount failed", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, errors.New("extra volume has no such label"), cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		err := g.Run(context.Background())

		Expect(err).To(HaveOccurred(), "a half mounted node must not boot")
		Expect(entry(g, cnst.OpMountBind).Executed).To(BeFalse())
	})

	It("fails the boot when an overlay path failed", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, nil, cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, errors.New("overlay mount failed"), cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).ToNot(Succeed())
	})

	// OpLoadConfig is what fills State.BindMounts. With no list there is
	// nothing to bind, which is the same half state as a bind that failed.
	It("fails the boot when the layout config could not be read", func() {
		stub(cnst.OpLoadConfig, errors.New("cannot read /run/cos/cos-layout.env"))
		stub(cnst.OpCustomMounts, nil, cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).ToNot(Succeed())
		Expect(entry(g, cnst.OpMountBind).Executed).To(BeFalse())
	})

	// The operator has to be able to tell which mount broke, not just that the
	// binds were skipped: that string is what reaches the boot failure banner
	// and the summary persisted under the log dir.
	It("names the step that broke in the failure reason", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, errors.New("extra volume has no such label"), cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		s := &state.State{Rootdir: GinkgoT().TempDir()}
		Expect(s.MountCustomBindsDagStep(g)).To(Succeed())

		Expect(g.Run(context.Background())).ToNot(Succeed())

		reason := s.FailureReason(g)
		Expect(reason).To(ContainSubstring(cnst.OpCustomMounts))
		Expect(reason).To(ContainSubstring("extra volume has no such label"))
		Expect(reason).To(ContainSubstring(cnst.OpMountBind))
	})

	// The policy only bites on a failure. A clean mount chain still has to run
	// the binds, and run them after the partition they bind from is mounted.
	It("mounts the binds after both mount steps when nothing failed", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, nil, cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).To(Succeed())

		Expect(order).To(ContainElements(cnst.OpLoadConfig, cnst.OpCustomMounts, cnst.OpOverlayMount))
		Expect(order).To(HaveLen(3), "the three stubs, and only those, record into order")
		bind := entry(g, cnst.OpMountBind)
		Expect(bind.Executed).To(BeTrue())
		Expect(bind.Error).ToNot(HaveOccurred())
		for _, dep := range []string{cnst.OpCustomMounts, cnst.OpOverlayMount} {
			Expect(entry(g, dep).Executed).To(BeTrue(),
				"herd must have finished %s before running the binds", dep)
		}
	})
})
