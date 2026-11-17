package ops

import (
	"context"
	"os"
	"path/filepath"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeISO builds a real ISO 9660 image holding files, keyed by their path
// inside the image. Rock Ridge is on because that is how the Kairos ISOs are
// built, and it is what keeps the names readable rather than "FOO.RAW;1".
func writeISO(files map[string]string) string {
	path := filepath.Join(GinkgoT().TempDir(), "test.iso")

	img, err := diskfs.Create(path, 8*1024*1024, diskfs.SectorSize(2048))
	Expect(err).NotTo(HaveOccurred())
	fs, err := img.CreateFilesystem(disk.FilesystemSpec{Partition: 0, FSType: filesystem.TypeISO9660, VolumeLabel: "TEST"})
	Expect(err).NotTo(HaveOccurred())

	for name, content := range files {
		if dir := filepath.Dir(name); dir != "/" && dir != "." {
			Expect(fs.Mkdir(dir)).To(Succeed())
		}
		f, err := fs.OpenFile(name, os.O_CREATE|os.O_RDWR)
		Expect(err).NotTo(HaveOccurred())
		_, err = f.Write([]byte(content))
		Expect(err).NotTo(HaveOccurred())
		Expect(f.Close()).To(Succeed())
	}

	Expect(fs.(*iso9660.FileSystem).Finalize(iso9660.FinalizeOptions{RockRidge: true})).To(Succeed())
	Expect(img.Close()).To(Succeed())
	return path
}

// netbootISO is the three paths every netboot extraction reads, so a spec only
// has to describe what it adds on top of them.
func netbootISO(extra map[string]string) string {
	files := map[string]string{
		"/rootfs.squashfs": "squashfs",
		"/boot/kernel":     "kernel",
		"/boot/initrd":     "initrd",
	}
	for name, content := range extra {
		files[name] = content
	}
	return writeISO(files)
}

// entryNames lists what a directory holds, which reads better in a failure
// message than the os.DirEntry structs a length assertion prints.
func entryNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred())
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func declaration(names ...string) string {
	body := "#cloud-config\ninstall:\n  extensions:\n"
	for _, name := range names {
		body += "    - name: " + name + "\n"
	}
	return body
}

