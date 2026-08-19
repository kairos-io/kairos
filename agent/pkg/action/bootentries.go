package action

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/erikgeiser/promptkit/confirmation"
	"github.com/erikgeiser/promptkit/selection"
	"github.com/foxboron/go-uefi/efi/attributes"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	"golang.org/x/sys/unix"
)

// SelectBootEntry sets the default boot entry to the selected entry
// This is the entrypoint for the bootentry action with --select flag
// also other actions can call this function to set the default boot entry
func SelectBootEntry(cfg *sdkConfig.Config, entry string) error {
	if utils.IsUkiWithFs(cfg.Fs) {
		return selectBootEntrySystemd(cfg, entry)
	}

	return selectBootEntryGrub(cfg, entry)
}

// ListBootEntries lists the boot entries available in the system and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func ListBootEntries(cfg *sdkConfig.Config) error {
	if utils.IsUkiWithFs(cfg.Fs) {
		return listBootEntriesSystemd(cfg)
	}

	return listBootEntriesGrub(cfg)
}

// writeStateGrubenvWhenOemEncrypted writes grubenv variables to STATE partition's grubenv
// when OEM is encrypted (we skip writing to STATE when OEM is not encrypted to maintain backwards compatibility).
// This is needed because GRUB can't read the OEM partition before decryption. However, note that setting
// next boot entry will NOT work reliably when OEM is encrypted because GRUB's grub.cfg
// (https://raw.githubusercontent.com/kairos-io/packages/refs/heads/main/packages/static/grub-config/files/grub.cfg)
// loads OEM env first (if available), then STATE env (which overwrites OEM). Even though
// we write to both OEM and STATE grubenv when OEM is encrypted, GRUB loads STATE env after
// OEM (which overwrites OEM), so if STATE grubenv has a stale value, it will overwrite the
// next_entry we set. There are more pending TODOs around encrypted OEM support:
// https://github.com/kairos-io/kairos-agent/blob/485d9f7ec23b84ccf5af5ba0e58569b10011ad08/internal/agent/hooks/gruboptions.go#L49-L51
func writeStateGrubenvWhenOemEncrypted(cfg *sdkConfig.Config, vars map[string]string) {
	cfg.Logger.Debugf("OEM is encrypted, also writing to STATE partition's grubenv")
	// Mount STATE partition and remount as RW (it's typically mounted as RO)
	stateDevice, err := utils.GetDeviceByLabel(cfg, cnst.StateLabel, 3)
	if err != nil {
		cfg.Logger.Debugf("Could not get STATE device by label: %s", err)
		return
	}
	// Ensure STATE is mounted
	_ = cfg.Mounter.Mount(stateDevice, cnst.StateDir, "auto", []string{})
	// Remount as RW
	remountErr := cfg.Mounter.Mount(stateDevice, cnst.StateDir, "auto", []string{"remount", "rw"})
	if remountErr != nil {
		cfg.Logger.Debugf("Could not remount STATE partition as RW: %s", remountErr)
	}

	stateGrubenvPath := filepath.Join(cnst.StateDir, cnst.GrubEnv)
	err = utils.SetPersistentVariables(stateGrubenvPath, vars, cfg)
	if err != nil {
		cfg.Logger.Warnf("Could not set default boot entry in STATE grubenv (non-critical): %s", err)
	} else {
		cfg.Logger.Debugf("Successfully set next_entry in STATE grubenv")
	}

	// Remount STATE as RO
	_ = cfg.Mounter.Mount(stateDevice, cnst.StateDir, "auto", []string{"remount", "ro"})
}

// selectBootEntryGrub sets the default boot entry to the selected entry by modifying /oem/grubenv
// also validates that the entry exists in our list of entries
func selectBootEntryGrub(cfg *sdkConfig.Config, entry string) error {
	// Validate if entry exists
	entries, err := listGrubEntries(cfg)
	if err != nil {
		return err
	}
	// Check that entry exists in the entries list
	err = entryInList(cfg, entry, entries)
	if err != nil {
		return err
	}
	cfg.Logger.Infof("Setting default boot entry to %s", entry)
	vars := map[string]string{
		"next_entry": entry,
	}

	// Check if COS_OEM is encrypted
	oemEncrypted := false
	if cfg.Install != nil && len(cfg.Install.Encrypt) > 0 {
		for _, part := range cfg.Install.Encrypt {
			if part == cnst.OEMLabel {
				oemEncrypted = true
				break
			}
		}
	}

	// Write to the appropriate grubenv file based on whether OEM is encrypted
	// When OEM is not encrypted: only write to OEM partition (grubenv) to avoid having two grubenv files
	// When OEM is encrypted: only write to STATE partition (grubenv) since GRUB can't read OEM before decryption
	if oemEncrypted {
		writeStateGrubenvWhenOemEncrypted(cfg, vars)
	} else {
		// Set the default entry to the selected entry on /oem/grubenv
		err = utils.SetPersistentVariables("/oem/grubenv", vars, cfg)
		if err != nil {
			cfg.Logger.Errorf("could not set default boot entry: %s\n", err)
			return err
		}
	}

	cfg.Logger.Infof("Default boot entry set to %s", entry)
	return nil
}

