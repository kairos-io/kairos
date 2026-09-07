/*
Copyright © 2026 SUSE LLC

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
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// hugeFile is larger than any /tmp a test host is going to hand out, so a
// check that asks for it is guaranteed to come back short. It has to stay
// within ext4's 16 TiB max file size or the sparse truncate itself returns
// EFBIG on the runner before the test can even call the function under test.
const hugeFile int64 = 4 << 40 // 4 TiB

var _ = Describe("CheckESPRefreshSpace", func() {
	var fs vfs.FS
	var cleanup func()
	var err error

	// write creates a sparse file of the given size under path.
	write := func(path string, size int64) {
		Expect(fsutils.MkdirAll(fs, pathDir(path), cnst.DirPerm)).ToNot(HaveOccurred())
		f, err := fs.Create(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).ToNot(HaveOccurred())
		Expect(fs.Truncate(path, size)).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		Expect(fsutils.MkdirAll(fs, "/source", cnst.DirPerm)).ToNot(HaveOccurred())
		Expect(fsutils.MkdirAll(fs, "/efi/EFI/boot", cnst.DirPerm)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	It("passes when the shim and grub bytes fit", func() {
		// Any real Hadron path is fine; the first-match walk will find it.
		write("/source/usr/lib/shim/shimx64.efi.signed", 1024)
		write("/source/usr/lib/grub/x86_64-efi/grubx64.efi", 2048)

		Expect(utils.CheckESPRefreshSpace(fs, "amd64", "/source", "/efi")).To(Succeed())
	})

	It("fails before writing when the shim will not fit", func() {
		write("/source/usr/lib/shim/shimx64.efi.signed", hugeFile)
		write("/source/usr/lib/grub/x86_64-efi/grubx64.efi", 2048)

		err := utils.CheckESPRefreshSpace(fs, "amd64", "/source", "/efi")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not enough space on the EFI partition"))
	})

	It("counts the current shim and grub bytes as reclaimable", func() {
		// A refresh that writes exactly the bytes it is overwriting has to
		// pass, otherwise a machine at the same fill level can never be
		// updated to a same-sized signed shim rotation.
		const size int64 = 4 << 20 // 4MiB
		write("/source/usr/lib/shim/shimx64.efi.signed", size)
		write("/source/usr/lib/grub/x86_64-efi/grubx64.efi", size)
		write("/efi/EFI/boot/shimx64.efi", size)
		write("/efi/EFI/boot/grubx64.efi", size)
		write("/efi/EFI/boot/BOOTX64.EFI", size)

		Expect(utils.CheckESPRefreshSpace(fs, "amd64", "/source", "/efi")).To(Succeed())
	})

	It("passes on a first-ever refresh with no existing shim or grub at the target", func() {
		write("/source/usr/lib/shim/shimx64.efi.signed", 1024)
		write("/source/usr/lib/grub/x86_64-efi/grubx64.efi", 2048)
		// no /efi/EFI/boot contents

		Expect(utils.CheckESPRefreshSpace(fs, "amd64", "/source", "/efi")).To(Succeed())
	})

	It("returns nil when the source has no shim or grub", func() {
		// RefreshESP itself surfaces this as an error; the space check must
		// not shadow that with a misleading "not enough space" message.
		Expect(utils.CheckESPRefreshSpace(fs, "amd64", "/source", "/efi")).To(Succeed())
	})

	It("does not require a shim on riscv64", func() {
		// riscv64 boots grub.efi directly with no shim.
		write("/source/usr/share/efi/riscv64/grub.efi", 2048)

		Expect(utils.CheckESPRefreshSpace(fs, "riscv64", "/source", "/efi")).To(Succeed())
	})
})

// pathDir returns the directory portion of path, since the test writes files
// with implicit directory creation.
func pathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}
