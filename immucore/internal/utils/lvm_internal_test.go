package utils

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ActivateLVM", func() {
	var ran []string
	var failures map[string]error

	BeforeEach(func() {
		ran = nil
		failures = map[string]error{}
		previous := lvmCommand
		lvmCommand = func(c string) (string, error) {
			ran = append(ran, c)
			return "", failures[c]
		}
		DeferCleanup(func() { lvmCommand = previous })
	})

	It("triggers udev with the trigger subcommand and waits for it after vgchange", func() {
		Expect(ActivateLVM()).To(Succeed())
		Expect(ran).To(Equal([]string{
			"rm /etc/lvm/lvm.conf",
			"lvm vgchange --activate y --sysinit",
			"udevadm trigger",
			"udevadm settle --timeout=10",
		}))
		Expect(ran).ToNot(ContainElement("udevadm --trigger"))
	})

	It("still triggers udev when vgchange fails, and returns the vgchange error", func() {
		vgErr := errors.New("exit status 5")
		failures["lvm vgchange --activate y --sysinit"] = vgErr
		Expect(ActivateLVM()).To(MatchError(vgErr))
		Expect(ran).To(ContainElements("udevadm trigger", "udevadm settle --timeout=10"))
	})

	It("does not fail the step when the udev trigger fails", func() {
		failures["udevadm trigger"] = errors.New("exit status 1")
		Expect(ActivateLVM()).To(Succeed())
		Expect(ran).To(ContainElement("udevadm settle --timeout=10"))
	})
})
