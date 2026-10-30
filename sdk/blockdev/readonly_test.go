package blockdev

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReadOnly", func() {
	var root string
	// answers is what the ioctl says for a device path; rdevs is what stat says.
	var answers map[string]bool
	var rdevs map[string]string

	// device declares a block device in the fixture sysfs, with its read-only
	// flag, and optionally as a dm-crypt mapper.
	device := func(rdev string, ro bool, crypt bool) {
		dir := filepath.Join(root, rdev)
		Expect(os.MkdirAll(dir, 0755)).To(Succeed())
		flag := "0\n"
		if ro {
			flag = "1\n"
		}
		Expect(os.WriteFile(filepath.Join(dir, "ro"), []byte(flag), 0644)).To(Succeed())
		if crypt {
			Expect(os.MkdirAll(filepath.Join(dir, "dm"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "dm", "uuid"), []byte("CRYPT-LUKS2-abc-name\n"), 0644)).To(Succeed())
		}
	}

	// stack declares that the mapper at rdev sits on these devices, the way
	// /sys/dev/block/<rdev>/slaves/<name>/dev does.
	stack := func(rdev string, slaves map[string]string) {
		for name, slaveRdev := range slaves {
			dir := filepath.Join(root, rdev, "slaves", name)
			Expect(os.MkdirAll(dir, 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "dev"), []byte(slaveRdev+"\n"), 0644)).To(Succeed())
		}
	}

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		answers = map[string]bool{}
		rdevs = map[string]string{}

		originalSys, originalProbe, originalRdev := sysDevBlock, probe, rdevOf
		sysDevBlock = root
		probe = func(path string) (bool, error) {
			ro, known := answers[path]
			if !known {
				return false, errors.New("no such device")
			}
			return ro, nil
		}
		rdevOf = func(path string) (string, error) {
			rdev, known := rdevs[path]
			if !known {
				return "", errors.New("not in sysfs")
			}
			return rdev, nil
		}
		DeferCleanup(func() {
			sysDevBlock, probe, rdevOf = originalSys, originalProbe, originalRdev
		})
	})

	Context("a plain partition", func() {
		It("reports what the kernel says", func() {
			answers["/dev/vda5"] = true
			Expect(ReadOnly("/dev/vda5")).To(BeTrue())

			answers["/dev/vdb5"] = false
			rdevs["/dev/vdb5"] = "253:21"
			device("253:21", false, false)
			Expect(ReadOnly("/dev/vdb5")).To(BeFalse())
		})

		It("returns the error rather than claiming the device is writable", func() {
			ro, err := ReadOnly("/dev/nope")
			Expect(err).To(HaveOccurred())
			Expect(ro).To(BeFalse(), "an unanswerable probe must not be read as permission to write")
		})

		It("trusts the kernel when the device is not in sysfs", func() {
			// The ioctl answered, so it is a device. Not finding it in the
			// fixture is not a reason to doubt that.
			answers["/dev/vda5"] = false
			Expect(ReadOnly("/dev/vda5")).To(BeFalse())
		})
	})

	Context("a dm-crypt mapper", func() {
		It("believes its own flag when that says read-only", func() {
			answers["/dev/mapper/persistent"] = true
			Expect(ReadOnly("/dev/mapper/persistent")).To(BeTrue())
		})

		It("also asks the device underneath, and believes it if it says read-only", func() {
			// Insurance for a stack assembled some way that left the mapper
			// claiming writable. On a Kairos boot the kernel refuses to build
			// such a mapper in the first place, so this is not the expected path.
			answers["/dev/mapper/persistent"] = false
			rdevs["/dev/mapper/persistent"] = "253:0"
			device("253:0", false, true)
			device("8:5", true, false)
			stack("253:0", map[string]string{"sda5": "8:5"})
			Expect(ReadOnly("/dev/mapper/persistent")).To(BeTrue())
		})

		It("reports writable when everything underneath is", func() {
			answers["/dev/mapper/persistent"] = false
			rdevs["/dev/mapper/persistent"] = "253:0"
			device("253:0", false, true)
			device("8:5", false, false)
			stack("253:0", map[string]string{"sda5": "8:5"})
			Expect(ReadOnly("/dev/mapper/persistent")).To(BeFalse())
		})

		It("recurses through a crypt mapper on a crypt mapper", func() {
			answers["/dev/mapper/outer"] = false
			rdevs["/dev/mapper/outer"] = "253:1"
			device("253:1", false, true)
			device("253:0", false, true)
			device("8:5", true, false)
			stack("253:1", map[string]string{"dm-0": "253:0"})
			stack("253:0", map[string]string{"sda5": "8:5"})
			Expect(ReadOnly("/dev/mapper/outer")).To(BeTrue())
		})

		It("reports read-only when any one of several devices underneath is", func() {
			answers["/dev/mapper/persistent"] = false
			rdevs["/dev/mapper/persistent"] = "253:0"
			device("253:0", false, true)
			device("8:5", false, false)
			device("8:21", true, false)
			stack("253:0", map[string]string{"sda5": "8:5", "sdb5": "8:21"})
			Expect(ReadOnly("/dev/mapper/persistent")).To(BeTrue())
		})

		It("surfaces the error when a device underneath cannot be read and none said read-only", func() {
			answers["/dev/mapper/persistent"] = false
			rdevs["/dev/mapper/persistent"] = "253:0"
			device("253:0", false, true)
			device("8:5", false, false)
			stack("253:0", map[string]string{"sda5": "8:5", "missing": "8:99"})
			ro, err := ReadOnly("/dev/mapper/persistent")
			Expect(err).To(HaveOccurred())
			Expect(ro).To(BeFalse())
		})
	})

	Context("a mapper that is not dm-crypt", func() {
		It("is not descended into, because a snapshot writes around a read-only origin", func() {
			answers["/dev/mapper/snap"] = false
			rdevs["/dev/mapper/snap"] = "253:3"
			device("253:3", false, false)
			Expect(os.MkdirAll(filepath.Join(root, "253:3", "dm"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "253:3", "dm", "uuid"), []byte("LVM-xyz\n"), 0644)).To(Succeed())
			device("8:5", true, false)
			stack("253:3", map[string]string{"sda5": "8:5"})
			Expect(ReadOnly("/dev/mapper/snap")).To(BeFalse())
		})
	})

	Context("a by-label symlink", func() {
		It("resolves through stat, not through the name", func() {
			// The fixture answers for the symlink path itself, which is what a
			// caller hands over; nothing in the package derives a kernel name
			// from the path any more.
			link := filepath.Join(GinkgoT().TempDir(), "COS_PERSISTENT")
			Expect(os.Symlink("/dev/vda5", link)).To(Succeed())
			answers[link] = true
			Expect(ReadOnly(link)).To(BeTrue())
		})
	})
})
