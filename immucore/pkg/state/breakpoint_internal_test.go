package state

import (
	"context"
	"os"
	"path/filepath"
	"time"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("step breakpoints", func() {
	stubHook := func(fn func(string)) {
		old := breakpointHook
		breakpointHook = fn
		DeferCleanup(func() { breakpointHook = old })
	}

	BeforeEach(func() {
		ResetTimeline()
	})

	It("consults the breakpoint before running the step, then resumes it", func() {
		var order []string
		stubHook(func(step string) { order = append(order, "break:"+step) })

		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpMountRoot, TimedCallback(cnst.OpMountRoot, func(_ context.Context) error {
			order = append(order, "run:"+cnst.OpMountRoot)
			return nil
		}))).To(Succeed())

		Expect(g.Run(context.Background())).To(Succeed())
		Expect(order).To(Equal([]string{"break:" + cnst.OpMountRoot, "run:" + cnst.OpMountRoot}))
		Expect(Timings()).To(HaveKey(cnst.OpMountRoot))
	})

	It("keeps the time spent in the shell out of the step timing", func() {
		stubHook(func(_ string) { time.Sleep(300 * time.Millisecond) })

		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpLoadConfig, TimedCallback(cnst.OpLoadConfig, func(_ context.Context) error {
			return nil
		}))).To(Succeed())

		Expect(g.Run(context.Background())).To(Succeed())
		Expect(Timings()[cnst.OpLoadConfig].Duration).To(BeNumerically("<", 300*time.Millisecond))
	})

	It("runs every step untouched when the cmdline asks for no breakpoint", func() {
		// Exercises the real hook, not a stub: no rd.immucore.break stanza
		// means every step goes straight through.
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE rd.immucore.debug\n"), 0o600)).To(Succeed())
		old, had := os.LookupEnv("HOST_PROC_CMDLINE")
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv("HOST_PROC_CMDLINE", old)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		})

		called := false
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpLoadConfig, TimedCallback(cnst.OpLoadConfig, func(_ context.Context) error {
			called = true
			return nil
		}))).To(Succeed())

		Expect(g.Run(context.Background())).To(Succeed())
		Expect(called).To(BeTrue())
	})
})
