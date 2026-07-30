package state

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/foxboron/go-uefi/efi"
	"github.com/hashicorp/go-multierror"
	cnst "github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/pkg/op"
	"github.com/kairos-io/immucore/pkg/schema"
	"github.com/kairos-io/kairos-sdk/signatures"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/spectrocloud-labs/herd"
)

// UKIExtendPCR extends the PCR with the given extension in a graceful way.
func UKIExtendPCR(extension string) error {
	return internalUtils.PCRExtend(cnst.DefaultPCR, []byte(extension))

}

// UKIMountBaseSystem mounts the base system for the UKI boot system
// as when booting in UKI mode we have a blank slate and we need to mount everything
// Make sure we set the directories as MS_SHARED
// This is important afterwards when running containers and they get unshared and so on
// And can lead to rootfs out of boundaries issues for them
// also it doesnt help when mounting the final rootfs as we want to broke the mounts into it and any submounts.
func (s *State) UKIMountBaseSystem(g *herd.Graph) error {
	type mount struct {
		where string
		what  string
		fs    string
		flags uintptr
		data  string
	}

	return g.Add(
		cnst.OpUkiBaseMounts,
		TimedCallback(cnst.OpUkiBaseMounts,
			func(_ context.Context) error {
				var err error
				// Mount base mounts
				mounts := []mount{
					{
						"/sys",
						"sysfs",
						"sysfs",
						syscall.MS_NOSUID | syscall.MS_NODEV | syscall.MS_NOEXEC | syscall.MS_RELATIME,
						"",
					},
					{
						"/sys",
						"",
						"",
						syscall.MS_SHARED,
						"",
					},
					{
						"/sys/kernel/security",
						"securityfs",
						"securityfs",
						0,
						"",
					},
					{
						"/sys/kernel/debug",
						"debugfs",
						"debugfs",
						0,
						"",
					},
					{
						"/sys/firmware/efi/efivars",
						"efivarfs",
						"efivarfs",
						syscall.MS_NOSUID | syscall.MS_NODEV | syscall.MS_NOEXEC | syscall.MS_RELATIME,
						"",
					},
					{
						"/dev",
						"devtmpfs",
						"devtmpfs",
						syscall.MS_NOSUID,
						"mode=755",
					},
					{
						"/dev",
						"",
						"",
						syscall.MS_SHARED,
						"",
					},
					{
						"/dev/pts",
						"devpts",
						"devpts",
						syscall.MS_NOSUID | syscall.MS_NOEXEC,
						"ptmxmode=000,gid=5,mode=620",
					},
					{
						"/dev/shm",
						"tmpfs",
						"tmpfs",
						0,
						"",
					},
					{
						"/tmp",
						"tmpfs",
						"tmpfs",
						syscall.MS_NOSUID | syscall.MS_NODEV,
						"",
					},
					{
						"/tmp",
						"",
						"",
						syscall.MS_SHARED,
						"",
					},
				}

				for dir, perm := range map[string]os.FileMode{
					"/proc":    0o555,
					"/dev":     0o777,
					"/dev/pts": 0o777,
					"/dev/shm": 0o777,
					"/sys":     0o555,
				} {
					e := os.MkdirAll(dir, perm)
					if e != nil {
						internalUtils.KLog.Logger.Err(e).Str("dir", dir).Interface("permissions", perm).Msg("Creating dir")
					}
				}
				for _, m := range mounts {
					e := os.MkdirAll(m.where, 0755)
					if e != nil {
						err = multierror.Append(err, e)
						internalUtils.KLog.Logger.Err(e).Msg("Creating dir")
					}

					e = internalUtils.Mount(m.what, m.where, m.fs, m.flags, m.data)
					if e != nil {
						err = multierror.Append(err, e)
						internalUtils.KLog.Logger.Err(e).Str("what", m.what).Str("where", m.where).Str("type", m.fs).Msg("Mounting")
					}
				}

				// Now that we have all the mounts, check if we got secureboot enabled
				if !efi.GetSecureBoot() && len(internalUtils.ReadCMDLineArg("rd.immucore.securebootdisabled")) == 0 {
					internalUtils.RebootOrWait("Secure boot is not enabled", nil)
				}
				return err
			},
		),
	)
}

