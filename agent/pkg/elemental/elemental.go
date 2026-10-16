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

package elemental

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/partitioner"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils/loop"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// Elemental is the struct meant to self-contain most utils and actions related to Elemental, like installing or applying selinux
type Elemental struct {
	config *sdkConfig.Config
}

func NewElemental(config *sdkConfig.Config) *Elemental {
	return &Elemental{
		config: config,
	}
}

// FormatPartition will format an already existing partition
func (e *Elemental) FormatPartition(part *sdkPartitions.Partition, opts ...string) error {
	e.config.Logger.Infof("Formatting '%s' partition", part.FilesystemLabel)
	return partitioner.FormatDevice(e.config.Logger, e.config.Runner, part.Path, part.FS, part.FilesystemLabel, opts...)
}

// PartitionAndFormatDevice creates a new empty partition table on target disk
// and applies the configured disk layout by creating and formatting all
// required partitions
func (e *Elemental) PartitionAndFormatDevice(i sdkSpec.SharedInstallSpec) error {
	if _, err := os.Stat(i.GetTarget()); os.IsNotExist(err) {
		e.config.Logger.Errorf("Disk %s does not exist", i.GetTarget())
		return fmt.Errorf("disk %s does not exist", i.GetTarget())
	}

	disk, err := partitioner.NewDisk(i.GetTarget(), partitioner.WithLogger(e.config.Logger))
	if err != nil {
		return err
	}

	diskClosed := false
	closeDisk := func() {
		if diskClosed {
			return
		}
		diskClosed = true
		if err := disk.Close(); err != nil {
			e.config.Logger.Errorf("Close disk: %s", err)
		}
	}
	defer closeDisk()

	e.config.Logger.Infof("Partitioning device...")
	parts := i.GetPartitions()
	err = disk.NewPartitionTable(i.GetPartTable(), parts.PartitionsByInstallOrder(i.GetExtraPartitions()))
	if err != nil {
		e.config.Logger.Errorf("Failed creating new partition table: %s", err)
		return err
	}

	// Try to make the kernel re-read the partition table a couple of times
	const rereadAttempts = 5
	for attempt := 0; attempt < rereadAttempts; attempt++ {
		err = disk.ReReadPartitionTable()
		if err == nil {
			break
		}
		e.config.Logger.Debugf("Reread table attempt %d failed: %s", attempt+1, err)
		if attempt < rereadAttempts-1 {
			// Back off a bit more on every attempt before retrying
			wait := time.Duration(attempt+1) * time.Second
			e.config.Logger.Debugf("Waiting %s before next attempt", wait)
			time.Sleep(wait)
		}
	}
	if err != nil {
		e.config.Logger.Errorf("Reread table: %s", err)
		return err
	}

	table, err := disk.GetPartitionTable()
	if err != nil {
		e.config.Logger.Errorf("table: %s", err)
		return err
	}
	closeDisk()
	// Sync changes
	syscall.Sync()
	// Trigger udevadm to refresh devices
	_, err = e.config.Runner.Run("udevadm", "trigger")
	if err != nil {
		e.config.Logger.Errorf("Udevadm trigger failed: %s", err)
	}
	_, err = e.config.Runner.Run("udevadm", "settle")
	if err != nil {
		e.config.Logger.Errorf("Udevadm settle failed: %s", err)
	}
	// Partitions are in order so we can format them via that
	parts = i.GetPartitions()
	for _, p := range table.GetPartitions() {
		for _, configPart := range parts.PartitionsByInstallOrder(i.GetExtraPartitions()) {
			if configPart.Name == cnst.BiosPartName {
				// Grub partition on non-EFI is not formatted. Grub is directly installed on it
				continue
			}
			// we have to match the Fs it was asked with the partition in the system
			if p.(*gpt.Partition).Name == configPart.Name {
				fs := configPart.FS
				if fs == "" {
					fs = cnst.LinuxImgFs
				}
				switch fs {
				case "-", "none", "noformat":
					e.config.Logger.Infof("Partition %s is configured without a filesystem, leaving it unformatted", configPart.Name)
					continue
				}
				e.config.Logger.Debugf("Formatting partition: %s", configPart.FilesystemLabel)
				// Get full partition path by the /dev/disk/by-partlabel/ facility
				// So we don't need to infer the actual device under it but get udev to tell us
				// So this works for "normal" devices that have the "expected" partitions (i.e. /dev/sda has /dev/sda1, /dev/sda2)
				// And "weird" devices that have special subdevices like mmc or nvme
				// i.e. /dev/mmcblk0 has /dev/mmcblk0p1, /dev/mmcblk0p2
				device, err := filepath.EvalSymlinks(fmt.Sprintf("/dev/disk/by-partlabel/%s", configPart.Name))
				if err != nil {
					e.config.Logger.Errorf("Failed finding partition %s by partition label: %s", configPart.FilesystemLabel, err)
				}
				err = partitioner.FormatDevice(e.config.Logger, e.config.Runner, device, fs, configPart.FilesystemLabel)
				if err != nil {
					e.config.Logger.Errorf("Failed formatting partition: %s", err)
					return err
				}
				syscall.Sync()
			}
		}
	}
	return nil
}

