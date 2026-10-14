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

	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// systemPartition is one entry of install.partitions, next to the fixed values
// the installer will really create it with.
type systemPartition struct {
	// key is the cloud-config key the user writes, e.g.
	// "install.partitions.oem".
	key string
	// name, label and fs are what NewInstallElementalPartitions and
	// SetFirmwarePartitions put on the partition, whatever the config says.
	name  string
	label string
	fs    string
	// got is what the install block left on the partition.
	got *sdkPartitions.Partition
}

// checkSystemPartitions rejects an install.partitions entry that sets a
// partition name, a filesystem label or a filesystem.
//
// The four system partitions plus the ESP are rebuilt from Kairos constants in
// NewInstallElementalPartitions and SetFirmwarePartitions, which read the size
// from the config and nothing else. Kairos then finds every one of them by
// that fixed name or label, on boot, on upgrade and on reset, so the values
// cannot simply be honored. Until they can (kairos-io/kairos#2159) a config
// that sets one installs a partition the user did not ask for and says
// nothing, so refuse it here, before the disk is touched.
func checkSystemPartitions(parts []systemPartition) error {
	for _, p := range parts {
		if p.got == nil {
			continue
		}
		if p.got.Name != "" && p.got.Name != p.name {
			return fmt.Errorf(
				"%s.name cannot be %q: the installer always names that partition %q, because the rest of Kairos finds it by that name. Remove the key, or set it to %q. Making it configurable is tracked in kairos-io/kairos#2159",
				p.key, p.got.Name, p.name, p.name)
		}
		if p.got.FilesystemLabel != "" && p.got.FilesystemLabel != p.label {
			return fmt.Errorf(
				"%s.label cannot be %q: the installer always labels that filesystem %q, because the initramfs mounts it from /dev/disk/by-label/%s. Remove the key, or set it to %q. Making it configurable is tracked in kairos-io/kairos#2159",
				p.key, p.got.FilesystemLabel, p.label, p.label, p.label)
		}
		if p.got.FS != "" && p.got.FS != p.fs {
			return fmt.Errorf(
				"%s.fs cannot be %q: the installer always creates %s on that partition and does not read this key, so the value would be dropped and you would get %s anyway. Remove the key, or set it to %s. Making it configurable is tracked in kairos-io/kairos#2159",
				p.key, p.got.FS, p.fs, p.fs, p.fs)
		}
	}
	return nil
}