// getSystemdBootMajorVersion returns the MajorImageVersion of the systemd-boot EFI binary
// on the given EFI mount point. Returns 0 when the binary cannot be read (e.g. on
// non-EFI systems). Declared as a variable so it can be overridden in tests.
var getSystemdBootMajorVersion = func(efiMountPoint string) uint16 {
	sdboot := "BOOTX64.EFI"
	if runtime.GOARCH == "arm64" {
		sdboot = "BOOTAA64.EFI"
	}
	ver, err := utils.GetMajorImageVersion(filepath.Join(efiMountPoint, "EFI/BOOT", sdboot))
	if err != nil {
		return 0
	}
	return ver
}

// findEntryWithAssessment searches the loader/entries directory for a .conf file whose
// name matches "<confBaseName>+N.conf" or "<confBaseName>+N-M.conf" (systemd boot
// assessment format). This is needed for systemd-boot 256, which uses the full filename
// including the assessment suffix as the entry ID in LoaderEntryOneShot. Later versions
// drop the assessment from the ID.
// Returns the base name (without .conf) of the matching file, or confBaseName unchanged
// if no file with an assessment suffix is found. Returns an error if the directory cannot
// be read or if multiple assessment files are found for the same base name.
func findEntryWithAssessment(cfg *sdkConfig.Config, efiMountPoint, confBaseName string) (string, error) {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(confBaseName) + `\+\d+(-\d+)?\.conf$`)
	entriesDir := filepath.Join(efiMountPoint, "loader/entries")
	var matches []string

	walkErr := fsutils.WalkDirFs(cfg.Fs, entriesDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if re.MatchString(info.Name()) {
			matches = append(matches, strings.TrimSuffix(info.Name(), ".conf"))
		}
		return nil
	})
	if walkErr != nil {
		return confBaseName, fmt.Errorf("failed to inspect %s for assessed boot entry %q: %w", entriesDir, confBaseName, walkErr)
	}
	if len(matches) > 1 {
		return confBaseName, fmt.Errorf("ambiguous boot assessment: multiple files match %q in %s: %v", confBaseName, entriesDir, matches)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return confBaseName, nil
}