// MountPartitions mounts configured partitions. Partitions with an unset mountpoint are not mounted.
// Note umounts must be handled by caller logic.
func (e Elemental) MountPartitions(parts sdkPartitions.PartitionList) error {
	e.config.Logger.Infof("Mounting disk partitions")
	var err error

	for _, part := range parts {
		if part.MountPoint != "" {
			err = e.MountPartition(part, "rw")
			if err != nil {
				_ = e.UnmountPartitions(parts)
				return err
			}
		}
	}

	return err
}

// UnmountPartitions unmounts configured partitiosn. Partitions with an unset mountpoint are not unmounted.
func (e Elemental) UnmountPartitions(parts sdkPartitions.PartitionList) error {
	e.config.Logger.Infof("Unmounting disk partitions")
	var err error
	errMsg := ""
	failure := false

	// If there is an early error we still try to unmount other partitions
	for _, part := range parts {
		if part.MountPoint != "" {
			err = e.UnmountPartition(part)
			if err != nil {
				errMsg += fmt.Sprintf("Failed to unmount %s\n Error: %s\n", part.MountPoint, err.Error())
				failure = true
			}
		}
	}
	if failure {
		return errors.New(errMsg)
	}
	return nil
}

// MountRWPartition mounts, or remounts if needed, a partition with RW permissions
func (e Elemental) MountRWPartition(part *sdkPartitions.Partition) (umount func() error, err error) {
	if mnt, _ := utils.IsMounted(e.config, part); mnt {
		err = e.MountPartition(part, "remount", "rw")
		if err != nil {
			e.config.Logger.Errorf("failed mounting %s partition: %v", part.Name, err)
			return nil, err
		}
		umount = func() error {
			e.config.Logger.Debugf("Remounting partition %s as read-only", part.FilesystemLabel)
			// Remount with bind and read-only options so we avoid in use errors
			return e.config.Syscall.Mount(part.MountPoint, part.MountPoint, "", syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_BIND, "")
		}
	} else {
		err = e.MountPartition(part, "rw")
		if err != nil {
			e.config.Logger.Errorf("failed mounting %s partition: %v", part.Name, err)
			return nil, err
		}
		umount = func() error { return e.UnmountPartition(part) }
	}
	return umount, nil
}

