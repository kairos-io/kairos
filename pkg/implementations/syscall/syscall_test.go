/*
Copyright © 2021 SUSE LLC

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

package syscall_test

import (
	"os"
	sc "syscall"

	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/syscall"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// unit test stolen from yip
var _ = Describe("Syscall", Label("types", "syscall"), func() {
	It("Calling chroot on the fake syscall should not fail", func() {
		r := v1mock.FakeSyscall{}
		err := r.Chroot("/tmp/")
		// We need elevated privs to chroot so this should fail
		Expect(err).To(BeNil())
	})

	It("Calling chdir on the real syscall should not fail", func() {
		r := v1.RealSyscall{}
		err := r.Chdir("/tmp/")
		Expect(err).To(BeNil())
	})

	It("Calling chroot on the fake syscall should not fail", func() {
		r := v1mock.FakeSyscall{}
		err := r.Chdir("/tmp/")
		// We need elevated privs to chroot so this should fail
		Expect(err).To(BeNil())
	})
	It("Calling mount on the fake syscall should not fail", func() {
		r := v1mock.FakeSyscall{}
		err := r.Mount("source", "target", "fstype", 0, "data")
		Expect(err).To(BeNil())
	})
	It("Calling mount on the real syscall fail (wrong args)", func() {
		r := v1.RealSyscall{}
		err := r.Mount("source", "target", "fstype", 0, "data")
		Expect(err).To(HaveOccurred())
	})
	It("Calling chroot on the real syscall fails (nonexistent path)", func() {
		r := v1.RealSyscall{}
		// Use a path that does not exist so the call fails regardless of privileges
		// and never actually chroots the test process.
		err := r.Chroot("/this/path/does/not/exist")
		Expect(err).To(HaveOccurred())
	})
	It("Calling Syscall on the real syscall works (getpid)", func() {
		r := v1.RealSyscall{}
		pid, _, errno := r.Syscall(sc.SYS_GETPID, 0, 0, 0)
		Expect(errno).To(BeEquivalentTo(0))
		Expect(int(pid)).To(Equal(os.Getpid()))
	})
})
