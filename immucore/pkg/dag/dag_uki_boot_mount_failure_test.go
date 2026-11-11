package dag_test

import (
	"context"
	"errors"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/dag"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	"github.com/spectrocloud-labs/herd"
)

// stubbedUKIGraph copies the edges and the failure policy (fatal, weak deps)
// of every node RegisterUKI wires, and swaps each callback for a stub. The
// real steps mount, run udev and exec init, none of which a unit test can do,
// but the edges and the policy are what decide whether a failure stops the
// boot. Every stub succeeds except the ones named in failing.
func stubbedUKIGraph(failing map[string]error) *herd.Graph {
	internalUtils.KLog = logger.NewNullLogger()
	wired := herd.DAG(herd.EnableInit)
	Expect(dag.RegisterUKI(&state.State{Rootdir: "/sysroot"}, wired)).To(Succeed())

	g := herd.DAG()
	for _, layer := range wired.Analyze() {
		for _, e := range layer {
			name := e.Name
			opts := []herd.OpOption{herd.WithCallback(func(_ context.Context) error {
				return failing[name]
			})}
			if hard, _ := lo.Difference(e.Dependencies, e.WeakDependencies); len(hard) > 0 {
				opts = append(opts, herd.WithDeps(hard...))
			}
			if len(e.WeakDependencies) > 0 {
				opts = append(opts, herd.WithWeakDeps(e.WeakDependencies...))
			}
			if e.WeakDeps {
				opts = append(opts, herd.WeakDeps)
			}
			if e.Fatal {
				opts = append(opts, herd.FatalOp)
			}
			Expect(g.Add(name, opts...)).To(Succeed())
		}
	}

	return g
}

var _ = Describe("the UKI boot dag when a mount fails", func() {
	// Under UKI COS_PERSISTENT comes in through custom-mount (VOLUMES in
	// cos-layout.env), so a failure anywhere in that chain leaves the node
	// without persistence. The binds must not run on top of the in-RAM root,
	// and the run must fail so the boot drops to the emergency shell.
	DescribeTable("skips the binds and fails the run",
		func(step string) {
			g := stubbedUKIGraph(map[string]error{step: errors.New(step + " failed")})

			Expect(g.Run(context.Background())).To(HaveOccurred())

			Expect(g.State(cnst.OpMountBind).Executed).To(BeFalse())
			Expect(g.State(cnst.OpWriteFstab).Executed).To(BeFalse())
			Expect(g.State(cnst.OpUkiInit).Executed).To(BeFalse())
		},
		Entry("custom mount", cnst.OpCustomMounts),
		Entry("overlay mount", cnst.OpOverlayMount),
		Entry("cos-layout.env read", cnst.OpLoadConfig),
	)

	It("runs the binds and hands over to init when nothing failed", func() {
		g := stubbedUKIGraph(nil)

		Expect(g.Run(context.Background())).To(Succeed())

		Expect(g.State(cnst.OpMountBind).Executed).To(BeTrue())
		Expect(g.State(cnst.OpWriteFstab).Executed).To(BeTrue())
		Expect(g.State(cnst.OpUkiInit).Executed).To(BeTrue())
	})
})
