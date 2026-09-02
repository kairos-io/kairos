package utils

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeBlkid answers for the blkid binary and counts what was asked of it.
type fakeBlkid struct {
	// answers maps an argument string to what blkid prints for it.
	answers map[string]string
	// err is returned alongside the answer, as blkid --help does on some
	// builds.
	err error
	// asked counts every call, keyed by the argument string.
	asked map[string]int
}

func (f *fakeBlkid) run(args string) (string, error) {
	if f.asked == nil {
		f.asked = map[string]int{}
	}
	f.asked[args]++

	return f.answers[args], f.err
}

// installFakeBlkid points DiskFSType at the fake for one spec and resets the
// cached BusyBox answer, so specs do not inherit each other's.
func installFakeBlkid(fake *fakeBlkid) {
	previousBlkid := blkid
	previousDetect := blkidIsBusyBox

	blkid = fake.run
	blkidIsBusyBox = sync.OnceValue(func() bool {
		out, _ := blkid("--help")
		return strings.Contains(out, "BusyBox")
	})

	DeferCleanup(func() {
		blkid = previousBlkid
		blkidIsBusyBox = previousDetect
	})
}

var _ = Describe("DiskFSType", func() {
	const device = "/dev/disk/by-label/COS_STATE"

	Describe("with util-linux blkid", func() {
		It("returns the value blkid printed", func() {
			installFakeBlkid(&fakeBlkid{answers: map[string]string{
				"--help":                     "Usage:\n blkid [options]",
				device + " -s TYPE -o value": "ext4\n",
			}})

			Expect(DiskFSType(device)).To(Equal("ext4"))
		})

		It("returns nothing for a device blkid knows nothing about", func() {
			installFakeBlkid(&fakeBlkid{answers: map[string]string{
				"--help": "Usage:\n blkid [options]",
			}})

			Expect(DiskFSType(device)).To(BeEmpty())
		})
	})

	Describe("with BusyBox blkid", func() {
		busyBoxHelp := "BusyBox v1.36.1 multi-call binary."

		It("picks the type out of the whole tag list", func() {
			installFakeBlkid(&fakeBlkid{answers: map[string]string{
				"--help":                     busyBoxHelp,
				device + " -s TYPE -o value": `/dev/sda2: LABEL="COS_STATE" UUID="6f7d" TYPE="ext4"`,
			}})

			Expect(DiskFSType(device)).To(Equal("ext4"))
		})

		// The guard on the split used to read "< 1", which strings.Split can
		// never satisfy, so a last field with no "=" in it reached index 1
		// and panicked instead of falling back.
		DescribeTable("falls back to ext4 rather than panicking",
			func(output string) {
				installFakeBlkid(&fakeBlkid{answers: map[string]string{
					"--help":                     busyBoxHelp,
					device + " -s TYPE -o value": output,
				}})

				Expect(func() { DiskFSType(device) }).ToNot(Panic())
				Expect(DiskFSType(device)).To(Equal("ext4"))
			},
			Entry("a device blkid found no tags for", "/dev/sda2:"),
			Entry("a trailing field carrying no separator", `/dev/sda2: LABEL="COS_STATE" ext4`),
			Entry("a bare error line", "blkid: unknown device"),
		)

		It("returns ext4 when blkid printed nothing at all", func() {
			installFakeBlkid(&fakeBlkid{answers: map[string]string{"--help": busyBoxHelp}})

			Expect(DiskFSType(device)).To(Equal("ext4"))
		})
	})

	// DiskFSType runs once per attempt inside the mount retry loop, so a
	// second fork per call is a fork per attempt.
	It("asks blkid which blkid it is only once", func() {
		fake := &fakeBlkid{answers: map[string]string{
			"--help": "Usage:\n blkid [options]",
		}}
		installFakeBlkid(fake)

		for i := 0; i < 5; i++ {
			DiskFSType(fmt.Sprintf("/dev/sda%d", i))
		}

		Expect(fake.asked["--help"]).To(Equal(1))
		// The device query itself is per call, and stays that way: the
		// answer is different for every device.
		Expect(fake.asked["/dev/sda3 -s TYPE -o value"]).To(Equal(1))
	})

	It("takes the BusyBox answer from the output even when blkid --help fails", func() {
		fake := &fakeBlkid{
			answers: map[string]string{
				"--help":                     "BusyBox v1.36.1 multi-call binary.",
				device + " -s TYPE -o value": `/dev/sda2: TYPE="xfs"`,
			},
			err: errors.New("exit status 1"),
		}
		installFakeBlkid(fake)

		Expect(DiskFSType(device)).To(Equal("xfs"))
	})
})