// MountPartition mounts a partition with the given mount options
func (e Elemental) MountPartition(part *sdkPartitions.Partition, opts ...string) error {
	e.config.Logger.Debugf("Mounting partition %s", part.FilesystemLabel)
	err := fsutils.MkdirAll(e.config.Fs, part.MountPoint, cnst.DirPerm)
	if err != nil {
		return err
	}
	if part.Path == "" {
		// Lets error out only after 10 attempts to find the device
		device, err := utils.GetDeviceByLabel(e.config, part.FilesystemLabel, 10)
		if err != nil {
			e.config.Logger.Errorf("Could not find a device with label %s", part.FilesystemLabel)
			return err
		}
		part.Path = device
	}
	err = e.config.Mounter.Mount(part.Path, part.MountPoint, "auto", opts)
	if err != nil {
		e.config.Logger.Errorf("Failed mounting device %s with label %s", part.Path, part.FilesystemLabel)
		return err
	}
	return nil
}

// UnmountPartition unmounts the given partition or does nothing if not mounted
func (e Elemental) UnmountPartition(part *sdkPartitions.Partition) error {
	if mnt, _ := utils.IsMounted(e.config, part); !mnt {
		e.config.Logger.Debugf("Not unmounting partition, %s doesn't look like mountpoint", part.MountPoint)
		return nil
	}
	e.config.Logger.Debugf("Unmounting partition %s", part.FilesystemLabel)
	return e.config.Mounter.Unmount(part.MountPoint)
}

// MountImage mounts an image with the given mount options
func (e Elemental) MountImage(img *sdkImages.Image, opts ...string) error {
	e.config.Logger.Debugf("Mounting image %s", img.Label)
	err := fsutils.MkdirAll(e.config.Fs, img.MountPoint, cnst.DirPerm)
	if err != nil {
		return err
	}
	loopDevice, err := loop.Loop(img, e.config)
	if err != nil {
		return err
	}

	err = e.config.Mounter.Mount(loopDevice, img.MountPoint, "auto", opts)
	if err != nil {
		return errors.Join(err, loop.Unloop(loopDevice, e.config))
	}

	// Store the loop device so we can later detach it
	img.LoopDevice = loopDevice
	return nil
}

// UnmountImage unmounts the given image or does nothing if not mounted
func (e Elemental) UnmountImage(img *sdkImages.Image) error {
	// Using IsLikelyNotMountPoint seams to be safe as we are not checking
	// for bind mounts here
	if notMnt, _ := e.config.Mounter.IsLikelyNotMountPoint(img.MountPoint); notMnt {
		e.config.Logger.Debugf("Not unmounting image, %s doesn't look like mountpoint", img.MountPoint)
		return nil
	}

	e.config.Logger.Debugf("Unmounting image %s", img.Label)
	err := e.config.Mounter.Unmount(img.MountPoint)
	if err != nil {
		return err
	}
	err = loop.Unloop(img.LoopDevice, e.config)
	if err != nil {
		return err
	}
	img.LoopDevice = ""
	return err
}

// CreateFileSystemImage creates the image file for config.target
func (e Elemental) CreateFileSystemImage(img *sdkImages.Image) error {
	e.config.Logger.Infof("Creating file system image %s with size %dMiB", img.File, img.Size)
	err := fsutils.MkdirAll(e.config.Fs, filepath.Dir(img.File), cnst.DirPerm)
	if err != nil {
		return err
	}
	actImg, err := e.config.Fs.Create(img.File)
	if err != nil {
		return err
	}

	err = actImg.Truncate(int64(img.Size * 1024 * 1024))
	if err != nil {
		actImg.Close()
		_ = e.config.Fs.RemoveAll(img.File)
		return err
	}
	err = actImg.Close()
	if err != nil {
		_ = e.config.Fs.RemoveAll(img.File)
		return err
	}

	mkfs := partitioner.NewMkfsCall(img.File, img.FS, img.Label, e.config.Runner)
	_, err = mkfs.Apply()
	if err != nil {
		_ = e.config.Fs.RemoveAll(img.File)
		return err
	}
	return nil
}

// DeployImage will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Creates the default system dirs by default (/sys,/proc,/dev, etc...)
func (e *Elemental) DeployImage(img *sdkImages.Image, leaveMounted bool, excludes ...string) (info interface{}, err error) {
	return e.deployImage(img, leaveMounted, true, excludes...)
}

