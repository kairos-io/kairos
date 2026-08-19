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

package utils

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/state"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/kairos-io/kairos-sdk/utils"
)

// Grub is the struct that will allow us to install grub to the target device
type Grub struct {
	config *sdkConfig.Config
}

func NewGrub(config *sdkConfig.Config) *Grub {
	g := &Grub{
		config: config,
	}

	return g
}

// Install installs grub into the device, copy the config file and add any extra TTY to grub
// TODO: Make it more generic to be able to call it from other places
// i.e.: filepath.Join(cnst.ActiveDir, "etc/kairos-release") seraches for the file in the active dir, we should be looking into the rootdir?
// filepath.Join(cnst.EfiDir, "EFI/boot/grub.cfg") we also write into the efi dir directly, this should be the a var maybe?
func (g Grub) Install(target, rootDir, bootDir, grubConf, tty string, efi bool, stateLabel string) (err error) { // nolint:gocyclo
	var grubargs []string
	var grubdir, finalContent string

	// At this point the active mountpoint has all the data from the installation source, so we should be able to use
	// the grub.cfg bundled in there
	systemgrub := "grub2"

	// only install grub on non-efi systems
	if !efi {
		// Workaround for livecd installs where the root dir is identified as LiveOS_rootfs
		// some grub versions try to probe the system and fail as they cant find anything linked to that name
		// so we create a symlink to the COS_STATE partition so grub can probe the target disk properly and then remove it
		// The path is CWD/LiveOS_rootfs so we need to get the CWD and create the symlink there

		r, _ := state.NewRuntimeWithLogger(g.config.Logger.Logger)
		if r.BootState == state.LiveCD {
			// grub-install runs chrooted into rootDir (see below), so its working
			// dir is the chroot root. Create the symlink under rootDir so it
			// resolves to CWD/LiveOS_rootfs from inside the chroot.
			liveOsDir := filepath.Join(rootDir, "LiveOS_rootfs")
			// Now create the symlink (source, target)
			err = g.config.Fs.Symlink(filepath.Join("/dev/disk/by-label/", cnst.StateLabel), liveOsDir)
			if err != nil {
				g.config.Logger.Warnf("Failed creating liveos symlink: %s", err)
				return err
			}

			// Remove the symlink at the end
			defer func() {
				err = g.config.Fs.Remove(liveOsDir)
				if err != nil {
					g.config.Logger.Warnf("Failed removing liveos symlink: %s", err)
				}
			}()
		}

		g.config.Logger.Info("Installing GRUB..")

		// Find where in the rootDir the grub2 files for i386-pc are
		// Use the modinfo.sh as a marker
		grubdir = findGrubDir(g.config.Fs, rootDir)
		if grubdir == "" {
			g.config.Logger.Logger.Error().Str("path", rootDir).Msg("Failed to find grub dir")
			return fmt.Errorf("failed to find grub dir under %s", rootDir)
		}

		grubBin := FindCommand(g.config.Fs, "", []string{
			filepath.Join(rootDir, "/usr/sbin/", "grub2-install"),
			filepath.Join(rootDir, "/usr/bin/", "grub2-install"),
			filepath.Join(rootDir, "/sbin/", "grub2-install"),
			filepath.Join(rootDir, "/usr/sbin/", "grub-install"),
			filepath.Join(rootDir, "/usr/bin/", "grub-install"),
			filepath.Join(rootDir, "/sbin/", "grub-install"),
		})
		g.config.Logger.Logger.Debug().Str("command", grubBin).Msg("Found grub binary")
		if grubBin == "" {
			g.config.Logger.Logger.Error().Str("path", rootDir).Msg("Grub binary not found in path")
			return fmt.Errorf("grub binary not found in path")
		}

		// grub-install is a dynamically linked binary shipped by the
		// installation source, not by the host. When installing from an
		// arbitrary liveCD, the host loader and shared libraries do not match
		// the ones this binary was linked against, so running it directly fails
		// with "no such file or directory" (missing ELF interpreter) or a
		// missing shared library (e.g. libdevmapper). To make installs work from
		// any liveCD we run grub-install chrooted into rootDir so it resolves
		// its interpreter and libraries from the installation source itself.
		// Paths handed to grub must therefore be relative to the chroot, and the
		// boot directory (which lives outside rootDir) is bind-mounted in keeping
		// its original path.
		chrootRoot := strings.TrimSuffix(rootDir, "/")
		grubBinChroot := strings.TrimPrefix(grubBin, chrootRoot)
		grubDirChroot := strings.TrimPrefix(grubdir, chrootRoot)

		grubargs = append(
			grubargs,
			fmt.Sprintf("--directory=%s", grubDirChroot),
			fmt.Sprintf("--boot-directory=%s", bootDir),
			"--target=i386-pc", "-v",
			target,
		)

		g.config.Logger.Debugf("Running grub with the following args: %s", grubargs)

		grubChroot := NewChroot(rootDir, g.config)
		grubChroot.SetExtraMounts(map[string]string{bootDir: bootDir})
		out, err := grubChroot.Run(grubBinChroot, grubargs...)
		if err != nil {
			g.config.Logger.Errorf(string(out))
			return err
		}
		g.config.Logger.Infof("Grub install to device %s complete", target)

		// Select the proper dir for grub - this assumes that we previously run a grub install command, which is not the case in EFI
		// In the EFI case we default to grub2
		if ok, _ := fsutils.IsDir(g.config.Fs, filepath.Join(bootDir, "grub")); ok {
			systemgrub = "grub"
		}
	}

	grubdir = filepath.Join(rootDir, grubConf)
	g.config.Logger.Infof("Using grub config dir %s", grubdir)

	grubCfg, err := g.config.Fs.ReadFile(grubdir)
	if err != nil {
		g.config.Logger.Errorf("Failed reading grub config file: %s", filepath.Join(rootDir, grubConf))
		return err
	}

	// Create Needed dir under state partition to store the grub.cfg and any needed modules
	err = fsutils.MkdirAll(g.config.Fs, filepath.Join(bootDir, fmt.Sprintf("%s/%s-efi", systemgrub, g.config.Arch)), cnst.DirPerm)
	if err != nil {
		return fmt.Errorf("error creating grub dir: %s", err)
	}

	grubConfTarget, err := g.config.Fs.Create(filepath.Join(bootDir, fmt.Sprintf("%s/grub.cfg", systemgrub)))
	if err != nil {
		return err
	}

	defer grubConfTarget.Close()

	if tty == "" {
		// Get current tty and remove /dev/ from its name
		out, err := g.config.Fs.Readlink("/dev/fd/0")
		tty = strings.TrimPrefix(strings.TrimSpace(out), "/dev/")
		if err != nil {
			g.config.Logger.Warnf("failed to find current tty, leaving it unset")
			tty = ""
		}
	}

	ttyExists, _ := fsutils.Exists(g.config.Fs, fmt.Sprintf("/dev/%s", tty))

	if ttyExists && tty != "" && tty != "console" && tty != constants.DefaultTty {
		// We need to add a tty to the grub file
		g.config.Logger.Infof("Adding extra tty (%s) to grub.cfg", tty)
		defConsole := fmt.Sprintf("console=%s", constants.DefaultTty)
		finalContent = strings.Replace(string(grubCfg), defConsole, fmt.Sprintf("%s console=%s", defConsole, tty), -1)
	} else {
		// We don't add anything, just read the file
		finalContent = string(grubCfg)
	}

	g.config.Logger.Infof("Copying grub contents from %s to %s", grubdir, filepath.Join(bootDir, fmt.Sprintf("%s/grub.cfg", systemgrub)))
	_, err = grubConfTarget.WriteString(finalContent)
	if err != nil {
		return err
	}

	if efi {
		// Copy required extra modules to boot dir under the state partition
		// otherwise if we insmod it will fail to find them
		// We no longer call grub-install here so the modules are not setup automatically in the state partition
		// as they were before. We now use the bundled grub.efi provided by the shim package
		g.config.Logger.Infof("Generating grub files for efi on %s", target)
		var foundModules bool
		for _, m := range constants.GetGrubModules() {
			err = fsutils.WalkDirFs(g.config.Fs, rootDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.Name() == m && strings.Contains(path, g.config.Arch) {
					fileWriteName := filepath.Join(bootDir, fmt.Sprintf("%s/%s-efi/%s", systemgrub, g.config.Arch, m))
					g.config.Logger.Debugf("Copying %s to %s", path, fileWriteName)
					fileContent, err := g.config.Fs.ReadFile(path)
					if err != nil {
						return fmt.Errorf("error reading %s: %s", path, err)
					}
					err = g.config.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
					if err != nil {
						return fmt.Errorf("error writing %s: %s", fileWriteName, err)
					}
					foundModules = true
					return nil
				}
				return err
			})
			if !foundModules {
				return fmt.Errorf("did not find grub modules under %s", rootDir)
			}
		}

		copyGrubFonts(g.config, rootDir, grubdir, systemgrub)

		err = fsutils.MkdirAll(g.config.Fs, filepath.Join(cnst.EfiDir, "EFI/boot/"), cnst.DirPerm)
		if err != nil {
			g.config.Logger.Errorf("Error creating dirs: %s", err)
			return err
		}

		flavor, err := utils.OSRelease("FLAVOR", filepath.Join(cnst.ActiveDir, "etc/kairos-release"))
		if err != nil {
			// Fallback to os-release
			flavor, err = utils.OSRelease("FLAVOR", filepath.Join(cnst.ActiveDir, "os/kairos-release"))
			if err != nil {
				g.config.Logger.Warnf("Failed reading release info from %s and %s: %v", filepath.Join(cnst.ActiveDir, "etc/kairos-release"), filepath.Join(cnst.ActiveDir, "os/kairos-release"), err)
			}
		}
		if flavor == "" {
			// If os-release is gone with our vars, we dont know what flavor are we in, we should know if we are on ubuntu as we need
			// a workaround for the grub efi install
			// So lets try to get the info from the normal keys shipped with the os
			flavorFromId, err := utils.OSRelease("ID", filepath.Join(cnst.ActiveDir, "etc/os-release"))
			if err != nil {
				g.config.Logger.Logger.Err(err).Msg("Getting flavor")
			}
			if strings.Contains(strings.ToLower(flavorFromId), "ubuntu") {
				flavor = "ubuntu"
			}
		}
		g.config.Logger.Debugf("Detected Flavor: %s", flavor)
		// Copy needed files for efi boot
		// This seems like a chore while we could provide a package for those bundled files as they are just a shim and a grub efi
		// BUT this is needed for secureboot
		// The shim contains the signature from microsoft and the shim provider (i.e. upstream distro like fedora, suse, etc)
		// So if we use the shim+grub from a generic package (i.e. ubuntu) it WILL boot with secureboot
		// but when loading the kernel it will fail because the kernel is not signed by the shim provider or grub provider
		// the kernel signature would be from fedora while the shim signature would be from ubuntu
		// This is why if we want to support secureboot we need to copy the shim+grub from the rootfs default paths instead of
		// providing a generic package

		// Shim is not available in Alpine + rpi
		var model string
		model, err = utils.OSRelease("KAIROS_MODEL", filepath.Join(cnst.ActiveDir, "etc/kairos-release"))
		if err != nil {
			// Fallback into os-release
			model, err = utils.OSRelease("KAIROS_MODEL", filepath.Join(cnst.ActiveDir, "etc/os-release"))
			if err != nil {
				g.config.Logger.Warnf("Failed reading model info from %s and %s: %v", filepath.Join(cnst.ActiveDir, "etc/kairos-release"), filepath.Join(cnst.ActiveDir, "os/kairos-release"), err)
			}
		}
		if strings.Contains(strings.ToLower(flavor), "alpine") && strings.Contains(strings.ToLower(model), "rpi") {
			g.config.Logger.Debug("Running on Alpine+RPI, not copying shim or grub.")
		} else {
			// RISC-V has no shim package on Fedora/RHEL; firmware boots grub.efi directly.
			if g.config.Arch != cnst.ArchRiscv64 {
				err = g.copyShim()
				if err != nil {
					return err
				}
			} else {
				g.config.Logger.Debug("Skipping shim copy for riscv64 - booting with grub EFI directly")
			}
			err = g.copyGrub()
			if err != nil {
				return err
			}
			err = g.writeEfiGrubCfg(stateLabel, systemgrub, flavor)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// findGrubDir will find the grub dir under the dir given if possible by searching for the modinfo.sh
// And it will return the full dir path where the modinfo.sh is contained
func findGrubDir(vfs sdkFS.KairosFS, dir string) string {
	var foundPath string
	_ = fsutils.WalkDirFs(vfs, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "modinfo.sh" && strings.Contains(path, "i386-pc") {
			// We found the grub dir, return it
			foundPath = filepath.Dir(path)
			return nil
		}
		return nil
	})

	return foundPath

}

func SetPersistentVariables(grubEnvFile string, vars map[string]string, c *sdkConfig.Config) error {
	var b bytes.Buffer
	// Write header
	b.WriteString("# GRUB Environment Block\n")

	// First we need to read the existing values from the grubenv file if they exist
	finalVars, err := ReadPersistentVariables(grubEnvFile, c)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("error reading existing grubenv file %s: %s", grubEnvFile, err)
		}
	}
	// Check if we have a nil var
	if len(finalVars) != 0 {
		c.Logger.Logger.Debug().Interface("existingVars", finalVars).Msg("Existing grubenv variables")
	}

	// Merge the existing vars with the new ones
	// existing vars will be overridden by the new ones from vars if they match
	for key, newValue := range vars {
		if oldValue, exists := finalVars[key]; exists {
			c.Logger.Logger.Warn().Str("key", key).Str("oldValue", oldValue).Str("newValue", newValue).Msg("Overriding existing grubenv variable")
		}
		finalVars[key] = newValue
	}

	keys := make([]string, 0, len(finalVars))

	for k := range finalVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if len(finalVars[k]) > 0 {
			b.WriteString(fmt.Sprintf("%s=%s\n", k, finalVars[k]))
		}
	}

	// https://www.gnu.org/software/grub/manual/grub/html_node/Environment-block.html
	// It has to be exactly 1024bytes, if its not it has to be filled with #
	toBeFilled := 1024 - b.Len()%1024
	for i := 0; i < toBeFilled; i++ {
		b.WriteByte('#')
	}
	return c.Fs.WriteFile(grubEnvFile, b.Bytes(), cnst.FilePerm)
}

