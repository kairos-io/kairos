package state

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/foxboron/go-uefi/efi"
	"github.com/itchyny/gojq"
	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos-sdk/signatures"
	"github.com/kairos-io/kairos-sdk/types/certs"
	"github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/rs/zerolog"
	"github.com/zcalusic/sysinfo"
	"gopkg.in/yaml.v3"
)

const (
	Active    Boot = "active_boot"
	Passive   Boot = "passive_boot"
	Recovery  Boot = "recovery_boot"
	LiveCD    Boot = "livecd_boot"
	AutoReset Boot = "autoreset_boot"
	Unknown   Boot = "unknown"

	UEFICurrentEntryFile = "/sys/firmware/efi/efivars/LoaderEntrySelected-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"

	// KairosInRAMCmdline is the kernel cmdline token that opts a boot into the
	// in-RAM workflow: the rootfs is copied to a tmpfs (via dracut's rd.live.ram)
	// while OEM and persistent partitions are still mounted from disk.
	// When present, BootState is forced to Active (the running system is the
	// current install) and Runtime.InRAM is set to true so callers that need
	// the tmpfs-rooted distinction can still discover it.
	KairosInRAMCmdline = "kairos.ram"
)

var Log zerolog.Logger

type Boot string

type PartitionState struct {
	Mounted         bool   `yaml:"mounted" json:"mounted"`
	Name            string `yaml:"name" json:"name"`
	Label           string `yaml:"label" json:"label"`
	FilesystemLabel string `yaml:"filesystemlabel" json:"filesystemlabel"`
	MountPoint      string `yaml:"mount_point" json:"mount_point"`
	SizeBytes       uint64 `yaml:"size_bytes" json:"size_bytes"`
	Type            string `yaml:"type" json:"type"`
	IsReadOnly      bool   `yaml:"read_only" json:"read_only"`
	Found           bool   `yaml:"found" json:"found"`
	UUID            string `yaml:"uuid" json:"uuid"` // This would be volume UUID on macOS, PartUUID on linux, empty on Windows
}

type Kairos struct {
	Flavor     string         `yaml:"flavor" json:"flavor"`
	Version    string         `yaml:"version" json:"version"`
	Init       string         `yaml:"init" json:"init"`
	SecureBoot bool           `yaml:"secureboot" json:"secureboot"`
	EfiCerts   certs.EfiCerts `yaml:"eficerts,omitempty" json:"eficerts,omitempty"`
}

type EncryptedParts struct {
	ByLabel  map[string]PartitionState `yaml:"by_label,omitempty" json:"by_label,omitempty"`
	ByDevice map[string]PartitionState `yaml:"by_device,omitempty" json:"by_device,omitempty"`
}

type Runtime struct {
	UUID                string           `yaml:"uuid" json:"uuid"`
	Persistent          PartitionState   `yaml:"persistent" json:"persistent"`
	Recovery            PartitionState   `yaml:"recovery" json:"recovery"`
	OEM                 PartitionState   `yaml:"oem" json:"oem"`
	State               PartitionState   `yaml:"state" json:"state"`
	EncryptedPartitions EncryptedParts   `yaml:"encrypted_partitions,omitempty" json:"encrypted_partitions,omitempty"`
	BootState           Boot             `yaml:"boot" json:"boot"`
	InRAM               bool             `yaml:"in_ram" json:"in_ram"`
	System              sysinfo.SysInfo  `yaml:"system" json:"system"`
	Addresses           []MachineAddress `yaml:"addresses,omitempty" json:"addresses,omitempty"`
	Kairos              Kairos           `yaml:"kairos" json:"kairos"`
}

type FndMnt struct {
	Filesystems []struct {
		Target    string `json:"target,omitempty"`
		FsOptions string `json:"fs-options,omitempty"`
	} `json:"filesystems,omitempty"`
}