// UkiPivotToSysroot moves the rootfs to the sysroot and chroots into it
// Making the /sysroot the new rootfs with a tmpfs fs
// And moving all the mounts into it and all the files as well.
func (s *State) UkiPivotToSysroot(g *herd.Graph) error {
	return g.Add(cnst.OpUkiPivotToSysroot,
		herd.WithDeps(cnst.OpUkiBaseMounts, cnst.OpUkiTPMKernelModules),
		TimedCallback(cnst.OpUkiPivotToSysroot, func(_ context.Context) error {
			var err error
			// Create the new sysroot and move to it
			// We need the sysroot to NOT be of type rootfs, otherwise kubernetes stuff doesnt really work
			internalUtils.KLog.Logger.Debug().Str("what", s.path(cnst.UkiSysrootDir)).Msg("Creating sysroot dir")
			err = os.MkdirAll(s.path(cnst.UkiSysrootDir), 0755) // #nosec G301 -- Sysroot needs to be 755 to be world readable
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("creating sysroot dir")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to create sysroot directory: %v", err))
			}

			// Mount a tmpfs under sysroot
			internalUtils.KLog.Logger.Debug().Msg("Mounting tmpfs on sysroot")
			// Pin the tmpfs root to 0755 explicitly. Without mode= the kernel uses
			// 0777 & ~umask, which is non-deterministic in early boot and can leave / as
			// 0777. Normal (non-UKI) boot mounts a real image fs whose root is 0755, so
			// match that. Some software (e.g. snapd) requires / to be 0755.
			err = internalUtils.Mount("tmpfs", s.path(cnst.UkiSysrootDir), "tmpfs", 0, "mode=0755")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("mounting tmpfs on sysroot")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to mount tmpfs on sysroot: %v", err))
			}

			// Move all the dirs in root FS that are not a mountpoint to the new root via cp -R
			rootDirs, err := os.ReadDir(s.Rootdir)
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("reading rootdir content")
			}

			var mountPoints []string
			for _, file := range rootDirs {
				if file.Name() == cnst.UkiSysrootDir {
					continue
				}
				if file.IsDir() {
					path := file.Name()
					fileInfo, err := os.Stat(s.path(path))
					if err != nil {
						return err
					}
					parentPath := filepath.Dir(s.path(path))
					parentInfo, err := os.Stat(parentPath)
					if err != nil {
						return err
					}
					// If the directory has the same device as its parent, it's not a mount point.
					if fileInfo.Sys().(*syscall.Stat_t).Dev == parentInfo.Sys().(*syscall.Stat_t).Dev {
						internalUtils.KLog.Logger.Debug().Str("what", path).Msg("simple directory")
						err = os.MkdirAll(filepath.Join(s.path(cnst.UkiSysrootDir), path), fileInfo.Mode())
						if err != nil {
							internalUtils.KLog.Logger.Err(err).Str("what", filepath.Join(s.path(cnst.UkiSysrootDir), path)).Msg("mkdir")
							return err
						}

						// Copy it over
						out, err := internalUtils.CommandWithPath(fmt.Sprintf("cp -a %s %s", s.path(path), s.path(cnst.UkiSysrootDir)))
						if err != nil {
							internalUtils.KLog.Logger.Err(err).Str("out", out).Str("what", s.path(path)).Str("where", s.path(cnst.UkiSysrootDir)).Msg("copying dir into sysroot")
						}
						continue
					}

					internalUtils.KLog.Logger.Debug().Str("what", path).Msg("mount point")
					mountPoints = append(mountPoints, s.path(path))

					continue
				}

				info, _ := file.Info()
				fileInfo, _ := os.Lstat(file.Name())

				// Symlink
				if fileInfo.Mode()&os.ModeSymlink != 0 {
					target, err := os.Readlink(file.Name())
					if err != nil {
						return fmt.Errorf("failed to read symlink: %w", err)
					}
					symlinkPath := s.path(filepath.Join(cnst.UkiSysrootDir, file.Name()))
					err = os.Symlink(target, symlinkPath)
					if err != nil {
						internalUtils.KLog.Logger.Err(err).Str("from", target).Str("to", symlinkPath).Msg("Symlink")
						internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to recreate symlink while pivoting to sysroot: %v", err))
					}
					internalUtils.KLog.Logger.Debug().Str("from", target).Str("to", symlinkPath).Msg("Symlinked file")
				} else {
					// If its a file in the root dir just copy it over
					content, _ := os.ReadFile(s.path(file.Name()))
					newFilePath := s.path(filepath.Join(cnst.UkiSysrootDir, file.Name()))
					_ = os.WriteFile(newFilePath, content, info.Mode())
					internalUtils.KLog.Logger.Debug().Str("from", s.path(file.Name())).Str("to", newFilePath).Msg("Copied file")
				}
			}

			// Now move the system mounts into the new dir
			for _, d := range mountPoints {
				newDir := filepath.Join(s.path(cnst.UkiSysrootDir), d)
				if _, err := os.Stat(newDir); err != nil {
					err = os.MkdirAll(newDir, 0755)
					if err != nil {
						internalUtils.KLog.Logger.Err(err).Str("what", newDir).Msg("mkdir")
					}
				}

				err = internalUtils.Mount(d, newDir, "", syscall.MS_MOVE, "")
				if err != nil {
					internalUtils.KLog.Logger.Err(err).Str("what", d).Str("where", newDir).Msg("move mount")
					continue
				}
				internalUtils.KLog.Logger.Debug().Str("from", d).Str("to", newDir).Msg("Mount moved")
			}

			internalUtils.KLog.Logger.Debug().Str("to", s.path(cnst.UkiSysrootDir)).Msg("Changing dir")
			if err = syscall.Chdir(s.path(cnst.UkiSysrootDir)); err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("chdir")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to chdir into sysroot: %v", err))
			}

			internalUtils.KLog.Logger.Debug().Str("what", s.path(cnst.UkiSysrootDir)).Str("where", "/").Msg("Moving mount")
			if err = internalUtils.Mount(s.path(cnst.UkiSysrootDir), "/", "", syscall.MS_MOVE, ""); err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("mount move")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to move sysroot mount to /: %v", err))
			}

			internalUtils.KLog.Logger.Debug().Str("to", ".").Msg("Chrooting")
			if err = syscall.Chroot("."); err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("chroot")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to chroot into the new root: %v", err))
			}

			ext := "enter-initrd"
			pcrErr := UKIExtendPCR(ext)
			if pcrErr != nil {
				internalUtils.KLog.Logger.Err(pcrErr).Str("ext", ext).Msg("extend-pcr")
			}

			err = os.MkdirAll("/run/systemd", 0755) // #nosec G301 -- Original dir has this permissions
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Creating /run/systemd dir")
			}
			// This dir is created by systemd-stub and passed to the kernel as a cpio archive
			// that gets mounted in the initial ramdisk where we run immucore from
			// It contains the tpm public key and signatures of the current uki
			out, err := internalUtils.CommandWithPath("cp -r /.extra/* /run/systemd/")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Str("out", out).Msg("Copying extra files")
			}
			return err
		}))
}

