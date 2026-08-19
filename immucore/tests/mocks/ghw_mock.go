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

package mocks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/linuxpath"
)

// GhwMock is used to construct a fake disk to present to ghw when scanning block devices
// The way this works is ghw will use the existing files in the system to determine the different disks, partitions and
// mountpoints. It uses /sys/block, /proc/self/mounts and /run/udev/data to gather everything
// It also has an entrypoint to overwrite the root dir from which the paths are constructed so that allows us to override
// it easily and make it read from a different location.
// This mock is used to construct a fake FS with all its needed files on a different chroot and just add a Disk with its
// partitions and let the struct do its thing creating files and mountpoints and such
// You can even just pass no disks to simulate a system in which there is no disk/no cos partitions.
type GhwMock struct {
	chroot string
	paths  *linuxpath.Paths
	disks  []block.Disk
	mounts []string
}

// AddDisk adds a disk to GhwMock.
func (g *GhwMock) AddDisk(disk block.Disk) {
	g.disks = append(g.disks, disk)
}

// AddPartitionToDisk will add a partition to the given disk and call Clean+CreateDevices, so we recreate all files
// It makes no effort checking if the disk exists.
func (g *GhwMock) AddPartitionToDisk(diskName string, partition *block.Partition) {
	for _, disk := range g.disks {
		if disk.Name == diskName {
			disk.Partitions = append(disk.Partitions, partition)
			g.Clean()
			g.CreateDevices()
		}
	}
}

// CreateDevices will create the paths for ghw using the Chroot value as base, then set the env var GHW_CHROOT so the
// ghw library picks that up and then iterate over the disks and partitions and create the necessary files.
func (g *GhwMock) CreateDevices() {
	d, _ := os.MkdirTemp("", "ghwmock")
	g.chroot = d
	// ghw >= v0.24.0 dropped the public pkg/context, and the chroot is now wired into ghw
	// through the GHW_CHROOT env var (set below). We build the paths relative to the chroot
	// ourselves, mirroring linuxpath's default layout, so the mock knows where to write files.
	g.paths = &linuxpath.Paths{
		SysBlock:    filepath.Join(d, "sys", "block"),
		RunUdevData: filepath.Join(d, "run", "udev", "data"),
		ProcMounts:  filepath.Join(d, "proc", "self", "mounts"),
	}
	_ = os.Setenv("GHW_CHROOT", g.chroot)
	// Create the /sys/block dir
	_ = os.MkdirAll(g.paths.SysBlock, 0755)
	// Create the /run/udev/data dir
	_ = os.MkdirAll(g.paths.RunUdevData, 0755)
	// Create only the /proc/self dir, we add the mounts file afterwards
	procDir, _ := filepath.Split(g.paths.ProcMounts)
	_ = os.MkdirAll(procDir, 0755)

	for indexDisk, disk := range g.disks {
		// For each dir we create the /sys/block/DISK_NAME
		diskPath := filepath.Join(g.paths.SysBlock, disk.Name)
		_ = os.Mkdir(diskPath, 0755)
		for indexPart, partition := range disk.Partitions {
			// For each partition we create the /sys/block/DISK_NAME/PARTITION_NAME
			_ = os.Mkdir(filepath.Join(diskPath, partition.Name), 0755)
			// Create the /sys/block/DISK_NAME/PARTITION_NAME/dev file which contains the major:minor of the partition
			_ = os.WriteFile(filepath.Join(diskPath, partition.Name, "dev"), []byte(fmt.Sprintf("%d:6%d\n", indexDisk, indexPart)), 0644)
			// Create the /run/udev/data/bMAJOR:MINOR file with the data inside to mimic the udev database
			data := []string{fmt.Sprintf("E:ID_PART_ENTRY_NAME=%s\n", partition.FilesystemLabel)}
			data = append(data, fmt.Sprintf("E:ID_FS_LABEL=%s\n", partition.Label))
			if partition.Type != "" {
				data = append(data, fmt.Sprintf("E:ID_FS_TYPE=%s\n", partition.Type))
			}
			_ = os.WriteFile(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b%d:6%d", indexDisk, indexPart)), []byte(strings.Join(data, "")), 0644)
			// If we got a mountpoint, add it to our fake /proc/self/mounts
			if partition.MountPoint != "" {
				// Check if the partition has a fs, otherwise default to ext4
				if partition.Type == "" {
					partition.Type = "ext4"
				}
				// Prepare the g.mounts with all the mount lines
				g.mounts = append(
					g.mounts,
					fmt.Sprintf("%s %s %s ro,relatime 0 0\n", filepath.Join("/dev", partition.Name), partition.MountPoint, partition.Type))
			}
		}
	}
	// Finally, write all the mounts
	_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
}

// RemoveDisk will remove the files for a disk. It makes no effort to check if the disk exists or not.
func (g *GhwMock) RemoveDisk(disk string) {
	// This could be simpler I think, just removing the /sys/block/DEVICE should make ghw not find anything and not search
	// for partitions, but just in case do it properly
	var newMounts []string
	diskPath := filepath.Join(g.paths.SysBlock, disk)
	_ = os.RemoveAll(diskPath)

	// Try to find any mounts that match the disk given and remove them from the mounts
	for _, mount := range g.mounts {
		fields := strings.Fields(mount)
		// If first field does not contain the /dev/DEVICE, add it to the newmounts
		if !strings.Contains(fields[0], filepath.Join("/dev", disk)) {
			newMounts = append(newMounts, mount)
		}
	}
	g.mounts = newMounts
	// Write the mounts again
	_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
}

// RemovePartitionFromDisk will remove the files for a partition
// It makes no effort checking if the disk/partition/files exist.
func (g *GhwMock) RemovePartitionFromDisk(diskName string, partitionName string) {
	var newMounts []string
	diskPath := filepath.Join(g.paths.SysBlock, diskName)
	// Read the dev major:minor
	devName, _ := os.ReadFile(filepath.Join(diskPath, partitionName, "dev"))
	// Remove the MAJOR:MINOR file from the udev database
	_ = os.RemoveAll(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b%s", devName)))
	// Remove the /sys/block/DISK/PARTITION dir
	_ = os.RemoveAll(filepath.Join(diskPath, partitionName))

	// Try to find any mounts that match the partition given and remove them from the mounts
	for _, mount := range g.mounts {
		fields := strings.Fields(mount)
		// If first field does not contain the /dev/PARTITION, add it to the newmounts
		if !strings.Contains(fields[0], filepath.Join("/dev", partitionName)) {
			newMounts = append(newMounts, mount)
		}
	}
	g.mounts = newMounts
	// Write the mounts again
	_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
	// Remove it from the partitions list
	for index, disk := range g.disks {
		if disk.Name == diskName {
			var newPartitions []*block.Partition
			for _, partition := range disk.Partitions {
				if partition.Name != partitionName {
					newPartitions = append(newPartitions, partition)
				}
			}
			g.disks[index].Partitions = newPartitions
		}
	}
}

// Clean will remove the chroot dir and unset the env var.
func (g *GhwMock) Clean() {
	_ = os.Unsetenv("GHW_CHROOT")
	_ = os.RemoveAll(g.chroot)
}