// DeployImageNodirs will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Does not create the default system dirs so it can be used to create generic images from any source
func (e *Elemental) DeployImageNodirs(img *sdkImages.Image, leaveMounted bool, excludes ...string) (info interface{}, err error) {
	return e.deployImage(img, leaveMounted, false, excludes...)
}

// deployImage is the real function that does the actual work
// Set leaveMounted to leave the image mounted, otherwise it unmounts before returning
// Set createDirStructure to create the directory structure in the target, which creates the expected dirs
// for a running system. This is so we can reuse this method for creating random images, not only system ones
func (e *Elemental) deployImage(img *sdkImages.Image, leaveMounted, createDirStructure bool, excludes ...string) (info interface{}, err error) {
	target := img.MountPoint
	if !img.Source.IsFile() {
		if img.FS != cnst.SquashFs {
			err = e.CreateFileSystemImage(img)
			if err != nil {
				return nil, err
			}

			err = e.MountImage(img, "rw")
			if err != nil {
				return nil, err
			}
		} else {
			target = utils.GetTempDir(e.config, "")
			err := fsutils.MkdirAll(e.config.Fs, target, cnst.DirPerm)
			if err != nil {
				return nil, err
			}
			defer e.config.Fs.RemoveAll(target) // nolint:errcheck
		}
	} else {
		target = img.File
	}
	info, err = e.DumpSource(target, img.Source, excludes...)
	if err != nil {
		_ = e.UnmountImage(img)
		return nil, err
	}
	if !img.Source.IsFile() {
		if createDirStructure {
			err = utils.CreateDirStructure(e.config.Fs, target)
			if err != nil {
				return nil, err
			}
		}
		if img.FS == cnst.SquashFs {
			squashOptions := append(cnst.GetDefaultSquashfsOptions(), e.config.SquashFsCompressionConfig...)
			err = utils.CreateSquashFS(e.config.Runner, e.config.Logger, target, img.File, squashOptions)
			if err != nil {
				return nil, err
			}
		}
	} else if img.Label != "" && img.FS != cnst.SquashFs {
		out, err := e.config.Runner.Run("tune2fs", "-L", img.Label, img.File)
		if err != nil {
			e.config.Logger.Errorf("Failed to apply label %s to %s: %s", img.Label, img.File, string(out))
			_ = e.config.Fs.Remove(img.File)
			return nil, err
		}
	}
	if leaveMounted && img.Source.IsFile() {
		err = e.MountImage(img, "rw")
		if err != nil {
			return nil, err
		}
	}
	if !leaveMounted {
		err = e.UnmountImage(img)
		if err != nil {
			return nil, err
		}
	}
	return info, nil
}

// DumpSource sets the image data according to the image source type
func (e *Elemental) DumpSource(target string, imgSrc *sdkImages.ImageSource, excludes ...string) (info interface{}, err error) { // nolint:gocyclo
	e.config.Logger.Infof("Copying %s source to %s", imgSrc.Value(), target)

	if imgSrc.IsDocker() {
		if e.config.Cosign {
			e.config.Logger.Infof("Running cosign verification for %s", imgSrc.Value())
			out, err := utils.CosignVerify(
				e.config.Fs, e.config.Runner, imgSrc.Value(),
				e.config.CosignPubKey,
			)
			if err != nil {
				e.config.Logger.Errorf("Cosign verification failed: %s", out)
				return nil, err
			}
		}
		err = e.config.ImageExtractor.ExtractImage(imgSrc.Value(), target, e.config.Platform.String(), excludes...)
		if err != nil {
			return nil, err
		}
	} else if imgSrc.IsOCIFile() {
		e.config.Logger.Infof("Loading OCI image from tar file %s", imgSrc.Value())

		img, release, err := e.imageFromArchive(imgSrc.Value())
		if err != nil {
			e.config.Logger.Errorf("Failed to load image from tar file: %v", err)
			return nil, err
		}
		defer release()

		if err := e.applyImageToTarget(img, target, excludes...); err != nil {
			e.config.Logger.Errorf("Failed to extract image contents: %v", err)
			return nil, fmt.Errorf("failed to extract image contents: %w", err)
		}
	} else if imgSrc.IsDir() {
		mergedExcludes := append(excludes, "/mnt", "/proc", "/sys", "/dev", "/tmp", "/host", "/run")
		err = utils.SyncData(e.config.Logger, e.config.Runner, e.config.Fs, imgSrc.Value(), target, mergedExcludes...)
		if err != nil {
			return nil, err
		}
	} else if imgSrc.IsFile() {
		err := fsutils.MkdirAll(e.config.Fs, filepath.Dir(target), cnst.DirPerm)
		if err != nil {
			return nil, err
		}
		err = utils.CopyFile(e.config.Fs, imgSrc.Value(), target)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown image source type")
	}
	e.config.Logger.Infof("Finished copying %s into %s", imgSrc.Value(), target)
	return info, nil
}

