package state

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("waitForSysroot", func() {
	var rootDir string

	// stretchPollInterval makes the interval far longer than any spec is
	// willing to wait, so a spec that comes back quickly proves the wait was
	// never entered, or was left early on purpose.
	stretchPollInterval := func() {
		previous := sysrootPollInterval
		sysrootPollInterval = 60 * time.Second
		DeferCleanup(func() { sysrootPollInterval = previous })
	}

	// setDeadline puts rd.immucore.sysrootwait= on the fake cmdline.
	setDeadline := func(seconds string) {
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte("rd.immucore.sysrootwait="+seconds), 0644)).To(Succeed())
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() { _ = os.Unsetenv("HOST_PROC_CMDLINE") })
	}

	BeforeEach(func() {
		rootDir = filepath.Join(GinkgoT().TempDir(), "sysroot")
	})

	It("returns before waiting when the sysroot is already there", func(ctx SpecContext) {
		// This is the normal case: dracut mounted /sysroot before immucore
		// ran, and the step is only there to close a race. Waiting first
		// charges every livecd, netboot and in-ram boot for the interval.
		Expect(os.MkdirAll(filepath.Join(rootDir, "system"), 0755)).To(Succeed())
		stretchPollInterval()

		state := &State{Rootdir: rootDir}

		start := time.Now()
		Expect(state.waitForSysroot(ctx)).To(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
	}, NodeTimeout(30*time.Second))

	It("keeps looking until the sysroot shows up", func(ctx SpecContext) {
		previous := sysrootPollInterval
		sysrootPollInterval = 10 * time.Millisecond
		DeferCleanup(func() { sysrootPollInterval = previous })

		state := &State{Rootdir: rootDir}
		go func() {
			time.Sleep(50 * time.Millisecond)
			_ = os.MkdirAll(filepath.Join(rootDir, "system"), 0755)
		}()

		Expect(state.waitForSysroot(ctx)).To(Succeed())
	}, NodeTimeout(30*time.Second))

	It("gives up on the deadline without sitting out the interval", func(ctx SpecContext) {
		// The deadline is a second and the interval a minute, so this only
		// comes back in time if the wait between two checks is interruptible.
		setDeadline("1")
		stretchPollInterval()

		state := &State{Rootdir: rootDir}

		start := time.Now()
		err := state.waitForSysroot(ctx)
		Expect(err).To(MatchError("timeout exhausted"))
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	}, NodeTimeout(30*time.Second))

	It("gives up on a canceled context without sitting out the interval", func(ctx SpecContext) {
		stretchPollInterval()

		canceled, cancel := context.WithCancel(ctx)
		cancel()

		state := &State{Rootdir: rootDir}

		start := time.Now()
		err := state.waitForSysroot(canceled)
		Expect(err).To(MatchError("context canceled"))
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
	}, NodeTimeout(30*time.Second))
})

var _ = Describe("missingSysrootPath", func() {
	var rootDir string

	BeforeEach(func() {
		rootDir = filepath.Join(GinkgoT().TempDir(), "sysroot")
	})

	It("names the sysroot while it is not there", func() {
		state := &State{Rootdir: rootDir}

		Expect(state.missingSysrootPath()).To(Equal(rootDir))
	})

	It("names the system directory while only the sysroot is there", func() {
		Expect(os.MkdirAll(rootDir, 0755)).To(Succeed())
		state := &State{Rootdir: rootDir}

		Expect(state.missingSysrootPath()).To(Equal(filepath.Join(rootDir, "system")))
	})

	It("names nothing once both are there", func() {
		Expect(os.MkdirAll(filepath.Join(rootDir, "system"), 0755)).To(Succeed())
		state := &State{Rootdir: rootDir}

		Expect(state.missingSysrootPath()).To(BeEmpty())
	})
})
