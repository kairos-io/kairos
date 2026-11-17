package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kairos-io/AuroraBoot/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InjectISO", Label("iso"), func() {
	// InjectISO must not mutate the process working directory: builds run as
	// concurrent goroutines in `web` mode and a global os.Chdir would corrupt
	// sibling builds. This spec drives the cloud-config injection branch (a
	// non-empty config.yaml in dst) and asserts the cwd is identical before and
	// after, and that the injected config is present in the ISO.
	It("injects cloud-config without changing the process working directory", func() {
		if _, err := exec.LookPath("xorriso"); err != nil {
			Skip("xorriso not installed")
		}

		dst := GinkgoT().TempDir()

		// A non-empty config.yaml in dst triggers the injection branch.
		Expect(os.WriteFile(filepath.Join(dst, "config.yaml"),
			[]byte("#cloud-config\nhostname: injected\n"), 0o644)).To(Succeed())

		// Build a minimal source ISO from a payload dir so xorriso has a real
		// -indev/-outdev to replay against.
		payload := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(payload, "placeholder"), []byte("x"), 0o644)).To(Succeed())
		isoFile := filepath.Join(GinkgoT().TempDir(), "source.iso")
		mk := exec.Command("xorriso", "-as", "mkisofs", "-o", isoFile, payload)
		out, err := mk.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))

		beforeWD, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		fn := InjectISO(
			func() string { return dst },
			func() string { return isoFile },
			schema.ISO{},
		)
		Expect(fn(context.Background())).To(Succeed())

		afterWD, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(afterWD).To(Equal(beforeWD), "InjectISO must not change the process cwd")

		// The injected config.yaml must now be present inside the ISO.
		ls := exec.Command("xorriso", "-indev", isoFile, "-find", "/config.yaml")
		lsOut, lsErr := ls.CombinedOutput()
		Expect(lsErr).NotTo(HaveOccurred(), string(lsOut))
		Expect(string(lsOut)).To(ContainSubstring("config.yaml"))
	})

	It("injects a Kubernetes-mounted (symlinked) overlay as plain files", func() {
		// Regression test for https://github.com/kairos-io/kairos/issues/4324:
		// kairos-operator's OSArtifact.spec.overlayISOVolume mounts a
		// Secret/ConfigMap directly as the overlay, which arrives in kubelet's
		// "atomic writer" layout — every top-level entry a symlink into a
		// hidden ..data directory. Injected verbatim, those symlinks end up in
		// the ISO root (and can shadow real dirs like boot/).
		if _, err := exec.LookPath("xorriso"); err != nil {
			Skip("xorriso not installed")
		}

		dst := GinkgoT().TempDir()

		// The overlay in the exact kubelet layout:
		//   overlay/..data -> ..2026_08_17_20_28_56.3347250082
		//   overlay/..2026_08_17_20_28_56.3347250082/boot/grub2/grub.cfg
		//   overlay/boot -> ..data/boot
		overlay := GinkgoT().TempDir()
		timestamped := filepath.Join(overlay, "..2026_08_17_20_28_56.3347250082")
		Expect(os.MkdirAll(filepath.Join(timestamped, "boot", "grub2"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(timestamped, "boot", "grub2", "grub.cfg"),
			[]byte("set timeout=5\n"), 0o644)).To(Succeed())
		Expect(os.Symlink(filepath.Base(timestamped), filepath.Join(overlay, "..data"))).To(Succeed())
		Expect(os.Symlink(filepath.Join("..data", "boot"), filepath.Join(overlay, "boot"))).To(Succeed())

		// Build a minimal source ISO so xorriso has a real -indev/-outdev.
		payload := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(payload, "placeholder"), []byte("x"), 0o644)).To(Succeed())
		isoFile := filepath.Join(GinkgoT().TempDir(), "source.iso")
		mk := exec.Command("xorriso", "-as", "mkisofs", "-o", isoFile, payload)
		out, err := mk.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))

		// InjectISO only runs xorriso when a non-empty config.yaml exists in
		// dst (the cloud-config injection branch); the overlay is staged into
		// the same tmp tree, so this drives the full overlay path end to end.
		Expect(os.WriteFile(filepath.Join(dst, "config.yaml"),
			[]byte("#cloud-config\nhostname: injected\n"), 0o644)).To(Succeed())

		fn := InjectISO(
			func() string { return dst },
			func() string { return isoFile },
			schema.ISO{OverlayISO: overlay},
		)
		Expect(fn(context.Background())).To(Succeed())

		// The overlay content must be in the ISO as a real file at the real path…
		ls := exec.Command("xorriso", "-indev", isoFile, "-find", "/boot/grub2/grub.cfg")
		lsOut, lsErr := ls.CombinedOutput()
		Expect(lsErr).NotTo(HaveOccurred(), string(lsOut))
		Expect(string(lsOut)).To(ContainSubstring("/boot/grub2/grub.cfg"))

		// …/boot must be a directory in the ISO, not a symlink…
		dir := exec.Command("xorriso", "-indev", isoFile, "-find", "/boot", "-type", "d")
		dirOut, dirErr := dir.CombinedOutput()
		Expect(dirErr).NotTo(HaveOccurred(), string(dirOut))
		Expect(string(dirOut)).To(ContainSubstring("/boot"))
		link := exec.Command("xorriso", "-indev", isoFile, "-find", "/boot", "-type", "l")
		linkOut, linkErr := link.CombinedOutput()
		Expect(linkErr).NotTo(HaveOccurred(), string(linkOut))
		Expect(string(linkOut)).NotTo(ContainSubstring("/boot"))

		// …and the kubelet internals must not appear in the ISO root. (".."
		// itself is the ISO9660 parent-dir entry, so match the hidden names.)
		root := exec.Command("xorriso", "-indev", isoFile, "-lsl", "/")
		rootOut, rootErr := root.CombinedOutput()
		Expect(rootErr).NotTo(HaveOccurred(), string(rootOut))
		Expect(string(rootOut)).NotTo(ContainSubstring("..data"))
		Expect(string(rootOut)).NotTo(ContainSubstring("..2026_08_17"))
	})

	It("does not change the process working directory when xorriso fails", func() {
		dst := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dst, "config.yaml"),
			[]byte("#cloud-config\nhostname: injected\n"), 0o644)).To(Succeed())

		beforeWD, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())

		// A bogus iso path makes xorriso fail; cwd must still be preserved.
		fn := InjectISO(
			func() string { return dst },
			func() string { return filepath.Join(GinkgoT().TempDir(), "does-not-exist.iso") },
			schema.ISO{},
		)
		_ = fn(context.Background())

		afterWD, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(afterWD).To(Equal(beforeWD), "InjectISO must not change the process cwd even on failure")
	})
	// kairos-io/kairos#5778: on the release-ISO path InjectISO is the only step
	// that applies iso.overlay_iso, and the deployer writes a 0-byte
	// config.yaml when no cloud_config is set. The overlay must still land.
	DescribeTable("injects the overlay without a cloud config",
		func(writeConfig bool) {
			if _, err := exec.LookPath("xorriso"); err != nil {
				Skip("xorriso not installed")
			}

			dst := GinkgoT().TempDir()
			if writeConfig {
				Expect(os.WriteFile(filepath.Join(dst, "config.yaml"), nil, 0o644)).To(Succeed())
			}

			overlay := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(overlay, "overlay-marker.txt"), []byte("x"), 0o644)).To(Succeed())

			payload := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(payload, "placeholder"), []byte("x"), 0o644)).To(Succeed())
			isoFile := filepath.Join(GinkgoT().TempDir(), "source.iso")
			out, err := exec.Command("xorriso", "-as", "mkisofs", "-o", isoFile, payload).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))

			fn := InjectISO(
				func() string { return dst },
				func() string { return isoFile },
				schema.ISO{OverlayISO: overlay},
			)
			Expect(fn(context.Background())).To(Succeed())

			lsOut, lsErr := exec.Command("xorriso", "-indev", isoFile, "-find", "/overlay-marker.txt").CombinedOutput()
			Expect(lsErr).NotTo(HaveOccurred(), string(lsOut))
			Expect(string(lsOut)).To(ContainSubstring("/overlay-marker.txt"))

			rootOut, rootErr := exec.Command("xorriso", "-indev", isoFile, "-lsl", "/").CombinedOutput()
			Expect(rootErr).NotTo(HaveOccurred(), string(rootOut))
			Expect(string(rootOut)).NotTo(ContainSubstring("config.yaml"))
		},
		Entry("with an empty config.yaml", true),
		Entry("with no config.yaml", false),
	)

	It("leaves the ISO untouched with neither an overlay nor a cloud config", func() {
		dst := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dst, "config.yaml"), nil, 0o644)).To(Succeed())

		// A missing ISO proves xorriso is not run: it would fail on it.
		fn := InjectISO(
			func() string { return dst },
			func() string { return filepath.Join(GinkgoT().TempDir(), "does-not-exist.iso") },
			schema.ISO{},
		)
		Expect(fn(context.Background())).To(Succeed())
	})
})