// imageFromArchive reads a local image archive. It understands the docker save
// layout, which keeps manifest.json at the root of the archive, and the OCI
// layout, which an engine backed by the containerd image store writes instead.
//
// An archive that is neither is an error. Such an archive still unpacks, but
// what it unpacks to is image metadata rather than a root filesystem, so
// copying it into the target would hand the caller an unusable tree under a
// success.
//
// The image reads its layers lazily, so the caller has to call the returned
// release once it is done with it and not before.
func (e *Elemental) imageFromArchive(archivePath string) (ggcrv1.Image, func(), error) {
	noop := func() {}

	img, dockerErr := tarball.ImageFromPath(archivePath, nil)
	if dockerErr == nil {
		return img, noop, nil
	}

	// Some tools save an image without a repository tag. Ask for the
	// conventional one before giving up on the docker layout.
	tag, err := name.NewTag("oci-image:latest")
	if err != nil {
		return nil, noop, fmt.Errorf("failed to create tag reference: %w", err)
	}
	if img, err = tarball.ImageFromPath(archivePath, &tag); err == nil {
		return img, noop, nil
	}

	e.config.Logger.Infof("%s is not a docker image archive (%v), reading it as an OCI layout", archivePath, dockerErr)
	img, release, ociErr := e.imageFromOCILayoutArchive(archivePath)
	if ociErr == nil {
		return img, release, nil
	}

	return nil, noop, fmt.Errorf(
		"reading %s as an image archive: not a docker archive (%w), not an OCI layout (%v)",
		archivePath, dockerErr, ociErr,
	)
}

// imageFromOCILayoutArchive unpacks an archive into a temporary directory and
// reads the result as an OCI layout. The layout stays on disk until the
// returned release is called, because that is where the image reads its layers
// from.
func (e *Elemental) imageFromOCILayoutArchive(archivePath string) (ggcrv1.Image, func(), error) {
	noop := func() {}

	tmpDir, err := fsutils.TempDir(e.config.Fs, "", "ocitar-extract")
	if err != nil {
		return nil, noop, fmt.Errorf("creating a temporary directory: %w", err)
	}
	release := func() { _ = e.config.Fs.RemoveAll(tmpDir) }

	// tar and the layout reader both work on the real filesystem, so they need
	// the path the configured filesystem maps this directory to.
	extractDir, err := e.config.Fs.RawPath(tmpDir)
	if err != nil {
		release()
		return nil, noop, fmt.Errorf("resolving %s: %w", tmpDir, err)
	}

	e.config.Logger.Infof("Extracting %s to %s", archivePath, extractDir)
	if out, err := e.config.Runner.Run("tar", "-xf", archivePath, "-C", extractDir); err != nil {
		release()
		return nil, noop, fmt.Errorf("extracting %s: %w: %s", archivePath, err, string(out))
	}

	index, err := layout.ImageIndexFromPath(extractDir)
	if err != nil {
		release()
		return nil, noop, err
	}

	img, err := e.imageFromIndex(index)
	if err != nil {
		release()
		return nil, noop, err
	}

	return img, release, nil
}

