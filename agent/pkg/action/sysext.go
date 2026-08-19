package action

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"github.com/distribution/reference"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
)

// Implementation details for not trusted boot
// sysext are stored under
// /var/lib/kairos/extensions/
// confext are stored under
// /var/lib/kairos/confext/
// we link them to /var/lib/kairos/extensions/{active,passive} depending on where we want it to be enabled
// we link them to /var/lib/kairos/confext/{active,passive} depending on where we want it to be enabled
// Immucore on boot after mounting the persistent dir, will check those dirs\
// it will then create the proper links to them under /run/extensions or /run/confext
// This means they are enabled on boot and they are ephemeral, nothing is left behind in the actual sysext dirs
// This prevents us from having to clean up in different dirs, we can just do cleaning in our dirs (remove links)
// and on reboot they will not be enabled on boot
// So all the actions (list, upgrade, download, remove) will be done on the persistent dir
// And on boot we dinamycally link and enable them based on the boot type (active,passive) via immucore

// TODO: Check which extensions are running? is that possible?
// TODO: On disable we should check if the extension is running and refresh systemd-sysext? YES
// TODO: On remove we should check if the extension is running and refresh systemd-sysext? YES

const (
	sysext             = "sysext"
	confext            = "confext"
	sysextDir          = "/var/lib/kairos/extensions/"
	confExtDir         = "/var/lib/kairos/confexts/"
	sysextDirActive    = sysextDir + "active"
	sysextDirPassive   = sysextDir + "passive"
	sysextDirRecovery  = sysextDir + "recovery"
	sysextDirCommon    = sysextDir + "common"
	confExtDirActive   = confExtDir + "active"
	confExtDirPassive  = confExtDir + "passive"
	confExtDirRecovery = confExtDir + "recovery"
	confExtDirCommon   = confExtDir + "common"
	sysextRunDir       = "/run/extensions/"
	confExtRunDir      = "/run/confexts/"
	sysextCommand      = "systemd-sysext"
	confextCommand     = "systemd-confext"
)

// Extension represents a system/config extension
type Extension struct {
	Name     string
	Location string
}

func (s *Extension) String() string {
	return s.Name
}

func dirFromBootState(bootState, extType string) string {
	switch extType {
	case sysext:
		switch bootState {
		case "active":
			return sysextDirActive
		case "passive":
			return sysextDirPassive
		case "recovery":
			return sysextDirRecovery
		case "common":
			return sysextDirCommon
		default:
			return sysextDir
		}
	case confext:
		switch bootState {
		case "active":
			return confExtDirActive
		case "passive":
			return confExtDirPassive
		case "recovery":
			return confExtDirRecovery
		case "common":
			return confExtDirCommon
		default:
			return confExtDir
		}
	default:
		return ""
	}
}

// ListExtensions lists the system extensions in the given directory
// If none is passed then it shows the generic ones
func ListExtensions(cfg *sdkConfig.Config, bootState, extType string) ([]Extension, error) {
	return getDirExtensions(cfg, dirFromBootState(bootState, extType))
}

// getDirExtensions lists the system extensions in the given directory
func getDirExtensions(cfg *sdkConfig.Config, dir string) ([]Extension, error) {
	var out []Extension
	// get all the extensions in the sysextDir
	// Try to create the dir if it does not exist
	if _, err := cfg.Fs.Stat(dir); os.IsNotExist(err) {
		if err := vfs.MkdirAll(cfg.Fs, dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target dir %s: %w", dir, err)
		}
	}
	entries, err := cfg.Fs.ReadDir(dir)
	// We don't care if the dir does not exist, we just return an empty list
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".raw" {
			out = append(out, Extension{Name: entry.Name(), Location: filepath.Join(dir, entry.Name())})
		}
	}
	return out, nil
}

// GetExtension returns the system extension for a given name
func GetExtension(cfg *sdkConfig.Config, name, bootState, extType string) (Extension, error) {
	// Get a list of all installed system extensions
	installed, err := ListExtensions(cfg, bootState, extType)
	if err != nil {
		return Extension{}, err
	}
	// Check if the extension is installed
	// regex against the name
	re, err := regexp.Compile(name)
	if err != nil {
		return Extension{}, err
	}
	for _, ext := range installed {
		if re.MatchString(ext.Name) {
			return ext, nil
		}
	}
	// If not, return an error
	return Extension{}, fmt.Errorf("extension %s not found", name)
}

