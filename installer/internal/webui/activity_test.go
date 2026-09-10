package webui

import (
	"os"
	"path/filepath"

	process "github.com/mudler/go-processmanager"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The installer's terminal UI asks an Activity whether the browser has an
// install running, so pressing `q` at the console cannot cut a remote
// operator's install short.
var _ = Describe("Activity", func() {
	It("reports no install before one is started", func() {
		a := &Activity{}
		Expect(a.InstallInFlight()).To(BeFalse())
		// Nothing to wait for, so this must not block the installer's exit.
		a.WaitForInstall()
	})

	It("is usable when nil, because Options.Activity is optional", func() {
		var a *Activity
		Expect(a.InstallInFlight()).To(BeFalse())
		a.WaitForInstall()
		a.started(process.New())
	})

	It("reports an install in flight until the process exits", func() {
		dir := GinkgoT().TempDir()
		release := filepath.Join(dir, "release")
		p := process.New(
			process.WithName("/bin/sh"),
			process.WithArgs("-c", "until [ -f "+release+" ]; do sleep 0.05; done"),
			process.WithStateDir(filepath.Join(dir, "state")),
		)
		Expect(p.Run()).To(Succeed())

		a := &Activity{}
		a.started(p)
		Expect(a.InstallInFlight()).To(BeTrue())

		Expect(os.WriteFile(release, []byte("go"), 0o600)).To(Succeed())
		a.WaitForInstall()
		Expect(a.InstallInFlight()).To(BeFalse())
	})
})