// imageFromIndex picks the image to deploy out of an index. An index that
// carries more than one image, as a multi-architecture save does, is resolved
// by the platform this run targets.
func (e *Elemental) imageFromIndex(index ggcrv1.ImageIndex) (ggcrv1.Image, error) {
	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("reading the index manifest: %w", err)
	}

	var candidates []ggcrv1.Image
	for _, descriptor := range manifest.Manifests {
		var img ggcrv1.Image

		switch {
		case descriptor.MediaType.IsIndex():
			nested, err := index.ImageIndex(descriptor.Digest)
			if err != nil {
				return nil, fmt.Errorf("reading the nested index %s: %w", descriptor.Digest, err)
			}
			if img, err = e.imageFromIndex(nested); err != nil {
				return nil, err
			}
		case descriptor.MediaType.IsImage():
			if img, err = index.Image(descriptor.Digest); err != nil {
				return nil, fmt.Errorf("reading the image %s: %w", descriptor.Digest, err)
			}
		default:
			continue
		}

		if e.platformMatches(descriptor.Platform) {
			return img, nil
		}
		candidates = append(candidates, img)
	}

	switch len(candidates) {
	case 0:
		return nil, errors.New("the OCI layout holds no image")
	case 1:
		return candidates[0], nil
	default:
		return nil, fmt.Errorf(
			"the OCI layout holds %d images and none of them is built for %s",
			len(candidates), e.config.Platform.String(),
		)
	}
}

// platformMatches reports whether a manifest descriptor names the platform this
// run targets. A descriptor with no platform matches nothing: a save of a
// single image usually omits it, and that case is covered by there being only
// one candidate.
func (e *Elemental) platformMatches(descriptorPlatform *ggcrv1.Platform) bool {
	if descriptorPlatform == nil || e.config.Platform == nil {
		return false
	}

	return descriptorPlatform.OS == e.config.Platform.OS &&
		descriptorPlatform.Architecture == e.config.Platform.GolangArch
}

// applyImageToTarget unpacks the layers of an image onto target, dropping any
// entry named in excludes.
func (e *Elemental) applyImageToTarget(img ggcrv1.Image, target string, excludes ...string) error {
	reader := mutate.Extract(img)
	defer func() { _ = reader.Close() }()

	if len(excludes) == 0 {
		_, err := archive.Apply(context.Background(), target, reader)
		return err
	}

	excluded := make(map[string]struct{}, len(excludes))
	for _, exclude := range excludes {
		excluded[exclude] = struct{}{}
	}

	filter := archive.WithFilter(func(hdr *tar.Header) (bool, error) {
		if _, found := excluded[hdr.Name]; found {
			e.config.Logger.Infof("Excluding file from extraction: %s", hdr.Name)
			return false, nil
		}
		return true, nil
	})

	_, err := archive.Apply(context.Background(), target, reader, filter)
	return err
}

// CopyCloudConfig will check if there is a cloud init in the config and store it on the target
func (e *Elemental) CopyCloudConfig(cloudInit []string) (err error) {
	e.config.Logger.Infof("List of cloud inits to copy: %+v\n", cloudInit)
	for i, ci := range cloudInit {
		customConfig := filepath.Join(cnst.OEMDir, fmt.Sprintf("9%d_custom.yaml", i))
		e.config.Logger.Infof("Starting copying cloud config file %s to %s", ci, customConfig)
		err = utils.GetSource(e.config, ci, customConfig)
		if err != nil {
			return err
		}
		if err = e.config.Fs.Chmod(customConfig, cnst.ConfigPerm); err != nil {
			e.config.Logger.Debugf("Error on chmod %s: %s\n", customConfig, err.Error())
			return err
		}
		e.config.Logger.Infof("Finished copying cloud config file %s to %s", ci, customConfig)
	}
	return nil
}

