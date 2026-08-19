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

package utils_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/runner"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"github.com/kairos-io/kairos-agent/v2/tests/matchers"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

func getNamesFromListFiles(list []fs.DirEntry) []string {
	var names []string
	for _, f := range list {
		names = append(names, f.Name())
	}
	return names
}

var _ = Describe("Utils", Label("utils"), func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var realRunner *v1.RealRunner
	var fs vfs.FS
	var cleanup func()

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		logger = sdkLogger.NewNullLogger()
		realRunner = &v1.RealRunner{Logger: &logger}
		// Ensure /tmp exists in the VFS
		fs, cleanup, _ = vfst.NewTestFS(nil)
		fs.Mkdir("/tmp", constants.DirPerm)
		fs.Mkdir("/run", constants.DirPerm)
		fs.Mkdir("/etc", constants.DirPerm)

		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
		)
	})
	AfterEach(func() { cleanup() })

	Describe("Chroot", Label("chroot"), func() {
		var chroot *utils.Chroot
		BeforeEach(func() {
			chroot = utils.NewChroot(
				"/whatever",
				config,
			)
		})
		Describe("ChrootedCallback method", func() {
			It("runs a callback in a chroot", func() {
				err := utils.ChrootedCallback(config, "/somepath", map[string]string{}, func() error {
					return nil
				})
				Expect(err).ShouldNot(HaveOccurred())
				err = utils.ChrootedCallback(config, "/somepath", map[string]string{}, func() error {
					return fmt.Errorf("callback error")
				})
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("callback error"))
			})
		})
		Describe("on success", func() {
			It("command should be called in the chroot", func() {
				_, err := chroot.Run("chroot-command")
				Expect(err).To(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
			})
			It("commands should be called with a customized chroot", func() {
				chroot.SetExtraMounts(map[string]string{"/real/path": "/in/chroot/path"})
				Expect(chroot.Prepare()).To(BeNil())
				defer chroot.Close()
				_, err := chroot.Run("chroot-command")
				Expect(err).To(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
				_, err = chroot.Run("chroot-another-command")
				Expect(err).To(BeNil())
			})
			It("runs a callback in a custom chroot", func() {
				called := false
				callback := func() error {
					called = true
					return nil
				}
				err := chroot.RunCallback(callback)
				Expect(err).To(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
				Expect(called).To(BeTrue())
			})
		})
		Describe("on failure", func() {
			It("should return error if chroot-command fails", func() {
				runner.ReturnError = errors.New("run error")
				_, err := chroot.Run("chroot-command")
				Expect(err).NotTo(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
			})
			It("should return error if callback fails", func() {
				called := false
				callback := func() error {
					called = true
					return errors.New("Callback error")
				}
				err := chroot.RunCallback(callback)
				Expect(err).NotTo(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
				Expect(called).To(BeTrue())
			})
			It("should return error if preparing twice before closing", func() {
				Expect(chroot.Prepare()).To(BeNil())
				defer chroot.Close()
				Expect(chroot.Prepare()).NotTo(BeNil())
				Expect(chroot.Close()).To(BeNil())
				Expect(chroot.Prepare()).To(BeNil())
			})
			It("should return error if failed to chroot", func() {
				syscall.ErrorOnChroot = true
				_, err := chroot.Run("chroot-command")
				Expect(err).ToNot(BeNil())
				Expect(syscall.WasChrootCalledWith("/whatever")).To(BeTrue())
				Expect(err.Error()).To(ContainSubstring("chroot error"))
			})
			It("should return error if failed to mount on prepare", Label("mount"), func() {
				mounter.ErrorOnMount = true
				_, err := chroot.Run("chroot-command")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("mount error"))
			})
			It("should return error if failed to unmount on close", Label("unmount"), func() {
				mounter.ErrorOnUnmount = true
				_, err := chroot.Run("chroot-command")
				Expect(err).ToNot(BeNil())
				Expect(err.Error()).To(ContainSubstring("failed closing chroot"))
			})
		})
	})
	Describe("GetDeviceByLabel", Label("lsblk", "partitions"), func() {
		var cmds [][]string
		BeforeEach(func() {
			cmds = [][]string{
				{"udevadm", "trigger"},
				{"udevadm", "settle"},
			}
		})
		It("returns found device", func() {
			ghwTest := ghwMock.GhwMock{}
			disk := sdkPartitions.Disk{Name: "device", Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "FAKE",
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()
			defer ghwTest.Clean()
			out, err := utils.GetDeviceByLabel(config, "FAKE", 1)
			Expect(err).To(BeNil())
			Expect(out).To(Equal("/dev/device1"))
			Expect(runner.CmdsMatch(cmds)).To(BeNil())
		})
		It("fails if no device is found in two attempts", func() {
			_, err := utils.GetDeviceByLabel(config, "FAKE", 2)
			Expect(err).NotTo(BeNil())
			Expect(runner.CmdsMatch(append(cmds, cmds...))).To(BeNil())
		})
	})
	Describe("GetAllPartitions", Label("lsblk", "partitions"), func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
			ghwTest = ghwMock.GhwMock{}
			disk1 := sdkPartitions.Disk{
				Name: "sda",
				Partitions: []*sdkPartitions.Partition{
					{
						Name: "sda1Test",
					},
					{
						Name: "sda2Test",
					},
				},
			}
			disk2 := sdkPartitions.Disk{
				Name: "sdb",
				Partitions: []*sdkPartitions.Partition{
					{
						Name: "sdb1Test",
					},
				},
			}
			ghwTest.AddDisk(disk1)
			ghwTest.AddDisk(disk2)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("returns all found partitions", func() {
			parts, err := partitions.GetAllPartitions(&logger)
			Expect(err).To(BeNil())
			var partNames []string
			for _, p := range parts {
				partNames = append(partNames, p.Name)
			}
			Expect(partNames).To(ContainElement("sda1Test"))
			Expect(partNames).To(ContainElement("sda1Test"))
			Expect(partNames).To(ContainElement("sdb1Test"))
		})
	})
	Describe("CosignVerify", Label("cosign"), func() {
		It("runs a keyless verification", func() {
			_, err := utils.CosignVerify(fs, runner, "some/image:latest", "")
			Expect(err).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{"cosign", "some/image:latest"}})).To(BeNil())
		})
		It("runs a verification using a public key", func() {
			_, err := utils.CosignVerify(fs, runner, "some/image:latest", "https://mykey.pub")
			Expect(err).To(BeNil())
			Expect(runner.CmdsMatch(
				[][]string{{"cosign", "-key", "https://mykey.pub", "some/image:latest"}},
			)).To(BeNil())
		})
		It("Fails to to create temporary directories", func() {
			_, err := utils.CosignVerify(vfs.NewReadOnlyFS(fs), runner, "some/image:latest", "")
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("Reboot and shutdown", Label("reboot", "shutdown"), func() {
		It("reboots", func() {
			start := time.Now()
			utils.Reboot(runner, 2)
			duration := time.Since(start)
			Expect(runner.CmdsMatch([][]string{{"reboot", "-f"}})).To(BeNil())
			Expect(duration.Seconds() >= 2).To(BeTrue())
		})
		It("shuts down", func() {
			start := time.Now()
			utils.Shutdown(runner, 3)
			duration := time.Since(start)
			Expect(runner.CmdsMatch([][]string{{"poweroff", "-f"}})).To(BeNil())
			Expect(duration.Seconds() >= 3).To(BeTrue())
		})
	})
	Describe("CopyFile", Label("CopyFile"), func() {
		It("Copies source file to target file", func() {
			err := fsutils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create("/some/file")
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Stat("/some/otherfile")
			Expect(err).Should(HaveOccurred())
			Expect(utils.CopyFile(fs, "/some/file", "/some/otherfile")).ShouldNot(HaveOccurred())
			e, err := fsutils.Exists(fs, "/some/otherfile")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeTrue())
		})
		It("Copies source file to target folder", func() {
			err := fsutils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = fsutils.MkdirAll(fs, "/someotherfolder", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create("/some/file")
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Stat("/someotherfolder/file")
			Expect(err).Should(HaveOccurred())
			Expect(utils.CopyFile(fs, "/some/file", "/someotherfolder")).ShouldNot(HaveOccurred())
			e, err := fsutils.Exists(fs, "/someotherfolder/file")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(e).To(BeTrue())
		})
		It("Fails to open non existing file", func() {
			Expect(utils.CopyFile(fs, "file:/file", "/some/otherfile")).NotTo(BeNil())
			_, err := fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
		})
		It("Fails to copy on non writable target", func() {
			err := fsutils.MkdirAll(fs, "/some", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			fs.Create("/some/file")
			_, err = fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
			fs = vfs.NewReadOnlyFS(fs)
			Expect(utils.CopyFile(fs, "/some/file", "/some/otherfile")).NotTo(BeNil())
			_, err = fs.Stat("/some/otherfile")
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("CreateDirStructure", Label("CreateDirStructure"), func() {
		It("Creates essential directories", func() {
			dirList := []string{"sys", "proc", "dev", "tmp", "boot", "usr/local", "oem"}
			for _, dir := range dirList {
				_, err := fs.Stat(fmt.Sprintf("/my/root/%s", dir))
				Expect(err).NotTo(BeNil())
			}
			Expect(utils.CreateDirStructure(fs, "/my/root")).To(BeNil())
			for _, dir := range dirList {
				fi, err := fs.Stat(fmt.Sprintf("/my/root/%s", dir))
				Expect(err).To(BeNil())
				if fi.Name() == "tmp" {
					Expect(fmt.Sprintf("%04o", fi.Mode().Perm())).To(Equal("0777"))
					Expect(fi.Mode() & os.ModeSticky).NotTo(Equal(0))
				}
				if fi.Name() == "sys" {
					Expect(fmt.Sprintf("%04o", fi.Mode().Perm())).To(Equal("0555"))
				}
			}
		})
		It("Fails on non writable target", func() {
			fs = vfs.NewReadOnlyFS(fs)
			Expect(utils.CreateDirStructure(fs, "/my/root")).NotTo(BeNil())
		})
	})
	Describe("SyncData", Label("SyncData"), func() {
		It("Copies all files from source to target", func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elementalsource")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elementaltarget")
			Expect(err).ShouldNot(HaveOccurred())

			for i := 0; i < 5; i++ {
				_, _ = fsutils.TempFile(fs, sourceDir, "file*")
			}

			Expect(utils.SyncData(logger, realRunner, fs, sourceDir, destDir)).To(BeNil())

			filesDest, err := fs.ReadDir(destDir)
			Expect(err).To(BeNil())

			destNames := getNamesFromListFiles(filesDest)
			filesSource, err := fs.ReadDir(sourceDir)
			Expect(err).To(BeNil())

			SourceNames := getNamesFromListFiles(filesSource)

			// Should be the same files in both dirs now
			Expect(destNames).To(Equal(SourceNames))
		})

		It("Copies all files from source to target respecting excludes", func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elementalsource")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elementaltarget")
			Expect(err).ShouldNot(HaveOccurred())

			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "host"), constants.DirPerm)
			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "run"), constants.DirPerm)

			// /tmp/run would be excluded as well, as we define an exclude without the "/" prefix
			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "tmp", "run"), constants.DirPerm)

			for i := 0; i < 5; i++ {
				_, _ = fsutils.TempFile(fs, sourceDir, "file*")
			}

			Expect(utils.SyncData(logger, realRunner, fs, sourceDir, destDir, "host", "run")).To(BeNil())

			filesDest, err := fs.ReadDir(destDir)
			Expect(err).To(BeNil())

			destNames := getNamesFromListFiles(filesDest)

			filesSource, err := fs.ReadDir(sourceDir)
			Expect(err).To(BeNil())

			SourceNames := getNamesFromListFiles(filesSource)

			// Shouldn't be the same
			Expect(destNames).ToNot(Equal(SourceNames))
			expected := []string{}

			for _, s := range SourceNames {
				if s != "host" && s != "run" {
					expected = append(expected, s)
				}
			}
			Expect(destNames).To(Equal(expected))

			// /tmp/run is not copied over
			Expect(fsutils.Exists(fs, filepath.Join(destDir, "tmp", "run"))).To(BeFalse())
		})

		It("Copies all files from source to target respecting excludes with '/' prefix", func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elementalsource")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elementaltarget")
			Expect(err).ShouldNot(HaveOccurred())

			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "host"), constants.DirPerm)
			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "run"), constants.DirPerm)
			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "var", "run"), constants.DirPerm)
			fsutils.MkdirAll(fs, filepath.Join(sourceDir, "tmp", "host"), constants.DirPerm)

			Expect(utils.SyncData(logger, realRunner, fs, sourceDir, destDir, "/host", "/run")).To(BeNil())

			filesDest, err := fs.ReadDir(destDir)
			Expect(err).To(BeNil())

			destNames := getNamesFromListFiles(filesDest)

			filesSource, err := fs.ReadDir(sourceDir)
			Expect(err).To(BeNil())

			SourceNames := getNamesFromListFiles(filesSource)

			// Shouldn't be the same
			Expect(destNames).ToNot(Equal(SourceNames))

			Expect(fsutils.Exists(fs, filepath.Join(destDir, "var", "run"))).To(BeTrue())
			Expect(fsutils.Exists(fs, filepath.Join(destDir, "tmp", "host"))).To(BeTrue())
			Expect(fsutils.Exists(fs, filepath.Join(destDir, "host"))).To(BeFalse())
			Expect(fsutils.Exists(fs, filepath.Join(destDir, "run"))).To(BeFalse())
		})

		It("should not fail if dirs are empty", func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elementalsource")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elementaltarget")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(utils.SyncData(logger, realRunner, fs, sourceDir, destDir)).To(BeNil())
		})
		It("should NOT fail if destination does not exist", func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elemental")
			err = fs.WriteFile(filepath.Join(sourceDir, "testfile"), []byte("sdjfnsdjkfjkdsanfkjsnda"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = utils.SyncData(logger, realRunner, fs, sourceDir, "/welp")
			Expect(err).To(BeNil())
		})
		It("should fail if source does not exist", func() {
			Expect(utils.SyncData(logger, realRunner, fs, "/welp", "/walp")).NotTo(BeNil())
		})
	})
	Describe("IsLocalURI", Label("uri"), func() {
		It("Detects a local url", func() {
			local, err := utils.IsLocalURI("file://some/path")
			Expect(err).To(BeNil())
			Expect(local).To(BeTrue())
		})
		It("Detects a local path", func() {
			local, err := utils.IsLocalURI("/some/path")
			Expect(err).To(BeNil())
			Expect(local).To(BeTrue())
		})
		It("Detects a remote uri", func() {
			local, err := utils.IsLocalURI("http://something.org")
			Expect(err).To(BeNil())
			Expect(local).To(BeFalse())
		})
		It("Detects a remote uri", func() {
			local, err := utils.IsLocalURI("some.domain.org:33/some/path")
			Expect(err).To(BeNil())
			Expect(local).To(BeFalse())
			local, err = utils.IsLocalURI("some.domain.org/some/path:latest")
			Expect(err).To(BeNil())
			Expect(local).To(BeFalse())
		})
		It("Fails on invalid URL", func() {
			local, err := utils.IsLocalURI("$htt:|//insane.stuff")
			Expect(err).NotTo(BeNil())
			Expect(local).To(BeFalse())
		})
	})
	Describe("IsHTTPURI", Label("uri"), func() {
		It("Detects a http url", func() {
			local, err := utils.IsHTTPURI("http://domain.org/path")
			Expect(err).To(BeNil())
			Expect(local).To(BeTrue())
		})
		It("Detects a https url", func() {
			local, err := utils.IsHTTPURI("https://domain.org/path")
			Expect(err).To(BeNil())
			Expect(local).To(BeTrue())
		})
		It("Detects it is a non http URL", func() {
			local, err := utils.IsHTTPURI("file://path")
			Expect(err).To(BeNil())
			Expect(local).To(BeFalse())
			local, err = utils.IsHTTPURI("container.reg.org:1024/some/repository")
			Expect(err).To(BeNil())
			Expect(local).To(BeFalse())
		})
		It("Fails on invalid URL", func() {
			local, err := utils.IsLocalURI("$htt:|//insane.stuff")
			Expect(err).NotTo(BeNil())
			Expect(local).To(BeFalse())
		})
	})
	Describe("GetSource", Label("GetSource"), func() {
		It("Fails on invalid url", func() {
			Expect(utils.GetSource(config, "$htt:|//insane.stuff", "/tmp/dest")).NotTo(BeNil())
		})
		It("Fails on readonly destination", func() {
			config.Fs = vfs.NewReadOnlyFS(fs)
			Expect(utils.GetSource(config, "http://something.org", "/tmp/dest")).NotTo(BeNil())
		})
		It("Fails on non existing local source", func() {
			Expect(utils.GetSource(config, "/some/missing/file", "/tmp/dest")).NotTo(BeNil())
		})
		It("Fails on http client error", func() {
			client.Error = true
			url := "https://missing.io"
			Expect(utils.GetSource(config, url, "/tmp/dest")).NotTo(BeNil())
			client.WasGetCalledWith(url)
		})
		It("Copies local file to destination", func() {
			fs.Create("/tmp/file")
			Expect(utils.GetSource(config, "file:///tmp/file", "/tmp/dest")).To(BeNil())
			_, err := fs.Stat("/tmp/dest")
			Expect(err).To(BeNil())
		})
	})
	Describe("ValidContainerReference", Label("reference"), func() {
		It("Returns true on valid references", func() {
			Expect(utils.ValidContainerReference("opensuse/leap:15.3")).To(BeTrue())
			Expect(utils.ValidContainerReference("opensuse")).To(BeTrue())
			Expect(utils.ValidContainerReference("registry.suse.com/opensuse/something")).To(BeTrue())
			Expect(utils.ValidContainerReference("registry.suse.com:8080/something:253")).To(BeTrue())
		})
		It("Returns false on invalid references", func() {
			Expect(utils.ValidContainerReference("opensuse/leap:15+3")).To(BeFalse())
			Expect(utils.ValidContainerReference("opensusE")).To(BeFalse())
			Expect(utils.ValidContainerReference("registry.suse.com:8080/Something:253")).To(BeFalse())
			Expect(utils.ValidContainerReference("http://registry.suse.com:8080/something:253")).To(BeFalse())
		})
	})
	Describe("ValidTaggedContainerReference", Label("reference"), func() {
		It("Returns true on valid references including explicit tag", func() {
			Expect(utils.ValidTaggedContainerReference("opensuse/leap:15.3")).To(BeTrue())
			Expect(utils.ValidTaggedContainerReference("registry.suse.com/opensuse/something:latest")).To(BeTrue())
			Expect(utils.ValidTaggedContainerReference("registry.suse.com:8080/something:253")).To(BeTrue())
		})
		It("Returns false on valid references without explicit tag", func() {
			Expect(utils.ValidTaggedContainerReference("opensuse")).To(BeFalse())
			Expect(utils.ValidTaggedContainerReference("registry.suse.com/opensuse/something")).To(BeFalse())
			Expect(utils.ValidTaggedContainerReference("registry.suse.com:8080/something")).To(BeFalse())
		})
	})
	Describe("DirSize", Label("fs"), func() {
		BeforeEach(func() {
			err := fsutils.MkdirAll(fs, "/folder/subfolder", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			f, err := fs.Create("/folder/file")
			Expect(err).ShouldNot(HaveOccurred())
			err = f.Truncate(1024)
			Expect(err).ShouldNot(HaveOccurred())
			f, err = fs.Create("/folder/subfolder/file")
			Expect(err).ShouldNot(HaveOccurred())
			err = f.Truncate(2048)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Returns the expected size of a test folder", func() {
			size, err := fsutils.DirSize(fs, "/folder")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(size).To(Equal(int64(3072)))
		})
	})
	Describe("FindFileWithPrefix", Label("find"), func() {
		BeforeEach(func() {
			err := fsutils.MkdirAll(fs, "/path/inner", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())

			_, err = fs.Create("/path/onefile")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = fs.Create("/path/somefile")
			Expect(err).ShouldNot(HaveOccurred())

			err = fs.Symlink("onefile", "/path/linkedfile")
			Expect(err).ShouldNot(HaveOccurred())

			err = fs.Symlink("/path/onefile", "/path/abslinkedfile")
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("finds a matching file", func() {
			f, err := utils.FindFileWithPrefix(fs, "/path", "prefix", "some")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(f).To(Equal("/path/somefile"))
		})
		It("finds a matching file, but returns the target of a relative symlink", func() {
			// apparently fs.Readlink returns the raw path so we need to
			// use raw paths here. This is an arguable behavior
			rawPath, err := fs.RawPath("/path")
			Expect(err).ShouldNot(HaveOccurred())

			f, err := utils.FindFileWithPrefix(vfs.OSFS, rawPath, "linked")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(err).ShouldNot(HaveOccurred())
			Expect(f).To(Equal(filepath.Join(rawPath, "onefile")))
		})
		It("finds a matching file, but returns the target of an absolute symlink", func() {
			// apparently fs.Readlink returns the raw path so we need to
			// use raw paths here. This is an arguable behavior
			rawPath, err := fs.RawPath("/path")
			Expect(err).ShouldNot(HaveOccurred())

			f, err := utils.FindFileWithPrefix(vfs.OSFS, rawPath, "abslinked")
			Expect(err).ShouldNot(HaveOccurred())

			Expect(err).ShouldNot(HaveOccurred())
			Expect(f).To(Equal(filepath.Join(rawPath, "onefile")))
		})
		It("fails to read given path", func() {
			_, err := utils.FindFileWithPrefix(fs, "nonexisting", "some")
			Expect(err).Should(HaveOccurred())
		})
		It("doesn't find any matching file in path", func() {
			fsutils.MkdirAll(fs, "/path", constants.DirPerm)
			_, err := utils.FindFileWithPrefix(fs, "/path", "prefix", "anotherprefix")
			Expect(err).Should(HaveOccurred())
		})
	})
	Describe("CalcFileChecksum", Label("checksum"), func() {
		It("compute correct sha256 checksum", func() {
			testData := strings.Repeat("abcdefghilmnopqrstuvz\n", 20)
			testDataSHA256 := "7f182529f6362ae9cfa952ab87342a7180db45d2c57b52b50a68b6130b15a422"

			err := fs.Mkdir("/iso", constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = fs.WriteFile("/iso/test.iso", []byte(testData), 0644)
			Expect(err).ShouldNot(HaveOccurred())

			checksum, err := utils.CalcFileChecksum(fs, "/iso/test.iso")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(checksum).To(Equal(testDataSHA256))
		})
	})
	Describe("Grub", Label("grub"), func() {
		Describe("Install", func() {
			var target, rootDir, bootDir string
			var buf *bytes.Buffer
			BeforeEach(func() {
				target = "/dev/test"
				rootDir = constants.ActiveDir
				bootDir = constants.StateDir
				buf = &bytes.Buffer{}
				logger = sdkLogger.NewBufferLogger(buf)
				logger.SetLevel("debug")
				config.Logger = logger

				err := fsutils.MkdirAll(fs, filepath.Join(bootDir, "grub2"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(fs, filepath.Dir(filepath.Join(rootDir, constants.GrubConf)), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fs.WriteFile(filepath.Join(rootDir, constants.GrubConf), []byte("console=tty1"), 0644)
				Expect(err).ShouldNot(HaveOccurred())
				// Create fake grub dir in rootfs and fake grub binaries
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "sbin"), constants.DirPerm)
				Expect(err).To(BeNil())
				f, err := fs.Create(filepath.Join(rootDir, "sbin", "grub2-install"))
				Expect(err).To(BeNil())
				Expect(f.Chmod(0755)).ToNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "usr", "lib", "grub", "i386-pc"), constants.DirPerm)
				Expect(err).To(BeNil())
				_, err = fs.Create(filepath.Join(rootDir, "usr", "lib", "grub", "i386-pc", "modinfo.sh"))
				Expect(err).To(BeNil())

			})
			It("installs with default values", func() {
				grub := utils.NewGrub(config)
				err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
				Expect(err).To(BeNil())

				Expect(buf).To(ContainSubstring("Installing GRUB.."))
				Expect(buf).To(ContainSubstring("Grub install to device /dev/test complete"))
				Expect(buf).ToNot(ContainSubstring("efi"))
				Expect(buf.String()).ToNot(ContainSubstring("Adding extra tty (serial) to grub.cfg"))
				targetGrub, err := fs.ReadFile(fmt.Sprintf("%s/grub2/grub.cfg", bootDir))
				Expect(err).To(BeNil())
				// Should not be modified at all
				Expect(targetGrub).To(ContainSubstring("console=tty1"))

			})
			It("installs with efi firmware", Label("efi"), func() {
				err := fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/grub.efi"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/etc/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/etc/os-release"), []byte("ID=\"suse\""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				grub := utils.NewGrub(config)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
				Expect(err).ShouldNot(HaveOccurred())

				// Check everything was copied
				_, err = fs.ReadFile(fmt.Sprintf("%s/grub2/grub.cfg", bootDir))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/", constants.SignedShim))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/grub.efi"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/bootx64.efi"))
				Expect(err).To(BeNil())

			})
			It("installs with efi firmware on riscv64 without shim", Label("efi"), func() {
				riscvConfig := agentConfig.NewConfig(
					agentConfig.WithFs(fs),
					agentConfig.WithRunner(runner),
					agentConfig.WithLogger(logger),
					agentConfig.WithMounter(mounter),
					agentConfig.WithSyscall(syscall),
					agentConfig.WithClient(client),
					agentConfig.WithPlatform("linux/riscv64"),
				)
				// WithPlatform only updates Platform; Arch defaults to the host (x86_64 on CI).
				riscvConfig.Arch = constants.ArchRiscv64
				err := fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/lib/grub/riscv64-efi/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/lib/grub/riscv64-efi/grubriscv64.efi"), []byte("grub"), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/lib/grub/riscv64-efi/loopback.mod"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/etc/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/etc/os-release"), []byte("ID=\"fedora\""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				grub := utils.NewGrub(riscvConfig)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "COS_STATE")
				Expect(err).ShouldNot(HaveOccurred())

				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/grubriscv64.efi"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/BOOTRISCV64.EFI"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/", constants.SignedShim))
				Expect(err).To(HaveOccurred())
			})
			It("installs with efi firmware on openSUSE riscv64 using /usr/share/efi path", Label("efi"), func() {
				riscvConfig := agentConfig.NewConfig(
					agentConfig.WithFs(fs),
					agentConfig.WithRunner(runner),
					agentConfig.WithLogger(logger),
					agentConfig.WithMounter(mounter),
					agentConfig.WithSyscall(syscall),
					agentConfig.WithClient(client),
					agentConfig.WithPlatform("linux/riscv64"),
				)
				riscvConfig.Arch = constants.ArchRiscv64
				err := fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/riscv64/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/riscv64/grub.efi"), []byte("grub"), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/lib/grub/riscv64-efi/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/lib/grub/riscv64-efi/loopback.mod"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/etc/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/etc/os-release"), []byte("ID=\"suse\""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				grub := utils.NewGrub(riscvConfig)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "COS_STATE")
				Expect(err).ShouldNot(HaveOccurred())

				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/grub.efi"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/BOOTRISCV64.EFI"))
				Expect(err).To(BeNil())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/grubriscv64.efi"))
				Expect(err).To(HaveOccurred())
				_, err = fs.Stat(filepath.Join(constants.EfiDir, "EFI/boot/", constants.SignedShim))
				Expect(err).To(HaveOccurred())
			})
			It("fails with bios if no grub2-install file exists", func() {
				Expect(fs.RemoveAll(filepath.Join(rootDir, "sbin", "grub2-install"))).ToNot(HaveOccurred())
				grub := utils.NewGrub(config)
				err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
				Expect(err).To(HaveOccurred())
			})
			It("fails with bios if no modules files exists", func() {
				Expect(fs.RemoveAll(filepath.Join(rootDir, "usr", "lib", "grub", "i386-pc"))).ToNot(HaveOccurred())
				grub := utils.NewGrub(config)
				err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")
				Expect(err).To(HaveOccurred())
			})
			It("fails with efi if no modules files exist", Label("efi"), func() {
				grub := utils.NewGrub(config)
				err := grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("grub"))
				Expect(err.Error()).To(ContainSubstring("modules"))
			})
			It("fails with efi if no shim/grub files exist", Label("efi"), func() {
				err := fsutils.MkdirAll(fs, filepath.Join(rootDir, "/x86_64/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/x86_64/loopback.mod"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/etc/os-release"), []byte("ID=\"suse\""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				grub := utils.NewGrub(config)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("could not find any shim file to copy"))
				// Create fake shim
				err = fsutils.MkdirAll(fs, filepath.Join(rootDir, "/usr/share/efi/x86_64/"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/", constants.SignedShim), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				err = fs.WriteFile(filepath.Join(rootDir, "/usr/share/efi/x86_64/shim.efi"), []byte(""), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())
				grub = utils.NewGrub(config)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "", true, "")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("could not find any grub efi file to copy"))

			})
			It("installs with extra tty", func() {
				err := fs.Mkdir("/dev", constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				_, err = fs.Create("/dev/serial")
				Expect(err).ShouldNot(HaveOccurred())

				grub := utils.NewGrub(config)
				err = grub.Install(target, rootDir, bootDir, constants.GrubConf, "serial", false, "")
				Expect(err).To(BeNil())

				Expect(buf.String()).To(ContainSubstring("Adding extra tty (serial) to grub.cfg"))
				targetGrub, err := fs.ReadFile(fmt.Sprintf("%s/grub2/grub.cfg", bootDir))
				Expect(err).To(BeNil())
				Expect(targetGrub).To(ContainSubstring("console=tty1 console=serial"))
			})
			It("Fails if it can't read grub config file", func() {
				err := fs.RemoveAll(filepath.Join(rootDir, constants.GrubConf))
				Expect(err).ShouldNot(HaveOccurred())
				grub := utils.NewGrub(config)
				Expect(grub.Install(target, rootDir, bootDir, constants.GrubConf, "", false, "")).NotTo(BeNil())

				Expect(buf).To(ContainSubstring("Failed reading grub config file"))
			})
		})
		Describe("SetPersistentVariables", func() {
			It("Sets the grub environment file", func() {
				temp, err := os.CreateTemp("", "grub-*")
				Expect(err).ShouldNot(HaveOccurred())
				defer os.Remove(temp.Name())
				Expect(utils.SetPersistentVariables(
					temp.Name(), map[string]string{"key1": "value1", "key2": "value2"},
					config,
				)).To(BeNil())
				readVars, err := utils.ReadPersistentVariables(temp.Name(), config)
				Expect(err).To(BeNil())
				Expect(readVars["key1"]).To(Equal("value1"))
				Expect(readVars["key2"]).To(Equal("value2"))
			})
			It("Fails setting variables", func() {
				e := utils.SetPersistentVariables(
					"badfilenopath", map[string]string{"key1": "value1"},
					config,
				)
				Expect(e).NotTo(BeNil())
			})
			It("respects existing variables", func() {
				temp, err := os.CreateTemp("", "grub-*")
				Expect(err).ShouldNot(HaveOccurred())
				defer os.Remove(temp.Name())
				Expect(utils.SetPersistentVariables(
					temp.Name(), map[string]string{"key1": "value1", "key2": "value2"},
					config,
				)).To(BeNil())
				readVars, err := utils.ReadPersistentVariables(temp.Name(), config)
				Expect(err).To(BeNil())
				Expect(readVars["key1"]).To(Equal("value1"))
				Expect(readVars["key2"]).To(Equal("value2"))

				// Now we do it again with a different value
				Expect(utils.SetPersistentVariables(
					temp.Name(), map[string]string{"key1": "value3", "key3": "value4"},
					config,
				)).To(BeNil())
				// Now there should be an extra key and key1 should be updated
				readVars, err = utils.ReadPersistentVariables(temp.Name(), config)
				Expect(err).To(BeNil())
				Expect(readVars["key1"]).To(Equal("value3"))
				Expect(readVars["key2"]).To(Equal("value2"))
				Expect(readVars["key3"]).To(Equal("value4"))
			})
			It("Should work with multiple extra cmdline", func() {
				temp, err := os.CreateTemp("", "grub-*")
				Expect(err).ShouldNot(HaveOccurred())
				defer os.Remove(temp.Name())
				data := `# GRUB Environment Block
extra_cmdline=fips=1 selinux=0
########################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################`
				Expect(fs.WriteFile(temp.Name(), []byte(data), constants.FilePerm)).To(BeNil())
				Expect(utils.SetPersistentVariables(
					temp.Name(), map[string]string{"next_entry": "statereset"}, config,
				)).To(BeNil())
				readVars, err := utils.ReadPersistentVariables(temp.Name(), config)
				Expect(err).To(BeNil())
				Expect(readVars["next_entry"]).To(Equal("statereset"))
				Expect(readVars["extra_cmdline"]).To(Equal("fips=1 selinux=0"))
			})
		})
	})
	Describe("CreateSquashFS", Label("CreateSquashFS"), func() {
		It("runs with no options if none given", func() {
			err := utils.CreateSquashFS(runner, logger, "source", "dest", []string{})
			Expect(runner.IncludesCmds([][]string{
				{"mksquashfs", "source", "dest"},
			})).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
		It("runs with options if given", func() {
			err := utils.CreateSquashFS(runner, logger, "source", "dest", constants.GetDefaultSquashfsOptions())
			cmd := []string{"mksquashfs", "source", "dest"}
			cmd = append(cmd, constants.GetDefaultSquashfsOptions()...)
			Expect(runner.IncludesCmds([][]string{
				cmd,
			})).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
		It("returns an error if it fails", func() {
			runner.ReturnError = errors.New("error")
			err := utils.CreateSquashFS(runner, logger, "source", "dest", []string{})
			Expect(runner.IncludesCmds([][]string{
				{"mksquashfs", "source", "dest"},
			})).To(BeNil())
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("CommandExists", Label("CommandExists"), func() {
		It("returns false if command does not exists", func() {
			exists := utils.CommandExists("THISCOMMANDSHOULDNOTBETHERECOMEON")
			Expect(exists).To(BeFalse())
		})
		It("returns true if command exists", func() {
			exists := utils.CommandExists("true")
			Expect(exists).To(BeTrue())
		})
	})
	Describe("LoadEnvFile", Label("LoadEnvFile"), func() {
		BeforeEach(func() {
			fs.Mkdir("/etc", constants.DirPerm)
		})
		It("returns proper map if file exists", func() {
			err := fs.WriteFile("/etc/envfile", []byte("TESTKEY=TESTVALUE\nTESTKEY2=TESTVALUE2\n"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())
			envData, err := utils.LoadEnvFile(fs, "/etc/envfile")
			Expect(err).ToNot(HaveOccurred())
			Expect(envData).To(HaveKeyWithValue("TESTKEY", "TESTVALUE"))
			Expect(envData).To(HaveKeyWithValue("TESTKEY2", "TESTVALUE2"))
		})
		It("returns error if file doesnt exist", func() {
			_, err := utils.LoadEnvFile(fs, "/etc/envfile")
			Expect(err).To(HaveOccurred())
		})

		It("returns error if it cant unmarshall the env file", func() {
			// Cant parse weird chars, only [A-Za-z0-9_.]
			err := fs.WriteFile("/etc/envfile", []byte("ñ = ÇÇ"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = utils.LoadEnvFile(fs, "/etc/envfile")
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("IsMounted", Label("ismounted"), func() {
		It("checks a mounted partition", func() {
			part := &sdkPartitions.Partition{
				MountPoint: "/some/mountpoint",
			}
			err := mounter.Mount("/some/device", "/some/mountpoint", "auto", []string{})
			Expect(err).ShouldNot(HaveOccurred())
			mnt, err := utils.IsMounted(config, part)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(mnt).To(BeTrue())
		})
		It("checks a not mounted partition", func() {
			part := &sdkPartitions.Partition{
				MountPoint: "/some/mountpoint",
			}
			mnt, err := utils.IsMounted(config, part)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(mnt).To(BeFalse())
		})
		It("checks a partition without mountpoint", func() {
			part := &sdkPartitions.Partition{}
			mnt, err := utils.IsMounted(config, part)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(mnt).To(BeFalse())
		})
		It("checks a nil partitiont", func() {
			mnt, err := utils.IsMounted(config, nil)
			Expect(err).Should(HaveOccurred())
			Expect(mnt).To(BeFalse())
		})
	})
	Describe("CleanStack", Label("CleanStack"), func() {
		var cleaner *utils.CleanStack
		BeforeEach(func() {
			cleaner = utils.NewCleanStack()
		})
		It("Adds a callback to the stack and pops it", func() {
			var flag bool
			callback := func() error {
				flag = true
				return nil
			}
			Expect(cleaner.Pop()).To(BeNil())
			cleaner.Push(callback)
			poppedJob := cleaner.Pop()
			Expect(poppedJob).NotTo(BeNil())
			poppedJob()
			Expect(flag).To(BeTrue())
		})
		It("On Cleanup runs callback stack in reverse order", func() {
			result := ""
			callback1 := func() error {
				result = result + "one "
				return nil
			}
			callback2 := func() error {
				result = result + "two "
				return nil
			}
			callback3 := func() error {
				result = result + "three "
				return nil
			}
			cleaner.Push(callback1)
			cleaner.Push(callback2)
			cleaner.Push(callback3)
			cleaner.Cleanup(nil)
			Expect(result).To(Equal("three two one "))
		})
		It("On Cleanup keeps former error and all callbacks are executed", func() {
			err := errors.New("Former error")
			count := 0
			callback := func() error {
				count++
				if count == 2 {
					return errors.New("Cleanup Error")
				}
				return nil
			}
			cleaner.Push(callback)
			cleaner.Push(callback)
			cleaner.Push(callback)
			err = cleaner.Cleanup(err)
			Expect(count).To(Equal(3))
			Expect(err.Error()).To(ContainSubstring("Former error"))
		})
		It("On Cleanup error reports first error and all callbacks are executed", func() {
			var err error
			count := 0
			callback := func() error {
				count++
				if count >= 2 {
					return errors.New(fmt.Sprintf("Cleanup error %d", count))
				}
				return nil
			}
			cleaner.Push(callback)
			cleaner.Push(callback)
			cleaner.Push(callback)
			err = cleaner.Cleanup(err)
			Expect(count).To(Equal(3))
			Expect(err.Error()).To(ContainSubstring("Cleanup error 2"))
			Expect(err.Error()).To(ContainSubstring("Cleanup error 3"))
		})
	})
	Describe("GetEfiPartition", func() {
		var ghwTest ghwMock.GhwMock

		BeforeEach(func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						FS:              "ext4",
						MountPoint:      "/efi",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("returns the efi partition", func() {
			efi, err := partitions.GetEfiPartition(&logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(efi.FilesystemLabel).To(Equal("COS_GRUB"))
			Expect(efi.Name).To(Equal("device1")) // Just to make sure its our mocked system
		})
		It("fails to find the efi partition", func() {
			ghwTest.Clean() // Remove the disk
			efi, err := partitions.GetEfiPartition(&logger)
			Expect(err).To(HaveOccurred())
			Expect(efi).To(BeNil())
		})
	})
	Describe("SystemdBootConfWriter/SystemdBootConfReader", func() {
		BeforeEach(func() {
			err := fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})
		It("writes the conf file with proper attrs", func() {
			conf := map[string]string{
				"timeout": "5",
				"default": "kairos",
				"empty":   "",
			}
			err := utils.SystemdBootConfWriter(fs, "/efi/loader/entries/test1.conf", conf)
			Expect(err).ToNot(HaveOccurred())
			reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/entries/test1.conf")
			Expect(err).ToNot(HaveOccurred())
			Expect(reader["timeout"]).To(Equal("5"))
			Expect(reader["default"]).To(Equal("kairos"))
			Expect(reader["recovery"]).To(Equal(""))
		})
		It("reads the conf file with proper k,v attrs", func() {
			err := fs.WriteFile("/efi/loader/entries/test2.conf", []byte("timeout 5\ndefault kairos\nrecovery\n"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/entries/test2.conf")
			Expect(err).ToNot(HaveOccurred())
			Expect(reader["timeout"]).To(Equal("5"))
			Expect(reader["default"]).To(Equal("kairos"))
			Expect(reader["recovery"]).To(Equal(""))
		})

	})
	Describe("IsUkiWithFs", func() {
		It("returns true if rd.immucore.uki is present", func() {
			err := fs.Mkdir("/proc", os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			Expect(utils.IsUkiWithFs(fs)).To(BeTrue())
		})
		It("returns false if rd.immucore.uki is not present", func() {
			Expect(utils.IsUkiWithFs(fs)).To(BeFalse())
		})
	})
	Describe("AddBootAssessment", func() {
		BeforeEach(func() {
			Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModePerm)).ToNot(HaveOccurred())
		})
		It("adds the boot assessment to a file", func() {
			err := fs.WriteFile("/efi/loader/entries/test.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = utils.AddBootAssessment(fs, "/efi/loader/entries", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect("/efi/loader/entries/test.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			// Should match with the +3
			Expect("/efi/loader/entries/test+3.conf").To(matchers.BeAnExistingFileFs(fs))
		})
		It("adds the boot assessment to several files", func() {
			err := fs.WriteFile("/efi/loader/entries/test1.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test3.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = utils.AddBootAssessment(fs, "/efi/loader/entries", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect("/efi/loader/entries/test1.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test2.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test3.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			// Should match with the +3
			Expect("/efi/loader/entries/test1+3.conf").To(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test2+3.conf").To(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test3+3.conf").To(matchers.BeAnExistingFileFs(fs))
		})
		It("leaves assessment in place for existing files", func() {
			err := fs.WriteFile("/efi/loader/entries/test1.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test2+3.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test3+1-2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = utils.AddBootAssessment(fs, "/efi/loader/entries", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect("/efi/loader/entries/test1.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test3+3.conf").ToNot(matchers.BeAnExistingFileFs(fs))
			// Should match with the +3 and the existing ones left in place
			Expect("/efi/loader/entries/test1+3.conf").To(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test2+3.conf").To(matchers.BeAnExistingFileFs(fs))
			Expect("/efi/loader/entries/test3+1-2.conf").To(matchers.BeAnExistingFileFs(fs))
		})
		It("fails to write the boot assessment in non existing dir", func() {
			err := utils.AddBootAssessment(fs, "/fake", logger)
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("ReadAssessmentFromEntry", func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
			Expect(fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModePerm)).ToNot(HaveOccurred())
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						FS:              "ext4",
						MountPoint:      "/efi",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("reads the assessment from a file", func() {
			err := fs.WriteFile("/efi/loader/entries/test+2-1.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			entry, err := utils.ReadAssessmentFromEntry(fs, "test", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+2-1"))
		})
		It("reads passive when using fallback", func() {
			err := fs.WriteFile("/efi/loader/entries/passive+2-1.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			// fallback should point to passive
			entry, err := utils.ReadAssessmentFromEntry(fs, "fallback", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+2-1"))

			// Should find the passive entry as well directly
			entry, err = utils.ReadAssessmentFromEntry(fs, "passive", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+2-1"))
		})
		It("reads active when using cos", func() {
			err := fs.WriteFile("/efi/loader/entries/active+1-2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			// cos should point to active
			entry, err := utils.ReadAssessmentFromEntry(fs, "cos", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+1-2"))

			// Should find the active entry as well directly
			entry, err = utils.ReadAssessmentFromEntry(fs, "active", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+1-2"))
		})

		It("empty assessment if it doesnt match", func() {
			entry, err := utils.ReadAssessmentFromEntry(fs, "cos", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))

			// Should find the active entry as well directly
			entry, err = utils.ReadAssessmentFromEntry(fs, "active", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
		})

		It("fails with no EFI partition", func() {
			ghwTest.Clean()
			entry, err := utils.ReadAssessmentFromEntry(fs, "cos", logger)
			Expect(err).To(HaveOccurred())
			Expect(entry).To(Equal(""))
		})

		It("errors if more than one file matches", func() {
			err := fs.WriteFile("/efi/loader/entries/active+1-2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/active+3-2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			entry, err := utils.ReadAssessmentFromEntry(fs, "active", logger)
			Expect(err).To(HaveOccurred())
			Expect(entry).To(Equal(""))
			Expect(err.Error()).To(Equal(fmt.Sprintf(constants.MultipleEntriesAssessmentError, "active")))
		})

		It("errors if dir doesn't exist", func() {
			// Remove all dirs
			cleanup()
			entry, err := utils.ReadAssessmentFromEntry(fs, "active", logger)
			Expect(err).To(HaveOccurred())
			Expect(entry).To(Equal(""))
			// Check that error is os.ErrNotExist
			Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
		})

		It("matches with weird but valid format", func() {
			// This are valid values, after all the asessment is just a string at the end that starts with + and has
			// and number and an optional dash after that. It has to be before the .conf so this are valid values
			// even if they are weird or stupid.
			// potentially the name can be this if someone is rebuilding efi files and adding the + to indicate the build number
			// for example.
			// We dont use this but still want to check if these are valid.
			err := fs.WriteFile("/efi/loader/entries/test1++++++5.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test2+3+3-1.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			entry, err := utils.ReadAssessmentFromEntry(fs, "test1", logger)
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+5"))
			entry, err = utils.ReadAssessmentFromEntry(fs, "test2", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal("+3-1"))
		})
		It("doesn't match assessment if format is wrong", func() {
			err := fs.WriteFile("/efi/loader/entries/test1+1djnfsdjknfsdajf2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test2+1-sadfsbauhdfkj.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test3+asdasd.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test4+-2.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/efi/loader/entries/test5+3&4.conf", []byte(""), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			entry, err := utils.ReadAssessmentFromEntry(fs, "test1", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
			entry, err = utils.ReadAssessmentFromEntry(fs, "test2", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
			entry, err = utils.ReadAssessmentFromEntry(fs, "test3", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
			entry, err = utils.ReadAssessmentFromEntry(fs, "test4", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
			entry, err = utils.ReadAssessmentFromEntry(fs, "test5", logger)
			// It actually does not error but just doesn't match
			Expect(err).ToNot(HaveOccurred())
			Expect(entry).To(Equal(""))
		})

	})
})