// UKIUdevDaemon launches the udevd daemon and triggers+settles in order to discover devices
// Needed if we expect to find devices by label...
func (s *State) UKIUdevDaemon(g *herd.Graph) error {
	return g.Add(cnst.OpUkiUdev,
		herd.WithDeps(cnst.OpUkiBaseMounts, cnst.OpUkiPivotToSysroot, cnst.OpUkiKernelModules),
		TimedCallback(cnst.OpUkiUdev, func(_ context.Context) error {
			// Should probably figure out other udevd binaries....
			var udevBin string
			if _, err := os.Stat("/usr/lib/systemd/systemd-udevd"); !os.IsNotExist(err) {
				udevBin = "/usr/lib/systemd/systemd-udevd"
			}
			cmd := fmt.Sprintf("%s --daemon", udevBin)
			out, err := internalUtils.CommandWithPath(cmd)
			internalUtils.KLog.Logger.Debug().Str("out", out).Str("cmd", cmd).Msg("Udev daemon")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Udev daemon")
				return err
			}
			out, err = internalUtils.CommandWithPath("udevadm trigger")
			internalUtils.KLog.Logger.Debug().Str("out", out).Msg("Udev trigger")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Udev trigger")
				return err
			}

			out, err = internalUtils.CommandWithPath("udevadm settle")
			internalUtils.KLog.Logger.Debug().Str("out", out).Msg("Udev settle")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Udev settle")
				return err
			}
			return nil
		}),
	)
}

// UKILoadTPMModules loads kernel modules needed for TPM support during uki boot.
// We need to load them ASAP as our measurement depends on this.
func (s *State) UKILoadTPMModules(g *herd.Graph) error {
	return g.Add(cnst.OpUkiTPMKernelModules,
		herd.WithDeps(cnst.OpUkiBaseMounts),
		TimedCallback(cnst.OpUkiTPMKernelModules, func(_ context.Context) error {
			// Run depmod to ensure all modules are loaded and modules.dep updated
			_, _ = internalUtils.CommandWithPath("depmod -a")
			drivers := cnst.TPMKernelModules()
			internalUtils.KLog.Logger.Debug().Strs("drivers", drivers).Msg("Detecting needed modules")
			for _, driver := range drivers {
				cmd := fmt.Sprintf("modprobe %s", driver)
				out, err := internalUtils.CommandWithPath(cmd)
				if err != nil {
					internalUtils.KLog.Logger.Debug().Err(err).Str("out", out).Msg("modprobe")
				}
			}
			return nil
		}),
	)
}