// copyGrubFonts will try to finds and copy the needed grub fonts into the system
// rootdir is the dir where to search for the fonts
// bootdir is the base dir where they will be copied
func copyGrubFonts(cfg *sdkConfig.Config, rootDir, bootDir, systemgrub string) {
	for _, m := range constants.GetGrubFonts() {
		var foundFont bool
		_ = fsutils.WalkDirFs(cfg.Fs, rootDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() == m && strings.Contains(path, cfg.Arch) {
				fileWriteName := filepath.Join(bootDir, fmt.Sprintf("%s/%s-efi/fonts/%s", systemgrub, cfg.Arch, m))
				cfg.Logger.Debugf("Copying %s to %s", path, fileWriteName)
				fileContent, err := cfg.Fs.ReadFile(path)
				if err != nil {
					return fmt.Errorf("error reading %s: %s", path, err)
				}
				err = cfg.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
				if err != nil {
					return fmt.Errorf("error writing %s: %s", fileWriteName, err)
				}
				foundFont = true
				return nil
			}
			return err
		})
		if !foundFont {
			// Not a real error as to fail install but a big warning
			cfg.Logger.Warnf("did not find grub font %s under %s", m, rootDir)
		}
	}
}

func (g Grub) copyShim() error {
	shimFiles := utils.GetEfiShimFiles(g.config.Arch)
	shimDone := false
	for _, f := range shimFiles {
		_, err := g.config.Fs.Stat(filepath.Join(cnst.ActiveDir, f))
		if err != nil {
			g.config.Logger.Debugf("skip copying %s: not found", filepath.Join(cnst.ActiveDir, f))
			continue
		}
		_, name := filepath.Split(f)
		// remove the .signed suffix if present
		name = strings.TrimSuffix(name, ".signed")
		fileWriteName := filepath.Join(cnst.EfiDir, fmt.Sprintf("EFI/boot/%s", name))
		g.config.Logger.Debugf("Copying %s to %s", f, fileWriteName)

		// Try to find the paths give until we succeed
		fileContent, err := g.config.Fs.ReadFile(filepath.Join(cnst.ActiveDir, f))

		if err != nil {
			g.config.Logger.Warnf("error reading %s: %s", filepath.Join(cnst.ActiveDir, f), err)
			continue
		}
		err = g.config.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
		if err != nil {
			return fmt.Errorf("error writing %s: %s", fileWriteName, err)
		}
		shimDone = true

		// Copy the shim content  to the fallback name so the system boots from fallback. This means that we do not create
		// any bootloader entries, so our recent installation has the lower priority if something else is on the bootloader
		writeShim := cnst.GetFallBackEfi(g.config.Arch)
		err = g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/boot/", writeShim), fileContent, cnst.FilePerm)
		if err != nil {
			return fmt.Errorf("could not write shim file %s at dir %s", writeShim, cnst.EfiDir)
		}
		break
	}
	if !shimDone {
		g.config.Logger.Debugf("List of shim files searched for: %s", shimFiles)
		return fmt.Errorf("could not find any shim file to copy")
	}
	return nil
}

