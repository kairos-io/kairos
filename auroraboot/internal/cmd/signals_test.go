package cmd_test

import (
	"os"
	"syscall"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("watchSignals", Label("cmd", "signal"), func() {
	var (
		dir   string
		sigs  chan os.Signal
		codes chan int
		exit  func(int)
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		Expect(os.WriteFile(dir+"/file", []byte("x"), 0o600)).To(Succeed())
		sigs = make(chan os.Signal, 1)
		codes = make(chan int, 1)
		exit = func(code int) { codes <- code }
	})

	It("removes the dir and exits with 130 on an interrupt", func() {
		stop := cmdpkg.WatchSignals(dir, sigs, exit)
		DeferCleanup(stop)

		sigs <- os.Interrupt

		Eventually(codes).Should(Receive(Equal(130)))
		Expect(dir).ToNot(BeADirectory())
	})

	It("removes the dir and exits with 143 on a terminate", func() {
		stop := cmdpkg.WatchSignals(dir, sigs, exit)
		DeferCleanup(stop)

		sigs <- syscall.SIGTERM

		Eventually(codes).Should(Receive(Equal(143)))
		Expect(dir).ToNot(BeADirectory())
	})

	It("keeps the dir and never exits when stopped without a signal", func() {
		stop := cmdpkg.WatchSignals(dir, sigs, exit)
		stop()

		Consistently(codes, "100ms").ShouldNot(Receive())
		Expect(dir).To(BeADirectory())
	})
})