// SelinuxRelabel will relabel the system if it finds the binary and the context
func (e *Elemental) SelinuxRelabel(rootDir string, raiseError bool) error {
	policyFile, err := utils.FindFileWithPrefix(e.config.Fs, filepath.Join(rootDir, cnst.SELinuxTargetedPolicyPath), "policy.")
	contextFile := filepath.Join(rootDir, cnst.SELinuxTargetedContextFile)
	contextExists, _ := fsutils.Exists(e.config.Fs, contextFile)

	if err == nil && contextExists && utils.CommandExists("setfiles") {
		var out []byte
		var err error
		if rootDir == "/" || rootDir == "" {
			out, err = e.config.Runner.Run("setfiles", "-c", policyFile, "-e", "/dev", "-e", "/proc", "-e", "/sys", "-F", contextFile, "/")
		} else {
			out, err = e.config.Runner.Run("setfiles", "-c", policyFile, "-F", "-r", rootDir, contextFile, rootDir)
		}
		e.config.Logger.Debugf("SELinux setfiles output: %s", string(out))
		if err != nil && raiseError {
			return err
		}

	} else {
		e.config.Logger.Debugf("No files relabelling as SELinux utilities are not found")
	}

	return nil
}

// LabelStateImage applies the boot_t context to a state image file so
// boot-time components (e.g. systemd-gpt-auto-generator) can access it.
// It is a no-op when chcon is not available or the file does not exist.
func (e *Elemental) LabelStateImage(imgFile string) {
	if !utils.CommandExists("chcon") {
		return
	}
	exists, _ := fsutils.Exists(e.config.Fs, imgFile)
	if !exists {
		return
	}
	if out, err := e.config.Runner.Run("chcon", "system_u:object_r:boot_t:s0", imgFile); err != nil {
		e.config.Logger.Warnf("SELinux state image relabel failed for %s: %s", imgFile, out)
	}
}

// CheckActiveDeployment returns true if at least one of the provided filesystem labels is found within the system
func (e *Elemental) CheckActiveDeployment(labels []string) bool {
	e.config.Logger.Infof("Checking for active deployment")

	for _, label := range labels {
		found, _ := utils.GetDeviceByLabel(e.config, label, 1)
		if found != "" {
			e.config.Logger.Debug("there is already an active deployment in the system")
			return true
		}
	}
	return false
}

// GetIso will try to:
// download the iso into a temporary folder and mount the iso file as loop
// in cnst.DownloadedIsoMnt
func (e *Elemental) GetIso(iso string) (tmpDir string, err error) {
	// TODO support ISO download in persistent storage?
	tmpDir, err = fsutils.TempDir(e.config.Fs, "", "elemental")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = e.config.Fs.RemoveAll(tmpDir)
		}
	}()

	isoMnt := filepath.Join(tmpDir, "iso")
	rootfsMnt := filepath.Join(tmpDir, "rootfs")

	tmpFile := filepath.Join(tmpDir, "cOs.iso")
	err = utils.GetSource(e.config, iso, tmpFile)
	if err != nil {
		return "", err
	}
	err = fsutils.MkdirAll(e.config.Fs, isoMnt, cnst.DirPerm)
	if err != nil {
		return "", err
	}
	e.config.Logger.Infof("Mounting iso %s into %s", tmpFile, isoMnt)
	err = e.config.Mounter.Mount(tmpFile, isoMnt, "auto", []string{"loop"})
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = e.config.Mounter.Unmount(isoMnt)
		}
	}()

	e.config.Logger.Infof("Mounting squashfs image from iso into %s", rootfsMnt)
	err = fsutils.MkdirAll(e.config.Fs, rootfsMnt, cnst.DirPerm)
	if err != nil {
		return "", err
	}
	err = e.config.Mounter.Mount(filepath.Join(isoMnt, cnst.IsoRootFile), rootfsMnt, "auto", []string{})
	return tmpDir, err
}

