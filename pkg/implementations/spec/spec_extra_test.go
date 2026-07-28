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

package spec_test

import (
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Spec accessors and uki specs", Label("types", "config"), func() {
	Describe("InstallSpec", func() {
		It("reports reboot and shutdown flags and accessors", func() {
			sp := spec.InstallSpec{
				Reboot:    true,
				PowerOff:  true,
				Target:    "/dev/sda",
				PartTable: sdkConstants.GPT,
				Partitions: sdkPartitions.ElementalPartitions{
					State: &sdkPartitions.Partition{Name: "state"},
				},
				ExtraPartitions: sdkPartitions.PartitionList{
					&sdkPartitions.Partition{Name: "extra"},
				},
			}
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
			Expect(sp.GetTarget()).To(Equal("/dev/sda"))
			Expect(sp.GetPartTable()).To(Equal(sdkConstants.GPT))
			Expect(sp.GetPartitions().State.Name).To(Equal("state"))
			Expect(sp.GetExtraPartitions()).To(HaveLen(1))
			Expect(sp.GetExtraPartitions()[0].Name).To(Equal("extra"))
		})
		It("returns false for unset reboot and shutdown flags", func() {
			sp := spec.InstallSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
		It("uses the squashfs recovery file name when recovery fs is squashfs", func() {
			sp := spec.InstallSpec{
				Active:   sdkImages.Image{Source: sdkImages.NewFileSrc("/tmp")},
				Recovery: sdkImages.Image{FS: constants.SquashFs},
				Partitions: sdkPartitions.ElementalPartitions{
					State:      &sdkPartitions.Partition{MountPoint: "/tmp"},
					OEM:        &sdkPartitions.Partition{},
					Recovery:   &sdkPartitions.Partition{},
					Persistent: &sdkPartitions.Partition{},
				},
				Firmware:  sdkConstants.BiosPartName,
				PartTable: sdkConstants.GPT,
			}
			err := sp.Sanitize()
			Expect(err).ToNot(HaveOccurred())
			Expect(sp.Recovery.File).To(Equal(filepath.Join(sdkConstants.RecoveryDirTransient, "cOS", constants.RecoverySquashFile)))
		})
		It("uses the recovery partition mountpoint when set", func() {
			sp := spec.InstallSpec{
				Active: sdkImages.Image{Source: sdkImages.NewFileSrc("/tmp")},
				Partitions: sdkPartitions.ElementalPartitions{
					State:      &sdkPartitions.Partition{MountPoint: "/tmp"},
					Recovery:   &sdkPartitions.Partition{MountPoint: "/custom/recovery"},
					OEM:        &sdkPartitions.Partition{},
					Persistent: &sdkPartitions.Partition{},
				},
				Firmware:  sdkConstants.BiosPartName,
				PartTable: sdkConstants.GPT,
			}
			err := sp.Sanitize()
			Expect(err).ToNot(HaveOccurred())
			Expect(sp.Recovery.File).To(Equal(filepath.Join("/custom/recovery", "cOS", sdkConstants.RecoveryImgFile)))
		})
	})

	Describe("ResetSpec", func() {
		It("reports reboot and shutdown flags", func() {
			sp := spec.ResetSpec{Reboot: true, PowerOff: true}
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
		})
		It("returns false for unset flags", func() {
			sp := spec.ResetSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
	})

	Describe("UpgradeSpec", func() {
		It("reports reboot and shutdown flags", func() {
			sp := spec.UpgradeSpec{Reboot: true, PowerOff: true}
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
		})
		It("returns false for unset flags", func() {
			sp := spec.UpgradeSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
		It("detects a recovery upgrade based on the entry", func() {
			sp := spec.UpgradeSpec{Entry: constants.BootEntryRecovery}
			Expect(sp.RecoveryUpgrade()).To(BeTrue())
			sp.Entry = "active"
			Expect(sp.RecoveryUpgrade()).To(BeFalse())
		})
	})

	Describe("EmptySpec", func() {
		It("sanitizes without error and never reboots or shuts down", func() {
			sp := spec.EmptySpec{}
			Expect(sp.Sanitize()).ToNot(HaveOccurred())
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
	})

	Describe("InstallUkiSpec", func() {
		It("exposes accessors", func() {
			sp := spec.InstallUkiSpec{
				Reboot:   true,
				PowerOff: true,
				Target:   "/dev/sda",
				Partitions: sdkPartitions.ElementalPartitions{
					State: &sdkPartitions.Partition{Name: "state"},
				},
				ExtraPartitions: sdkPartitions.PartitionList{
					&sdkPartitions.Partition{Name: "extra"},
				},
			}
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
			Expect(sp.GetTarget()).To(Equal("/dev/sda"))
			Expect(sp.GetPartTable()).To(Equal("gpt"))
			Expect(sp.GetPartitions().State.Name).To(Equal("state"))
			Expect(sp.GetExtraPartitions()).To(HaveLen(1))
			Expect(sp.GetExtraPartitions()[0].Name).To(Equal("extra"))
		})
		It("returns false for unset flags", func() {
			sp := spec.InstallUkiSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
	})

	Describe("UpgradeUkiSpec", func() {
		It("sanitizes without error and reports flags", func() {
			sp := spec.UpgradeUkiSpec{Reboot: true, PowerOff: true}
			Expect(sp.Sanitize()).ToNot(HaveOccurred())
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
		})
		It("returns false for unset flags", func() {
			sp := spec.UpgradeUkiSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
		It("detects a recovery upgrade based on the entry", func() {
			sp := spec.UpgradeUkiSpec{Entry: constants.BootEntryRecovery}
			Expect(sp.RecoveryUpgrade()).To(BeTrue())
			sp.Entry = "active"
			Expect(sp.RecoveryUpgrade()).To(BeFalse())
		})
	})

	Describe("ResetUkiSpec", func() {
		It("sanitizes without error and reports flags", func() {
			sp := spec.ResetUkiSpec{Reboot: true, PowerOff: true}
			Expect(sp.Sanitize()).ToNot(HaveOccurred())
			Expect(sp.ShouldReboot()).To(BeTrue())
			Expect(sp.ShouldShutdown()).To(BeTrue())
		})
		It("returns false for unset flags", func() {
			sp := spec.ResetUkiSpec{}
			Expect(sp.ShouldReboot()).To(BeFalse())
			Expect(sp.ShouldShutdown()).To(BeFalse())
		})
	})
})
