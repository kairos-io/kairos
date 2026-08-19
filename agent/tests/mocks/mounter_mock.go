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
	"errors"

	"k8s.io/mount-utils"
)

// ErrorMounter is a fake mounter for tests that can error out.
type ErrorMounter struct {
	mount.Interface
	ErrorOnMount   bool
	ErrorOnUnmount bool
}

// NewErrorMounter returns an ErrorMounter with an instance of FakeMounter inside so we can use its functions
func NewErrorMounter() *ErrorMounter {
	return &ErrorMounter{
		Interface: &mount.FakeMounter{},
	}
}

// Mount will return an error if ErrorOnMount is true
func (e ErrorMounter) Mount(source string, target string, fstype string, options []string) error {
	if e.ErrorOnMount {
		return errors.New("mount error")
	}
	return e.Interface.Mount(source, target, fstype, options)
}

// Unmount will return an error if ErrorOnUnmount is true
func (e ErrorMounter) Unmount(target string) error {
	if e.ErrorOnUnmount {
		return errors.New("unmount error")
	}
	return e.Interface.Unmount(target)
}

func (e ErrorMounter) List() ([]mount.MountPoint, error) {
	return e.Interface.List()
}

func (e ErrorMounter) IsLikelyNotMountPoint(file string) (bool, error) {
	mnts, _ := e.List()
	for _, mnt := range mnts {
		if file == mnt.Path {
			return false, nil
		}
	}
	return true, nil
}

// We need to have this below to fulfill the interface for mount.Interface

func (e ErrorMounter) MountSensitive(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return nil
}
func (e ErrorMounter) MountSensitiveWithoutSystemd(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return nil
}
func (e ErrorMounter) MountSensitiveWithoutSystemdWithMountFlags(source string, target string, fstype string, options []string, sensitiveOptions []string, mountFlags []string) error {
	return nil
}
func (e ErrorMounter) GetMountRefs(pathname string) ([]string, error) { return []string{}, nil }