// UKILoadKernelModules loads kernel modules needed during uki boot to load the disks for.
// Mainly block devices and net devices
// probably others down the line.
func (s *State) UKILoadKernelModules(g *herd.Graph) error {
	return g.Add(cnst.OpUkiKernelModules,
		herd.WithDeps(cnst.OpUkiBaseMounts, cnst.OpUkiPivotToSysroot, cnst.OpUkiTPMKernelModules),
		TimedCallback(cnst.OpUkiKernelModules, func(_ context.Context) error {
			// Run depmod to ensure all modules are loaded and modules.dep updated
			_, _ = internalUtils.CommandWithPath("depmod -a")
			drivers := cnst.GenericKernelDrivers()
			internalUtils.KLog.Logger.Debug().Strs("drivers", drivers).Msg("Detecting needed modules")
			for _, driver := range drivers {
				cmd := fmt.Sprintf("modprobe -v %s", driver)
				out, err := internalUtils.CommandWithPath(cmd)
				if err != nil {
					internalUtils.KLog.Logger.Debug().Err(err).Str("out", out).Msg("modprobe")
				}
			}
			return nil
		}),
	)
}

// UKISetupNetwork initializes network interfaces in UKI mode for remote KMS access.
// This is needed before attempting to unlock encrypted partitions with remote attestation.
// Network setup is only performed if a remote KMS server is configured in the kernel cmdline.
func (s *State) UKISetupNetwork(g *herd.Graph) error {
	return g.Add(cnst.OpUkiNetwork,
		herd.WithDeps(cnst.OpUkiBaseMounts, cnst.OpUkiPivotToSysroot, cnst.OpUkiKernelModules, cnst.OpUkiUdev),
		TimedCallback(cnst.OpUkiNetwork, func(_ context.Context) error {

			// TODO: Make this function actually work.
			internalUtils.KLog.Logger.Warn().Msg("UKISetupNetwork: Not implemented yet!")
			return nil

			// Skip network setup if booting in livecd mode because
			// network is handled by the normal boot process
			if !state.EfiBootFromInstall(internalUtils.KLog.Logger) {
				internalUtils.KLog.Logger.Info().Msg("UKISetupNetwork: Skipping network setup in install/livecd mode")
				return nil
			}

			// Check if remote KMS is configured by looking for challenger_server in cmdline
			cmdline, err := os.ReadFile("/proc/cmdline")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("UKISetupNetwork: Failed to read /proc/cmdline")
				return err
			}

			cmdlineStr := string(cmdline)
			internalUtils.KLog.Logger.Info().Str("cmdline", cmdlineStr).Msg("UKISetupNetwork: Read cmdline")

			// Check for remote KMS configuration in various formats:
			// - kairos.kcrypt.challenger.challenger_server= (dot notation from config)
			// - kairos.kcrypt.challenger_server= (underscore notation)
			// - challenger_server= (legacy format)
			// - mdns=true (mDNS discovery)
			hasRemoteKMS := strings.Contains(cmdlineStr, "challenger_server=") ||
				strings.Contains(cmdlineStr, "mdns=true")

			if !hasRemoteKMS {
				internalUtils.KLog.Logger.Info().Msg("UKISetupNetwork: No remote KMS configured, skipping network setup")
				return nil
			}

			internalUtils.KLog.Logger.Info().Msg("UKISetupNetwork: Remote KMS detected, setting up network")

			// Try to bring up all network interfaces with DHCP
			out, err := internalUtils.CommandWithPath("ip link show")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Str("out", out).Msg("Failed to list network interfaces")
				return err
			}

			// Parse interfaces and bring them up
			lines := strings.Split(out, "\n")
			for _, line := range lines {
				// Look for interface lines like "2: eth0: <BROADCAST,MULTICAST,UP,LOWER_UP>"
				if strings.Contains(line, ": <") && !strings.Contains(line, "lo:") {
					parts := strings.Split(line, ":")
					if len(parts) >= 2 {
						iface := strings.TrimSpace(parts[1])
						internalUtils.KLog.Logger.Debug().Str("interface", iface).Msg("Bringing up network interface")

						// Bring interface up
						_, err := internalUtils.CommandWithPath(fmt.Sprintf("ip link set %s up", iface))
						if err != nil {
							internalUtils.KLog.Logger.Warn().Err(err).Str("interface", iface).Msg("Failed to bring up interface")
							continue
						}

						// Request DHCP lease using dhclient
						internalUtils.KLog.Logger.Debug().Str("interface", iface).Msg("Requesting DHCP lease")
						_, err = internalUtils.CommandWithPath(fmt.Sprintf("dhclient -1 -v %s", iface))
						if err != nil {
							internalUtils.KLog.Logger.Warn().Err(err).Str("interface", iface).Msg("DHCP failed, trying next interface")
							continue
						}

						// Check if we got an IP address
						out, err := internalUtils.CommandWithPath(fmt.Sprintf("ip addr show %s", iface))
						if err == nil && strings.Contains(out, "inet ") {
							internalUtils.KLog.Logger.Info().Str("interface", iface).Msg("Network interface configured successfully")

							// dhclient should have set up /etc/resolv.conf, but in initramfs it might not work
							// Check if resolv.conf exists and has nameservers
							resolvConf, err := os.ReadFile("/etc/resolv.conf")
							if err != nil || !strings.Contains(string(resolvConf), "nameserver") {
								internalUtils.KLog.Logger.Warn().Msg("No DNS configuration found, setting up default DNS")
								// Use common public DNS servers as fallback
								dnsConfig := "nameserver 8.8.8.8\nnameserver 8.8.4.4\n"
								if err := os.WriteFile("/etc/resolv.conf", []byte(dnsConfig), 0644); err != nil {
									internalUtils.KLog.Logger.Err(err).Msg("Failed to write /etc/resolv.conf")
								} else {
									internalUtils.KLog.Logger.Info().Msg("Configured fallback DNS servers")
								}
							} else {
								internalUtils.KLog.Logger.Debug().Str("resolv.conf", string(resolvConf)).Msg("DNS configuration found")
							}

							// Wait a moment for DNS to be ready
							time.Sleep(2 * time.Second)

							return nil
						}
					}
				}
			}

			internalUtils.KLog.Logger.Warn().Msg("No network interfaces could be configured")
			return fmt.Errorf("failed to configure any network interface")
		}),
	)
}

