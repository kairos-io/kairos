package mocks

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

const ext4 = "ext4"

// GhwMock is used to construct a fake disk to present to ghw when scanning block devices
// The way this works is ghw will use the existing files in the system to determine the different disks, partitions and
// mountpoints. It uses /sys/block, /proc/self/mounts and /run/udev/data to gather everything
// It also has an entrypoint to overwrite the root dir from which the paths are constructed so that allows us to override
// it easily and make it read from a different location.
// This mock is used to construct a fake FS with all its needed files on a different chroot and just add a Disk with its
// partitions and let the struct do its thing creating files and mountpoints and such
// You can even just pass no disks to simulate a system in which there is no disk/no cos partitions
type GhwMock struct {
	Chroot string
	paths  *ghw.Paths
	disks  []partitions.Disk
	mounts []string
}

// AddDisk adds a disk to GhwMock
func (g *GhwMock) AddDisk(disk partitions.Disk) {
	g.disks = append(g.disks, disk)
}

// AddPartitionToDisk will add a partition to the given disk and call Clean+CreateDevices, so we recreate all files
// It makes no effort checking if the disk exists
func (g *GhwMock) AddPartitionToDisk(diskName string, partition *partitions.Partition) {
	for _, disk := range g.disks {
		if disk.Name == diskName {
			disk.Partitions = append(disk.Partitions, partition)
			g.Clean()
			g.CreateDevices()
		}
	}
}

// CreateDevices will create a new context and paths for ghw using the Chroot value as base, then set the env var GHW_ROOT so the
// ghw library picks that up and then iterate over the disks and partitions and create the necessary files
func (g *GhwMock) CreateDevices() {
	d, _ := os.MkdirTemp("", "ghwmock")
	g.Chroot = d
	g.paths = ghw.NewPaths(d)
	// Set the env override to the chroot
	_ = os.Setenv("GHW_CHROOT", d)
	// Create the /sys/block dir
	_ = os.MkdirAll(g.paths.SysBlock, 0755)
	// Create the /run/udev/data dir
	_ = os.MkdirAll(g.paths.RunUdevData, 0755)
	// Create only the /proc/ dir, we add the mounts file afterwards
	procDir, _ := filepath.Split(g.paths.ProcMounts)
	_ = os.MkdirAll(procDir, 0755)
	for indexDisk, disk := range g.disks {
		// For each dir we create the /sys/block/DISK_NAME
		diskPath := filepath.Join(g.paths.SysBlock, disk.Name)
		_ = os.Mkdir(diskPath, 0755)
		// We create a dev file to indicate the devicenumber for a given disk
		_ = os.WriteFile(filepath.Join(g.paths.SysBlock, disk.Name, "dev"), []byte(fmt.Sprintf("%d:0\n", indexDisk)), 0644)
		// Also write the size
		_ = os.WriteFile(filepath.Join(g.paths.SysBlock, disk.Name, "size"), []byte(strconv.FormatUint(disk.SizeBytes, 10)), 0644)
		// Create the udevdata for this disk
		var diskUdevData []string
		diskUdevData = append(diskUdevData, fmt.Sprintf("E:ID_PART_TABLE_UUID=%s\n", disk.UUID))

		// Add DM_NAME for dm devices (needed for isMultipathDevice detection)
		if strings.HasPrefix(disk.Name, "dm-") {
			diskUdevData = append(diskUdevData, fmt.Sprintf("E:DM_NAME=%s\n", disk.Name))
			// Add DM_UUID for multipath devices - must be prefixed with "mpath" for multipath detection
			diskUdevData = append(diskUdevData, fmt.Sprintf("E:DM_UUID=%s\n", disk.UUID))
		}

		_ = os.WriteFile(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b%d:0", indexDisk)), []byte(strings.Join(diskUdevData, "")), 0644)
		for indexPart, partition := range disk.Partitions {
			// For each partition we create the /sys/block/DISK_NAME/PARTITION_NAME
			_ = os.Mkdir(filepath.Join(diskPath, partition.Name), 0755)
			// Create the /sys/block/DISK_NAME/PARTITION_NAME/dev file which contains the major:minor of the partition
			_ = os.WriteFile(filepath.Join(diskPath, partition.Name, "dev"), []byte(fmt.Sprintf("%d:6%d\n", indexDisk, indexPart)), 0644)
			_ = os.WriteFile(filepath.Join(diskPath, partition.Name, "size"), []byte(fmt.Sprintf("%d\n", partition.Size)), 0644)
			// Create the /run/udev/data/bMAJOR:MINOR file with the data inside to mimic the udev database
			var data []string
			if partition.FilesystemLabel != "" {
				data = append(data, fmt.Sprintf("E:ID_FS_LABEL=%s\n", partition.FilesystemLabel))
			}
			if partition.PartitionLabel != "" {
				data = append(data, fmt.Sprintf("E:ID_PART_ENTRY_NAME=%s\n", partition.PartitionLabel))
			}
			if partition.FS != "" {
				data = append(data, fmt.Sprintf("E:ID_FS_TYPE=%s\n", partition.FS))
			}
			if partition.UUID != "" {
				data = append(data, fmt.Sprintf("E:ID_PART_ENTRY_UUID=%s\n", partition.UUID))
			}
			_ = os.WriteFile(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b%d:6%d", indexDisk, indexPart)), []byte(strings.Join(data, "")), 0644)
			// If we got a mountpoint, add it to our fake /proc/self/mounts
			if partition.MountPoint != "" {
				// Check if the partition has a fs, otherwise default to ext4
				if partition.FS == "" {
					partition.FS = ext4
				}
				// Prepare the g.mounts with all the mount lines
				g.mounts = append(
					g.mounts,
					fmt.Sprintf("%s %s %s ro,relatime 0 0\n", filepath.Join("/dev", partition.Name), partition.MountPoint, partition.FS))
			}
		}
	}
	// Finally, write all the mounts
	_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
}

// RemoveDisk will remove the files for a disk. It makes no effort to check if the disk exists or not
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
// It makes no effort checking if the disk/partition/files exist
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
			var newPartitions partitions.PartitionList
			for _, partition := range disk.Partitions {
				if partition.Name != partitionName {
					newPartitions = append(newPartitions, partition)
				}
			}
			g.disks[index].Partitions = newPartitions
		}
	}
}

