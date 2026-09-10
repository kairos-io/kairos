package webui

import (
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
		a.started(make(chan struct{}))
	})

	It("reports an install in flight until the run ends", func() {
		log := newProgressLog()

		a := &Activity{}
		a.started(log.doneChan())
		Expect(a.InstallInFlight()).To(BeTrue())

		// Only the done message ends a run: ordinary output must not make
		// the TUI think the browser is finished.
		log.publish(Message{Type: MessageLog, Message: "still going"})
		Expect(a.InstallInFlight()).To(BeTrue())

		log.publish(Message{Type: MessageDone, OK: true})
		a.WaitForInstall()
		Expect(a.InstallInFlight()).To(BeFalse())
	})

	It("reports a run that already ended as not in flight", func() {
		log := newProgressLog()
		log.publish(Message{Type: MessageDone, OK: false})

		a := &Activity{}
		a.started(log.doneChan())
		Expect(a.InstallInFlight()).To(BeFalse())
		a.WaitForInstall()
	})
})