// UKIUnlock unlocks encrypted partitions in UKI mode.
// It wraps the unified unlockEncryptedPartitions with UKI-specific setup
// (PATH, removable media check, reboot on error).
func (s *State) UKIUnlock(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpUkiKcrypt, append(opts, TimedCallback(cnst.OpUkiKcrypt, func(_ context.Context) error {
		// Check if booting from removable media (live CD). The in-RAM
		// workflow boots from removable/network media on purpose and still
		// owns encrypted partitions on disk, so it never skips.
		if internalUtils.SkipUKIUnlock(state.EfiBootFromInstall(internalUtils.KLog.Logger), s.InRAM) {
			internalUtils.KLog.Logger.Debug().Msg("Not unlocking disks as we think we are booting from removable media")
			return nil
		}

		// Set full path on UKI to get all the binaries
		_ = os.Setenv("PATH", "/usr/bin:/usr/sbin:/bin:/sbin")

		// Use the unified unlock logic
		err := unlockEncryptedPartitions()
		if err != nil {
			// UKI-specific error handling: reboot on failure
			internalUtils.RebootOrWait("Unlocking partitions failed", err)
			return err
		}

		return nil
	}))...)
}

// UKIMountLiveCd tries to mount the livecd if we are booting from one into /run/initramfs/live
// to mimic the same behavior as the livecd on non-uki boot.
func (s *State) UKIMountLiveCd(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpUkiMountLivecd, append(opts, TimedCallback(cnst.OpUkiMountLivecd, func(_ context.Context) error {
		// If we are booting from Install Media
		if state.EfiBootFromInstall(internalUtils.KLog.Logger) {
			internalUtils.KLog.Logger.Debug().Msg("Not mounting livecd as we think we are booting from removable media")
			return nil
		}

		err := os.MkdirAll(s.path(cnst.UkiLivecdMountPoint), 0755)
		if err != nil {
			internalUtils.KLog.Logger.Err(err).Msg(fmt.Sprintf("Creating %s", cnst.UkiLivecdMountPoint))
			return err
		}
		err = os.MkdirAll(s.path(cnst.UkiIsoBaseTree), 0755)
		if err != nil {
			internalUtils.KLog.Logger.Err(err).Msg(fmt.Sprintf("Creating %s", cnst.UkiIsoBaseTree))
			return nil
		}

		// Select the correct device to mount
		// Try to find the CDROM device by label /dev/disk/by-label/UKI_ISO_INSTALL
		// try a couple of times as the udev daemon can take a bit of time to populate the devices
		var cdrom string

		for i := 0; i < 5; i++ {
			_, err = os.Stat(cnst.UkiLivecdPath)
			// if found, set it
			if err == nil {
				cdrom = cnst.UkiLivecdPath
				break
			}

			internalUtils.KLog.Logger.Debug().Msg(fmt.Sprintf("No media with label found at %s", cnst.UkiLivecdPath))
			out, _ := internalUtils.CommandWithPath("ls -ltra /dev/disk/by-label/")
			internalUtils.KLog.Logger.Debug().Str("out", out).Msg("contents of /dev/disk/by-label/")
			time.Sleep(time.Duration(i) * time.Second)
		}

		// Fallback to try to get the /dev/sr0 device directly, no retry as that wont take time to appear
		if cdrom == "" {
			_, err = os.Stat(cnst.UkiDefaultcdrom)
			if err == nil {
				cdrom = cnst.UkiDefaultcdrom
			} else {
				internalUtils.KLog.Logger.Debug().Msg(fmt.Sprintf("No media found at %s", cnst.UkiDefaultcdrom))
			}
		}

		// Mount it
		if cdrom != "" {
			err = internalUtils.Mount(cdrom, s.path(cnst.UkiLivecdMountPoint), cnst.UkiDefaultcdromFsType, syscall.MS_RDONLY, "")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg(fmt.Sprintf("Mounting %s", cdrom))
				return err
			}
			internalUtils.KLog.Logger.Debug().Msg(fmt.Sprintf("Mounted %s", cdrom))

			// This needs the loop module to be inserted in the kernel!
			cmd := fmt.Sprintf("losetup --show -f %s", s.path(filepath.Join(cnst.UkiLivecdMountPoint, cnst.UkiIsoBootImage)))
			out, err := internalUtils.CommandWithPath(cmd)
			loop := strings.TrimSpace(out)

			if err != nil || loop == "" {
				internalUtils.KLog.Logger.Err(err).Str("out", out).Msg(cmd)
				return err
			}

			err = internalUtils.Mount(loop, s.path(cnst.UkiIsoBaseTree), cnst.UkiDefaultEfiimgFsType, syscall.MS_RDONLY, "")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg(fmt.Sprintf("Mounting %s into %s", loop, s.path(cnst.UkiIsoBaseTree)))
				return err
			}
			return nil
		}
		internalUtils.KLog.Logger.Debug().Msg("No livecd/install media found")
		return nil
	}))...)
}