// EnableExtension enables a extension that is already in the system for a given bootstate
// It creates a symlink to the extension in the target dir according to the bootstate given
// It will create the target dir if it does not exist
// It will check if the extension is already enabled but not fail if it is
// It will check if the extension is installed
// If now is true, it will enable the extension immediately by linking it to the proper dir and refreshing the service
func EnableExtension(cfg *sdkConfig.Config, ext, bootState, extType string, now bool) error {
	// first check if the extension is installed
	extension, err := GetExtension(cfg, ext, "", extType)
	if err != nil {
		return err
	}

	targetDir := dirFromBootState(bootState, extType)

	// Check if the target dir exists and create it if it doesn't
	if _, err := cfg.Fs.Stat(targetDir); os.IsNotExist(err) {
		if err := vfs.MkdirAll(cfg.Fs, targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create target dir %s: %w", targetDir, err)
		}
	}

	// Check if the extension is already enabled
	enabled, err := GetExtension(cfg, ext, bootState, extType)
	// This doesnt fail if we have it already enabled
	if err == nil {
		if enabled.Name == extension.Name {
			cfg.Logger.Infof("Extension %s is already enabled in %s", extension.Name, bootState)
			return nil
		}
	}

	// Create a symlink to the extension in the target dir
	if err := cfg.Fs.Symlink(extension.Location, filepath.Join(targetDir, extension.Name)); err != nil {
		return fmt.Errorf("failed to create symlink for %s: %w", extension.Name, err)
	}
	cfg.Logger.Infof("Extension %s enabled in %s", extension.Name, bootState)

	if now {
		// Check if the boot state is the same as the one we are enabling
		// This is to avoid enabling the extension in the wrong boot state
		_, stateMatches := cfg.Fs.Stat(fmt.Sprintf("/run/cos/%s_mode", bootState))
		// TODO: Check in UKI?
		cfg.Logger.Logger.Debug().Str("boot_state", bootState).Str("filecheck", fmt.Sprintf("/run/cos/%s_state", bootState)).Msg("Checking boot state")
		if stateMatches == nil || bootState == "common" {
			linkTarget := sysextRunDir
			reloadTarget := sysextCommand
			if extType == confext {
				linkTarget = confExtRunDir
				reloadTarget = confextCommand
			}
			err = cfg.Fs.Symlink(filepath.Join(targetDir, extension.Name), filepath.Join(linkTarget, extension.Name))
			if err != nil {
				return fmt.Errorf("failed to create symlink for %s: %w", extension.Name, err)
			}
			cfg.Logger.Infof("Extension %s enabled in %s", extension.Name, linkTarget)
			// It makes the sysext/confext check the extension for a valid signature
			// Refresh systemd-sysext/confext by restarting the service. As the config is set via the service overrides to nice things
			output, err := cfg.Runner.Run("systemctl", "restart", reloadTarget)
			if err != nil {
				cfg.Logger.Logger.Err(err).Str("output", string(output)).Msgf("Failed to refresh %s", reloadTarget)
				return err
			}
			cfg.Logger.Infof("Extension %s merged by %s", extension.Name, reloadTarget)
		} else {
			cfg.Logger.Infof("Extension %s enabled in %s but not merged as we are currently not booted in %s", extension.Name, bootState, bootState)
		}

	}
	return nil
}