var _ = Describe("ExtractNetboot", Label("iso"), func() {
	var dst string

	BeforeEach(func() {
		dst = GinkgoT().TempDir()
	})

	extract := func(src string) error {
		return ExtractNetboot(
			func() string { return src },
			func() string { return dst },
			"kairos",
		)(context.Background())
	}

	It("extracts the squashfs, the kernel and the initrd", func() {
		Expect(extract(netbootISO(nil))).To(Succeed())

		Expect(os.ReadFile(filepath.Join(dst, "kairos.squashfs"))).To(BeEquivalentTo("squashfs"))
		Expect(os.ReadFile(filepath.Join(dst, "kairos-kernel"))).To(BeEquivalentTo("kernel"))
		Expect(os.ReadFile(filepath.Join(dst, "kairos-initrd"))).To(BeEquivalentTo("initrd"))
	})

	// Regression for kairos-io/kairos#5040: a bundled extension is materialized
	// into the ISO root, not into rootfs.squashfs, so before this it was dropped
	// on the way to the netboot artifacts and a netboot tree built from an
	// artifact spec with extensions silently carried none.
	It("carries the bundled extension images named by the declaration", func() {
		src := netbootISO(map[string]string{
			"/extensions.yaml":  declaration("/run/initramfs/live/work.sysext.raw", "/run/initramfs/live/tools.sysext.raw"),
			"/work.sysext.raw":  "work image",
			"/tools.sysext.raw": "tools image",
		})

		Expect(extract(src)).To(Succeed())

		Expect(os.ReadFile(filepath.Join(dst, "work.sysext.raw"))).To(BeEquivalentTo("work image"))
		Expect(os.ReadFile(filepath.Join(dst, "tools.sysext.raw"))).To(BeEquivalentTo("tools image"))
	})

	It("carries nothing when the ISO declares no extensions", func() {
		Expect(extract(netbootISO(map[string]string{"/work.sysext.raw": "not declared"}))).To(Succeed())

		Expect(filepath.Join(dst, "work.sysext.raw")).NotTo(BeAnExistingFile())
		Expect(entryNames(dst)).To(ConsistOf("kairos.squashfs", "kairos-kernel", "kairos-initrd"))
	})

	// StartPixiecore has only the netboot directory to work from, so the
	// declaration has to travel with the images for the kairos.extensions
	// keyword to be fillable at all. It is equally what an operator serving
	// this tree themselves reads to write install.extensions.
	It("leaves the declaration in the netboot directory next to the images", func() {
		src := netbootISO(map[string]string{
			"/extensions.yaml":  declaration("/run/initramfs/live/work.sysext.raw", "/run/initramfs/live/tools.sysext.raw"),
			"/work.sysext.raw":  "work image",
			"/tools.sysext.raw": "tools image",
		})

		Expect(extract(src)).To(Succeed())

		Expect(filepath.Join(dst, "extensions.yaml")).To(BeAnExistingFile())
		Expect(NetbootExtensionImages(dst)).To(Equal([]string{
			filepath.Join(dst, "work.sysext.raw"),
			filepath.Join(dst, "tools.sysext.raw"),
		}))
	})

	// A user's own extensions.yaml can arrive through --overlay-iso naming an
	// image the installed system fetches for itself. Nothing on the ISO root
	// answers to that name, and it is not this build's to carry.
	It("skips a declared extension that is not a file on the live media", func() {
		src := netbootISO(map[string]string{
			"/extensions.yaml": declaration("oci://ghcr.io/example/tools.sysext.raw", "https://example.com/more.sysext.raw"),
		})

		Expect(extract(src)).To(Succeed())

		Expect(entryNames(dst)).To(ConsistOf("kairos.squashfs", "kairos-kernel", "kairos-initrd", "extensions.yaml"))
		Expect(NetbootExtensionImages(dst)).To(BeEmpty())
	})

	// An extensions.yaml this build did not write can name a nested live-media
	// path. Only an entry directly under the live directory names a file
	// materializeISOExtensions put at the ISO root, so a nested one is as much
	// the installed system's business as an oci:// one. Failing on it would
	// break an ISO that netboot-extracted fine before kairos-io/kairos#5040.
	It("skips a declared extension nested below the live media directory", func() {
		src := netbootISO(map[string]string{
			"/extensions.yaml":           declaration("/run/initramfs/live/extensions/foo.sysext.raw"),
			"/extensions/foo.sysext.raw": "nested image",
		})

		Expect(extract(src)).To(Succeed())

		Expect(entryNames(dst)).To(ConsistOf("kairos.squashfs", "kairos-kernel", "kairos-initrd", "extensions.yaml"))
		Expect(NetbootExtensionImages(dst)).To(BeEmpty())
	})
	// AuroraBoot writes the declaration and the images together, so the two
	// disagreeing means the ISO is inconsistent. Failing is what keeps a netboot
	// tree from quietly lacking an extension the ISO install gets.
	It("fails when the declaration names an image the ISO root does not carry", func() {
		src := netbootISO(map[string]string{
			"/extensions.yaml": declaration("/run/initramfs/live/missing.sysext.raw"),
		})

		err := extract(src)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing.sysext.raw"))
		Expect(err.Error()).To(ContainSubstring("the ISO root does not carry it"))
	})
})