// UKIBootInitDagStep tries to launch /sbin/init in root and pass over the system
// booting to the real init process
// Drops to emergency if not able to. Panic if it cant even launch emergency.
func (s *State) UKIBootInitDagStep(g *herd.Graph) error {
	return g.Add(cnst.OpUkiInit,
		herd.WeakDeps,
		herd.WithWeakDeps(cnst.OpRootfsHook, cnst.OpInitramfsHook, cnst.OpWriteFstab),
		TimedCallback(cnst.OpUkiInit, func(_ context.Context) error {
			var err error

			ext := "leave-initrd"
			err = UKIExtendPCR(ext)
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Str("ext", ext).Msg("extend-pcr")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to extend PCR (leave-initrd): %v", err))
			}

			// Print dag before exit, otherwise its never printed as we never exit the program
			internalUtils.KLog.Logger.Info().Msg(s.WriteDAG(g))

			internalUtils.KLog.Logger.Debug().Str("what", s.path(s.Rootdir)).Msg("Mount / RO")
			// Close the logger before we remount the rootfs to not leave open file descriptors
			internalUtils.KLog.Close()
			if err = internalUtils.Mount("", s.path(s.Rootdir), "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "ro"); err != nil {
				internalUtils.SetLogger() // Set the logger again as we closed it
				internalUtils.KLog.Logger.Err(err).Msg("Mount / RO")
				internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to remount / read-only before exec init: %v", err))
			}

			internalUtils.SetLogger() // Set the logger again as we closed it
			internalUtils.KLog.Logger.Debug().Msg("Executing init callback!")
			if err := syscall.Exec("/sbin/init", []string{"/sbin/init"}, os.Environ()); err != nil {
				// Try under bin
				internalUtils.KLog.Logger.Warn().Err(err).Msg("Executing init failed, trying /bin/init")
				if err = syscall.Exec("/bin/init", []string{"/bin/init"}, os.Environ()); err != nil {
					internalUtils.KLog.Logger.Error().Err(err).Msg("Executing init failed, dropping to emergency shell")
					internalUtils.DropToEmergencyShellWithError(fmt.Sprintf("failed to exec init (/sbin/init and /bin/init): %v", err))
				}
			}
			return nil
		}))
}