// selectBootEntrySystemd sets the one shot boot entry to the selected entry by setting it in the LoaderEntryOneShot efivar
func selectBootEntrySystemd(cfg *sdkConfig.Config, entry string) error {
	cfg.Logger.Infof("Setting default boot entry to %s", entry)

	// Get EFI partition
	efiPartition, err := partitions.GetEfiPartition(&cfg.Logger)
	if err != nil {
		return err
	}
	// Validate entry exists
	entries, err := listSystemdEntries(cfg, efiPartition)
	if err != nil {
		return err

	}
	originalEntries := entries
	// when there are only 4 entries, we can assume they are either cos (which will be replaced eventually), fallback, recovery or statereset
	if len(entries) == len(cnst.UkiDefaultMenuEntries()) {
		entries = cnst.UkiDefaultMenuEntries()
	}

	// Check that entry exists in the entries list
	err = entryInList(cfg, entry, entries)
	// we also accept "active" as a selection so we can migrate eventually from cos
	if err != nil && !strings.HasPrefix(entry, "active") {
		return err
	}

	// Check if efi is RW or RO
	if utils.IsMountReadOnly(efiPartition.MountPoint) {
		// Mount it RW
		err = cfg.Syscall.Mount("", efiPartition.MountPoint, "", syscall.MS_REMOUNT, "")
		if err != nil {
			cfg.Logger.Errorf("could not remount EFI partition: %s", err)
			return err
		}
		// Remount it RO when finished
		defer func(source string, target string, fstype string, flags uintptr, data string) {
			err = cfg.Syscall.Mount(source, target, fstype, flags, data)
			if err != nil {
				cfg.Logger.Errorf("could not remount EFI partition as RO: %s", err)
			}
		}("", efiPartition.MountPoint, "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")
	}

	originalEntry := entry
	if !reflect.DeepEqual(originalEntries, entries) {
		// since we temporarily allow also active, here we need to first set entry to "cos" so it will match with the originalEntries
		if strings.HasPrefix(entry, "active") {
			entry = "cos"
		}
		for _, e := range originalEntries {
			if strings.HasPrefix(e, entry) {
				entry = e
			}
		}
	}
	bootConfigName, err := bootNameToSystemdConf(entry)
	if err != nil {
		return err
	}

	// Workaround for systemd-boot 256: the LoaderEntryOneShot EFI variable must
	// contain the full filename including the boot assessment suffix
	// (e.g. "active+3.conf"). Version 257+ dropped the assessment from the entry ID.
	if getSystemdBootMajorVersion(efiPartition.MountPoint) == 256 {
		cfg.Logger.Debugf("systemd-boot 256 detected, resolving boot entry with assessment suffix")
		bootConfigName, err = findEntryWithAssessment(cfg, efiPartition.MountPoint, bootConfigName)
		if err != nil {
			cfg.Logger.Errorf("could not resolve boot entry with assessment suffix: %s", err)
			return err
		}
		cfg.Logger.Debugf("resolved boot config name: %s", bootConfigName)
	}

	err = WriteOneShotEfiVar(cfg, fmt.Sprintf("%s.conf", bootConfigName))
	if err != nil {
		cfg.Logger.Errorf("could not write EFI variable: %s", err)
		return err
	}
	cfg.Logger.Infof("Default boot entry set to %s", originalEntry)
	return err
}

// WriteOneShotEfiVar writes the LoaderEntryOneShot efi variable with the selected boot entry
// Works with systemd-boot >= 256. For version 256 the value written must be the full filename
// including the boot assessment suffix and the .conf extension
// (e.g. "active+3.conf"); from version 257 onward the assessment suffix
// is omitted and only the bare conf name is used (e.g. "active.conf").
func WriteOneShotEfiVar(cfg *sdkConfig.Config, data string) error {
	efivar := "/sys/firmware/efi/efivars/LoaderEntryOneShot-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

	err := clearImmutable(efivar)
	if err != nil {
		return err
	}
	attrs := attributes.EFI_VARIABLE_NON_VOLATILE | attributes.EFI_VARIABLE_BOOTSERVICE_ACCESS | attributes.EFI_VARIABLE_RUNTIME_ACCESS

	f, err := cfg.Fs.OpenFile(efivar, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("couldn't open file: %w", err)
	}
	defer f.Close()

	buf := append(attrs.Bytes(), EncondeUtf16LEStringNullTerminated(data)...)
	if n, err := f.Write(buf); err != nil {
		return fmt.Errorf("couldn't write efi variable: %w", err)
	} else if n != len(buf) {
		return errors.New("could not write the entire buffer")
	}

	return nil
}

// ReadOneShotEfiVar reads the one shot loader efivar and returns the value as a string
func ReadOneShotEfiVar(cfg *sdkConfig.Config) (string, error) {
	efivar := "/sys/firmware/efi/efivars/LoaderEntryOneShot-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

	f, err := cfg.Fs.OpenFile(efivar, os.O_RDONLY, 0)
	if err != nil {
		return "", fmt.Errorf("couldn't open file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("couldn't stat file: %w", err)
	}

	size := stat.Size()
	buf := make([]byte, size)
	n, err := f.Read(buf)
	if err != nil {
		return "", fmt.Errorf("couldn't read efi variable: %w", err)
	} else if int64(n) != size {
		return "", errors.New("could not read the entire buffer")
	}

	// First 4 bytes are attributes so we can drop them
	return ReadUtf16LEStringNullTerminated(buf[4:]), nil
}

func ReadUtf16LEStringNullTerminated(b []byte) string {
	// Decode UTF-16LE
	u := make([]uint16, len(b)/2)
	for i := 0; i < len(u); i++ {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}

	// Find NUL terminator
	var end int
	for end = 0; end < len(u); end++ {
		if u[end] == 0 {
			break
		}
	}

	return string(utf16.Decode(u[:end]))
}

// EncondeUtf16LEStringNullTerminated encodes a string to UTF-16LE with a NUL terminator
// Dont ask me, this is what systemd-boot expects
func EncondeUtf16LEStringNullTerminated(s string) []byte {
	// Encode runes to UTF-16
	u := utf16.Encode([]rune(s))
	// Add explicit NUL terminator
	u = append(u, 0)

	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	return b
}

// clearImmutable removes the immutable flag from a file
// needed for efivars files that are immutable by default
func clearImmutable(path string) error {
	const (
		fsIOCGetflags = 0x80086601
		fsIOCSetflags = 0x40086602
		fsImmutableFl = 0x00000010
	)
	fd, err := unix.Open(path, unix.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}
	defer unix.Close(fd)

	var flags int
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), fsIOCGetflags, uintptr(unsafe.Pointer(&flags))); errno != 0 {
		return errno
	}

	if flags&fsImmutableFl == 0 {
		return nil
	}

	flags &^= fsImmutableFl
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), fsIOCSetflags, uintptr(unsafe.Pointer(&flags))); errno != 0 {
		return errno
	}
	return nil
}

