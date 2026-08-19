/*
Copyright © 2022 SUSE LLC
Copyright © 2023 Kairos authors

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

package utils

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Chroot represents the struct that will allow us to run commands inside a given chroot.
type Chroot struct {
	path          string
	defaultMounts []string
	activeMounts  []string
}

func NewChroot(path string) *Chroot {
	return &Chroot{
		path: path,
		defaultMounts: []string{
			"/sys", "/dev", "/dev/pts", "/dev/shm", "/proc", "/tmp",
			"/run/rootfsbase", "/run/initramfs/live", "/run",
		},
		activeMounts: []string{},
	}
}

// Prepare will mount the defaultMounts as bind mounts, to be ready when we run chroot.
func (c *Chroot) Prepare() error {
	var err error

	if len(c.activeMounts) > 0 {
		return errors.New("there are already active mountpoints for this instance")
	}

	for _, mnt := range c.defaultMounts {
		mountPoint := filepath.Join(c.path, mnt)
		if _, err := os.Stat(mnt); os.IsNotExist(err) {
			// Source doesnt exist, skip it
			KLog.Logger.Debug().Str("what", mnt).Msg("Source does not exists, not mounting in chroot")
			continue
		}

		err = CreateIfNotExists(mountPoint)
		if err != nil {
			KLog.Logger.Err(err).Str("what", mountPoint).Msg("Creating dir")
			return err
		}
		// Don't mount /sys, /dev or /run as MS_REC as this brings a lot of submounts for cgroups and such and those are not needed
		// and prevents up from cleaning up the chroot afterwards
		// For example you can also have a cdrom device mounted under /dev/sr0 or /dev/cdrom and we dont know how to find it and mark it private
		switch {
		case mnt == "/sys", mnt == "/dev", mnt == "/run":
			err = Mount(mnt, mountPoint, "", syscall.MS_BIND, "")
		default:
			err = Mount(mnt, mountPoint, "", syscall.MS_BIND|syscall.MS_REC, "")
		}

		if err != nil {
			KLog.Logger.Err(err).Str("where", mountPoint).Str("what", mnt).Msg("Mounting chroot bind")
			return err
		}
		// "remount" with private so unmount events do not propagate
		err = Mount("", mountPoint, "", syscall.MS_PRIVATE, "")
		if err != nil {
			KLog.Logger.Err(err).Str("where", mountPoint).Str("what", mnt).Msg("Mounting chroot bind")
			return err
		}
		c.activeMounts = append(c.activeMounts, mountPoint)
	}

	return nil
}

// Close will unmount all active mounts created in Prepare on reverse order.
func (c *Chroot) Close() error {
	failures := []string{}
	KLog.Logger.Debug().Strs("activeMounts", c.activeMounts).Msg("Closing chroot")
	// Something mounts this due to selinux, so we need to try to manually unmount and ignore any errors
	_ = syscall.Unmount(filepath.Join(c.path, "/sys/fs/selinux"), 0)
	for len(c.activeMounts) > 0 {
		curr := c.activeMounts[len(c.activeMounts)-1]
		KLog.Logger.Debug().Str("what", curr).Msg("Unmounting from chroot")
		c.activeMounts = c.activeMounts[:len(c.activeMounts)-1]
		err := syscall.Unmount(curr, 0)
		if err != nil {
			KLog.Logger.Err(err).Str("what", curr).Msg("Error unmounting")
			failures = append(failures, curr)
		}
	}
	if len(failures) > 0 {
		c.activeMounts = failures
		return fmt.Errorf("failed closing chroot environment. Unmount failures: %v", failures)
	}
	return nil
}

// RunCallback runs the given callback in a chroot environment.
func (c *Chroot) RunCallback(callback func() error) (err error) {
	var currentPath string
	var oldRootF *os.File

	// Store current path
	currentPath, err = os.Getwd()
	if err != nil {
		KLog.Logger.Err(err).Msg("Failed to get current path")
		return err
	}
	defer func() {
		tmpErr := os.Chdir(currentPath)
		if err == nil && tmpErr != nil {
			err = tmpErr
		}
	}()

	// Store current root
	oldRootF, err = os.Open("/")
	if err != nil {
		KLog.Logger.Err(err).Msg("Can't open current root")
		return err
	}
	defer oldRootF.Close()

	if len(c.activeMounts) == 0 {
		err = c.Prepare()
		if err != nil {
			KLog.Logger.Err(err).Msg("Can't mount default mounts")
			return err
		}
		defer func(c *Chroot) {
			err = c.Close()
		}(c)
	}
	// Change to new dir before running chroot!
	err = syscall.Chdir(c.path)
	if err != nil {
		KLog.Logger.Err(err).Str("path", c.path).Msg("Can't chdir")
		return err
	}

	err = syscall.Chroot(c.path)
	if err != nil {
		KLog.Logger.Err(err).Str("path", c.path).Msg("Can't chroot")
		return err
	}

	// Restore to old root
	defer func() {
		tmpErr := oldRootF.Chdir()
		if tmpErr != nil {
			KLog.Logger.Err(tmpErr).Str("path", oldRootF.Name()).Msg("Can't change to old root dir")
			if err == nil {
				err = tmpErr
			}
		} else {
			tmpErr = syscall.Chroot(".")
			if tmpErr != nil {
				KLog.Logger.Err(tmpErr).Str("path", oldRootF.Name()).Msg("Can't chroot back to old root")
				if err == nil {
					err = tmpErr
				}
			}
		}
	}()

	return callback()
}

// Run executes a command inside a chroot.
func (c *Chroot) Run(command string) (string, error) {
	var err error
	var out []byte
	callback := func() error {
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Env = os.Environ()
		out, err = cmd.CombinedOutput()
		return err
	}
	err = c.RunCallback(callback)
	if err != nil {
		KLog.Logger.Err(err).Str("cmd", command).Msg("Cant run command on chroot")
	}
	return string(out), err
}