// UKIMountESPPartition tries to mount the ESP into /efi
// Doesnt matter if it fails, its just for niceness.
func (s *State) UKIMountESPPartition(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add("mount-esp", append(opts, TimedCallback("mount-esp", func(_ context.Context) error {
		if !state.EfiBootFromInstall(internalUtils.KLog.Logger) {
			internalUtils.KLog.Logger.Debug().Msg("Not mounting ESP as we think we are booting from removable media")
			return nil
		}
		cmd := "lsblk -J -o NAME,PARTTYPE"
		out, err := internalUtils.CommandWithPath(cmd)
		internalUtils.KLog.Logger.Debug().Str("out", out).Str("cmd", cmd).Msg("ESP")
		if err != nil {
			internalUtils.KLog.Logger.Err(err).Msg("ESP")
			return nil
		}

		lsblk := &schema.LsblkOutput{}
		err = json.Unmarshal([]byte(out), lsblk)
		if err != nil {
			return nil
		}

		for _, bd := range lsblk.Blockdevices {
			for _, cd := range bd.Children {
				if strings.TrimSpace(cd.Parttype) == "c12a7328-f81f-11d2-ba4b-00a0c93ec93b" {
					// This is the ESP device
					device := filepath.Join("/dev", cd.Name)
					if !internalUtils.IsMounted(device) {
						fstab, err := op.MountOPWithFstab(
							device,
							s.path("/efi"),
							"vfat",
							[]string{
								"ro",
							}, 5*time.Second,
						)
						for _, f := range fstab {
							s.fstabs = append(s.fstabs, f)
						}
						return err
					}
				}
			}

		}
		return nil
	}))...)
}

// ExtractCerts extracts the public keys from the EFI variables and writes them to `/run/verity.d`.
// This is used by the sysextensions to verify the signatures of the images
// TODO: A public cert could be provided in the config that its used for this, so we should
// expand this in the future to also extract that cert during boot from the config into the /run/verity.d.
func (s *State) ExtractCerts(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpUkiExtractCerts, append(opts, TimedCallback(cnst.OpUkiExtractCerts, func(_ context.Context) error {
		// Get all the full certs
		certs, err := signatures.GetAllFullCerts()
		if err != nil {
			return err
		}

		err = os.MkdirAll(s.path(cnst.VerityCertDir), 0755)
		if err != nil {
			return err
		}
		// Write all certs in x509 PEM format to /run/verity.d/ for sysextensions to verify against
		for i, cert := range certs.PK {
			publicKeyBlock := pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}
			publicKeyPem := pem.EncodeToMemory(&publicKeyBlock)
			err := os.WriteFile(filepath.Join(s.path(cnst.VerityCertDir), fmt.Sprintf("PK%d.crt", i)), publicKeyPem, 0644)
			if err != nil {
				return err
			}
		}
		for i, cert := range certs.KEK {
			publicKeyBlock := pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}
			publicKeyPem := pem.EncodeToMemory(&publicKeyBlock)
			err := os.WriteFile(filepath.Join(s.path(cnst.VerityCertDir), fmt.Sprintf("KEK%d.crt", i)), publicKeyPem, 0644)
			if err != nil {
				return err
			}
		}
		for i, cert := range certs.DB {
			publicKeyBlock := pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}
			publicKeyPem := pem.EncodeToMemory(&publicKeyBlock)
			err := os.WriteFile(filepath.Join(s.path(cnst.VerityCertDir), fmt.Sprintf("DB%d.crt", i)), publicKeyPem, 0644)
			if err != nil {
				return err
			}
		}

		return nil
	}))...)
}

