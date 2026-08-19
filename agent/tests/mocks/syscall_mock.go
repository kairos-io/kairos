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
	"syscall"
)

// FakeSyscall is a test helper method to track calls to syscall
// It can also fail on Chroot command
type FakeSyscall struct {
	chrootHistory     []string // Track calls to chroot
	ErrorOnChroot     bool
	ReturnValue       int // What to return when FakeSyscall.Syscall is called
	mounts            []FakeMount
	SideEffectSyscall func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) // Function to stub the result of calling FakeSyscall.Syscall
}

type FakeMount struct {
	Source string
	Target string
	Fstype string
	Flags  uintptr
	Data   string
}

// Chroot will store the chroot call
// It can return a failure if ErrorOnChroot is true
func (f *FakeSyscall) Chroot(path string) error {
	f.chrootHistory = append(f.chrootHistory, path)
	if f.ErrorOnChroot {
		return errors.New("chroot error")
	}
	return nil
}

func (f *FakeSyscall) Chdir(path string) error {
	return nil
}

// WasChrootCalledWith is a helper method to check if Chroot was called with the given path
func (f *FakeSyscall) WasChrootCalledWith(path string) bool {
	for _, c := range f.chrootHistory {
		if c == path {
			return true
		}
	}
	return false
}

func (f *FakeSyscall) Mount(source string, target string, fstype string, flags uintptr, data string) error {
	f.mounts = append(f.mounts, FakeMount{
		Source: source,
		Target: target,
		Fstype: fstype,
		Flags:  flags,
		Data:   data,
	})
	return nil
}

func (f *FakeSyscall) WasMountCalledWith(source string, target string, fstype string, flags uintptr, data string) bool {
	for _, m := range f.mounts {
		if m.Source == source && m.Target == target && m.Fstype == fstype && m.Flags == flags && m.Data == data {
			return true
		}
	}
	return false
}

func (f *FakeSyscall) Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	if f.SideEffectSyscall != nil {
		return f.SideEffectSyscall(trap, a1, a2, a3)
	}
	return 0, 0, syscall.Errno(f.ReturnValue)
}
