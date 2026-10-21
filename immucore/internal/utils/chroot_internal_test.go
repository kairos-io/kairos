package utils

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeChrootSyscalls stands in for the system calls Chroot makes, so that
// RunCallback can be driven without root. It records what it was asked and
// returns the errors a test asks it to return.
type fakeChrootSyscalls struct {
	// chrootErr is returned for a Chroot of that exact path. The restore
	// call at the end of RunCallback uses ".", so a test can fail entering
	// the chroot and leaving it independently.
	chrootErr map[string]error
	// chdirErr is returned by every Chdir.
	chdirErr error
	// mountErr is returned by every Mount.
	mountErr error
	// unmountErr is returned by every Unmount.
	unmountErr error

	chrootCalls []string
	unmounted   []string
}

func newFakeChrootSyscalls() *fakeChrootSyscalls {
	return &fakeChrootSyscalls{chrootErr: map[string]error{}}
}

func (f *fakeChrootSyscalls) Chdir(_ string) error { return f.chdirErr }

func (f *fakeChrootSyscalls) Chroot(path string) error {
	f.chrootCalls = append(f.chrootCalls, path)
	return f.chrootErr[path]
}

func (f *fakeChrootSyscalls) Mount(_, _, _ string, _ uintptr, _ string) error {
	return f.mountErr
}

func (f *fakeChrootSyscalls) Unmount(target string, _ int) error {
	f.unmounted = append(f.unmounted, target)
	return f.unmountErr
}

var _ = Describe("Chroot.RunCallback", func() {
	var sys *fakeChrootSyscalls
	var chroot *Chroot

	BeforeEach(func() {
		sys = newFakeChrootSyscalls()
		chroot = NewChroot(GinkgoT().TempDir())
		chroot.sys = sys
	})

	It("runs the callback and reports success", func() {
		called := false
		Expect(chroot.RunCallback(func() error {
			called = true
			return nil
		})).To(Succeed())
		Expect(called).To(BeTrue())
		Expect(sys.chrootCalls).To(ContainElement(chroot.path))
	})

	It("unmounts what it mounted before returning", func() {
		Expect(chroot.RunCallback(func() error { return nil })).To(Succeed())
		Expect(sys.unmounted).ToNot(BeEmpty())
		Expect(chroot.activeMounts).To(BeEmpty())
	})

	It("returns the callback's error", func() {
		err := chroot.RunCallback(func() error { return errors.New("callback error") })
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("callback error"))
	})

	It("returns a failure to chroot back to the old root", func() {
		// "." is the restore call at the end of RunCallback. Leaving the
		// chroot behind is worse than failing, so it has to be reported.
		sys.chrootErr["."] = errors.New("cannot leave the chroot")
		err := chroot.RunCallback(func() error { return nil })
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot leave the chroot"))
	})

	It("returns a failure to enter the chroot, and does not run the callback", func() {
		sys.chrootErr[chroot.path] = errors.New("cannot enter the chroot")
		called := false
		err := chroot.RunCallback(func() error {
			called = true
			return nil
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot enter the chroot"))
		Expect(called).To(BeFalse())
	})

	It("keeps the callback's error when the unmounts also fail", func() {
		sys.unmountErr = errors.New("unmount error")
		err := chroot.RunCallback(func() error { return errors.New("callback error") })
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("callback error"))
	})

	It("reports a failure to unmount when nothing else failed", func() {
		sys.unmountErr = errors.New("unmount error")
		err := chroot.RunCallback(func() error { return nil })
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed closing chroot environment"))
	})

	It("returns a failure to prepare the mounts, and does not chroot", func() {
		sys.mountErr = errors.New("mount error")
		err := chroot.RunCallback(func() error { return nil })
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mount error"))
		Expect(sys.chrootCalls).To(BeEmpty())
	})
})
