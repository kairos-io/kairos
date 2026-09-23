/*
Copyright © 2022 SUSE LLC

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

package config

import (
	"fmt"
	"slices"
	"strings"

	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// mountableImgFS are the filesystems the initramfs can mount as the root
// image. immucore passes a literal "ext4" to mount(2) in OpMountRoot
// (immucore/pkg/state/steps.go), and the ext4 driver also reads ext2 and ext3,
// which is why the default LinuxImgFs of ext2 works. With anything else the
// device is there and the mount fails.
var mountableImgFS = []string{"ext2", "ext3", "ext4"}

// imageSlot is one install image slot as the installer computed it, next to
// what the cloud-config left on it.
type imageSlot struct {
	// key is the cloud-config key the user writes, e.g. "install.system".
	key string
	// boundTo is the filesystem label the initramfs looks this image up by.
	// It is not always computed.Label: a squashfs recovery image is built
	// with no label and gets SystemLabel later, in elemental.DeployImage.
	boundTo string
	// computed is the slot before the install block was decoded over it.
	computed sdkImages.Image
	// got is the slot after.
	got sdkImages.Image
}

// checkImageSlots rejects an install block that changed a value on an image
// slot which the boot path does not read from the config.
//
// The initramfs finds each image by a fixed filesystem label
// (bootStateToSysrootLabel in immucore/internal/utils/common.go) and mounts it
// as ext4, so a label the installer did not choose leaves the root mount with
// no device, and a filesystem outside the ext2/ext3/ext4 family leaves it with
// a device it cannot mount. Both install cleanly and then boot to an emergency
// shell, which is why this fails here, before the disk is touched, rather than
// being applied. See kairos-io/kairos#4914.
func checkImageSlots(slots []imageSlot) error {
	for _, s := range slots {
		if s.got.Label != s.computed.Label {
			return fmt.Errorf(
				"%s.label cannot be set: the initramfs finds this image at /dev/disk/by-label/%s, so labeling it %q installs a system that cannot mount its root. Remove the key",
				s.key, s.boundTo, s.got.Label)
		}
		if s.got.FS != s.computed.FS && !slices.Contains(mountableImgFS, s.got.FS) {
			return fmt.Errorf(
				"%s.fs cannot be %q: the initramfs mounts this image as ext4, which reads only %s. Remove the key",
				s.key, s.got.FS, strings.Join(mountableImgFS, ", "))
		}
	}
	return nil
}