// UpdateSourcesFormDownloadedISO checks a downaloaded and mounted ISO in workDir and updates the active and recovery image
// descriptions to use the squashed rootfs from the downloaded ISO.
func (e Elemental) UpdateSourcesFormDownloadedISO(workDir string, activeImg *sdkImages.Image, recoveryImg *sdkImages.Image) error {
	rootfsMnt := filepath.Join(workDir, "rootfs")
	isoMnt := filepath.Join(workDir, "iso")

	if activeImg != nil {
		activeImg.Source = sdkImages.NewDirSrc(rootfsMnt)
	}
	if recoveryImg != nil {
		squashedImgSource := filepath.Join(isoMnt, cnst.RecoverySquashFile)
		if exists, _ := fsutils.Exists(e.config.Fs, squashedImgSource); exists {
			recoveryImg.Source = sdkImages.NewFileSrc(squashedImgSource)
			recoveryImg.FS = cnst.SquashFs
		} else if activeImg != nil {
			recoveryImg.Source = sdkImages.NewFileSrc(activeImg.File)
			recoveryImg.FS = sdkConstants.LinuxImgFs
			// Only update label if unset, it could happen if the host is running form another ISO.
			if recoveryImg.Label == "" {
				recoveryImg.Label = sdkConstants.SystemLabel
			}
		} else {
			return fmt.Errorf("can't set recovery image from ISO, source image is missing")
		}
	}
	return nil
}

// SetDefaultGrubEntry Sets the default_menu_entry value in Config.GrubOEMEnv file at in
// State partition mountpoint. If there is not a custom value in the kairos-release file, we do nothing
// As the grub config already has a sane default
func (e Elemental) SetDefaultGrubEntry(partMountPoint string, imgMountPoint string, defaultEntry string) error {
	if defaultEntry == "" {
		osRelease, err := utils.LoadEnvFile(e.config.Fs, filepath.Join(imgMountPoint, "etc", "kairos-release"))
		if err != nil {
			// Fallback to os-release for images predating kairos-release.
			osRelease, err = utils.LoadEnvFile(e.config.Fs, filepath.Join(imgMountPoint, "etc", "os-release"))
			if err != nil {
				e.config.Logger.Warnf("Could not load kairos-release or os-release: %v", err)
				return nil
			}
		}
		defaultEntry = osRelease["GRUB_ENTRY_NAME"]
		// If its still empty then do nothing
		if defaultEntry == "" {
			return nil
		}
	}
	e.config.Logger.Infof("Setting default grub entry to %s", defaultEntry)
	return utils.SetPersistentVariables(
		filepath.Join(partMountPoint, cnst.GrubOEMEnv),
		map[string]string{"default_menu_entry": defaultEntry},
		e.config,
	)
}

// FindKernelInitrd finds for kernel and intird files inside the /boot directory of a given
// root tree path. It assumes kernel and initrd files match certain file name prefixes.
func (e Elemental) FindKernelInitrd(rootDir string) (kernel string, initrd string, err error) {
	kernelNames := []string{"uImage", "Image", "zImage", "vmlinuz", "image"}
	initrdNames := []string{"initrd", "initramfs"}
	kernel, err = utils.FindFileWithPrefix(e.config.Fs, filepath.Join(rootDir, "boot"), kernelNames...)
	if err != nil {
		e.config.Logger.Errorf("No Kernel file found")
		return "", "", err
	}
	initrd, err = utils.FindFileWithPrefix(e.config.Fs, filepath.Join(rootDir, "boot"), initrdNames...)
	if err != nil {
		e.config.Logger.Errorf("No initrd file found")
		return "", "", err
	}
	return kernel, initrd, nil
}

// DeactivateDevice deactivates unmounted the block devices present within the system.
// Useful to deactivate LVM volumes, if any, related to the target device.
func (e Elemental) DeactivateDevices() error {
	out, err := e.config.Runner.Run(
		"blkdeactivate", "--lvmoptions", "retry,wholevg",
		"--dmoptions", "force,retry", "--errors",
	)
	e.config.Logger.Debugf("blkdeactivate command output: %s", string(out))
	return err
}
