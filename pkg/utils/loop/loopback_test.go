/*
Copyright © 2026 Kairos authors

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

package loop_test

import (
	"bytes"
	"fmt"
	sc "syscall"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/loop"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	Collector "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
	"golang.org/x/sys/unix"
)

var _ = Describe("Loopback", Label("loop"), func() {
	var config *sdkConfig.Config
	var syscallMock *v1mock.FakeSyscall
	var fs *vfst.TestFS
	var cleanup func()
	var memLog *bytes.Buffer
	var logger Collector.KairosLogger
	var devLoopInt int
	var img *sdkImages.Image

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = Collector.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		syscallMock = &v1mock.FakeSyscall{}
		devLoopInt = 7
		syscallMock.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
			if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
				// Return the free loop device number
				return uintptr(devLoopInt), 0, sc.Errno(syscallMock.ReturnValue)
			}
			return 0, 0, sc.Errno(syscallMock.ReturnValue)
		}
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control":                    "",
			fmt.Sprintf("/dev/loop%d", devLoopInt): "",
			"/image.img":                           "image data",
		})
		Expect(err).ToNot(HaveOccurred())
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithLogger(logger),
			agentConfig.WithSyscall(syscallMock),
		)
		img = &sdkImages.Image{File: "/image.img"}
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Loop", func() {
		It("sets up a loop device successfully", func() {
			device, err := loop.Loop(img, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(device).To(Equal(fmt.Sprintf("/dev/loop%d", devLoopInt)))
		})

		It("fails if /dev/loop-control cannot be opened", func() {
			Expect(fs.RemoveAll("/dev/loop-control")).To(Succeed())
			_, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to open /dev/loop-control"))
		})

		It("fails if getting a free loop device returns an error", func() {
			syscallMock.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
					return 0, 0, sc.EBUSY
				}
				return 0, 0, 0
			}
			_, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to get loop device"))
		})

		It("fails if the loop device cannot be opened", func() {
			// Point the free loop device to one that does not exist in the test fs
			devLoopInt = 99
			device, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(device).To(Equal("/dev/loop99"))
			Expect(memLog.String()).To(ContainSubstring("failed to open loop device"))
		})

		It("fails if the image file cannot be opened", func() {
			img.File = "/nonexistent.img"
			_, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to open image file"))
		})

		It("fails if setting the loop device fd returns an error", func() {
			syscallMock.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
					return uintptr(devLoopInt), 0, 0
				}
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_SET_FD {
					return 0, 0, sc.EBUSY
				}
				return 0, 0, 0
			}
			_, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to set loop device"))
		})

		It("fails if setting the loop device status returns an error", func() {
			syscallMock.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
					return uintptr(devLoopInt), 0, 0
				}
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_SET_STATUS64 {
					return 0, 0, sc.EINVAL
				}
				return 0, 0, 0
			}
			_, err := loop.Loop(img, config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to set loop device status"))
		})
	})

	Describe("Unloop", func() {
		It("clears a loop device successfully", func() {
			err := loop.Unloop(fmt.Sprintf("/dev/loop%d", devLoopInt), config)
			Expect(err).ToNot(HaveOccurred())
		})

		It("fails if the loop device cannot be opened", func() {
			err := loop.Unloop("/dev/loop99", config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to set open loop device"))
		})

		It("fails if clearing the loop device returns an error", func() {
			syscallMock.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CLR_FD {
					return 0, 0, sc.EBUSY
				}
				return 0, 0, 0
			}
			err := loop.Unloop(fmt.Sprintf("/dev/loop%d", devLoopInt), config)
			Expect(err).To(HaveOccurred())
			Expect(memLog.String()).To(ContainSubstring("failed to set loop device status"))
		})
	})
})