// Clean will remove the chroot dir and unset the env var
func (g *GhwMock) Clean() {
	// Uset the test override
	_ = os.Unsetenv("GHW_CHROOT")
	_ = os.RemoveAll(g.Chroot)
}

// CreateMultipathDevicesWithDmMounts creates multipath device structure using /dev/dm-<n> mount format
// This is the same as CreateMultipathDevices but mounts partitions as /dev/dm-<n> instead of /dev/mapper/<name>.
func (g *GhwMock) CreateMultipathDevicesWithDmMounts() {
	g.createMultipathDevicesWithMountFormat(true)
}

// CreateMultipathDevices creates multipath device structure in the mock filesystem
// This sets up the basic dm device structure needed for multipath devices.
func (g *GhwMock) CreateMultipathDevices() {
	g.createMultipathDevicesWithMountFormat(false)
}

// createMultipathDevicesWithMountFormat is the common implementation
// useDmMount determines whether to use /dev/dm-<n> (true) or /dev/mapper/<name> (false) for mounts.
func (g *GhwMock) createMultipathDevicesWithMountFormat(useDmMount bool) {
	// Store multipath partitions before clearing them
	multipathPartitions := make(map[string][]*partitions.Partition)

	// Clear partitions from multipath devices before creating basic structure
	// We'll recreate them as multipath partitions after
	for i := range g.disks {
		if strings.HasPrefix(g.disks[i].Name, "dm-") {
			multipathPartitions[g.disks[i].Name] = g.disks[i].Partitions
			g.disks[i].Partitions = nil // Clear existing partitions
		}
	}

	// First create the basic devices (now without partitions for dm devices)
	g.CreateDevices()

	// Now add multipath-specific structure for dm- devices
	for _, disk := range g.disks {
		if strings.HasPrefix(disk.Name, "dm-") {
			diskPath := filepath.Join(g.paths.SysBlock, disk.Name)

			// Create dm/name file
			dmDir := filepath.Join(diskPath, "dm")
			_ = os.MkdirAll(dmDir, 0755)
			_ = os.WriteFile(filepath.Join(dmDir, "name"), []byte(disk.Name), 0644)
			_ = os.WriteFile(filepath.Join(dmDir, "uuid"), []byte(disk.UUID), 0644)

			// Create holders directory for partitions
			holdersDir := filepath.Join(diskPath, "holders")
			_ = os.MkdirAll(holdersDir, 0755)

			// Create slaves directory to indicate this is a multipath device
			slavesDir := filepath.Join(diskPath, "slaves")
			_ = os.MkdirAll(slavesDir, 0755)
			// Add some fake slave devices
			_ = os.WriteFile(filepath.Join(slavesDir, "sda"), []byte(""), 0644)
			_ = os.WriteFile(filepath.Join(slavesDir, "sdb"), []byte(""), 0644)

			// Convert stored partitions to multipath partitions
			if partitions, exists := multipathPartitions[disk.Name]; exists {
				for partIndex, partition := range partitions {
					g.createMultipathPartitionWithMountFormat(disk.Name, partition, partIndex+1, useDmMount)
				}
			}
		}
	}
}