// DisableExtension disables a extension that is already in the system for a given bootstate
// It removes the symlink from the target dir according to the bootstate given
func DisableExtension(cfg *sdkConfig.Config, ext string, bootState, extType string, now bool) error {
	targetDir := dirFromBootState(bootState, extType)

	// Check if the target dir exists
	if _, err := cfg.Fs.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("target dir %s does not exist", targetDir)
	}

	// Check if the extension is enabled, do not fail if it is not
	extension, err := GetExtension(cfg, ext, bootState, extType)
	if err != nil {
		cfg.Logger.Infof("Extension %s is not enabled in %s", ext, bootState)
		return nil
	}

	// Remove the symlink
	if err := cfg.Fs.Remove(extension.Location); err != nil {
		return fmt.Errorf("failed to remove symlink for %s: %w", ext, err)
	}
	if now {
		// Check if the boot state is the same as the one we are disabling
		// This is to avoid disabling the extension in the wrong boot state
		_, stateMatches := cfg.Fs.Stat(fmt.Sprintf("/run/cos/%s_mode", bootState))
		cfg.Logger.Logger.Debug().Str("boot_state", bootState).Str("filecheck", fmt.Sprintf("/run/cos/%s_mode", bootState)).Msg("Checking boot state")
		if stateMatches == nil || bootState == "common" {
			linkTarget := sysextRunDir
			reloadTarget := sysextCommand
			if extType == confext {
				linkTarget = confExtRunDir
				reloadTarget = confextCommand
			}
			// Remove the symlink from /run/extensions if is in there
			cfg.Logger.Logger.Debug().Str("stat", filepath.Join(linkTarget, extension.Name)).Msg("Checking if symlink exists")
			_, stat := cfg.Fs.Readlink(filepath.Join(linkTarget, extension.Name))
			if stat == nil {
				err = cfg.Fs.Remove(filepath.Join(linkTarget, extension.Name))
				if err != nil {
					return fmt.Errorf("failed to remove symlink for %s: %w", extension.Name, err)
				}
				cfg.Logger.Infof("Extension %s disabled from %s", extension.Name, linkTarget)
				// Now that its removed we refresh systemd-sysext
				output, err := cfg.Runner.Run("systemctl", "restart", reloadTarget)
				if err != nil {
					cfg.Logger.Logger.Err(err).Str("output", string(output)).Msgf("Failed to refresh %s", reloadTarget)
					return err
				}
				cfg.Logger.Infof("Extension %s refreshed by %s", extension.Name, reloadTarget)
			} else {
				cfg.Logger.Logger.Info().Msgf("Extension not in %s, not refreshing", linkTarget)
			}
		} else {
			cfg.Logger.Infof("Extension %s disabled in %s but not refreshed as we are currently not booted in %s", extension.Name, bootState, bootState)
		}
	}
	cfg.Logger.Infof("Extension %s disabled in %s", ext, bootState)
	return nil
}

// InstallExtension installs a extension from a given URI
// It will download the extension and extract it to the target dir
// It will check if the extension is already installed before doing anything
func InstallExtension(cfg *sdkConfig.Config, uri, extType string) error {
	linkTarget := sysextDir
	if extType == confext {
		linkTarget = confExtDir
	}
	// Parse the URI
	download, err := parseURI(cfg, uri)
	if err != nil {
		return fmt.Errorf("failed to parse URI %s: %w", uri, err)
	}
	// Check if directory exists or create it
	if _, err := cfg.Fs.Stat(linkTarget); os.IsNotExist(err) {
		if err := vfs.MkdirAll(cfg.Fs, linkTarget, 0755); err != nil {
			return fmt.Errorf("failed to create target dir %s: %w", linkTarget, err)
		}
	}
	// Download the extension
	if err := download.Download(linkTarget); err != nil {
		return err
	}

	return nil
}

// RemoveExtension removes a extension from the system
// It will remove any symlinks to the extension
// Then it will remove the extension
// It will check if the extension is installed before doing anything
func RemoveExtension(cfg *sdkConfig.Config, extension, extType string, now bool) error {
	// Check if the extension is installed
	installed, err := GetExtension(cfg, extension, "", extType)
	if err != nil {
		return err
	}
	if installed.Name == "" && installed.Location == "" {
		cfg.Logger.Infof("Extension %s is not installed", extension)
		return nil
	}
	// Check if the extension is enabled in active or passive
	for _, state := range []string{"active", "passive", "recovery", "common"} {
		enabled, err := GetExtension(cfg, extension, state, extType)
		if err == nil {
			// Remove the symlink
			if err := cfg.Fs.Remove(enabled.Location); err != nil {
				return fmt.Errorf("failed to remove symlink for %s: %w", enabled.Name, err)
			}
			cfg.Logger.Infof("Extension %s disabled from %s", enabled.Name, state)
		}
	}
	// Remove the extension
	if err := cfg.Fs.RemoveAll(installed.Location); err != nil {
		return fmt.Errorf("failed to remove extension %s: %w", installed.Name, err)
	}

	if now {
		linkTarget := sysextRunDir
		reloadTarget := sysextCommand
		if extType == confext {
			linkTarget = confExtRunDir
			reloadTarget = confextCommand
		}
		// Here as we are removing the extension we need to check if its in /run/extensions
		// We dont care about the bootState because we are removing it from all
		_, stat := cfg.Fs.Readlink(filepath.Join(linkTarget, installed.Name))
		if stat == nil {
			err = cfg.Fs.Remove(filepath.Join(linkTarget, installed.Name))
			if err != nil {
				return fmt.Errorf("failed to remove symlink for %s: %w", installed.Name, err)
			}
			cfg.Logger.Infof("Extension %s removed from %s", installed.Name, linkTarget)
			// Now that its removed we refresh systemd-sysext
			output, err := cfg.Runner.Run("systemctl", "restart", reloadTarget)
			if err != nil {
				cfg.Logger.Logger.Err(err).Str("output", string(output)).Msgf("Failed to refresh %s", reloadTarget)
				return err
			}
			cfg.Logger.Infof("Extension %s refreshed by %s", installed.Name, reloadTarget)
		} else {
			cfg.Logger.Logger.Info().Msgf("Extension not in %s, not refreshing", linkTarget)
		}
	}

	cfg.Logger.Infof("Extension %s removed", installed.Name)
	return nil
}

