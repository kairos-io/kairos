package agent

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/pterm/pterm"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("configErrorNotice", func() {
	It("says nothing when the scan went through", func() {
		Expect(configErrorNotice(nil)).To(BeEmpty())
	})

	It("names the error the scan reported", func() {
		Expect(configErrorNotice(errors.New("ERROR: bad config"))).To(Equal("The configuration on this node could not be read: ERROR: bad config"))
	})
})

var _ = Describe("printPairing", func() {
	captureStdout := func(f func()) string {
		r, w, err := os.Pipe()
		Expect(err).ToNot(HaveOccurred())

		stdout := os.Stdout
		os.Stdout = w
		pterm.SetDefaultOutput(w)
		restore := func() {
			os.Stdout = stdout
			pterm.SetDefaultOutput(stdout)
		}
		DeferCleanup(restore)

		out := make(chan string)
		go func() {
			b, _ := io.ReadAll(r)
			out <- string(b)
		}()

		f()
		restore()
		Expect(w.Close()).To(Succeed())
		return <-out
	}

	scanErr := errors.New("ERROR: install.ssh_hardening: true requires at least one user with ssh_authorized_keys")

	It("prints the config error below the QR code and the interfaces", func() {
		out := captureStdout(func() { printPairing("pairing-token", scanErr) })

		info := strings.Index(out, "Interfaces:")
		notice := strings.Index(out, configErrorNotice(scanErr))
		Expect(info).To(BeNumerically(">", 0), "the QR code is drawn before the interfaces line")
		Expect(notice).To(BeNumerically(">", info))
	})

	It("prints the config error when there is no QR code to draw", func() {
		out := captureStdout(func() { printPairing("", scanErr) })

		Expect(out).To(ContainSubstring(configErrorNotice(scanErr)))
		Expect(out).ToNot(ContainSubstring("Interfaces:"))
	})

	It("prints no notice when the config was read", func() {
		out := captureStdout(func() { printPairing("pairing-token", nil) })

		Expect(out).To(ContainSubstring("Interfaces:"))
		Expect(out).ToNot(ContainSubstring("could not be read"))
	})
})
