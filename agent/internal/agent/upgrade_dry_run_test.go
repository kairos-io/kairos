package agent

import (
	"bytes"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upgrade dry run summary", func() {
	var out *bytes.Buffer

	BeforeEach(func() {
		out = &bytes.Buffer{}
	})

	newSpec := func() *spec.UpgradeSpec {
		return &spec.UpgradeSpec{
			Active: sdkImages.Image{
				File:   "/run/initramfs/cos-state/cOS/transition.img",
				Size:   4200,
				FS:     "ext2",
				Label:  "COS_ACTIVE",
				Source: sdkImages.NewDockerSrc("quay.io/kairos/hadron:v4.0.0"),
			},
			Recovery: sdkImages.Image{
				File:   "/run/initramfs/cos-recovery/cOS/transition.img",
				Size:   3000,
				FS:     "squashfs",
				Source: sdkImages.NewDockerSrc("quay.io/kairos/hadron:v4.0.1"),
			},
			Partitions: sdkPartitions.ElementalPartitions{
				State:    &sdkPartitions.Partition{Path: "/dev/sda3", MountPoint: "/run/initramfs/cos-state"},
				Recovery: &sdkPartitions.Partition{Path: "/dev/sda2", MountPoint: "/run/initramfs/cos-recovery"},
			},
		}
	}

	It("reports the active image values from the resolved spec", func() {
		s := newSpec()
		s.ExcludedPaths = []string{"/var/lib/foo", "/opt/bar"}
		writeUpgradeSummary(out, s)

		Expect(out.String()).To(ContainSubstring("nothing was changed"))
		Expect(out.String()).To(MatchRegexp(`Target:\s+active`))
		Expect(out.String()).To(MatchRegexp(`Source:\s+oci://quay.io/kairos/hadron:v4.0.0`))
		Expect(out.String()).To(MatchRegexp(`Transition image:\s+/run/initramfs/cos-state/cOS/transition.img`))
		Expect(out.String()).To(MatchRegexp(`Image size:\s+4200 MiB`))
		Expect(out.String()).To(MatchRegexp(`Image label:\s+COS_ACTIVE`))
		Expect(out.String()).To(MatchRegexp(`State partition:\s+/dev/sda3 mounted at /run/initramfs/cos-state`))
		Expect(out.String()).To(MatchRegexp(`Excluded paths:\s+/var/lib/foo, /opt/bar`))
		Expect(out.String()).To(ContainSubstring("image manifest resolved"))
		Expect(out.String()).To(ContainSubstring("layers are not pulled"))
		Expect(out.String()).ToNot(ContainSubstring("v4.0.1"))
	})

	It("reports the recovery image when the recovery entry is the target", func() {
		s := newSpec()
		s.Entry = constants.BootEntryRecovery
		writeUpgradeSummary(out, s)

		Expect(out.String()).To(MatchRegexp(`Target:\s+recovery`))
		Expect(out.String()).To(MatchRegexp(`Source:\s+oci://quay.io/kairos/hadron:v4.0.1`))
		Expect(out.String()).To(MatchRegexp(`Transition image:\s+/run/initramfs/cos-recovery/cOS/transition.img`))
		Expect(out.String()).To(MatchRegexp(`Image size:\s+3000 MiB`))
		Expect(out.String()).To(MatchRegexp(`Recovery partition:\s+/dev/sda2 mounted at /run/initramfs/cos-recovery`))
		Expect(out.String()).ToNot(ContainSubstring("State partition"))
	})

	It("mentions insecure registries only when they are allowed", func() {
		s := newSpec()
		writeUpgradeSummary(out, s)
		Expect(out.String()).ToNot(ContainSubstring("Insecure registries"))

		out.Reset()
		s.AllowInsecureRegistries = true
		writeUpgradeSummary(out, s)
		Expect(out.String()).To(MatchRegexp(`Insecure registries:\s+allowed`))
	})

	It("warns that a directory source size is measured now", func() {
		s := newSpec()
		s.Active.Source = sdkImages.NewDirSrc("/")
		writeUpgradeSummary(out, s)

		Expect(out.String()).To(MatchRegexp(`Source:\s+dir:///`))
		Expect(out.String()).To(ContainSubstring("measured now"))
		Expect(out.String()).ToNot(ContainSubstring("Registry:"))
	})

	It("reports a missing partition instead of failing", func() {
		s := newSpec()
		s.Partitions.State = nil
		writeUpgradeSummary(out, s)

		Expect(out.String()).To(MatchRegexp(`State partition:\s+not found`))
	})

	It("reports the trusted boot upgrade values", func() {
		s := &spec.UpgradeUkiSpec{
			Entry: "my-entry",
			Active: sdkImages.Image{
				Size:   350,
				Source: sdkImages.NewDockerSrc("quay.io/kairos/hadron:v4.0.0-uki"),
			},
			EfiPartition: &sdkPartitions.Partition{Path: "/dev/sda1", MountPoint: "/efi"},
		}
		writeUkiUpgradeSummary(out, s)

		Expect(out.String()).To(ContainSubstring("nothing was changed"))
		Expect(out.String()).To(MatchRegexp(`Target:\s+my-entry`))
		Expect(out.String()).To(MatchRegexp(`Source:\s+oci://quay.io/kairos/hadron:v4.0.0-uki`))
		Expect(out.String()).To(MatchRegexp(`Image size:\s+350 MiB`))
		Expect(out.String()).To(MatchRegexp(`EFI partition:\s+/dev/sda1 mounted at /efi`))
		Expect(out.String()).ToNot(ContainSubstring("Transition image"))
	})

	It("defaults the trusted boot target to active", func() {
		writeUkiUpgradeSummary(out, &spec.UpgradeUkiSpec{})

		Expect(out.String()).To(MatchRegexp(`Target:\s+active`))
		Expect(out.String()).To(MatchRegexp(`Source:\s+none`))
		Expect(out.String()).To(MatchRegexp(`EFI partition:\s+not found`))
	})
})
