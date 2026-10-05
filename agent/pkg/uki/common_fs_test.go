/*
Copyright © 2026 Kairos authors

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

package uki

import (
	"bytes"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// The rest of the common.go coverage runs against vfs.OSFS rooted in a real
// temp dir, which cannot tell a helper that writes through the filesystem it
// was given from one that writes through the global os package: both resolve
// the same absolute path. These specs use vfst.NewTestFS, where the two differ,
// so they are the ones that hold the helpers to their own signature.
var _ = Describe("Common helpers on a filesystem that is not the OS root", func() {
	var fs vfs.FS
	var cleanup func()
	var logger sdkLogger.KairosLogger

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { cleanup() })

		logger = sdkLogger.NewBufferLogger(&bytes.Buffer{})
		Expect(fsutils.MkdirAll(fs, "/efi/EFI/Kairos", constants.DirPerm)).To(Succeed())
	})

	It("removeArtifactSetWithRole deletes from the given filesystem", func() {
		Expect(fs.WriteFile("/efi/EFI/Kairos/norole.efi", []byte("new"), 0o644)).To(Succeed())
		Expect(fs.WriteFile("/efi/EFI/Kairos/norole.conf", []byte("efi /EFI/Kairos/norole.efi\n"), 0o644)).To(Succeed())
		Expect(fs.WriteFile("/efi/EFI/Kairos/active.efi", []byte("old"), 0o644)).To(Succeed())

		Expect(removeArtifactSetWithRole(fs, "/efi", UnassignedArtifactRole)).To(Succeed())

		for _, gone := range []string{"/efi/EFI/Kairos/norole.efi", "/efi/EFI/Kairos/norole.conf"} {
			exists, err := fsutils.Exists(fs, gone)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse(), "%s should have been removed from the given filesystem", gone)
		}

		kept, err := fs.ReadFile("/efi/EFI/Kairos/active.efi")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(kept)).To(Equal("old"))
	})

	It("replaceRoleInKey rewrites the conf on the given filesystem", func() {
		path := "/efi/EFI/Kairos/passive.conf"
		Expect(fs.WriteFile(path, []byte("efi /EFI/Kairos/active.efi\ntitle Kairos\n"), 0o644)).To(Succeed())

		Expect(replaceRoleInKey(fs, path, "efi", "active", "passive", logger)).To(Succeed())

		content, err := fs.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("efi /EFI/Kairos/passive.efi"))
		Expect(string(content)).ToNot(ContainSubstring("active.efi"))
	})

	It("replaceConfTitle rewrites the title on the given filesystem", func() {
		path := "/efi/EFI/Kairos/passive.conf"
		Expect(fs.WriteFile(path, []byte("efi /EFI/Kairos/passive.efi\ntitle Kairos\n"), 0o644)).To(Succeed())

		Expect(replaceConfTitle(fs, path, "passive")).To(Succeed())

		content, err := fs.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("title Kairos (fallback)"))
	})

	It("AddSystemdConfSortKey writes the sort key on the given filesystem", func() {
		path := "/efi/EFI/Kairos/active.conf"
		Expect(fs.WriteFile(path, []byte("efi /EFI/Kairos/active.efi\ntitle Kairos\n"), 0o644)).To(Succeed())

		Expect(AddSystemdConfSortKey(fs, "/efi", logger)).To(Succeed())

		content, err := fs.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("sort-key 0001"))
	})

	It("copyArtifactSetRole produces a usable set on the given filesystem", func() {
		Expect(fs.WriteFile("/efi/EFI/Kairos/active.efi", []byte("body"), 0o644)).To(Succeed())
		Expect(fs.WriteFile("/efi/EFI/Kairos/active.conf", []byte("efi /EFI/Kairos/active.efi\ntitle Kairos\n"), 0o644)).To(Succeed())

		Expect(copyArtifactSetRole(fs, "/efi", "active", "passive", logger)).To(Succeed())

		conf, err := fs.ReadFile("/efi/EFI/Kairos/passive.conf")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(conf)).To(ContainSubstring("efi /EFI/Kairos/passive.efi"))
		Expect(string(conf)).To(ContainSubstring("title Kairos (fallback)"))
	})
})