// createMultipathPartitionWithMountFormat creates a multipath partition structure
// useDmMount determines the mount format: true for /dev/dm-<n>, false for /dev/mapper/<name>.
func (g *GhwMock) createMultipathPartitionWithMountFormat(parentDiskName string, partition *partitions.Partition, partNum int, useDmMount bool) {
	parentDiskPath := filepath.Join(g.paths.SysBlock, parentDiskName)
	holdersDir := filepath.Join(parentDiskPath, "holders")
	partitionSuffix := fmt.Sprintf("p%d", partNum)

	// Create the partition as a top-level device in /sys/block/
	partitionPath := filepath.Join(g.paths.SysBlock, partition.Name)
	_ = os.MkdirAll(partitionPath, 0755)

	// Create partition dev file (use unique device numbers)
	udevDataIndex, err := strconv.Atoi((strings.TrimPrefix(partition.Name, "dm-")))
	if err != nil {
		udevDataIndex = 0 // Fallback in case of error
	}

	partIndex := 100 + partNum + udevDataIndex // Ensure unique device numbers
	_ = os.WriteFile(filepath.Join(partitionPath, "dev"), []byte(fmt.Sprintf("253:%d\n", partIndex)), 0644)
	_ = os.WriteFile(filepath.Join(partitionPath, "size"), []byte(fmt.Sprintf("%d\n", partition.Size)), 0644)

	// Create dm structure for partition
	partDmDir := filepath.Join(partitionPath, "dm")
	_ = os.MkdirAll(partDmDir, 0755)
	_ = os.WriteFile(filepath.Join(partDmDir, "name"), []byte(fmt.Sprintf("%s%s", parentDiskName, partitionSuffix)), 0644)
	_ = os.WriteFile(filepath.Join(partDmDir, "uuid"), []byte(partition.UUID), 0644)

	// Create slaves directory for partition pointing to parent
	partSlavesDir := filepath.Join(partitionPath, "slaves")
	_ = os.MkdirAll(partSlavesDir, 0755)
	_ = os.WriteFile(filepath.Join(partSlavesDir, parentDiskName), []byte(""), 0644)

	// Create holder symlink from parent to partition
	_ = os.WriteFile(filepath.Join(holdersDir, partition.Name), []byte(""), 0644)

	// Create udev data for the partition with multipath-specific entries
	udevData := []string{
		fmt.Sprintf("E:DM_NAME=%s%s\n", parentDiskName, partitionSuffix),
		fmt.Sprintf("E:DM_UUID=%s\n", partition.UUID), // This indicates it's a multipath partition
		fmt.Sprintf("E:DM_PART=%d\n", partNum),        // This indicates it's a multipath partition
	}
	if partition.FilesystemLabel != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_FS_LABEL=%s\n", partition.FilesystemLabel))
	}
	if partition.PartitionLabel != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_PART_ENTRY_NAME=%s\n", partition.PartitionLabel))
	}
	if partition.FS != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_FS_TYPE=%s\n", partition.FS))
	}
	if partition.UUID != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_PART_ENTRY_UUID=%s\n", partition.UUID))
	}

	_ = os.WriteFile(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b253:%d", partIndex)), []byte(strings.Join(udevData, "")), 0644)

	// Add mount if specified
	if partition.MountPoint != "" {
		if partition.FS == "" {
			partition.FS = ext4
		}

		var mountDevice string
		if useDmMount {
			// Use /dev/dm-<n> format for mounting
			mountDevice = fmt.Sprintf("/dev/%s", partition.Name)
		} else {
			// Use /dev/mapper/<name> format for mounting
			mountDevice = fmt.Sprintf("/dev/mapper/%s%s", parentDiskName, partitionSuffix)
		}

		g.mounts = append(
			g.mounts,
			fmt.Sprintf("%s %s %s ro,relatime 0 0\n", mountDevice, partition.MountPoint, partition.FS))

		// Rewrite mounts file
		_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
	}
}