// The consuming half of kairos-io/kairos#5040. Carrying the images into the
// netboot tree is not enough on its own: a netbooted node has no live media
// to sweep and no build-time URL to put in the cloud config, so without the
// keyword it still installs none of them.
var _ = Describe("extensionsCmdline", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	// bundle writes a netboot tree the way ExtractNetboot leaves one: the
	// declaration, plus the images it names.
	bundle := func(names ...string) {
		declared := make([]string, 0, len(names))
		for _, name := range names {
			declared = append(declared, filepath.Join("/run/initramfs/live", name))
			Expect(os.WriteFile(filepath.Join(dir, name), []byte("image"), 0o644)).To(Succeed())
		}
		Expect(os.WriteFile(filepath.Join(dir, "extensions.yaml"), []byte(declaration(declared...)), 0o644)).To(Succeed())
	}

	It("declares nothing for a netboot tree with no bundled extension", func() {
		Expect(extensionsCmdline(dir, "")).To(BeEmpty())
	})

	// Pixiecore mints one served URL per ID while it expands the template, so
	// the paths go in as IDs and come out as URLs the node can fetch.
	It("names every bundled image as a pixiecore ID, in declaration order", func() {
		bundle("work.sysext.raw", "tools.sysext.raw")

		Expect(extensionsCmdline(dir, "")).To(Equal(
			`kairos.extensions={{ ID "` + filepath.Join(dir, "work.sysext.raw") + `" }},{{ ID "` + filepath.Join(dir, "tools.sysext.raw") + `" }}`,
		))
	})

	// kairos-agent accumulates a repeated key, so emitting ours next to the
	// operator's would install the union of two sets neither side asked for.
	It("leaves a cmdline that already declares extensions alone", func() {
		bundle("work.sysext.raw")

		Expect(extensionsCmdline(dir, "console=ttyS0 kairos.extensions=oci://ghcr.io/example/mine.sysext.raw")).To(BeEmpty())
	})

	// kairos.extensions.catalogs is a different key, and an operator who set
	// it has still declared no extension.
	It("still declares the bundled images beside an unrelated operator cmdline", func() {
		bundle("work.sysext.raw")

		Expect(extensionsCmdline(dir, "console=ttyS0 kairos.extensions.catalogs=https://example/i.json")).To(Equal(
			`kairos.extensions={{ ID "` + filepath.Join(dir, "work.sysext.raw") + `" }}`,
		))
	})

	// Netbooting a node that silently lacks an extension the same build's ISO
	// install gets is the failure this path exists to close, so a tree that
	// contradicts its own declaration stops the server rather than booting
	// nodes short of what was asked for.
	It("fails when the declaration names an image the directory does not hold", func() {
		Expect(os.WriteFile(filepath.Join(dir, "extensions.yaml"),
			[]byte(declaration("/run/initramfs/live/gone.sysext.raw")), 0o644)).To(Succeed())

		_, err := extensionsCmdline(dir, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("gone.sysext.raw"))
		Expect(err.Error()).To(ContainSubstring("the netboot directory does not carry it"))
	})
})

