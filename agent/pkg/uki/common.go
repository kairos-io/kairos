package uki

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"
	"github.com/sanity-io/litter"
)

const UnassignedArtifactRole = "norole"

// overwriteArtifactSetRole first deletes all artifacts prefixed with newRole
// (because they are going to be replaced, e.g. old "passive") and then installs
// the artifacts prefixed with oldRole as newRole.
// E.g. removes "passive" and moved "active" to "passive"
// This is a step that should happen before a new passive is installed on upgrades.
func overwriteArtifactSetRole(fs sdkFs.KairosFS, dir, oldRole, newRole string, logger sdkLogger.KairosLogger) error {
	if err := removeArtifactSetWithRole(fs, dir, newRole); err != nil {
		return fmt.Errorf("deleting role %s: %w", newRole, err)
	}

	if err := copyArtifactSetRole(fs, dir, oldRole, newRole, logger); err != nil {
		return fmt.Errorf("copying artifact set role from %s to %s: %w", oldRole, newRole, err)
	}

	return nil
}

// copy the source file but rename the base name to as
func copyArtifact(fs sdkFs.KairosFS, source, oldRole, newRole string) (string, error) {
	dir := filepath.Dir(source)
	base := filepath.Base(source)

	// Replace the substring in the base name
	newBase := strings.ReplaceAll(base, oldRole, newRole)

	// Join the directory and the new base name
	newName := filepath.Join(dir, newBase)

	return newName, fsutils.Copy(fs, source, newName)
}

func removeArtifactSetWithRole(fs sdkFs.KairosFS, artifactDir, role string) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if !info.IsDir() && strings.HasPrefix(info.Name(), role) {
			return os.Remove(path)
		}

		return nil
	})
}

func copyArtifactSetRole(fs sdkFs.KairosFS, artifactDir, oldRole, newRole string, logger sdkLogger.KairosLogger) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasPrefix(info.Name(), oldRole) {
			return nil
		}

		newPath, err := copyArtifact(fs, path, oldRole, newRole)
		if err != nil {
			return fmt.Errorf("copying artifact from %s to %s: %w", path, newPath, err)
		}
		if strings.HasSuffix(path, ".conf") {
			if err := replaceRoleInKey(newPath, "efi", oldRole, newRole, logger); err != nil {
				// Maybe this is a newer system where we use the "uki" key instead of "efi"
				if err := replaceRoleInKey(newPath, "uki", oldRole, newRole, logger); err != nil {
					return err
				}
			}
			if err := replaceConfTitle(newPath, newRole); err != nil {
				return err
			}
		}

		return nil
	})
}

func replaceRoleInKey(path, key, oldRole, newRole string, logger sdkLogger.KairosLogger) (err error) {
	// Extract the values
	conf, err := sdkutils.SystemdBootConfReader(path)
	if err != nil {
		logger.Errorf("Error reading conf file %s: %s", path, err)
		return err
	}
	logger.Debugf("Conf file %s has values %v", path, litter.Sdump(conf))

	_, hasKey := conf[key]
	if !hasKey {
		return fmt.Errorf("no %s entry in .conf file", key)
	}

	conf[key] = strings.ReplaceAll(conf[key], oldRole, newRole)
	newContents := ""
	for k, v := range conf {
		newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
	}
	logger.Debugf("Conf file %s new values %v", path, litter.Sdump(conf))

	return os.WriteFile(path, []byte(newContents), os.ModePerm)
}

func replaceConfTitle(path, role string) error {
	conf, err := sdkutils.SystemdBootConfReader(path)
	if err != nil {
		return err
	}

	if len(conf["title"]) == 0 {
		return errors.New("no title in .conf file")
	}

	newTitle, err := constants.BootTitleForRole(role, conf["title"])
	if err != nil {
		return err
	}

	conf["title"] = newTitle
	newContents := ""
	for k, v := range conf {
		newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
	}

	return os.WriteFile(path, []byte(newContents), os.ModePerm)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		panic(err)
	}
	defer destinationFile.Close()

	if _, err = io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	// Flushes any buffered data to the destination file
	if err = destinationFile.Sync(); err != nil {
		return err
	}

	if err = sourceFile.Close(); err != nil {
		return err
	}

	return destinationFile.Close()
}