// Lsblk is the struct to marshal the output of lsblk
type Lsblk struct {
	BlockDevices []struct {
		Path       string `json:"path,omitempty"`
		Mountpoint string `json:"mountpoint,omitempty"`
		FsType     string `json:"fstype,omitempty"`
		Size       string `json:"size,omitempty"`
		Label      string `json:"label,omitempty"`
		RO         bool   `json:"ro,omitempty"`
	} `json:"blockdevices,omitempty"`
}

func detectPartitionByFindmnt(b *block.Partition) PartitionState {
	// If mountpoint seems empty, try to get the mountpoint of the partition label also the RO status
	// This is a current shortcoming of ghw which only identifies mountpoints via device, not by label/uuid/anything else
	mountpoint := b.MountPoint
	readOnly := b.IsReadOnly
	if b.MountPoint == "" && b.FilesystemLabel != "" {
		out, err := utils.SH(fmt.Sprintf("findmnt /dev/disk/by-label/%s -f -J -o TARGET,FS-OPTIONS", b.FilesystemLabel))
		mnt := &FndMnt{}
		if err == nil {
			err = json.Unmarshal([]byte(out), mnt)
			// This should not happen, if there were no targets, the command would have returned an error, but you never know...
			if err == nil && len(mnt.Filesystems) == 1 {
				mountpoint = mnt.Filesystems[0].Target
				// Don't assume its ro or rw by default, check both. One should match
				regexRW := regexp.MustCompile("^rw,|^rw$|,rw,|,rw$")
				regexRO := regexp.MustCompile("^ro,|^ro$|,ro,|,ro$")
				if regexRW.Match([]byte(mnt.Filesystems[0].FsOptions)) {
					readOnly = false
				}
				if regexRO.Match([]byte(mnt.Filesystems[0].FsOptions)) {
					readOnly = true
				}
			}
		}
	}
	return PartitionState{
		Type:            b.Type,
		IsReadOnly:      readOnly,
		UUID:            b.UUID,
		Name:            fmt.Sprintf("/dev/%s", b.Name),
		SizeBytes:       b.SizeBytes,
		Label:           b.Label,
		FilesystemLabel: b.FilesystemLabel,
		MountPoint:      mountpoint,
		Mounted:         mountpoint != "",
		Found:           true,
	}
}

// DetectBoot returns the current boot state, UKI-aware: it inspects
// /proc/cmdline and, on a UKI system, the EFI current-entry file. It is
// lightweight (no partition or hardware probing, unlike NewRuntime) and returns
// Unknown when it cannot classify. Useful for callers — such as fleet agents —
// that only need the boot state.
func DetectBoot(logger zerolog.Logger) Boot {
	return detectBoot(logger)
}

func detectBoot(logger zerolog.Logger) Boot {
	logger.Debug().Msg("detecting boot state")
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		logger.Debug().Err(err).Msg("Error reading /proc/cmdline file " + err.Error())
		return Unknown
	}

	cmdlineS := string(cmdline)

	// kairos.ram boots run the OS out of a tmpfs but are the currently
	// running system (not the ISO livecd, not a recovery slot). Classify them
	// as Active so upgrade/reset gates in downstream consumers behave the same
	// as on a real installation. Runtime.InRAM still exposes the distinction
	// to code that cares.
	if DetectInRAM(cmdlineS) {
		logger.Debug().Msg("Detected in-RAM boot; classifying as Active")
		return Active
	}

	if DetectUKIboot(cmdlineS) {
		logger.Debug().Msg("Detected uki boot")
		return getUKIBootState(logger)
	}

	return getNonUKIBootState(cmdlineS)
}

func getUKIBootState(logger zerolog.Logger) Boot {
	if !EfiBootFromInstall(logger) {
		return LiveCD
	}

	currentEntryBytes, err := os.ReadFile(UEFICurrentEntryFile)
	if err != nil {
		logger.Debug().Err(err).Msg(fmt.Sprintf("Error reading %s file %s", UEFICurrentEntryFile, err.Error()))
		return Unknown
	}

	// Create a regular expression to remove non-printable characters
	regex := regexp.MustCompile("[[:cntrl:]]")
	currentEntry := regex.ReplaceAllString(string(currentEntryBytes), "")

	logger.Debug().Msg("Current entry: " + currentEntry)

	if !strings.HasSuffix(currentEntry, ".conf") {
		return Unknown
	}

	switch {
	case strings.HasPrefix(currentEntry, "active"):
		return Active
	case strings.HasPrefix(currentEntry, "passive"):
		return Passive
	case strings.HasPrefix(currentEntry, "recovery"):
		return Recovery
	case strings.HasPrefix(currentEntry, "statereset"):
		return AutoReset
	default:
		return Unknown
	}
}