func (g Grub) copyGrub() error {
	// Get standard grub efi file paths (includes Hadron paths)
	grubFiles := utils.GetEfiGrubFiles(g.config.Arch)

	grubDone := false
	for _, f := range grubFiles {
		_, err := g.config.Fs.Stat(filepath.Join(constants.ActiveDir, f))
		if err != nil {
			g.config.Logger.Debugf("skip copying %s: not found", filepath.Join(constants.ActiveDir, f))
			continue
		}

		_, name := filepath.Split(f)
		// remove the .signed suffix if present
		name = strings.TrimSuffix(name, ".signed")
		fileWriteName := filepath.Join(cnst.EfiDir, fmt.Sprintf("EFI/boot/%s", name))

		g.config.Logger.Debugf("Copying %s to %s", f, fileWriteName)

		// Try to find the paths give until we succeed
		fileContent, err := g.config.Fs.ReadFile(filepath.Join(cnst.ActiveDir, f))
		if err != nil {
			g.config.Logger.Warnf("error reading %s: %s", filepath.Join(cnst.ActiveDir, f), err)
			continue
		}
		err = g.config.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
		if err != nil {
			return fmt.Errorf("error writing %s: %s", fileWriteName, err)
		}
		if g.config.Arch == cnst.ArchRiscv64 {
			fallback := cnst.GetFallBackEfi(g.config.Arch)
			err = g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/boot/", fallback), fileContent, cnst.FilePerm)
			if err != nil {
				return fmt.Errorf("could not write grub fallback %s at dir %s: %w", fallback, cnst.EfiDir, err)
			}
		}
		grubDone = true
		break
	}
	if !grubDone {
		g.config.Logger.Debugf("List of grub files searched for: %s", grubFiles)
		return fmt.Errorf("could not find any grub efi file to copy")
	}
	return nil
}