func AddSystemdConfSortKey(fs sdkFs.KairosFS, artifactDir string, log sdkLogger.KairosLogger) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Only do files that are conf files but dont match the loader.conf
		if !info.IsDir() && filepath.Ext(path) == ".conf" && !strings.Contains(info.Name(), "loader.conf") {
			log.Logger.Debug().Str("path", path).Msg("Adding sort key to file")
			conf, err := sdkutils.SystemdBootConfReader(path)
			if err != nil {
				log.Errorf("Error reading conf file to extract values %s: %s", conf, path)
			}
			// Now check and put the proper sort key
			var sortKey string
			// If we have 2 different files that start with active, like with the extra-cmdline, how do we set this?
			// Ideally if they both have the same sort key, they will be sorted by name so the single one will be first
			// and the extra-cmdline will be second. This is the best we can do currently without making this a mess
			// Maybe we need the bootentry command to also set the sort key somehow?
			switch {
			case strings.Contains(info.Name(), "active"):
				sortKey = "0001"
			case strings.Contains(info.Name(), "passive"):
				sortKey = "0002"
			case strings.Contains(info.Name(), "recovery"):
				sortKey = "0003"
			case strings.Contains(info.Name(), "statereset"):
				sortKey = "0004"
			default: // Anything that dont matches, goes to the bottom
				sortKey = "0010"
			}
			conf["sort-key"] = sortKey
			newContents := ""
			for k, v := range conf {
				newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
			}
			log.Logger.Trace().Str("contents", litter.Sdump(conf)).Str("path", path).Msg("Final values for conf file")

			return os.WriteFile(path, []byte(newContents), 0600)
		}

		return nil
	})
}

func removeDefaultKeysFromLoaderConf(fs sdkFs.KairosFS, efiDir string, logger sdkLogger.KairosLogger) error {
	systemdConf, err := utils.SystemdBootConfReader(fs, filepath.Join(efiDir, "loader/loader.conf"))
	if err != nil {
		logger.Errorf("could not read loader.conf: %s", err)
		return err
	}

	if _, ok := systemdConf["default"]; ok {
		// Only remove if it points to active, otherwise leave it alone and warn about a potential boot loop
		if strings.Contains(systemdConf["default"], "active") {
			logger.Infof("removing default key from loader.conf")
			delete(systemdConf, "default")
			err = utils.SystemdBootConfWriter(fs, filepath.Join(efiDir, "loader/loader.conf"), systemdConf)
			if err != nil {
				logger.Errorf("could not write loader.conf: %s", err)
				return err
			}
		} else {
			logger.Warnf("loader.conf default key points to %s\n"+
				"Its not recommended to have a default key set to avoid an entry being bad and systemd-boot trying to boot from it indefinitevely, causing a boot loop\n"+
				"We recommend removing it manually if possible. Please check the Kairos docs for more information.", systemdConf["default"])
		}
	}

	return nil
}

// upgradeEfiKeysInLoaderEntries checks the systemd-boot version and if its >= 259
// it upgrades the efi keys in the loader entries to use the new "uki" key instead of "efi"
// so it changes "efi /EFI/kairos/active.efi" to "uki /EFI/kairos/active.efi"
func upgradeEfiKeysInLoaderEntries(arch string, fs sdkFs.KairosFS, efiDir string, logger sdkLogger.KairosLogger) error {
	// Check sdboot version
	sdboot := "BOOTX64.EFI"
	if arch == "arm64" {
		sdboot = "BOOTAA64.EFI"
	}
	majorVer, err := utils.GetMajorImageVersion(filepath.Join(efiDir, "EFI/BOOT/", sdboot))
	if err != nil {
		logger.Warnf("could not get systemd-boot version, skipping efi key upgrade: %s", err)
		return nil
	}
	logger.Infof("Identified systemd-boot major version: %d", majorVer)
	if majorVer >= 259 {
		logger.Infof("systemd-boot version >= 259, upgrading efi keys in loader entries")
		return fsutils.WalkDirFs(fs, filepath.Join(efiDir, "loader/entries"), func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".conf" {
				return nil
			}
			logger.Debugf("upgrading efi keys in loader entry: %s", path)
			conf, err := utils.SystemdBootConfReader(fs, path)
			if err != nil {
				logger.Errorf("could not read loader entry %s: %s", path, err)
				return err
			}
			if val, ok := conf["efi"]; ok {
				logger.Debugf("found efi key in loader entry %s, upgrading to uki", path)
				delete(conf, "efi")
				conf["uki"] = val
				logger.Debugf("new loader entry conf: %s", litter.Sdump(conf))
				err = utils.SystemdBootConfWriter(fs, path, conf)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}

	return nil
}