var _ = Describe("netbootCmdline", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "work.sysext.raw"), []byte("image"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "extensions.yaml"),
			[]byte(declaration("/run/initramfs/live/work.sysext.raw")), 0o644)).To(Succeed())
	})

	squashfs := func() string { return filepath.Join(dir, "kairos.squashfs") }

	It("appends the bundled extensions to the default cmdline", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(HavePrefix(`rd.live.overlay.overlayfs rd.neednet=1 ip=dhcp rd.cos.disable root=live:{{ ID "` + squashfs() + `" }}`))
		Expect(line).To(ContainSubstring(`config_url={{ ID "/config.yaml" }}`))
		Expect(line).To(HaveSuffix(`kairos.extensions={{ ID "` + filepath.Join(dir, "work.sysext.raw") + `" }}`))
	})

	// A configured cmdline replaces the default boot options; it does not mean
	// the extensions this build bundled should be dropped.
	It("appends them to a configured cmdline too", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{Cmdline: "console=ttyS0 quiet"})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(ContainSubstring("console=ttyS0 quiet"))
		Expect(line).To(HaveSuffix(`kairos.extensions={{ ID "` + filepath.Join(dir, "work.sysext.raw") + `" }}`))
	})

	It("leaves the cmdline untouched when nothing is bundled", func() {
		empty := GinkgoT().TempDir()

		line, err := netbootCmdline(filepath.Join(empty, "kairos.squashfs"), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).NotTo(ContainSubstring("kairos.extensions"))
	})

	// An image name is not AuroraBoot's to choose, so a % in it must reach the
	// node rather than being read as a Sprintf verb.
	It("keeps a percent sign in an image name intact", func() {
		Expect(os.WriteFile(filepath.Join(dir, "100%.sysext.raw"), []byte("image"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "extensions.yaml"),
			[]byte(declaration("/run/initramfs/live/100%.sysext.raw")), 0o644)).To(Succeed())

		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(HaveSuffix(`kairos.extensions={{ ID "` + filepath.Join(dir, "100%.sysext.raw") + `" }}`))
		Expect(line).NotTo(ContainSubstring("%!"))
	})

	It("fails rather than serving a tree that contradicts its declaration", func() {
		Expect(os.Remove(filepath.Join(dir, "work.sysext.raw"))).To(Succeed())

		_, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("work.sysext.raw"))
	})

	// Without the keyword immucore writes no install sentinel, so
	// 52_installer.yaml writes no installer unit and nothing else on a
	// netbooted node runs the install. See kairos-io/kairos#5373.
	It("asks for the installer", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(ContainSubstring(" install-mode-interactive "))
	})

	// The interactive installer is the dispatcher: it runs AutoInstall first,
	// so install.auto still installs unattended, and it offers the pairing
	// flow. Plain install-mode would install with no prompt and no way back,
	// which is the entry the ISO menu dropped. install-mode is a prefix of
	// install-mode-interactive, so the assertion is space-delimited the same
	// way the kairos stage guard is.
	It("does not ask for the unattended installer", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).ToNot(MatchRegexp(`(^| )install-mode( |$)`))
	})

	// nodepair.enable started the plain installer before the install intent
	// moved into immucore (kairos-io/kairos#5464), and starts nothing now. The
	// pairing flow it stood for is offered by the interactive installer.
	It("does not carry the legacy pairing keyword", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).NotTo(ContainSubstring("nodepair.enable"))
	})

	// kairos-sdk reads the boot state off this keyword, and immucore only
	// writes /run/cos/live_mode when it says LiveCD. The install keyword on
	// its own would not be enough.
	It("still marks the boot as a netboot", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(ContainSubstring(" netboot "))
	})

	// The override is how an operator asks for a netbooted node that does not
	// install itself, so the default boot options must not survive underneath
	// it. The bundled extensions are a separate matter and are still appended.
	It("drops the installer keyword when a cmdline is configured", func() {
		line, err := netbootCmdline(squashfs(), "/config.yaml", nil, schema.NetBoot{Cmdline: "kairos.ram console=ttyS0"})
		Expect(err).NotTo(HaveOccurred())

		Expect(line).To(ContainSubstring("kairos.ram console=ttyS0"))
		Expect(line).NotTo(ContainSubstring("install-mode"))
		Expect(line).NotTo(ContainSubstring("rd.live.overlay.overlayfs"))
		Expect(line).To(ContainSubstring(`root=live:{{ ID "` + squashfs() + `" }}`))
		Expect(line).To(ContainSubstring(`config_url={{ ID "/config.yaml" }}`))
	})
})

// A malformed value is an InvalidDeclarationError on the agent side, which
// fails the install. The build already has the string in hand, so saying so
// before any node boots beats every node discovering it separately.
var _ = Describe("extensionsCmdline with a cmdline the agent will refuse", func() {
	It("reports the operator's malformed declaration", func() {
		_, err := extensionsCmdline(GinkgoT().TempDir(), "console=ttyS0 kairos.extensions=@2.1.7")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("the agent will refuse"))
	})
})
