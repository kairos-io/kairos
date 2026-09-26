package state

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("boot state to extension sub-directory", func() {
	DescribeTable("resolves the sub-directory",
		func(boot state.Boot, expected string, known bool) {
			subDir, ok := bootStateToExtensionSubDir(boot)
			Expect(ok).To(Equal(known))
			Expect(subDir).To(Equal(expected))
		},
		Entry("active", state.Active, "active", true),
		Entry("passive", state.Passive, "passive", true),
		Entry("recovery", state.Recovery, "recovery", true),
		Entry("autoreset runs the recovery image", state.AutoReset, "recovery", true),
		Entry("livecd installs nothing", state.LiveCD, "", false),
		Entry("unknown installs nothing", state.Unknown, "", false),
	)
})

var _ = Describe("sysext image policy", func() {
	// The two policies the shipped drop-ins enforce, spelled out here as
	// systemd-sysext reads them in
	// kairos-init/pkg/bundled/cloudconfigs/99_sysext.yaml. immucore validates
	// with systemd-dissect and systemd-sysext refreshes with these, so the two
	// have to name the same policy or immucore enables images the refresh then
	// refuses.
	const (
		ukiDropIn  = `root=signed+absent:usr=signed+absent`
		grubDropIn = `root=verity+absent:usr=verity+absent`
	)

	It("validates against the signed policy the UKI drop-in enforces", func() {
		Expect(sysextImagePolicy(true)).To(ContainSubstring(ukiDropIn))
	})

	It("validates against the verity policy the non-UKI drop-in enforces", func() {
		Expect(sysextImagePolicy(false)).To(ContainSubstring(grubDropIn))
	})

	It("does not hand a GRUB boot the UKI policy", func() {
		Expect(sysextImagePolicy(false)).ToNot(Equal(sysextImagePolicy(true)))
	})
})

var _ = Describe("enabling extensions from a directory", func() {
	var s *State
	var source, dest string

	// rejects returns a check that fails for the named images only, standing in
	// for systemd-dissect refusing an image that does not satisfy the policy.
	rejects := func(names ...string) func(string) bool {
		bad := map[string]bool{}
		for _, name := range names {
			bad[name] = true
		}
		return func(path string) bool { return !bad[filepath.Base(path)] }
	}

	// The symlink target is deliberately unprefixed, so under a test root the
	// link dangles and os.Stat on it fails. Lstat asks the question the code
	// answers: is the extension enabled.
	linked := func(name string) bool {
		_, err := os.Lstat(filepath.Join(dest, name))
		return err == nil
	}

	write := func(dir, name string) {
		Expect(os.WriteFile(filepath.Join(dir, name), []byte("image"), 0644)).To(Succeed())
	}

	BeforeEach(func() {
		root := GinkgoT().TempDir()
		s = &State{Rootdir: root}
		source = "/var/lib/kairos/extensions/active"
		dest = GinkgoT().TempDir()
		Expect(os.MkdirAll(s.path(source), 0755)).To(Succeed())
	})

	It("links an image that satisfies the policy", func() {
		write(s.path(source), "work.sysext.raw")

		Expect(enableExtensionsFrom(s, source, dest, "sysext", rejects())).To(Succeed())

		Expect(linked("work.sysext.raw")).To(BeTrue())
	})

	It("links to the path without the sysroot prefix, because /run moves into it", func() {
		write(s.path(source), "work.sysext.raw")

		Expect(enableExtensionsFrom(s, source, dest, "sysext", nil)).To(Succeed())

		target, err := os.Readlink(filepath.Join(dest, "work.sysext.raw"))
		Expect(err).ToNot(HaveOccurred())
		Expect(target).To(Equal("/var/lib/kairos/extensions/active/work.sysext.raw"))
	})

	It("skips an image that does not satisfy the policy", func() {
		write(s.path(source), "hello-broke.sysext.raw")

		Expect(enableExtensionsFrom(s, source, dest, "sysext", rejects("hello-broke.sysext.raw"))).To(Succeed())

		entries, err := os.ReadDir(dest)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	// The reason the skip has to happen at all: systemd-sysext refreshes all or
	// nothing, so linking one image it refuses costs the node every other
	// extension. kairos-io/kairos#4988.
	It("keeps a rejected image from taking a valid one down with it", func() {
		write(s.path(source), "work.sysext.raw")
		write(s.path(source), "hello-broke.sysext.raw")

		Expect(enableExtensionsFrom(s, source, dest, "sysext", rejects("hello-broke.sysext.raw"))).To(Succeed())

		Expect(linked("work.sysext.raw")).To(BeTrue())
		Expect(linked("hello-broke.sysext.raw")).To(BeFalse())
	})

	It("links every image when the policy cannot be evaluated on this system", func() {
		write(s.path(source), "work.sysext.raw")
		write(s.path(source), "hello-broke.sysext.raw")

		Expect(enableExtensionsFrom(s, source, dest, "sysext", nil)).To(Succeed())

		Expect(linked("work.sysext.raw")).To(BeTrue())
		Expect(linked("hello-broke.sysext.raw")).To(BeTrue())
	})

	It("ignores a file that is not an image, and a directory", func() {
		write(s.path(source), "notes.txt")
		Expect(os.MkdirAll(filepath.Join(s.path(source), "nested.raw"), 0755)).To(Succeed())

		Expect(enableExtensionsFrom(s, source, dest, "sysext", rejects())).To(Succeed())

		entries, err := os.ReadDir(dest)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("leaves an already enabled image alone", func() {
		write(s.path(source), "work.sysext.raw")
		Expect(os.WriteFile(filepath.Join(dest, "work.sysext.raw"), []byte("already"), 0644)).To(Succeed())

		Expect(enableExtensionsFrom(s, source, dest, "sysext", rejects())).To(Succeed())

		content, err := os.ReadFile(filepath.Join(dest, "work.sysext.raw"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("already"))
	})

	It("is not an error when the directory does not exist", func() {
		Expect(enableExtensionsFrom(s, "/var/lib/kairos/extensions/recovery", dest, "sysext", rejects())).To(Succeed())
	})
})

var _ = Describe("the fall back when an image policy cannot be evaluated", func() {
	It("enables the extensions on a GRUB boot, which is what that boot did before", func() {
		Expect(unvalidatedExtensionCheck(false)).To(BeNil())
	})

	It("enables none of them on a trusted boot, which must not load an unsigned one", func() {
		check := unvalidatedExtensionCheck(true)
		Expect(check).ToNot(BeNil())
		Expect(check("/var/lib/kairos/extensions/active/work.sysext.raw")).To(BeFalse())
	})
})
