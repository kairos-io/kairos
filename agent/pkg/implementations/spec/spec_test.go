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

package spec_test

import (
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Types", Label("types", "config"), func() {
	Describe("ElementalPartitions", func() {
		var p sdkPartitions.PartitionList
		var ep sdkPartitions.ElementalPartitions
		BeforeEach(func() {
			ep = sdkPartitions.ElementalPartitions{}
			p = sdkPartitions.PartitionList{
				&sdkPartitions.Partition{
					FilesystemLabel: "COS_OEM",
					Size:            0,
					Name:            "oem",
					FS:              "",
					Flags:           nil,
					MountPoint:      "/some/path/nested",
					Path:            "",
					Disk:            "",
				},
				&sdkPartitions.Partition{
					FilesystemLabel: "COS_CUSTOM",
					Size:            0,
					Name:            "persistent",
					FS:              "",
					Flags:           nil,
					MountPoint:      "/some/path",
					Path:            "",
					Disk:            "",
				},
				&sdkPartitions.Partition{
					FilesystemLabel: "SOMETHING",
					Size:            0,
					Name:            "somethingelse",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
			}
		})
		It("sets firmware partitions on efi", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ep.EFI != nil && ep.BIOS == nil).To(BeTrue())
		})
		It("sets firmware partitions on bios", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(sdkConstants.BIOS, sdkConstants.GPT)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ep.EFI == nil && ep.BIOS != nil).To(BeTrue())
		})
		It("sets firmware partitions on msdos", func() {
			ep.State = &sdkPartitions.Partition{}
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(sdkConstants.BIOS, sdkConstants.MSDOS)
			Expect(err).Should(HaveOccurred())
		})
		It("fails to set firmware partitions of state is not defined on msdos", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(sdkConstants.BIOS, sdkConstants.MSDOS)
			Expect(err).Should(HaveOccurred())
		})
		It("initializes an ElementalPartitions from a PartitionList", func() {
			ep := spec.NewElementalPartitionsFromList(p)
			Expect(ep.Persistent != nil).To(BeTrue())
			Expect(ep.OEM != nil).To(BeTrue())
			Expect(ep.BIOS == nil).To(BeTrue())
			Expect(ep.EFI == nil).To(BeTrue())
			Expect(ep.State == nil).To(BeTrue())
			Expect(ep.Recovery == nil).To(BeTrue())
		})
		Describe("returns a partition list by install order", func() {
			It("with no extra parts", func() {
				ep := spec.NewElementalPartitionsFromList(p)
				lst := ep.PartitionsByInstallOrder([]*sdkPartitions.Partition{})
				Expect(len(lst)).To(Equal(2))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
			})
			It("with extra parts with size > 0", func() {
				ep := spec.NewElementalPartitionsFromList(p)
				var extraParts []*sdkPartitions.Partition
				extraParts = append(extraParts, &sdkPartitions.Partition{Name: "extra", Size: 5})

				lst := ep.PartitionsByInstallOrder(extraParts)
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "extra").To(BeTrue())
				Expect(lst[2].Name == "persistent").To(BeTrue())
			})
			It("with extra part with size == 0 and persistent.Size == 0", func() {
				ep := spec.NewElementalPartitionsFromList(p)
				var extraParts []*sdkPartitions.Partition
				extraParts = append(extraParts, &sdkPartitions.Partition{Name: "extra", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Should ignore the wrong partition had have the persistent over it
				Expect(len(lst)).To(Equal(2))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
			})
			It("with extra part with size == 0 and persistent.Size > 0", func() {
				ep := spec.NewElementalPartitionsFromList(p)
				ep.Persistent.Size = 10
				var extraParts []*sdkPartitions.Partition
				extraParts = append(extraParts, &sdkPartitions.Partition{Name: "extra", FilesystemLabel: "LABEL", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Will have our size == 0 partition the latest
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
				Expect(lst[2].Name == "extra").To(BeTrue())
			})
			It("with several extra parts with size == 0 and persistent.Size > 0", func() {
				ep := spec.NewElementalPartitionsFromList(p)
				ep.Persistent.Size = 10
				var extraParts []*sdkPartitions.Partition
				extraParts = append(extraParts, &sdkPartitions.Partition{Name: "extra1", Size: 0})
				extraParts = append(extraParts, &sdkPartitions.Partition{Name: "extra2", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Should ignore the wrong partition had have the first partition with size 0 added last
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
				Expect(lst[2].Name == "extra1").To(BeTrue())
			})
		})

		It("returns a partition list by mount order", func() {
			ep := spec.NewElementalPartitionsFromList(p)
			lst := ep.PartitionsByMountPoint(false)
			Expect(len(lst)).To(Equal(2))
			Expect(lst[0].Name == "persistent").To(BeTrue())
			Expect(lst[1].Name == "oem").To(BeTrue())
		})
		It("returns a partition list by mount reverse order", func() {
			ep := spec.NewElementalPartitionsFromList(p)
			lst := ep.PartitionsByMountPoint(true)
			Expect(len(lst)).To(Equal(2))
			Expect(lst[0].Name == "oem").To(BeTrue())
			Expect(lst[1].Name == "persistent").To(BeTrue())
		})
	})
	Describe("PartitionList", func() {
		var p sdkPartitions.PartitionList
		BeforeEach(func() {
			p = sdkPartitions.PartitionList{
				&sdkPartitions.Partition{
					FilesystemLabel: "ONE",
					Size:            0,
					Name:            "one",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
				&sdkPartitions.Partition{
					FilesystemLabel: "TWO",
					Size:            0,
					Name:            "two",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
			}
		})
		It("returns partitions by name", func() {
			Expect(spec.GetPartitionByNameOrLabel("two", "", p)).To(Equal(&sdkPartitions.Partition{
				FilesystemLabel: "TWO",
				Size:            0,
				Name:            "two",
				FS:              "",
				Flags:           nil,
				MountPoint:      "",
				Path:            "",
				Disk:            "",
			}))
		})
		It("returns nil if partiton name not found", func() {
			Expect(spec.GetPartitionByNameOrLabel("dsd", "nonexistent", p)).To(BeNil())
		})
		It("returns partitions by filesystem label", func() {
			Expect(spec.GetPartitionByNameOrLabel("", "TWO", p)).To(Equal(&sdkPartitions.Partition{
				FilesystemLabel: "TWO",
				Size:            0,
				Name:            "two",
				FS:              "",
				Flags:           nil,
				MountPoint:      "",
				Path:            "",
				Disk:            "",
			}))
		})
		It("returns nil if filesystem label not found", func() {
			Expect(spec.GetPartitionByNameOrLabel("sd", "nonexistent", p)).To(BeNil())
		})
	})
	Describe("Specs", func() {
		Describe("InstallSpec sanitize", func() {
			var sp spec.InstallSpec
			BeforeEach(func() {
				sp = spec.InstallSpec{
					Active: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
					Recovery: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
					Passive: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
					Partitions: sdkPartitions.ElementalPartitions{
						OEM:        &sdkPartitions.Partition{},
						Recovery:   &sdkPartitions.Partition{},
						Persistent: &sdkPartitions.Partition{},
					},
					ExtraPartitions: sdkPartitions.PartitionList{},
				}
			})
			It("fails with empty source", func() {
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined system source to install"))
			})
			It("fails with missing state partition", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined state partition"))
			})
			It("passes if state and source are ready", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Firmware = sdkConstants.BiosPartName
				sp.PartTable = sdkConstants.GPT
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})
			It("fills the sp with defaults (BIOS)", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Firmware = sdkConstants.BiosPartName
				sp.PartTable = sdkConstants.GPT
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
				Expect(sp.Recovery.File).ToNot(BeEmpty())
				Expect(sp.Recovery.File).To(Equal(filepath.Join(sdkConstants.RecoveryDirTransient, "cOS", sdkConstants.RecoveryImgFile)))
				Expect(sp.Partitions.EFI).To(BeNil())
				Expect(sp.Partitions.BIOS).ToNot(BeNil())
				Expect(sp.Partitions.BIOS.Flags).To(ContainElement("bios_grub"))
				Expect(sp.Partitions.OEM.FilesystemLabel).To(Equal(sdkConstants.OEMLabel))
				Expect(sp.Partitions.OEM.Name).To(Equal(sdkConstants.OEMPartName))
				Expect(sp.Partitions.Recovery.FilesystemLabel).To(Equal(sdkConstants.RecoveryLabel))
				Expect(sp.Partitions.Recovery.Name).To(Equal(sdkConstants.RecoveryPartName))
				Expect(sp.Partitions.Persistent.FilesystemLabel).To(Equal(sdkConstants.PersistentLabel))
				Expect(sp.Partitions.Persistent.Name).To(Equal(sdkConstants.PersistentPartName))
				Expect(sp.Partitions.State.FilesystemLabel).To(Equal(sdkConstants.StateLabel))
				Expect(sp.Partitions.State.Name).To(Equal(sdkConstants.StatePartName))
			})
			It("fills the sp with defaults (EFI)", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Firmware = sdkConstants.EfiPartName
				sp.PartTable = sdkConstants.GPT
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
				Expect(sp.Recovery.File).ToNot(BeEmpty())
				Expect(sp.Recovery.File).To(Equal(filepath.Join(sdkConstants.RecoveryDirTransient, "cOS", sdkConstants.RecoveryImgFile)))
				Expect(sp.Partitions.BIOS).To(BeNil())
				Expect(sp.Partitions.EFI).ToNot(BeNil())
				Expect(sp.Partitions.EFI.FilesystemLabel).To(Equal(sdkConstants.EfiLabel))
				Expect(sp.Partitions.EFI.MountPoint).To(Equal(sdkConstants.EfiDirTransient))
				Expect(sp.Partitions.EFI.Size).To(Equal(sdkConstants.EfiSize))
				Expect(sp.Partitions.EFI.FS).To(Equal(sdkConstants.EfiFs))
				Expect(sp.Partitions.EFI.Flags).To(ContainElement("esp"))
				Expect(sp.Partitions.OEM.FilesystemLabel).To(Equal(sdkConstants.OEMLabel))
				Expect(sp.Partitions.OEM.Name).To(Equal(sdkConstants.OEMPartName))
				Expect(sp.Partitions.Recovery.FilesystemLabel).To(Equal(sdkConstants.RecoveryLabel))
				Expect(sp.Partitions.Recovery.Name).To(Equal(sdkConstants.RecoveryPartName))
				Expect(sp.Partitions.Persistent.FilesystemLabel).To(Equal(sdkConstants.PersistentLabel))
				Expect(sp.Partitions.Persistent.Name).To(Equal(sdkConstants.PersistentPartName))
				Expect(sp.Partitions.State.FilesystemLabel).To(Equal(sdkConstants.StateLabel))
				Expect(sp.Partitions.State.Name).To(Equal(sdkConstants.StatePartName))
			})
			It("Cannot add extra partitions with 0 size + persistent with 0 size", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Partitions.Persistent = &sdkPartitions.Partition{
					Size: 0,
				}
				sp.Firmware = sdkConstants.BiosPartName
				sp.PartTable = sdkConstants.GPT
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{
						Size: 0,
					},
				}
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("both persistent partition and extra partitions have size set to 0"))
			})
			It("Cannot add more than 1 extra partition with 0 size", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Partitions.Persistent = &sdkPartitions.Partition{
					Size: 100,
				}
				sp.Firmware = sdkConstants.BiosPartName
				sp.PartTable = sdkConstants.GPT
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{
						Size: 0,
					},
					&sdkPartitions.Partition{
						Size: 0,
					},
				}
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("more than one extra partition has its size set to 0"))
			})
			It("Can add 1 extra partition with 0 size", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Partitions.Persistent = &sdkPartitions.Partition{
					Size: 100,
				}
				sp.Firmware = sdkConstants.BiosPartName
				sp.PartTable = sdkConstants.GPT
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{
						Size: 0,
					},
				}
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})
			Describe("partition sizes against the target disk", func() {
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					// Present a 100MiB target disk to ghw. The ghw mock writes this
					// value into the kernel "size" file, which ghw reads back as a
					// count of 512-byte sectors, so we express the wanted byte size
					// in 512-byte sectors here to end up with a 100MiB disk.
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(sdkPartitions.Disk{
						Name:      "sda",
						SizeBytes: 100 * 1024 * 1024 / 512,
					})
					ghwTest.CreateDevices()

					sp.Active.Source = sdkImages.NewFileSrc("/tmp")
					sp.Partitions.State = &sdkPartitions.Partition{MountPoint: "/tmp"}
					sp.Firmware = sdkConstants.BiosPartName
					sp.PartTable = sdkConstants.GPT
					sp.Target = "/dev/sda"
				})
				AfterEach(func() {
					ghwTest.Clean()
				})
				It("fails when the requested partitions exceed the disk size", func() {
					sp.Partitions.Persistent = &sdkPartitions.Partition{Size: 200}
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("does not fit in the target disk"))
				})
				It("fails when the partitions leave no room for the partition metadata", func() {
					// 99MiB persistent + 1MiB BIOS = exactly the 100MiB disk, leaving
					// no room for the 1MiB start alignment and the 1MiB backup GPT
					// header that the partitioner reserves.
					sp.Partitions.Persistent = &sdkPartitions.Partition{Size: 99}
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("does not fit in the target disk"))
				})
				It("passes when the requested partitions fit the disk size", func() {
					sp.Partitions.Persistent = &sdkPartitions.Partition{Size: 10}
					err := sp.Sanitize()
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
		Describe("ResetSpec sanitize", func() {
			var sp spec.ResetSpec
			BeforeEach(func() {
				sp = spec.ResetSpec{
					Active: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
				}
			})
			It("fails with empty source", func() {
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined system source to reset"))
			})
			It("fails with missing state partition", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined state partition"))
			})
			It("passes if state and source are ready", func() {
				sp.Active.Source = sdkImages.NewFileSrc("/tmp")
				sp.Partitions.State = &sdkPartitions.Partition{
					MountPoint: "/tmp",
				}
				sp.Partitions.OEM = &sdkPartitions.Partition{}
				sp.Partitions.Recovery = &sdkPartitions.Partition{}
				sp.Partitions.Persistent = &sdkPartitions.Partition{}
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})

		})
		Describe("InstallUkiSpec sanitize", func() {
			var sp spec.InstallUkiSpec
			var ghwTest ghwMock.GhwMock
			BeforeEach(func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(sdkPartitions.Disk{
					Name:      "sda",
					SizeBytes: 100 * 1024 * 1024 / 512,
				})
				ghwTest.CreateDevices()

				sp = spec.InstallUkiSpec{
					Target: "/dev/sda",
					Partitions: sdkPartitions.ElementalPartitions{
						EFI:        &sdkPartitions.Partition{Size: 10, Name: sdkConstants.EfiPartName},
						OEM:        &sdkPartitions.Partition{Size: 10, Name: sdkConstants.OEMPartName},
						Persistent: &sdkPartitions.Partition{Size: 10, Name: sdkConstants.PersistentPartName},
					},
				}
			})
			AfterEach(func() {
				ghwTest.Clean()
			})
			It("passes when the requested partitions fit the disk size", func() {
				err := sp.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})
			It("fails when the requested partitions exceed the disk size", func() {
				sp.Partitions.Persistent.Size = 200
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not fit in the target disk"))
			})
			It("fails when extra_partitions push the layout over the disk size", func() {
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{Name: "data", Size: 200},
				}
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not fit in the target disk"))
			})
			It("fails when more than one extra partition has size 0", func() {
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{Name: "data1", Size: 0},
					&sdkPartitions.Partition{Name: "data2", Size: 0},
				}
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("more than one extra partition has its size set to 0"))
			})
			It("fails when persistent and an extra partition both have size 0", func() {
				sp.Partitions.Persistent.Size = 0
				sp.ExtraPartitions = sdkPartitions.PartitionList{
					&sdkPartitions.Partition{Name: "data", Size: 0},
				}
				err := sp.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("both persistent partition and extra partitions have size set to 0"))
			})
		})
		Describe("UpgradeSpec sanitize", func() {
			var sp spec.UpgradeSpec
			BeforeEach(func() {
				sp = spec.UpgradeSpec{
					Active: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
					Recovery: sdkImages.Image{
						Source: sdkImages.NewEmptySrc(),
					},
				}
			})
			Describe("Active upgrade", func() {
				It("fails with empty source", func() {
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(constants.UpgradeNoSourceError))
				})
				It("fails with missing state partition", func() {
					sp.Active.Source = sdkImages.NewFileSrc("/tmp")
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("undefined state partition"))
				})
				It("passes if state and source are ready", func() {
					sp.Active.Source = sdkImages.NewFileSrc("/tmp")
					sp.Partitions.State = &sdkPartitions.Partition{
						MountPoint: "/tmp",
					}
					err := sp.Sanitize()
					Expect(err).ToNot(HaveOccurred())
				})
			})
			Describe("Recovery upgrade", func() {
				BeforeEach(func() {
					sp.Entry = constants.BootEntryRecovery
				})
				It("fails with empty source", func() {
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(constants.UpgradeRecoveryNoSourceError))
				})
				It("fails with missing recovery partition", func() {
					sp.Recovery.Source = sdkImages.NewFileSrc("/tmp")
					err := sp.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("undefined recovery partition"))
				})
				It("passes if state and source are ready", func() {
					sp.Recovery.Source = sdkImages.NewFileSrc("/tmp")
					sp.Partitions.Recovery = &sdkPartitions.Partition{
						MountPoint: "/tmp",
					}
					err := sp.Sanitize()
					Expect(err).ToNot(HaveOccurred())
				})
			})

		})
	})
})