func getNonUKIBootState(cmdline string) Boot {
	switch {
	// The automatic state-reset boot (the statereset GRUB entry) boots the
	// recovery system with `kairos.reset` on the command line; match it before
	// the COS_RECOVERY case below, which it also satisfies. The UKI path already
	// maps the `statereset` boot entry to AutoReset in getUKIBootState.
	case strings.Contains(cmdline, "kairos.reset"):
		return AutoReset
	case strings.Contains(cmdline, "COS_ACTIVE"):
		return Active
	case strings.Contains(cmdline, "COS_PASSIVE"):
		return Passive
	case strings.Contains(cmdline, "COS_RECOVERY"), strings.Contains(cmdline, "COS_SYSTEM"), strings.Contains(cmdline, "recovery-mode"):
		return Recovery
	case strings.Contains(cmdline, "live:LABEL"), strings.Contains(cmdline, "live:CDLABEL"), strings.Contains(cmdline, "netboot"):
		return LiveCD
	default:
		return Unknown
	}
}

// Detects if we are on uki mode
func DetectUKIboot(cmdline string) bool {
	Log.Info().Msg("checking cmdline for uki:" + cmdline)
	return strings.Contains(cmdline, "rd.immucore.uki")
}

// DetectInRAM returns true when the kernel cmdline opts this boot into the
// in-RAM workflow. Any of these token shapes enables the mode:
//
//   - `kairos.ram`               — bare toggle
//   - `kairos.ram=<anything>`    — bare toggle with an ignored value
//   - `kairos.ram.<child>[=...]` — any child under the namespace (e.g.
//     `kairos.ram.auto_create_partitions`, `kairos.ram.oem=64`). This lets
//     operators skip the bare stanza when they are already setting one of the
//     sub-flags — the intent is unambiguous.
//
// It is a pure string check with no side effects; callers can also read
// Runtime.InRAM, which is populated from /proc/cmdline by NewRuntime.
func DetectInRAM(cmdline string) bool {
	for _, tok := range strings.Fields(cmdline) {
		if tok == KairosInRAMCmdline ||
			strings.HasPrefix(tok, KairosInRAMCmdline+".") ||
			strings.HasPrefix(tok, KairosInRAMCmdline+"=") {
			return true
		}
	}
	return false
}

// DetectInRAMFromProc reads /proc/cmdline and returns whether the in-RAM
// workflow is enabled. Returns false if /proc/cmdline cannot be read.
func DetectInRAMFromProc() bool {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return false
	}
	return DetectInRAM(string(cmdline))
}

// DetectInRAMWithVFS mirrors DetectInRAMFromProc but uses a KairosFS so it can
// be exercised from tests.
func DetectInRAMWithVFS(fs fs.KairosFS) (bool, error) {
	cmdline, err := fs.ReadFile("/proc/cmdline")
	if err != nil {
		return false, err
	}
	return DetectInRAM(string(cmdline)), nil
}

