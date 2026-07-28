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

package utils_test

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	"github.com/kairos-io/kairos-sdk/state"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// failingMounter wraps the fake mounter to error out on IsLikelyNotMountPoint
type failingMounter struct {
	*v1mock.ErrorMounter
}

func (f *failingMounter) IsLikelyNotMountPoint(_ string) (bool, error) {
	return false, errors.New("mounter failure")
}

var _ = Describe("Utils extra coverage", Label("utils"), func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var fs *vfst.TestFS
	var cleanup func()

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		logger = sdkLogger.NewNullLogger()
		fs, cleanup, _ = vfst.NewTestFS(nil)
		Expect(fs.Mkdir("/tmp", constants.DirPerm)).To(Succeed())
		Expect(fs.Mkdir("/run", constants.DirPerm)).To(Succeed())
		Expect(fs.Mkdir("/etc", constants.DirPerm)).To(Succeed())

		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscallMock),
			agentConfig.WithClient(client),
		)
	})
	AfterEach(func() { cleanup() })

	Describe("ConcatFiles", Label("ConcatFiles"), func() {
		It("fails with an empty sources list", func() {
			err := utils.ConcatFiles(fs, []string{}, "/target.txt")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Empty sources list"))
		})
		It("copies into a directory target using the source basename", func() {
			Expect(fs.WriteFile("/src.txt", []byte("content"), constants.FilePerm)).To(Succeed())
			Expect(fs.Mkdir("/destdir", constants.DirPerm)).To(Succeed())
			Expect(utils.ConcatFiles(fs, []string{"/src.txt"}, "/destdir")).To(Succeed())
			data, err := fs.ReadFile("/destdir/src.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).To(Equal("content"))
		})
		It("concatenates multiple sources in order", func() {
			Expect(fs.WriteFile("/one.txt", []byte("one"), constants.FilePerm)).To(Succeed())
			Expect(fs.WriteFile("/two.txt", []byte("two"), constants.FilePerm)).To(Succeed())
			Expect(utils.ConcatFiles(fs, []string{"/one.txt", "/two.txt"}, "/target.txt")).To(Succeed())
			data, err := fs.ReadFile("/target.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).To(Equal("onetwo"))
		})
		It("fails when the target cannot be created", func() {
			Expect(fs.WriteFile("/src.txt", []byte("content"), constants.FilePerm)).To(Succeed())
			err := utils.ConcatFiles(fs, []string{"/src.txt"}, "/missing-dir/target.txt")
			Expect(err).To(HaveOccurred())
		})
		It("stops on a missing source", func() {
			// NOTE: the error from fs.Open inside the loop is shadowed by the
			// `:=` declaration, so ConcatFiles currently returns nil even
			// though the source could not be read. This test documents the
			// current behavior while covering the break path.
			err := utils.ConcatFiles(fs, []string{"/does-not-exist.txt"}, "/target.txt")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("CreateDirStructure failures", Label("CreateDirStructure"), func() {
		It("fails when a first level dir cannot be created", func() {
			Expect(fs.Mkdir("/target", constants.DirPerm)).To(Succeed())
			// Block /target/run with a file
			Expect(fs.WriteFile("/target/run", []byte(""), constants.FilePerm)).To(Succeed())
			Expect(utils.CreateDirStructure(fs, "/target")).ToNot(Succeed())
		})
		It("fails when a no-write dir cannot be created", func() {
			Expect(fs.Mkdir("/target", constants.DirPerm)).To(Succeed())
			// Block /target/proc with a file
			Expect(fs.WriteFile("/target/proc", []byte(""), constants.FilePerm)).To(Succeed())
			Expect(utils.CreateDirStructure(fs, "/target")).ToNot(Succeed())
		})
		It("fails when the tmp dir cannot be created", func() {
			Expect(fs.Mkdir("/target", constants.DirPerm)).To(Succeed())
			// Block /target/tmp with a file so MkdirAll fails
			Expect(fs.WriteFile("/target/tmp", []byte(""), constants.FilePerm)).To(Succeed())
			Expect(utils.CreateDirStructure(fs, "/target")).ToNot(Succeed())
		})
	})

	Describe("SyncData extra", Label("SyncData"), func() {
		It("works with a nil fs and relies on the runner", func() {
			err := utils.SyncData(logger, runner, nil, "/source", "/target")
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{{constants.Rsync}})).To(BeNil())
		})
		It("fails if the target can't be created", func() {
			rofs := vfs.NewReadOnlyFS(fs)
			err := utils.SyncData(logger, runner, rofs, "/source", "/missing-target")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetTempDir", Label("tmpdir"), func() {
		var oldTmpDir string
		var hadTmpDir bool

		BeforeEach(func() {
			oldTmpDir, hadTmpDir = os.LookupEnv("TMPDIR")
		})
		AfterEach(func() {
			if hadTmpDir {
				Expect(os.Setenv("TMPDIR", oldTmpDir)).To(Succeed())
			} else {
				Expect(os.Unsetenv("TMPDIR")).To(Succeed())
			}
		})

		It("respects the TMPDIR var", func() {
			Expect(os.Setenv("TMPDIR", "/customtmp")).To(Succeed())
			Expect(utils.GetTempDir(config, "suffix")).To(Equal("/customtmp/elemental-suffix"))
		})
		It("generates a random suffix when empty", func() {
			Expect(os.Setenv("TMPDIR", "/customtmp")).To(Succeed())
			dir := utils.GetTempDir(config, "")
			Expect(dir).To(HavePrefix("/customtmp/elemental-"))
			Expect(dir).ToNot(Equal("/customtmp/elemental-"))
		})
		It("uses the persistent partition if mounted", func() {
			Expect(os.Unsetenv("TMPDIR")).To(Succeed())
			ghwTest := ghwMock.GhwMock{}
			ghwTest.AddDisk(sdkPartitions.Disk{Name: "device", Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: constants.PersistentLabel,
					FS:              "ext4",
					MountPoint:      "/usr/local",
				},
			}})
			ghwTest.CreateDevices()
			defer ghwTest.Clean()
			// Register the mountpoint in the fake mounter so IsMounted agrees
			Expect(mounter.Mount("/dev/device1", "/usr/local", "ext4", []string{})).To(Succeed())

			Expect(utils.GetTempDir(config, "suffix")).To(Equal("/usr/local/tmp/elemental-suffix"))
		})
		It("defaults to /tmp when there is no persistent partition", func() {
			Expect(os.Unsetenv("TMPDIR")).To(Succeed())
			ghwTest := ghwMock.GhwMock{}
			ghwTest.CreateDevices()
			defer ghwTest.Clean()

			Expect(utils.GetTempDir(config, "suffix")).To(Equal("/tmp/elemental-suffix"))
		})
		It("defaults to /tmp when the persistent partition is not mounted", func() {
			Expect(os.Unsetenv("TMPDIR")).To(Succeed())
			ghwTest := ghwMock.GhwMock{}
			ghwTest.AddDisk(sdkPartitions.Disk{Name: "device", Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: constants.PersistentLabel,
					FS:              "ext4",
				},
			}})
			ghwTest.CreateDevices()
			defer ghwTest.Clean()

			Expect(utils.GetTempDir(config, "suffix")).To(Equal("/tmp/elemental-suffix"))
		})
	})

	Describe("IsHTTPURI extra", Label("uri"), func() {
		It("returns the error for an unparseable uri", func() {
			_, err := utils.IsHTTPURI("http://foo\nbar")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetSource extra", Label("GetSource"), func() {
		It("fails on an unparseable source url", func() {
			err := utils.GetSource(config, "http://foo\nbar", "/destination/file")
			Expect(err).To(HaveOccurred())
		})
		It("fails if the destination dir cannot be created", func() {
			Expect(fs.WriteFile("/blocked", []byte(""), constants.FilePerm)).To(Succeed())
			err := utils.GetSource(config, "file:///source-file", "/blocked/dir/file")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetSource copy failure", Label("GetSource"), func() {
		It("fails when the file cannot be copied to the destination", func() {
			Expect(fs.WriteFile("/src.txt", []byte("data"), constants.FilePerm)).To(Succeed())
			// destination is a dir which already contains a dir named after
			// the source file, so the copy target cannot be created
			Expect(fsutils.MkdirAll(fs, "/destdir/src.txt", constants.DirPerm)).To(Succeed())
			err := utils.GetSource(config, "file:///src.txt", "/destdir")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IsMounted mounter failure", Label("ismounted"), func() {
		It("returns the error from the mounter", func() {
			failConfig := agentConfig.NewConfig(
				agentConfig.WithFs(fs),
				agentConfig.WithRunner(runner),
				agentConfig.WithLogger(logger),
				agentConfig.WithMounter(&failingMounter{ErrorMounter: mounter}),
				agentConfig.WithSyscall(syscallMock),
			)
			part := &sdkPartitions.Partition{MountPoint: "/some/mountpoint"}
			mounted, err := utils.IsMounted(failConfig, part)
			Expect(err).To(HaveOccurred())
			Expect(mounted).To(BeFalse())
		})
	})

	Describe("ValidTaggedContainerReference extra", Label("reference"), func() {
		It("returns false for an invalid reference", func() {
			Expect(utils.ValidTaggedContainerReference("INVALID UPPERCASE:tag")).To(BeFalse())
		})
		It("returns false for a name-only reference", func() {
			Expect(utils.ValidTaggedContainerReference("quay.io/kairos/opensuse")).To(BeFalse())
		})
		It("returns true for a tagged reference", func() {
			Expect(utils.ValidTaggedContainerReference("quay.io/kairos/opensuse:latest")).To(BeTrue())
		})
	})

	Describe("CalcFileChecksum extra", Label("checksum"), func() {
		It("fails if the file cannot be opened", func() {
			_, err := utils.CalcFileChecksum(fs, "/no-such-file")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FindCommand", Label("FindCommand"), func() {
		It("returns the first full path that is executable", func() {
			Expect(fsutils.MkdirAll(fs, "/usr/sbin", constants.DirPerm)).To(Succeed())
			f, err := fs.Create("/usr/sbin/grub2-install")
			Expect(err).ToNot(HaveOccurred())
			Expect(f.Chmod(0755)).To(Succeed())
			Expect(f.Close()).To(Succeed())
			found := utils.FindCommand(fs, "default", []string{"/usr/sbin/grub2-install"})
			Expect(found).To(Equal("/usr/sbin/grub2-install"))
		})
		It("skips full paths that do not exist", func() {
			found := utils.FindCommand(fs, "default", []string{"/no/such/binary"})
			Expect(found).To(Equal("default"))
		})
		It("skips full paths that are directories", func() {
			Expect(fsutils.MkdirAll(fs, "/some/dir", constants.DirPerm)).To(Succeed())
			found := utils.FindCommand(fs, "default", []string{"/some/dir"})
			Expect(found).To(Equal("default"))
		})
		It("skips full paths that are not executable", func() {
			Expect(fs.WriteFile("/notexec", []byte(""), 0644)).To(Succeed())
			found := utils.FindCommand(fs, "default", []string{"/notexec"})
			Expect(found).To(Equal("default"))
		})
		It("resolves bare names via the system PATH", func() {
			found := utils.FindCommand(fs, "default", []string{"sh"})
			Expect(found).ToNot(Equal("default"))
			Expect(found).To(HaveSuffix("/sh"))
		})
		It("returns the default when nothing matches", func() {
			found := utils.FindCommand(fs, "default", []string{"this-command-does-not-exist-kairos"})
			Expect(found).To(Equal("default"))
		})
	})

	Describe("IsUki and UkiBootMode", Label("uki"), func() {
		// IsUki and UkiBootMode read the real /proc/cmdline, so derive the
		// expectation from the host cmdline to keep the test deterministic.
		It("matches the host kernel cmdline", func() {
			cmdline, _ := os.ReadFile("/proc/cmdline")
			expected := strings.Contains(string(cmdline), "rd.immucore.uki")
			Expect(utils.IsUki()).To(Equal(expected))
		})
		It("returns Unknown boot mode when not running UKI", func() {
			cmdline, _ := os.ReadFile("/proc/cmdline")
			if strings.Contains(string(cmdline), "rd.immucore.uki") {
				Skip("running on a UKI system")
			}
			Expect(utils.UkiBootMode()).To(Equal(state.Unknown))
		})
	})

	Describe("CheckFailedInstallation", Label("install"), func() {
		It("returns false when the state file does not exist", func() {
			failed, err := utils.CheckFailedInstallation("/no/such/state/file")
			Expect(err).ToNot(HaveOccurred())
			Expect(failed).To(BeFalse())
		})
		It("returns true and the content when the state file exists", func() {
			temp, err := os.CreateTemp("", "kairos-failed-install")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.Remove(temp.Name()) })
			_, err = temp.WriteString("something went wrong")
			Expect(err).ToNot(HaveOccurred())
			Expect(temp.Close()).To(Succeed())

			failed, err := utils.CheckFailedInstallation(temp.Name())
			Expect(failed).To(BeTrue())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("something went wrong"))
		})
		It("returns true and an error when the state file cannot be read", func() {
			// A directory stats fine but fails to be read as a file
			dir, err := os.MkdirTemp("", "kairos-failed-install-dir")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(dir) })

			failed, err := utils.CheckFailedInstallation(dir)
			Expect(failed).To(BeTrue())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).ToNot(ContainSubstring("Installation failed"))
		})
	})

	Describe("GetMajorImageVersion", Label("pe"), func() {
		// writePE writes a minimal valid PE binary with the given optional
		// header magic so debug/pe can parse it.
		writePE := func(path string, is64 bool) {
			var buf bytes.Buffer
			dos := make([]byte, 0x40)
			dos[0] = 'M'
			dos[1] = 'Z'
			binary.LittleEndian.PutUint32(dos[0x3c:], 0x40)
			buf.Write(dos)
			buf.WriteString("PE\x00\x00")
			var optSize int
			if is64 {
				optSize = binary.Size(pe.OptionalHeader64{})
			} else {
				optSize = binary.Size(pe.OptionalHeader32{})
			}
			fh := pe.FileHeader{
				Machine:              pe.IMAGE_FILE_MACHINE_AMD64,
				NumberOfSections:     0,
				SizeOfOptionalHeader: uint16(optSize),
			}
			Expect(binary.Write(&buf, binary.LittleEndian, fh)).To(Succeed())
			if is64 {
				oh := pe.OptionalHeader64{Magic: 0x20b, MajorImageVersion: 42, NumberOfRvaAndSizes: 16}
				Expect(binary.Write(&buf, binary.LittleEndian, oh)).To(Succeed())
			} else {
				oh := pe.OptionalHeader32{Magic: 0x10b, MajorImageVersion: 41, NumberOfRvaAndSizes: 16}
				Expect(binary.Write(&buf, binary.LittleEndian, oh)).To(Succeed())
			}
			Expect(os.WriteFile(path, buf.Bytes(), 0644)).To(Succeed())
		}

		It("reads the major image version from a PE32+ binary", func() {
			dir, err := os.MkdirTemp("", "kairos-pe")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(dir) })
			peFile := filepath.Join(dir, "systemd-bootx64.efi")
			writePE(peFile, true)

			version, err := utils.GetMajorImageVersion(peFile)
			Expect(err).ToNot(HaveOccurred())
			Expect(version).To(Equal(uint16(42)))
		})
		It("fails on a PE32 (non 64-bit) binary", func() {
			dir, err := os.MkdirTemp("", "kairos-pe")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(dir) })
			peFile := filepath.Join(dir, "systemd-bootia32.efi")
			writePE(peFile, false)

			_, err = utils.GetMajorImageVersion(peFile)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unexpected PE optional header type"))
		})
		It("fails when the file does not exist", func() {
			_, err := utils.GetMajorImageVersion("/no/such/file.efi")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IsMountReadOnly", Label("mount"), func() {
		It("returns false for a writable dir", func() {
			dir, err := os.MkdirTemp("", "kairos-rw")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(dir) })
			Expect(utils.IsMountReadOnly(dir)).To(BeFalse())
		})
		It("returns false when statfs fails", func() {
			Expect(utils.IsMountReadOnly("/no/such/mountpoint")).To(BeFalse())
		})
	})

	Describe("SystemdBootConf reader/writer errors", func() {
		It("reader fails on a missing file", func() {
			_, err := utils.SystemdBootConfReader(fs, "/missing.conf")
			Expect(err).To(HaveOccurred())
		})
		It("writer fails when the file cannot be created", func() {
			err := utils.SystemdBootConfWriter(fs, "/missing-dir/test.conf", map[string]string{"timeout": "5"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("AddBootAssessment rename failure", func() {
		It("fails when the conf file cannot be renamed", func() {
			if os.Geteuid() == 0 {
				Skip("running as root, permissions are not enforced")
			}
			Expect(fsutils.MkdirAll(fs, "/entries", constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile("/entries/test.conf", []byte(""), constants.FilePerm)).To(Succeed())
			// Read-only dir: listing works but renaming inside fails
			Expect(fs.Chmod("/entries", 0o555)).To(Succeed())

			err := utils.AddBootAssessment(fs, "/entries", logger)
			// Restore permissions right away so the test fs can be cleaned up
			Expect(fs.Chmod("/entries", 0o755)).To(Succeed())
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ReadPersistentVariables extra", Label("grub"), func() {
		It("fails on lines without an equals sign", func() {
			Expect(fs.WriteFile("/grubenv", []byte("# GRUB Environment Block\ninvalidline\n"), constants.FilePerm)).To(Succeed())
			_, err := utils.ReadPersistentVariables("/grubenv", config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid format"))
		})
		It("fails when the file cannot be read", func() {
			_, err := utils.ReadPersistentVariables("/no-such-grubenv", config)
			Expect(err).To(HaveOccurred())
		})
		It("skips empty lines and comments", func() {
			Expect(fs.WriteFile("/grubenv", []byte("# header\nkey=value\n\nkey2=a=b\n"), constants.FilePerm)).To(Succeed())
			vars, err := utils.ReadPersistentVariables("/grubenv", config)
			Expect(err).ToNot(HaveOccurred())
			Expect(vars).To(HaveKeyWithValue("key", "value"))
			// values containing '=' are joined back together
			Expect(vars).To(HaveKeyWithValue("key2", "a=b"))
		})
	})

	Describe("Chroot extra failures", Label("chroot"), func() {
		It("fails to run the callback when the root cannot be opened", func() {
			badFs := vfs.NewPathFS(vfs.OSFS, "/nonexistent-kairos-chroot-dir")
			badConfig := agentConfig.NewConfig(
				agentConfig.WithFs(badFs),
				agentConfig.WithRunner(runner),
				agentConfig.WithLogger(logger),
				agentConfig.WithMounter(mounter),
				agentConfig.WithSyscall(syscallMock),
			)
			chroot := utils.NewChroot("/whatever", badConfig)
			err := chroot.RunCallback(func() error { return nil })
			Expect(err).To(HaveOccurred())
		})
		It("fails to prepare when a default mountpoint cannot be created", func() {
			// Block the chroot base path with a file
			Expect(fs.WriteFile("/whatever", []byte(""), constants.FilePerm)).To(Succeed())
			chroot := utils.NewChroot("/whatever", config)
			Expect(chroot.Prepare()).ToNot(Succeed())
		})
		It("fails to prepare when an extra mountpoint cannot be created", func() {
			Expect(fsutils.MkdirAll(fs, "/chrootdir", constants.DirPerm)).To(Succeed())
			// Block the extra mountpoint path with a file
			Expect(fs.WriteFile("/chrootdir/blocked", []byte(""), constants.FilePerm)).To(Succeed())
			chroot := utils.NewChroot("/chrootdir", config)
			chroot.SetExtraMounts(map[string]string{"/real/path": "/blocked/path"})
			Expect(chroot.Prepare()).ToNot(Succeed())
		})
	})

	Describe("Grub extra", Label("grub"), func() {
		var target, rootDir, bootDir string

		BeforeEach(func() {
			target = "/dev/test"
			rootDir = constants.ActiveDir
			bootDir = constants.StateDir

			Expect(fsutils.MkdirAll(fs, filepath.Join(bootDir, "grub2"), constants.DirPerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Dir(filepath.Join(rootDir, constants.GrubConf)), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, constants.GrubConf), []byte("console=tty1"), 0644)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "sbin"), constants.DirPerm)).To(Succeed())
			f, err := fs.Create(filepath.Join(rootDir, "sbin", "grub2-install"))
			Expect(err).ToNot(HaveOccurred())
			Expect(f.Chmod(0755)).To(Succeed())
			Expect(f.Close()).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "usr", "lib", "grub", "i386-pc"), constants.DirPerm)).To(Succeed())
			_, err = fs.Create(filepath.Join(rootDir, "usr", "lib", "grub", "i386-pc", "modinfo.sh"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("fails when the grub install command fails", func() {
			runner.ReturnError = errors.New("grub install failed")
			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("grub install failed"))
		})
		It("uses the grub dir when it exists in the boot dir", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(bootDir, "grub"), constants.DirPerm)).To(Succeed())
			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
			Expect(err).ToNot(HaveOccurred())
			// grub.cfg should land under grub instead of grub2
			_, err = fs.Stat(fmt.Sprintf("%s/grub/grub.cfg", bootDir))
			Expect(err).ToNot(HaveOccurred())
		})
		It("fails when the grub state dir cannot be created", func() {
			// Block the arch-efi dir with a file
			Expect(fs.WriteFile(filepath.Join(bootDir, "grub2", fmt.Sprintf("%s-efi", config.Arch)), []byte(""), constants.FilePerm)).To(Succeed())
			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error creating grub dir"))
		})
		It("fails when the grub.cfg target cannot be created", func() {
			// Block grub.cfg with a directory so Create fails
			Expect(fsutils.MkdirAll(fs, filepath.Join(bootDir, "grub2", "grub.cfg"), constants.DirPerm)).To(Succeed())
			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
			Expect(err).To(HaveOccurred())
		})
		It("attempts to copy the grub fonts in efi mode", func() {
			// Full efi setup with shim, grub and modules
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/grub.efi"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			// One font present, the others missing so the warn path runs too.
			// NOTE: Install passes the grub.cfg *file* path as the bootDir
			// argument of copyGrubFonts, so the font write target is always
			// under a file path and the copy never succeeds; fonts are only
			// logged as missing. This exercises the lookup and write-error
			// paths and documents the current (broken) behavior.
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/unicode.pf2"), []byte("font"), constants.FilePerm)).To(Succeed())
			// A font that matches by name but cannot be read (it is a dir),
			// exercising the read-error path of the font lookup
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/euro.pf2"), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).ToNot(HaveOccurred())

			// The font does not end up in the boot dir due to the wrong dir
			// being passed in the production code
			_, err = fs.Stat(filepath.Join(bootDir, "grub2", fmt.Sprintf("%s-efi", config.Arch), "fonts", "unicode.pf2"))
			Expect(err).To(HaveOccurred())
		})
		It("fails in efi mode when a module cannot be read", func() {
			// A module path which matches the name but is a directory
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/loopback.mod"), constants.DirPerm)).To(Succeed())
			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("did not find grub modules"))
		})
		It("fails in bios mode when the root dir does not exist", func() {
			grub := utils.NewGrub(config)
			err := grub.Install(target, "/nonexistent-root", bootDir, constants.GrubConf, "", false, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to find grub dir"))
		})
		It("skips unreadable shim files in efi mode", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			// A shim path which stats fine but cannot be read as a file
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not find any shim file to copy"))
		})
		It("fails in efi mode when the shim target cannot be written", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte("shim"), constants.FilePerm)).To(Succeed())
			// Block the shim write target with a directory
			Expect(fsutils.MkdirAll(fs, filepath.Join(constants.EfiDir, "EFI/boot", constants.SignedShim), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error writing"))
		})
		It("fails in efi mode when the shim fallback target cannot be written", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte("shim"), constants.FilePerm)).To(Succeed())
			// Block the fallback efi write target with a directory
			Expect(fsutils.MkdirAll(fs, filepath.Join(constants.EfiDir, "EFI/boot", constants.GetFallBackEfi(config.Arch)), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not write shim file"))
		})
		It("skips unreadable grub efi files in efi mode", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte("shim"), constants.FilePerm)).To(Succeed())
			// A grub efi path which stats fine but cannot be read as a file
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/grub.efi"), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not find any grub efi file to copy"))
		})
		It("fails in efi mode when the grub efi target cannot be written", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte("shim"), constants.FilePerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/grub.efi"), []byte("grub"), constants.FilePerm)).To(Succeed())
			// Block the grub efi write target with a directory
			Expect(fsutils.MkdirAll(fs, filepath.Join(constants.EfiDir, "EFI/boot/grub.efi"), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error writing"))
		})
		It("fails in efi mode when the efi grub.cfg cannot be written", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte("shim"), constants.FilePerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/grub.efi"), []byte("grub"), constants.FilePerm)).To(Succeed())
			// Block the EFI grub.cfg write target with a directory
			Expect(fsutils.MkdirAll(fs, filepath.Join(constants.EfiDir, "EFI/boot/grub.cfg"), constants.DirPerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "COS_STATE")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error writing"))
		})
		It("fails in efi mode when the EFI dir cannot be created", func() {
			Expect(fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)).To(Succeed())
			// Block the EFI dir with a file
			Expect(fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDir), constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile(constants.EfiDir, []byte(""), constants.FilePerm)).To(Succeed())

			grub := utils.NewGrub(config)
			err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
			Expect(err).To(HaveOccurred())
		})
	})
})
