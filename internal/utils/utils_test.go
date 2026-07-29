package utils_test

import (
	"os"
	"path/filepath"

	"github.com/containerd/containerd/mount"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4"
	"github.com/twpayne/go-vfs/v4/vfst"
)

var _ = Describe("mount utils", func() {
	var fs vfs.FS
	var cleanup func()

	BeforeEach(func() {
		fs, cleanup, _ = vfst.NewTestFS(map[string]interface{}{
			"/proc/cmdline": "",
		})
		_, err := fs.Stat("/proc/cmdline")
		Expect(err).ToNot(HaveOccurred())
		fakeCmdline, _ := fs.RawPath("/proc/cmdline")
		err = os.Setenv("HOST_PROC_CMDLINE", fakeCmdline)
		Expect(err).ToNot(HaveOccurred())
	})
	AfterEach(func() {
		cleanup()
	})

	Context("ReadCMDLineArg", func() {
		BeforeEach(func() {
			err := fs.WriteFile("/proc/cmdline", []byte("test/key=value1 rd.immucore.debug rd.immucore.uki rd.cos.oemlabel=FAKE_LABEL empty=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})
		It("splits arguments from cmdline", func() {
			value := utils.ReadCMDLineArg("test/key=")
			Expect(len(value)).To(Equal(1))
			Expect(value[0]).To(Equal("value1"))
			value = utils.ReadCMDLineArg("rd.cos.oemlabel=")
			Expect(len(value)).To(Equal(1))
			Expect(value[0]).To(Equal("FAKE_LABEL"))
			// This is mostly wrong, it should return and empty value, not a []string of 1 empty value
			// Requires refactoring
			value = utils.ReadCMDLineArg("empty=")
			Expect(len(value)).To(Equal(1))
			Expect(value[0]).To(Equal(""))

		})
		It("returns properly for stanzas without value", func() {
			Expect(len(utils.ReadCMDLineArg("rd.immucore.debug"))).To(Equal(1))
			Expect(len(utils.ReadCMDLineArg("rd.immucore.uki"))).To(Equal(1))
		})
	})
	Context("GetRootDir", func() {
		It("Returns / for uki", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetRootDir()).To(Equal("/"))
		})
		It("Returns /sysroot", func() {
			Expect(utils.GetRootDir()).To(Equal("/sysroot"))
		})
	})
	Context("UniqueSlice", func() {
		It("Removes duplicates", func() {
			dups := []string{"a", "b", "c", "d", "b", "a"}
			dupsRemoved := utils.UniqueSlice(dups)
			Expect(len(dupsRemoved)).To(Equal(4))
		})
	})
	Context("ReadEnv", func() {
		It("Parses correctly an env file", func() {
			tmpDir, err := os.MkdirTemp("", "")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)
			err = os.WriteFile(filepath.Join(tmpDir, "layout.env"), []byte("OVERLAY=\"tmpfs:25%\"\nPERSISTENT_STATE_BIND=\"true\"\nPERSISTENT_STATE_PATHS=\"/home /opt /root\"\nRW_PATHS=\"/var /etc /srv\"\nVOLUMES=\"LABEL=COS_OEM:/oem LABEL=COS_PERSISTENT:/usr/local\""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			env, err := utils.ReadEnv(filepath.Join(tmpDir, "layout.env"))
			Expect(err).ToNot(HaveOccurred())
			Expect(env).To(HaveKey("OVERLAY"))
			Expect(env).To(HaveKey("PERSISTENT_STATE_BIND"))
			Expect(env).To(HaveKey("PERSISTENT_STATE_PATHS"))
			Expect(env).To(HaveKey("RW_PATHS"))
			Expect(env).To(HaveKey("VOLUMES"))
			Expect(env["OVERLAY"]).To(Equal("tmpfs:25%"))
			Expect(env["PERSISTENT_STATE_BIND"]).To(Equal("true"))
			Expect(env["PERSISTENT_STATE_PATHS"]).To(Equal("/home /opt /root"))
			Expect(env["RW_PATHS"]).To(Equal("/var /etc /srv"))
			Expect(env["VOLUMES"]).To(Equal("LABEL=COS_OEM:/oem LABEL=COS_PERSISTENT:/usr/local"))
		})
	})
	Context("CleanupSlice", func() {
		It("Cleans up the slice of empty values", func() {
			slice := []string{"", " "}
			sliceCleaned := utils.CleanupSlice(slice)
			Expect(len(sliceCleaned)).To(Equal(0))
		})
	})
	Context("GetTarget", func() {
		It("Returns a fake target if called with dry run", func() {
			target, label, err := utils.GetTarget(true)
			Expect(err).ToNot(HaveOccurred())
			Expect(target).To(Equal("fake"))
			// We cant manipulate runtime, so it will return an empty label as it cant identify where are we
			Expect(label).To(Equal(""))
		})
		It("Returns a fake target if immucore is disabled", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.disabled\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			target, label, err := utils.GetTarget(false)
			Expect(err).ToNot(HaveOccurred())
			Expect(target).To(Equal("fake"))
			// We cant manipulate runtime, so it will return an empty label as it cant identify where are we
			Expect(label).To(Equal(""))
		})
		It("Returns the proper target from cmdline", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("cos-img/filename=active.img\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			target, label, err := utils.GetTarget(false)
			Expect(err).ToNot(HaveOccurred())
			Expect(target).To(Equal("active.img"))
			// We cant manipulate runtime, so it will return an empty label as it cant identify where are we
			Expect(label).To(Equal(""))
		})
		It("Returns an empty target if we are on UKI", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			target, label, err := utils.GetTarget(false)
			Expect(err).ToNot(HaveOccurred())
			Expect(target).To(Equal(""))
			// We cant manipulate runtime, so it will return an empty label as it cant identify where are we
			Expect(label).To(Equal(""))
		})
		It("Returns an error if we dont have the target in the cmdline", func() {
			target, label, err := utils.GetTarget(false)
			Expect(err).To(HaveOccurred())
			Expect(target).To(Equal(""))
			Expect(label).To(Equal(""))
		})
	})
	Context("DisableImmucore", func() {
		It("Disables immucore if cmdline contains live:LABEL", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("root=live:LABEL=COS_LIVE\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.DisableImmucore()).To(BeTrue())
		})
		It("Disables immucore if cmdline contains live:CDLABEL", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("root=live:CDLABEL=COS_LIVE\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.DisableImmucore()).To(BeTrue())
		})
		It("Disables immucore if cmdline contains netboot", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("netboot\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.DisableImmucore()).To(BeTrue())
		})
		It("Disables immucore if cmdline contains rd.cos.disable", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.disable\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.DisableImmucore()).To(BeTrue())
		})
		It("Disables immucore if cmdline contains rd.immucore.disable", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.disable\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.DisableImmucore()).To(BeTrue())
		})
		It("Enables immucore by default", func() {
			Expect(utils.DisableImmucore()).To(BeFalse())
		})
	})
	Context("RootRW", func() {
		It("Defaults to RO", func() {
			Expect(utils.RootRW()).To(Equal("ro"))
		})
		It("Sets RW if set via cmdline with rd.cos.debugrw", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.debugrw\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.RootRW()).To(Equal("rw"))
		})
		It("Sets RW if set via cmdline with rd.immucore.debugrw", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.debugrw\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.RootRW()).To(Equal("rw"))
		})
		It("Sets RW if set via cmdline with both rd.cos.debugrw and rd.immucore.debugrw at the same time", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.debugrw rd.immucore.debugrw\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.RootRW()).To(Equal("rw"))
		})
	})
	Context("IsUKI", func() {
		It("Returns false in a normal boot", func() {
			Expect(utils.IsUKI()).To(BeFalse())
		})
		It("Returns true if set via cmdline with rd.immucore.uki", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.IsUKI()).To(BeTrue())
		})
	})
	Context("ConsoleDevices", func() {
		It("Falls back to the conventional trio when no console= present", func() {
			Expect(utils.ConsoleDevices()).To(Equal([]string{"/dev/tty1", "/dev/ttyS0", "/dev/console"}))
		})
		It("Parses a single console= entry and appends /dev/console", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("console=ttyS1\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.ConsoleDevices()).To(Equal([]string{"/dev/ttyS1", "/dev/console"}))
		})
		It("Parses multiple console= entries and strips baud options", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("console=tty0 console=ttyS0,115200n8\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.ConsoleDevices()).To(Equal([]string{"/dev/tty0", "/dev/ttyS0", "/dev/console"}))
		})
		It("Does not duplicate /dev/console when explicitly listed", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("console=console\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.ConsoleDevices()).To(Equal([]string{"/dev/console"}))
		})
	})
	Context("BootInRAM", func() {
		It("Returns false when kairos.ram is absent", func() {
			Expect(utils.BootInRAM()).To(BeFalse())
		})
		It("Returns true when kairos.ram is set on its own", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("kairos.ram\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.BootInRAM()).To(BeTrue())
		})
		It("Returns true when kairos.ram is set alongside a live: cmdline", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("root=live:LABEL=COS_LIVE ro kairos.ram rd.live.ram=1\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.BootInRAM()).To(BeTrue())
		})
		It("Returns true when kairos.ram is set alongside netboot", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("netboot kairos.ram\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.BootInRAM()).To(BeTrue())
		})
		It("Does not match a substring token", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("kairos.ram_experimental=1\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.BootInRAM()).To(BeFalse())
		})
	})
	Context("ParseMount", func() {
		It("Returns disk path by LABEL", func() {
			Expect(utils.ParseMount("LABEL=MY_LABEL")).To(Equal("/dev/disk/by-label/MY_LABEL"))
		})
		It("Returns disk path by UUID", func() {
			Expect(utils.ParseMount("UUID=9999")).To(Equal("/dev/disk/by-uuid/9999"))
		})
	})
	Context("AppendSlash", func() {
		It("Appends a slash if it doesnt have one", func() {
			noSlash := "/noslash"
			Expect(utils.AppendSlash(noSlash)).To(Equal("/noslash/"))
		})
		It("Does not append a slash if it already has one", func() {
			slash := "/yesslash/"
			Expect(utils.AppendSlash(slash)).To(Equal("/yesslash/"))
		})
	})
	Context("MountToFstab", func() {
		It("Generates teh proper fstab config", func() {
			m := mount.Mount{
				Type:    "fakefs",
				Source:  "/dev/fake",
				Options: []string{"option1", "option=2"},
			}
			fstab := utils.MountToFstab(m)
			fstab.File = "/mnt/fake"
			// Options can be shown in whatever order, so regexp that
			Expect(fstab.String()).To(MatchRegexp("/dev/fake /mnt/fake fakefs (option1|option=2),(option=2|option1) 0 0"))
			Expect(fstab.Spec).To(Equal("/dev/fake"))
			Expect(fstab.VfsType).To(Equal("fakefs"))
			Expect(fstab.MntOps).To(HaveKey("option1"))
			Expect(fstab.MntOps).To(HaveKey("option"))
			Expect(fstab.MntOps["option1"]).To(Equal(""))
			Expect(fstab.MntOps["option"]).To(Equal("2"))
		})
	})
	Context("CleanSysrootForFstab", func() {
		It("Removes /sysroot", func() {
			Expect(utils.CleanSysrootForFstab("/sysroot/dev")).To(Equal("/dev"))
			Expect(utils.CleanSysrootForFstab("/sysroot/sysroot/dev")).To(Equal("/dev"))
			Expect(utils.CleanSysrootForFstab("sysroot/dev")).To(Equal("sysroot/dev"))
			Expect(utils.CleanSysrootForFstab("/dev/sysroot/dev")).To(Equal("/dev/dev"))
			Expect(utils.CleanSysrootForFstab("/dev/")).To(Equal("/dev/"))
			Expect(utils.CleanSysrootForFstab("/dev")).To(Equal("/dev"))
			Expect(utils.CleanSysrootForFstab("//sysroot/dev")).To(Equal("//dev"))
			Expect(utils.CleanSysrootForFstab("/sysroot//dev")).To(Equal("//dev"))
		})
	})
	Context("GetOemTimeout", func() {
		It("Gets timeout from rd.cos.oemtimeout", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.oemtimeout=100\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemTimeout()).To(Equal(100))
		})
		It("Gets timeout from rd.immucore.oemtimeout", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.oemtimeout=200\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemTimeout()).To(Equal(200))
		})
		It("Gets timeout from both rd.cos.oemtimeout and rd.immucore.oemtimeout(immucore has precedence)", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.oemtimeout=100 rd.immucore.oemtimeout=200\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemTimeout()).To(Equal(200))
		})
		It("Fails to parse from cmdline and gets default", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.oemtimeout=really\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemTimeout()).To(Equal(5))
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.oemtimeout=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemTimeout()).To(Equal(5))
		})
		It("Gets default timeout", func() {
			Expect(utils.GetOemTimeout()).To(Equal(5))
		})
	})
	Context("GetOverlayBase", func() {
		It("Gets overlay from rd.cos.overlay", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.overlay=tmpfs:100%\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:100%"))
		})
		It("Gets overlay from rd.immucore.overlay", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.overlay=tmpfs:200%\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:200%"))
		})
		It("Gets overlay from both rd.cos.overlay and rd.immucore.overlay(immucore has precedence)", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.overlay=tmpfs:100% rd.immucore.overlay=tmpfs:200%\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:200%"))
		})
		It("Fails to parse from cmdline and gets default", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.overlay=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:20%"))
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.overlay=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:20%"))
		})
		It("Gets default overlay", func() {
			Expect(utils.GetOverlayBase()).To(Equal("tmpfs:20%"))
		})
	})
	Context("GetOemLabel", func() {
		It("Gets label from rd.cos.oemlabel", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.oemlabel=COS_LABEL\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemLabel()).To(Equal("COS_LABEL"))
		})
		It("Gets label from rd.immucore.oemlabel", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.immucore.oemlabel=IMMUCORE_LABEL\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemLabel()).To(Equal("IMMUCORE_LABEL"))
		})
		It("Gets label from both rd.cos.oemlabel and rd.immucore.oemlabel(immucore has precedence)", func() {
			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.oemlabel=COS_LABEL rd.immucore.oemlabel=IMMUCORE_LABEL\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemLabel()).To(Equal("IMMUCORE_LABEL"))
		})
		It("Fails to parse from cmdline and gets default from runtime", func() {
			mainDisk := block.Disk{
				Name: "device",
				Partitions: []*block.Partition{
					{
						Name:            "device2",
						FilesystemLabel: "COS_OEM",
						Label:           "COS_OEM",
						Type:            "ext4",
						MountPoint:      "/oem",
					},
				},
			}
			ghwTest := mocks.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
			defer ghwTest.Clean()

			err := fs.WriteFile("/proc/cmdline", []byte("rd.cos.oemlabel=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemLabel()).To(Equal("COS_OEM"))
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.oemlabel=\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.GetOemLabel()).To(Equal("COS_OEM"))
		})
	})
})