// listBootEntriesGrub lists the boot entries available in the grub config files
// and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func listBootEntriesGrub(cfg *sdkConfig.Config) error {
	entries, err := listGrubEntries(cfg)
	if err != nil {
		return err
	}
	// create a selector
	selector := selection.New("Select Next Boot Entry", entries)
	selector.Filter = nil        // Remove the filter
	selector.ResultTemplate = `` // Do not print the result as we are asking for confirmation afterwards
	selected, _ := selector.RunPrompt()
	c := confirmation.New("Are you sure you want to change the boot entry to "+selected, confirmation.Yes)
	c.ResultTemplate = ``
	confirm, err := c.RunPrompt()
	if confirm {
		return SelectBootEntry(cfg, selected)
	}
	return err
}

func systemdConfToBootName(conf string) (string, error) {
	if !strings.HasSuffix(conf, ".conf") {
		return "", fmt.Errorf("unknown systemd-boot conf: %s", conf)
	}

	fileName := strings.TrimSuffix(conf, ".conf")

	// Remove the boot assesment from the name we show
	re := regexp.MustCompile(`\+\d+(-\d+)?$`)
	fileName = re.ReplaceAllString(fileName, "")

	if strings.HasPrefix(fileName, "active") {
		bootName := "cos"
		confName := strings.TrimPrefix(fileName, "active")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	if strings.HasPrefix(fileName, "passive") {
		bootName := "fallback"
		confName := strings.TrimPrefix(fileName, "passive")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	if strings.HasPrefix(conf, "recovery") {
		bootName := "recovery"
		confName := strings.TrimPrefix(fileName, "recovery")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	if strings.HasPrefix(conf, "statereset") {
		bootName := "statereset"
		confName := strings.TrimPrefix(fileName, "statereset")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	return strings.ReplaceAll(fileName, "_", " "), nil
}

// bootNameToSystemdConf converts a boot name to a systemd-boot conf file name
// skips the .conf extension
func bootNameToSystemdConf(name string) (string, error) {
	differenciator := ""

	if strings.HasPrefix(name, "cos") {
		if name != "cos" {
			differenciator = "_" + strings.TrimPrefix(name, "cos ")
		}
		return "active" + differenciator, nil
	}

	if strings.HasPrefix(name, "active") {
		if name != "active" {
			differenciator = "_" + strings.TrimPrefix(name, "active ")
		}
		return "active" + differenciator, nil
	}

	if strings.HasPrefix(name, "fallback") {
		if name != "fallback" {
			differenciator = "_" + strings.TrimPrefix(name, "fallback ")
		}
		return "passive" + differenciator, nil
	}

	if strings.HasPrefix(name, "recovery") {
		if name != "recovery" {
			differenciator = "_" + strings.TrimPrefix(name, "recovery ")
		}
		return "recovery" + differenciator, nil
	}

	if strings.HasPrefix(name, "statereset") {
		if name != "statereset" {
			differenciator = "_" + strings.TrimPrefix(name, "statereset ")
		}
		return "statereset" + differenciator, nil
	}

	return strings.ReplaceAll(name, " ", "_"), nil
}

// listBootEntriesSystemd lists the boot entries available in the systemd-boot config files
// and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func listBootEntriesSystemd(cfg *sdkConfig.Config) error {
	var err error
	e := elemental.NewElemental(cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Get EFI partition
	efiPartition, err := partitions.GetEfiPartition(&cfg.Logger)
	if err != nil {
		return err
	}
	// mount if not mounted
	if mounted, err := utils.IsMounted(cfg, efiPartition); !mounted && err == nil {
		if efiPartition.MountPoint == "" {
			efiPartition.MountPoint = cnst.EfiDir
		}
		err = e.MountPartition(efiPartition)
		if err != nil {
			cfg.Logger.Errorf("could not mount EFI partition: %s", err)
			return err
		}
		cleanup.Push(func() error { return e.UnmountPartition(efiPartition) })
	}

	entries, err := listSystemdEntries(cfg, efiPartition)
	if err != nil {
		return err
	}

	selector := selection.New("Select Boot Entry", entries)
	selector.Filter = nil        // Remove the filter
	selector.ResultTemplate = `` // Do not print the result as we are asking for confirmation afterwards
	selected, _ := selector.RunPrompt()
	c := confirmation.New("Are you sure you want to change the oneshot boot entry to "+selected, confirmation.Yes)
	c.ResultTemplate = ``
	confirm, err := c.RunPrompt()
	if err != nil {
		return err
	}

	if confirm {
		return SelectBootEntry(cfg, selected)
	}
	return err
}

// ListSystemdEntries reads the systemd-boot entries and returns a list of entries found
func listSystemdEntries(cfg *sdkConfig.Config, efiPartition *sdkPartitions.Partition) ([]string, error) {
	var entries []string
	err := fsutils.WalkDirFs(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/entries/"), func(path string, info os.DirEntry, err error) error {
		if err != nil {
			cfg.Logger.Errorf("Walking the dir %s", err.Error())
		}

		cfg.Logger.Debugf("Checking file %s", path)
		if info == nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(info.Name()) != ".conf" {
			return nil
		}
		entry, err := systemdConfToBootName(info.Name())
		if err != nil {
			return err
		}

		entries = append(entries, entry)
		return nil
	})
	return entries, err
}

// listGrubEntries reads the grub config files and returns a list of entries found
func listGrubEntries(cfg *sdkConfig.Config) ([]string, error) {
	// Read grub config from 4 places
	// /etc/cos/grub.cfg
	// /run/initramfs/cos-state/grub/grub.cfg
	// /run/initramfs/cos-state/grub2/grub.cfg
	// /etc/kairos/branding/grubmenu.cfg
	// Prefer explicit --id values and fall back to menuentry titles when an ID
	// is not present, as is common for entries supplied through grubcustom.
	// TODO: Check how to run this from livecd as it requires mounting state and grub?
	var entries []string
	idRe := regexp.MustCompile(`--id\s([A-z0-9]*)\s{`)
	menuEntryRe := regexp.MustCompile(`(?m)^\s*menuentry\s+(?:"([^"]+)"|'([^']+)')(?:[^\n{]*)\{`)
	files := []string{
		"/etc/cos/grub.cfg",
		"/run/initramfs/cos-state/grub/grub.cfg",
		"/run/initramfs/cos-state/grub2/grub.cfg",
		"/etc/kairos/branding/grubmenu.cfg",
	}
	foundAnyGrubFile := false
	for _, file := range files {
		f, err := cfg.Fs.ReadFile(file)
		if err != nil {
			continue
		}
		foundAnyGrubFile = true
		matches := idRe.FindAllStringSubmatch(string(f), -1)
		for _, match := range matches {
			entries = append(entries, match[1])
		}
		menuEntries := menuEntryRe.FindAllStringSubmatch(string(f), -1)
		for _, match := range menuEntries {
			if idRe.MatchString(match[0]) {
				continue
			}
			if match[1] != "" {
				entries = append(entries, match[1])
			} else {
				entries = append(entries, match[2])
			}
		}
	}
	if !foundAnyGrubFile {
		cfg.Logger.Errorf("No GRUB configuration file was found: %v", files)
		return nil, fmt.Errorf("failed to get any required GRUB configuration file")
	}

	entries = uniqueStringArray(entries)
	return entries, nil
}

// I lost count on how many places I had to implement this function
// Is that difficult to provide a simple []string.Unique() method from the standard lib????
// Or at least a Set type?
func uniqueStringArray(arr []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, s := range arr {
		if !unique[s] {
			unique[s] = true
			result = append(result, s)
		}
	}
	return result
}

// Another one. Seriously there is nothing to check if something is in a list?
func entryInList(cfg *sdkConfig.Config, entry string, list []string) error {
	// Check that entry exists in the entries list
	for _, e := range list {
		if e == entry {
			return nil
		}
	}
	cfg.Logger.Errorf("entry %s does not exist", entry)
	cfg.Logger.Debugf("entries: %v", list)
	return fmt.Errorf("entry %s does not exist", entry)
}