// EfiBootFromInstall will try to check the /sys/firmware/efi/LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f
// systemd vendor Id is 4a67b082-0a4c-41cf-b6c7-440b29bb8c4f and will never change
// LoaderDevicePartUUID contains the partition UUID of the EFI System Partition the boot loader was run from. Set by the boot loader.
// This will return true if we are running from a DISK device, which sets the efivar
// This wil return false when running from a volatile media, like CD or netboot as it cannot infer where it was booted from
// Useful to check if we are on install phase or not
// This efi var is VOLATILE so once we reboot is GONE. No way of keeping it across reboots, its set by the bootloader.
func EfiBootFromInstall(logger zerolog.Logger) bool {
	file := "/sys/firmware/efi/efivars/LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"
	readFile, err := os.ReadFile(file)
	if err != nil && os.IsNotExist(err) {
		logger.Trace().Err(err).Msg("File LoaderDevicePartUUID does not exist")
		return false
	}
	if len(readFile) == 0 || string(readFile) == "" {
		logger.Debug().Str("file", string(readFile)).Msg("Error reading LoaderDevicePartUUID file")
		return false
	}
	return true
}

// DetectBootWithVFS will detect the boot state using a vfs so it can be used for tests as well
func DetectBootWithVFS(fs fs.KairosFS) (Boot, error) {
	cmdline, err := fs.ReadFile("/proc/cmdline")
	if err != nil {
		return Unknown, err
	}
	cmdlineS := string(cmdline)
	switch {
	// kairos.ram wins over everything else: the medium may still be an ISO
	// (live:LABEL...) but the running system is treated as Active. See
	// detectBoot for the same rule.
	case DetectInRAM(cmdlineS):
		return Active, nil
	// See getNonUKIBootState: the statereset boot carries `kairos.reset` on a
	// recovery command line, so match it first.
	case strings.Contains(cmdlineS, "kairos.reset"):
		return AutoReset, nil
	case strings.Contains(cmdlineS, "COS_ACTIVE"):
		return Active, nil
	case strings.Contains(cmdlineS, "COS_PASSIVE"):
		return Passive, nil
	case strings.Contains(cmdlineS, "COS_RECOVERY"), strings.Contains(cmdlineS, "COS_SYSTEM"), strings.Contains(cmdlineS, "recovery-mode"):
		return Recovery, nil
	case strings.Contains(cmdlineS, "live:LABEL"), strings.Contains(cmdlineS, "live:CDLABEL"), strings.Contains(cmdlineS, "netboot"):
		return LiveCD, nil
	default:
		return Unknown, nil
	}
}

func detectRuntimeState(r *Runtime) error {
	blockDevices, err := block.New(ghw.WithDisableTools(), ghw.WithDisableWarnings())
	// ghw currently only detects if partitions are mounted via the device
	// If we mount them via label, then its set as not mounted.
	if err != nil {
		return err
	}
	for _, d := range blockDevices.Disks {
		for _, part := range d.Partitions {
			switch part.FilesystemLabel {
			case "COS_PERSISTENT":
				r.Persistent = detectPartitionByFindmnt(part)
			case "COS_RECOVERY":
				r.Recovery = detectPartitionByFindmnt(part)
			case "COS_OEM":
				r.OEM = detectPartitionByFindmnt(part)
			case "COS_STATE":
				r.State = detectPartitionByFindmnt(part)
			}
		}
	}
	if !r.Persistent.Found {
		r.Persistent = detectPartitionByLabelLsblk("COS_PERSISTENT")
	}
	if !r.OEM.Found {
		r.OEM = detectPartitionByLabelLsblk("COS_OEM")
	}
	if !r.Recovery.Found {
		r.Recovery = detectPartitionByLabelLsblk("COS_RECOVERY")
	}
	return nil
}

// detectPartitionByLsblk will try to detect info about a partition by using lsblk and the given LABEL
// Useful for LVM partitions which ghw is unable to find
func detectPartitionByLabelLsblk(label string) PartitionState {
	return detectPartitionByLsblk(fmt.Sprintf("/dev/disk/by-label/%s", label))
}