// ParseURI parses a URI and returns a SourceDownload
// implementation based on the scheme of the URI
func parseURI(cfg *sdkConfig.Config, uri string) (SourceDownload, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	value := u.Opaque
	if value == "" {
		value = filepath.Join(u.Host, u.Path)
	}
	switch scheme {
	case "oci", "docker", "container":
		n, err := reference.ParseNormalizedNamed(value)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference %s", value)
		} else if reference.IsNameOnly(n) {
			value += ":latest"
		}
		return &dockerSource{value, cfg}, nil
	case "file":
		return &fileSource{value, cfg}, nil
	case "http", "https":
		// Pass the full uri including the protocol
		return &httpSource{uri, cfg}, nil
	default:
		return nil, fmt.Errorf("invalid URI reference %s", uri)
	}
}

// SourceDownload is an interface for downloading system extensions
// from different sources. It allows for different implementations
// for different sources of system extensions, such as files, directories,
// or docker images. The interface defines a single method, Download,
// which takes a destination path as an argument and returns an error
type SourceDownload interface {
	Download(string) error
}

// fileSource is a struct that implements the SourceDownload interface
// for downloading system extensions from a file. It has two fields,
// uri, which is the URI of the file to be downloaded and cfg which points to the Config
// The Download method takes a destination path as an argument and returns an error if the
// download fails.
type fileSource struct {
	uri string
	cfg *sdkConfig.Config
}

// Download streams the file to the destination with bounded memory usage
// Uses io.Copy instead of buffering the entire file to avoid OOM on pods with limited memory
func (f *fileSource) Download(dst string) error {
	// Open source file for reading
	srcFile, err := f.cfg.Fs.Open(f.uri)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", f.uri, err)
	}
	defer srcFile.Close()

	// Get file info for permissions
	stat, err := f.cfg.Fs.Stat(f.uri)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", f.uri, err)
	}

	// Create destination file with same permissions
	dstFile := filepath.Join(dst, filepath.Base(f.uri))
	dstFileHandle, err := f.cfg.Fs.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dstFile, err)
	}
	defer dstFileHandle.Close()

	// Stream copy (bounded memory usage)
	f.cfg.Logger.Logger.Debug().Str("uri", f.uri).Str("target", dstFile).Msg("Copying system extension")
	if _, err := io.Copy(dstFileHandle, srcFile); err != nil {
		return fmt.Errorf("failed to copy file %s to %s: %w", f.uri, dstFile, err)
	}

	return nil
}

type httpSource struct {
	uri string
	cfg *sdkConfig.Config
}

func (h httpSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	h.cfg.Logger.Logger.Debug().Str("uri", h.uri).Str("target", filepath.Join(s, filepath.Base(h.uri))).Msg("Downloading system extension")
	return h.cfg.Client.GetURL(sdkLogger.NewNullLogger(), h.uri, filepath.Join(s, filepath.Base(h.uri)))
}

type dockerSource struct {
	uri string
	cfg *sdkConfig.Config
}

func (d dockerSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	err := d.cfg.ImageExtractor.ExtractImage(d.uri, s, "")
	if err != nil {
		return err
	}
	return nil
}
