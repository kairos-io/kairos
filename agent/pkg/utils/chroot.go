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

package utils

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

// Chroot represents the struct that will allow us to run commands inside a given chroot
type Chroot struct {
	path          string
	defaultMounts []string
	extraMounts   map[string]string
	activeMounts  []string
	config        *sdkConfig.Config
}

func NewChroot(path string, config *sdkConfig.Config) *Chroot {
	chroot := &Chroot{
		path:          path,
		defaultMounts: []string{"/dev", "/dev/pts", "/proc", "/sys"},
		extraMounts:   map[string]string{},
		activeMounts:  []string{},
		config:        config,
	}

	// If we have systemd running, we need to mount the journal directory to keep logger open
	if fi, err := os.Lstat("/run/systemd/system"); err == nil && fi.IsDir() {
		chroot.defaultMounts = append(chroot.defaultMounts, "/run/systemd/journal/")
	}

	return chroot
}

// ChrootedCallback runs the given callback in a chroot environment
func ChrootedCallback(cfg *sdkConfig.Config, path string, bindMounts map[string]string, callback func() error) error {
	cfg.Logger.Debugf("Extra mounts: %v", bindMounts)
	chroot := NewChroot(path, cfg)
	chroot.SetExtraMounts(bindMounts)
	return chroot.RunCallback(callback)
}

// Sets additional bind mounts for the chroot enviornment. They are represented
// in a map where the key is the path outside the chroot and the value is the
// path inside the chroot.
func (c *Chroot) SetExtraMounts(extraMounts map[string]string) {
	c.extraMounts = extraMounts
}

// Prepare will mount the defaultMounts as bind mounts, to be ready when we run chroot
func (c *Chroot) Prepare() error {
	var err error
	keys := []string{}
	mountOptions := []string{"bind"}

	if len(c.activeMounts) > 0 {
		return errors.New("There are already active mountpoints for this instance")
	}

	defer func() {
		if err != nil {
			c.Close()
		}
	}()

	for _, mnt := range c.defaultMounts {
		c.config.Logger.Debugf("Mounting %s to chroot", mnt)
		mountPoint := fmt.Sprintf("%s%s", strings.TrimSuffix(c.path, "/"), mnt)
		err = fsutils.MkdirAll(c.config.Fs, mountPoint, constants.DirPerm)
		if err != nil {
			return err
		}
		err = c.config.Mounter.Mount(mnt, mountPoint, "bind", mountOptions)
		if err != nil {
			return err
		}
		c.activeMounts = append(c.activeMounts, mountPoint)
		c.config.Logger.Debugf("Mounted %s to %s", mnt, mountPoint)
	}

	for k := range c.extraMounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.config.Logger.Debugf("Mounting %s to chroot", k)
		mountPoint := fmt.Sprintf("%s%s", strings.TrimSuffix(c.path, "/"), c.extraMounts[k])
		err = fsutils.MkdirAll(c.config.Fs, mountPoint, constants.DirPerm)
		if err != nil {
			return err
		}
		err = c.config.Mounter.Mount(k, mountPoint, "bind", mountOptions)
		if err != nil {
			return err
		}
		c.activeMounts = append(c.activeMounts, mountPoint)
		c.config.Logger.Debugf("Mounted %s to %s", k, mountPoint)
	}

	return nil
}

// Close will unmount all active mounts created in Prepare on reverse order
func (c *Chroot) Close() error {
	failures := []string{}
	for len(c.activeMounts) > 0 {
		curr := c.activeMounts[len(c.activeMounts)-1]
		c.config.Logger.Debugf("Unmounting %s from chroot", curr)
		c.activeMounts = c.activeMounts[:len(c.activeMounts)-1]
		err := c.config.Mounter.Unmount(curr)
		if err != nil {
			c.config.Logger.Errorf("Error unmounting %s: %s", curr, err)
			failures = append(failures, curr)
		}

	}
	if len(failures) > 0 {
		c.activeMounts = failures
		return fmt.Errorf("failed closing chroot environment. Unmount failures: %v", failures)
	}
	return nil
}

// RunCallback runs the given callback in a chroot environment
func (c *Chroot) RunCallback(callback func() error) (err error) {
	var currentPath string
	var oldRootF *os.File

	// Store current path
	currentPath, err = os.Getwd()
	if err != nil {
		c.config.Logger.Error("Failed to get current path")
		return err
	}
	defer func() {
		tmpErr := os.Chdir(currentPath)
		if err == nil && tmpErr != nil {
			err = tmpErr
		}
	}()

	// Store current root
	oldRootF, err = c.config.Fs.OpenFile("/", os.O_RDONLY, 0)
	if err != nil {
		c.config.Logger.Errorf("Can't open current root")
		return err
	}
	defer oldRootF.Close()

	if len(c.activeMounts) == 0 {
		err = c.Prepare()
		if err != nil {
			c.config.Logger.Errorf("Can't mount default mounts")
			return err
		}
		defer func() {
			tmpErr := c.Close()
			if err == nil {
				err = tmpErr
			}
		}()
	}
	// Change to new dir before running chroot!
	err = c.config.Syscall.Chdir(c.path)
	if err != nil {
		c.config.Logger.Errorf("Can't chdir %s: %s", c.path, err)
		return err
	}

	err = c.config.Syscall.Chroot(c.path)
	if err != nil {
		c.config.Logger.Errorf("Can't chroot %s: %s", c.path, err)
		return err
	}

	// Restore to old root
	defer func() {
		tmpErr := oldRootF.Chdir()
		if tmpErr != nil {
			c.config.Logger.Errorf("Can't change to old root dir")
			if err == nil {
				err = tmpErr
			}
		} else {
			tmpErr = c.config.Syscall.Chroot(".")
			if tmpErr != nil {
				c.config.Logger.Errorf("Can't chroot back to old root")
				if err == nil {
					err = tmpErr
				}
			}
		}
	}()

	return callback()
}

// Run executes a command inside a chroot
func (c *Chroot) Run(command string, args ...string) (out []byte, err error) {
	callback := func() error {
		out, err = c.config.Runner.Run(command, args...)
		return err
	}
	err = c.RunCallback(callback)
	if err != nil {
		c.config.Logger.Errorf("Cant run command %s with args %v on chroot: %s", command, args, err)
		c.config.Logger.Debugf("Output from command: %s", out)
	}
	return out, err
}