// detectPartitionByLsblk generic function to get info about a partition via any given path
// Could be /dev/disk/by-{label,path,uuid} for example or even a device directly like /dev/sda1
func detectPartitionByLsblk(path string) PartitionState {
	out, err := utils.SH(fmt.Sprintf("lsblk %s -o PATH,FSTYPE,MOUNTPOINT,SIZE,RO,LABEL -J", path))
	mnt := &Lsblk{}
	part := PartitionState{}
	if err == nil {
		err = json.Unmarshal([]byte(out), mnt)
		// This should not happen, if there were no targets, the command would have returned an error, but you never know...
		if err == nil && len(mnt.BlockDevices) == 1 {
			blk := mnt.BlockDevices[0]
			part.Found = true
			part.Name = blk.Path
			part.Mounted = blk.Mountpoint != ""
			part.MountPoint = blk.Mountpoint
			part.Type = blk.FsType
			part.FilesystemLabel = blk.Label
			// this seems to report always false. We can try to use findmnt here to know if its ro/rw
			part.IsReadOnly = blk.RO
		}
	}

	return part
}

func detectSystem(r *Runtime) {
	var si sysinfo.SysInfo

	si.GetSysInfo()
	r.System = si
}

func detectKairos(r *Runtime) {
	k := &Kairos{}
	k.Flavor = utils.Flavor()

	v, err := utils.OSRelease("VERSION")
	if err == nil {
		k.Version = v
	}
	k.Init = utils.GetInit()
	k.EfiCerts = getEfiCertsCommonNames()
	k.SecureBoot = efi.GetSecureBoot()
	r.Kairos = *k

}

func detectEncryptedPartitions(runtime *Runtime) {
	results := EncryptedParts{
		ByDevice: make(map[string]PartitionState),
		ByLabel:  make(map[string]PartitionState),
	}
	blockDevices, err := block.New(ghw.WithDisableTools(), ghw.WithDisableWarnings())
	// ghw currently only detects if partitions are mounted via the device
	// If we mount them via label, then its set as not mounted.
	if err != nil {
		return
	}
	for _, d := range blockDevices.Disks {
		for _, part := range d.Partitions {
			if part.Type == "crypto_LUKS" {
				// detect partition by the mapper + part.name (i.e. vda2)
				p := detectPartitionByLsblk(fmt.Sprintf("/dev/mapper/%s", part.Name))
				if p.Found {
					results.ByLabel[part.Label] = p
					results.ByDevice[fmt.Sprintf("/dev/%s", part.Name)] = p
				}
			}
		}
	}
	runtime.EncryptedPartitions = results
}

// getEfiCertsCommonNames returns a simple list of the Common names of the certs
func getEfiCertsCommonNames() certs.EfiCerts {
	var data certs.EfiCerts
	certs, _ := signatures.GetAllCerts() // Ignore errors here, we dont care about them, we only want the presentation of the names
	for _, c := range certs.PK {
		data.PK = append(data.PK, c.Issuer.CommonName)
	}
	for _, c := range certs.KEK {
		data.KEK = append(data.KEK, c.Issuer.CommonName)
	}
	for _, c := range certs.DB {
		data.DB = append(data.DB, c.Issuer.CommonName)
	}
	return data
}

func NewRuntimeWithLogger(logger zerolog.Logger) (Runtime, error) {
	runtime := &Runtime{
		BootState: detectBoot(logger),
		InRAM:     DetectInRAMFromProc(),
		UUID:      utils.UUID(),
		Addresses: DetectAddresses(),
	}

	detectSystem(runtime)
	detectKairos(runtime)
	detectEncryptedPartitions(runtime)
	err := detectRuntimeState(runtime)

	return *runtime, err
}

func NewRuntime() (Runtime, error) {
	return NewRuntimeWithLogger(Log)
}

func (r Runtime) String() string {
	dat, err := yaml.Marshal(r)
	if err == nil {
		return string(dat)
	}
	return ""
}

func (r Runtime) Query(s string) (res string, err error) {
	s = fmt.Sprintf(".%s", s)
	jsondata := map[string]interface{}{}
	var dat []byte
	dat, err = json.Marshal(r)
	if err != nil {
		return
	}
	err = json.Unmarshal(dat, &jsondata)
	if err != nil {
		return
	}
	query, err := gojq.Parse(s)
	if err != nil {
		return res, err
	}
	iter := query.Run(jsondata) // or query.RunWithContext
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return res, err
		}
		res += fmt.Sprint(v)
	}
	return
}