// writeEfiGrubCfg writes the grub.cfg file in EFI that chainloads the grub.cfg in state
// It also handles Ubuntu-specific workarounds if the flavor is ubuntu
func (g Grub) writeEfiGrubCfg(stateLabel, systemgrub, flavor string) error {
	// Add grub.cfg in EFI that chainloads the grub.cfg in state
	// Notice that we set the config to /grub2/grub.cfg which means the above we need to copy the file from
	// the installation source into that dir
	grubCfgContent := []byte(fmt.Sprintf("search --no-floppy --label --set=root %s\nset prefix=($root)/%s\nconfigfile ($root)/%s/grub.cfg", stateLabel, systemgrub, systemgrub))
	err := g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/boot/grub.cfg"), grubCfgContent, cnst.FilePerm)
	if err != nil {
		return fmt.Errorf("error writing %s: %s", filepath.Join(cnst.EfiDir, "EFI/boot/grub.cfg"), err)
	}
	// Ubuntu efi searches for the grub.cfg file under /EFI/ubuntu/grub.cfg while we store it under /boot/grub2/grub.cfg
	// workaround this by copying it there as well
	// read the kairos-release from the rootfs to know if we are creating a ubuntu based iso
	if strings.Contains(strings.ToLower(flavor), "ubuntu") {
		g.config.Logger.Infof("Ubuntu based ISO detected, copying grub.cfg to /EFI/ubuntu/grub.cfg")
		err = fsutils.MkdirAll(g.config.Fs, filepath.Join(cnst.EfiDir, "EFI/ubuntu/"), constants.DirPerm)
		if err != nil {
			g.config.Logger.Errorf("Failed writing grub.cfg: %v", err)
			return err
		}
		err = g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/ubuntu/grub.cfg"), grubCfgContent, constants.FilePerm)
		if err != nil {
			g.config.Logger.Errorf("Failed writing grub.cfg: %v", err)
			return err
		}
	}
	return nil
}

// ReadPersistentVariables will read a grub env file and parse the values
func ReadPersistentVariables(grubEnvFile string, c *sdkConfig.Config) (map[string]string, error) {
	vars := make(map[string]string)

	f, err := c.Fs.ReadFile(grubEnvFile)
	if err != nil {
		return vars, err
	}
	for a := range strings.SplitSeq(string(f), "\n") {
		// comment or fillup, so skip
		if strings.HasPrefix(a, "#") {
			continue
		}
		if a == "" {
			continue
		}
		splitted := strings.Split(a, "=")
		if len(splitted) >= 2 {
			vars[splitted[0]] = strings.Join(splitted[1:], "=")
		} else {
			return vars, fmt.Errorf("invalid format for %s", a)
		}
	}
	return vars, nil
}