// AddMultipathPartition adds a multipath partition to a multipath device
// This creates the partition as a holder of the parent device and sets up
// the necessary dm structure for the partition.
func (g *GhwMock) AddMultipathPartition(parentDiskName string, partition *partitions.Partition) {
	if g.paths == nil {
		return // Must call CreateMultipathDevices first
	}

	parentDiskPath := filepath.Join(g.paths.SysBlock, parentDiskName)
	holdersDir := filepath.Join(parentDiskPath, "holders")

	// Count existing holders to determine partition number
	existingHolders, _ := os.ReadDir(holdersDir)
	partNum := len(existingHolders) + 1
	partitionSuffix := fmt.Sprintf("p%d", partNum)

	// Create the partition as a top-level device in /sys/block/
	partitionPath := filepath.Join(g.paths.SysBlock, partition.Name)
	_ = os.MkdirAll(partitionPath, 0755)

	// Create partition dev file (use unique device numbers)
	partIndex := len(g.mounts) + 100 + partNum // Ensure unique device numbers
	_ = os.WriteFile(filepath.Join(partitionPath, "dev"), []byte(fmt.Sprintf("253:%d\n", partIndex)), 0644)
	_ = os.WriteFile(filepath.Join(partitionPath, "size"), []byte(fmt.Sprintf("%d\n", partition.Size)), 0644)

	// Create dm structure for partition
	partDmDir := filepath.Join(partitionPath, "dm")
	_ = os.MkdirAll(partDmDir, 0755)
	_ = os.WriteFile(filepath.Join(partDmDir, "name"), []byte(fmt.Sprintf("%s%s", parentDiskName, partitionSuffix)), 0644)
	_ = os.WriteFile(filepath.Join(partDmDir, "uuid"), []byte(partition.UUID), 0644)

	// Create slaves directory for partition pointing to parent
	partSlavesDir := filepath.Join(partitionPath, "slaves")
	_ = os.MkdirAll(partSlavesDir, 0755)
	_ = os.WriteFile(filepath.Join(partSlavesDir, parentDiskName), []byte(""), 0644)

	// Create holder symlink from parent to partition
	_ = os.WriteFile(filepath.Join(holdersDir, partition.Name), []byte(""), 0644)

	// Create udev data for the partition with multipath-specific entries
	udevData := []string{
		fmt.Sprintf("E:DM_NAME=%s%s\n", parentDiskName, partitionSuffix),
		fmt.Sprintf("E:DM_PART=%d\n", partNum), // This indicates it's a multipath partition
	}
	if partition.FilesystemLabel != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_FS_LABEL=%s\n", partition.FilesystemLabel))
	}
	if partition.PartitionLabel != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_PART_ENTRY_NAME=%s\n", partition.PartitionLabel))
	}
	if partition.FS != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_FS_TYPE=%s\n", partition.FS))
	}
	if partition.UUID != "" {
		udevData = append(udevData, fmt.Sprintf("E:ID_PART_ENTRY_UUID=%s\n", partition.UUID))
	}

	_ = os.WriteFile(filepath.Join(g.paths.RunUdevData, fmt.Sprintf("b253:%d", partIndex)), []byte(strings.Join(udevData, "")), 0644)

	// Add mount if specified
	if partition.MountPoint != "" {
		if partition.FS == "" {
			partition.FS = ext4
		}
		// For multipath partitions, they can be mounted by /dev/mapper/ name or /dev/dm- name
		g.mounts = append(
			g.mounts,
			fmt.Sprintf("/dev/mapper/%s%s %s %s ro,relatime 0 0\n", parentDiskName, partitionSuffix, partition.MountPoint, partition.FS))
	}

	// Rewrite mounts file
	_ = os.WriteFile(g.paths.ProcMounts, []byte(strings.Join(g.mounts, "")), 0644)
}
