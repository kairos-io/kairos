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

package partitioner

import (
	"errors"

	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/tests/mocks"
)

var _ = ginkgo.Describe("Mkfs", ginkgo.Label("mkfs"), func() {
	var runner *mocks.FakeRunner

	ginkgo.BeforeEach(func() {
		runner = mocks.NewFakeRunner()
	})

	ginkgo.Describe("MkfsCall.Apply", func() {
		ginkgo.It("runs mkfs for ext4 with label", func() {
			mkfs := NewMkfsCall("/dev/device1", "ext4", "OEM", runner)
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.ext4", "-L", "OEM", "/dev/device1"},
			})).To(Succeed())
		})

		ginkgo.It("runs mkfs for ext2 without label", func() {
			mkfs := NewMkfsCall("/dev/device1", "ext2", "", runner)
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.ext2", "/dev/device1"},
			})).To(Succeed())
		})

		ginkgo.It("runs mkfs for ext3 with custom options", func() {
			mkfs := NewMkfsCall("/dev/device1", "ext3", "BOOT", runner, "-F", "-b", "4096")
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.ext3", "-L", "BOOT", "-F", "-b", "4096", "/dev/device1"},
			})).To(Succeed())
		})

		ginkgo.It("runs mkfs for xfs with label", func() {
			mkfs := NewMkfsCall("/dev/device2", "xfs", "STATE", runner)
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.xfs", "-L", "STATE", "/dev/device2"},
			})).To(Succeed())
		})

		ginkgo.It("runs mkfs for vfat with label using -n flag", func() {
			mkfs := NewMkfsCall("/dev/device3", "vfat", "EFI", runner)
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.vfat", "-n", "EFI", "/dev/device3"},
			})).To(Succeed())
		})

		ginkgo.It("runs mkfs for fat with custom options and no label", func() {
			mkfs := NewMkfsCall("/dev/device3", "fat", "", runner, "-F", "32")
			_, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.fat", "-F", "32", "/dev/device3"},
			})).To(Succeed())
		})

		ginkgo.It("returns the runner output", func() {
			runner.ReturnValue = []byte("mkfs output here")
			mkfs := NewMkfsCall("/dev/device1", "ext4", "OEM", runner)
			out, err := mkfs.Apply()
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("mkfs output here"))
		})

		ginkgo.It("fails for an unsupported filesystem", func() {
			mkfs := NewMkfsCall("/dev/device1", "btrfs", "OEM", runner)
			_, err := mkfs.Apply()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported filesystem: btrfs"))
			Expect(runner.CmdsMatch([][]string{})).To(Succeed())
		})

		ginkgo.It("propagates runner errors", func() {
			runner.ReturnError = errors.New("mkfs exploded")
			mkfs := NewMkfsCall("/dev/device1", "ext4", "OEM", runner)
			_, err := mkfs.Apply()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mkfs exploded"))
		})
	})

	ginkgo.Describe("FormatDevice", func() {
		var log logger.KairosLogger

		ginkgo.BeforeEach(func() {
			log = logger.NewNullLogger()
		})

		ginkgo.It("formats a device with the proper command", func() {
			Expect(FormatDevice(log, runner, "/dev/device1", "ext4", "MY_LABEL")).To(Succeed())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.ext4", "-L", "MY_LABEL", "/dev/device1"},
			})).To(Succeed())
		})

		ginkgo.It("formats a device with extra options", func() {
			Expect(FormatDevice(log, runner, "/dev/device1", "vfat", "EFI", "-F", "32")).To(Succeed())
			Expect(runner.CmdsMatch([][]string{
				{"mkfs.vfat", "-n", "EFI", "-F", "32", "/dev/device1"},
			})).To(Succeed())
		})

		ginkgo.It("fails and logs when mkfs fails", func() {
			runner.ReturnError = errors.New("command failed")
			err := FormatDevice(log, runner, "/dev/device1", "ext4", "MY_LABEL")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command failed"))
		})

		ginkgo.It("fails for an unsupported filesystem", func() {
			err := FormatDevice(log, runner, "/dev/device1", "ntfs", "MY_LABEL")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported filesystem"))
		})
	})
})
