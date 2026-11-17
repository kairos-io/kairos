/*
Copyright © 2026 The Kairos Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/AuroraBoot/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
)

// Regression tests for https://github.com/kairos-io/kairos/issues/4324:
// an --overlay-iso directory that is a Kubernetes Secret/ConfigMap mount
// (kubelet "atomic writer" symlink layout) must be flattened to a plain
// tree before it is merged onto the ISO root.
var _ = Describe("MaterializeOverlay", func() {
	var src string

	BeforeEach(func() {
		var err error
		src, err = os.MkdirTemp("", "overlay-src")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(src)
	})

	// kubeletLayout builds the exact structure kubelet produces for a
	// Secret/ConfigMap volume holding the given files (paths relative to the
	// volume root): a timestamped dir with the real content, a `..data`
	// symlink to it, and one top-level symlink per first path component.
	kubeletLayout := func(files map[string]string) {
		timestamped := filepath.Join(src, "..2026_08_17_20_28_56.3347250082")
		linked := map[string]bool{}
		for name, content := range files {
			real := filepath.Join(timestamped, filepath.FromSlash(name))
			Expect(os.MkdirAll(filepath.Dir(real), 0755)).To(Succeed())
			Expect(os.WriteFile(real, []byte(content), 0644)).To(Succeed())
			// One top-level symlink per first path component, into ..data.
			first := firstComponent(name)
			if !linked[first] {
				Expect(os.Symlink(filepath.Join("..data", first), filepath.Join(src, first))).To(Succeed())
				linked[first] = true
			}
		}
		Expect(os.Symlink(filepath.Base(timestamped), filepath.Join(src, "..data"))).To(Succeed())
	}

	It("copies a plain overlay unchanged", func() {
		Expect(os.MkdirAll(filepath.Join(src, "boot", "grub2"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(src, "boot", "grub2", "grub.cfg"), []byte("set timeout=5"), 0644)).To(Succeed())

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		content, err := os.ReadFile(filepath.Join(dst, "boot", "grub2", "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("set timeout=5"))
	})

	It("flattens a Kubernetes atomic-writer mount with a directory symlink", func() {
		kubeletLayout(map[string]string{"boot/grub2/grub.cfg": "set timeout=5"})

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		// boot must be a REAL directory in the materialized tree, not a symlink
		info, err := os.Lstat(filepath.Join(dst, "boot"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).To(BeZero())
		Expect(info.IsDir()).To(BeTrue())

		content, err := os.ReadFile(filepath.Join(dst, "boot", "grub2", "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("set timeout=5"))

		// kubelet internals must not leak into the materialized tree
		Expect(dirNames(dst)).To(Equal([]string{"boot"}))
	})

	It("flattens a Kubernetes atomic-writer mount with a file symlink", func() {
		// The most common kubelet layout: each ConfigMap/Secret key is a plain
		// file, so the top-level symlink points at a file, not a directory.
		kubeletLayout(map[string]string{"grub.cfg": "set timeout=5"})

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		info, err := os.Lstat(filepath.Join(dst, "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).To(BeZero())
		Expect(info.Mode().IsRegular()).To(BeTrue())

		content, err := os.ReadFile(filepath.Join(dst, "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("set timeout=5"))

		Expect(dirNames(dst)).To(Equal([]string{"grub.cfg"}))
	})

	It("flattens a mixed mount: file symlink, directory symlink, plain entries", func() {
		kubeletLayout(map[string]string{
			"boot/grub2/grub.cfg": "set timeout=5",
			"meta-data":           "instance-id: i-123",
		})
		// A plain file and a plain directory alongside the symlinks (a manually
		// constructed dir + projected volume, or an overlay tar extracted next
		// to a mount).
		Expect(os.WriteFile(filepath.Join(src, "user-data"), []byte("#cloud-config"), 0644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(src, "EFI", "BOOT"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(src, "EFI", "BOOT", "grub.cfg"), []byte("chainload"), 0644)).To(Succeed())

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		for name, want := range map[string]string{
			"boot/grub2/grub.cfg": "set timeout=5",
			"meta-data":           "instance-id: i-123",
			"user-data":           "#cloud-config",
			"EFI/BOOT/grub.cfg":   "chainload",
		} {
			content, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(name)))
			Expect(err).ToNot(HaveOccurred(), name)
			Expect(string(content)).To(Equal(want), name)
		}
		Expect(dirNames(dst)).To(ConsistOf("boot", "meta-data", "user-data", "EFI"))
	})

	It("dereferences a nested symlink inside a real directory", func() {
		// Defense in depth: kubelet only symlinks top-level entries, but an
		// overlay could carry a symlink deeper in the tree.
		Expect(os.MkdirAll(filepath.Join(src, "boot"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(src, "real.cfg"), []byte("real"), 0644)).To(Succeed())
		Expect(os.Symlink(filepath.Join("..", "real.cfg"), filepath.Join(src, "boot", "grub.cfg"))).To(Succeed())

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		info, err := os.Lstat(filepath.Join(dst, "boot", "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).To(BeZero())
		content, err := os.ReadFile(filepath.Join(dst, "boot", "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("real"))
	})

	It("keeps top-level dotfiles that are not kubelet internals", func() {
		Expect(os.WriteFile(filepath.Join(src, ".ignition"), []byte("{}"), 0644)).To(Succeed())

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		_, err = os.Stat(filepath.Join(dst, ".ignition"))
		Expect(err).ToNot(HaveOccurred())
	})

	It("leaves the source overlay untouched", func() {
		kubeletLayout(map[string]string{"boot/grub2/grub.cfg": "set timeout=5"})

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		// The source must still be the symlinked kubelet layout — the provider
		// pod's mounted Secret is read-only in the real pipeline.
		info, err := os.Lstat(filepath.Join(src, "boot"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).ToNot(BeZero())
		Expect(dirNames(src)).To(ConsistOf("boot", "..data", "..2026_08_17_20_28_56.3347250082"))
	})

	It("returns a fresh directory for an empty overlay", func() {
		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)
		Expect(dirNames(dst)).To(BeEmpty())
	})

	It("fails loudly on a dangling symlink instead of silently dropping it", func() {
		Expect(os.Symlink("does-not-exist", filepath.Join(src, "boot"))).To(Succeed())

		_, err := utils.MaterializeOverlay(src)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a symlink with an absolute target outside the overlay root", func() {
		// e.g. an untrusted overlay carrying `etc -> /etc` must not leak host
		// files into the ISO.
		Expect(os.Symlink("/etc", filepath.Join(src, "etc"))).To(Succeed())

		_, err := utils.MaterializeOverlay(src)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("outside the overlay root"))
	})

	It("rejects a symlink escaping the overlay root via '..'", func() {
		outside := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(outside, "secret"), []byte("top secret"), 0644)).To(Succeed())
		Expect(os.Symlink(filepath.Join("..", filepath.Base(outside), "secret"), filepath.Join(src, "evil"))).To(Succeed())

		_, err := utils.MaterializeOverlay(src)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("outside the overlay root"))
	})

	It("rejects a symlink to its own ancestor directory", func() {
		// Dereferencing `boot/loop -> ..` would copy boot/ into itself forever.
		Expect(os.MkdirAll(filepath.Join(src, "boot"), 0755)).To(Succeed())
		Expect(os.Symlink("..", filepath.Join(src, "boot", "loop"))).To(Succeed())

		_, err := utils.MaterializeOverlay(src)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ancestor"))
	})

	It("allows an absolute symlink whose target stays inside the overlay root", func() {
		Expect(os.WriteFile(filepath.Join(src, "real.cfg"), []byte("real"), 0644)).To(Succeed())
		Expect(os.Symlink(filepath.Join(src, "real.cfg"), filepath.Join(src, "alias.cfg"))).To(Succeed())

		dst, err := utils.MaterializeOverlay(src)
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dst)

		content, err := os.ReadFile(filepath.Join(dst, "alias.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("real"))
	})

	It("fails when the overlay does not exist", func() {
		_, err := utils.MaterializeOverlay(filepath.Join(src, "no-such-dir"))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("OverlayCopyOptions", func() {
	var src, dst string

	BeforeEach(func() {
		var err error
		src, err = os.MkdirTemp("", "overlay-src")
		Expect(err).ToNot(HaveOccurred())
		dst, err = os.MkdirTemp("", "overlay-dst")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(src)
		os.RemoveAll(dst)
	})

	It("merges a symlinked overlay directory into a pre-existing real directory", func() {
		// The InjectISO case: the destination tree already has content (a real
		// boot/ directory, as the EFI step would have created) and the overlay
		// must merge into it, not replace it with a symlink.
		timestamped := filepath.Join(src, "..2026_08_17_20_28_56.3347250082")
		Expect(os.MkdirAll(filepath.Join(timestamped, "boot", "grub2"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(timestamped, "boot", "grub2", "grub.cfg"), []byte("custom"), 0644)).To(Succeed())
		Expect(os.Symlink(filepath.Base(timestamped), filepath.Join(src, "..data"))).To(Succeed())
		Expect(os.Symlink(filepath.Join("..data", "boot"), filepath.Join(src, "boot"))).To(Succeed())

		Expect(os.MkdirAll(filepath.Join(dst, "boot"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dst, "boot", "vmlinuz"), []byte("kernel"), 0644)).To(Succeed())

		Expect(copy.Copy(src, dst, utils.OverlayCopyOptions(src))).To(Succeed())

		// boot stays a real directory and both the old and the new content exist
		info, err := os.Lstat(filepath.Join(dst, "boot"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).To(BeZero())
		Expect(info.IsDir()).To(BeTrue())

		content, err := os.ReadFile(filepath.Join(dst, "boot", "grub2", "grub.cfg"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("custom"))
		content, err = os.ReadFile(filepath.Join(dst, "boot", "vmlinuz"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("kernel"))

		Expect(dirNames(dst)).To(Equal([]string{"boot"}))
	})
})

// firstComponent returns the first path element of a slash-separated
// relative path ("boot/grub2/grub.cfg" -> "boot", "grub.cfg" -> "grub.cfg").
func firstComponent(name string) string {
	for i, c := range name {
		if c == '/' {
			return name[:i]
		}
	}
	return name
}

// dirNames returns the sorted names of the entries directly under dir.
func dirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	Expect(err).ToNot(HaveOccurred())
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