// MigrateSysExt is a workaround for upgrades from `3.3.x` to `>= 3.4.x`.
// In 3.3.x we had the extensions in the EFI dir directly, under /efi/EFI/kairos/{active,passive}.efi.extra.d/
// In 3.4.x we moved them to /var/lib/kairos/extensions/ for generic and for enabled ones to /var/lib/kairos/extensions/{active,passive}/
// This is a workaround to move the extensions from the old location to the new one to help with upgrades
// The order is:
// Check both active and passive dirs
// If something is found, move it to the new location at /var/lib/kairos/extensions/
// Enable it by creating a softlink from /var/lib/kairos/extensions/{active,passive}/EXTENSION to /var/lib/kairos/extensions/EXTENSION
// Remove it from the old location.
func (s *State) MigrateSysExt(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpUkiTransitionSysext, append(opts, TimedCallback(cnst.OpUkiTransitionSysext, func(_ context.Context) error {
		if !state.EfiBootFromInstall(internalUtils.KLog.Logger) {
			internalUtils.KLog.Logger.Debug().Msg("Not transitioning sysext as we think we are booting from removable media")
			return nil
		}

		// Check or create target dir
		if _, err := os.Stat(s.path("/var/lib/kairos/extensions")); os.IsNotExist(err) {
			err = os.MkdirAll(s.path("/var/lib/kairos/extensions"), 0755)
			if err != nil {
				return err
			}
		}

		// We have to remount the EFI partition as RW to be able to move the files
		err := syscall.Mount(cnst.EfiDir, cnst.EfiDir, cnst.UkiDefaultEfiimgFsType, syscall.MS_REMOUNT, "rw")
		if err != nil {
			internalUtils.KLog.Logger.Err(err).Msg("Mounting EFI partition")
			return err
		}
		// We need to remount it as RO after we are done
		defer func() {
			err := syscall.Mount(cnst.EfiDir, cnst.EfiDir, cnst.UkiDefaultEfiimgFsType, syscall.MS_REMOUNT|syscall.MS_RDONLY, "")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Mounting EFI partition as RO")
			} else {
				internalUtils.KLog.Logger.Debug().Msg("Remounting EFI partition as RO")
			}
		}()

		for _, bootState := range []string{"active", "passive"} {
			sourceDir := s.path(fmt.Sprintf("/efi/EFI/kairos/%s.efi.extra.d/", bootState))
			internalUtils.KLog.Logger.Debug().Str("dir", sourceDir).Msg("Checking for sysextensions")
			targetDir := s.path(fmt.Sprintf("/var/lib/kairos/extensions/%s", bootState))
			if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
				internalUtils.KLog.Logger.Debug().Str("dir", sourceDir).Msg("No sysextensions found")
				continue
			}
			// Create target dirs as well
			if _, err := os.Stat(targetDir); os.IsNotExist(err) {
				err = os.MkdirAll(targetDir, 0755)
				if err != nil {
					return err
				}
			}
			// Move the files over to the main extensions dir
			files, err := os.ReadDir(sourceDir)
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Reading dir")
				continue
			}
			for _, file := range files {
				if file.IsDir() {
					// Skip directories
					continue
				}
				source := filepath.Join(sourceDir, file.Name())
				target := filepath.Join(s.path("/var/lib/kairos/extensions"), file.Name())
				// Copy the file to the main extensions dir
				internalUtils.KLog.Logger.Debug().Str("source", source).Str("target", target).Msg("Moving sysextension")
				err = internalUtils.Copy(source, target)
				if err != nil {
					internalUtils.KLog.Logger.Err(err).Str("source", source).Str("target", target).Msg("Moving sysextension")
					continue
				}
				internalUtils.KLog.Logger.Debug().Str("source", source).Str("target", target).Msg("Moved sysextension")

				internalUtils.KLog.Logger.Debug().Str("target", target).Str("to", s.path(filepath.Join("/var/lib/kairos/extensions", bootState, file.Name()))).Msg("Creating symlink")
				// Create a symlink to the new location
				err = os.Symlink(target, s.path(filepath.Join("/var/lib/kairos/extensions", bootState, file.Name())))
				if err != nil {
					internalUtils.KLog.Logger.Err(err).Str("target", target).Str("to", s.path(filepath.Join("/var/lib/kairos/extensions", bootState, file.Name()))).Msg("Creating symlink")
					continue
				}
				// If no errors at this point, remove the original sysext
				err = os.Remove(source)
				if err != nil {
					internalUtils.KLog.Logger.Err(err).Str("source", source).Msg("Removing old sysext")
					continue
				}
				internalUtils.KLog.Logger.Debug().Str("source", source).Msg("Done sysext")
			}
		}
		return nil
	}))...)
}
