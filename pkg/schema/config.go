package schema

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// Config represent the AuroraBoot
// configuration
type Config struct {
	// CloudConfig to use for generating installation mediums
	CloudConfig string `yaml:"cloud_config"`

	// NoDefaultCloudConfig to skip injecting default cloud config if user doesn't provide one
	NoDefaultCloudConfig bool `yaml:"no_default_cloud_config"`

	// Disable Netboot
	DisableNetboot bool `yaml:"disable_netboot"`

	// Disable HTTP Server
	DisableHTTPServer bool `yaml:"disable_http_server"`

	// Disable manual ISO boot
	DisableISOboot bool `yaml:"disable_iso"`

	// PixieCore HTTPServer Port
	NetBootHTTPPort string `yaml:"netboot_http_port"`

	// PixieCore Listen addr
	NetBootListenAddr string `yaml:"netboot_listen_addr"`

	State string `yaml:"state_dir"`

	ListenAddr string `yaml:"listen_addr"`

	// Architecture to use for container image pulling (e.g., "amd64", "arm64")
	Arch string `yaml:"arch"`

	// AllowInsecureRegistries allows pulling container images from registries
	// over plain HTTP or presenting untrusted/self-signed TLS certificates.
	// It is a pointer so that HandleDeprecations can distinguish "explicitly
	// set to false" from "absent"; the new key always wins when both it and
	// the deprecated "insecure" key are present.
	AllowInsecureRegistries *bool `yaml:"allow-insecure-registries"`

	// DeprecatedInsecure preserves the old "insecure" config key so configs
	// written against v0.22.0 keep working. Prefer "allow-insecure-registries".
	// Reconciled into AllowInsecureRegistries by HandleDeprecations.
	DeprecatedInsecure bool `yaml:"insecure"`

	// ISO block configuration
	ISO ISO `yaml:"iso"`

	// Netboot block configuration
	NetBoot NetBoot `yaml:"netboot"`

	Disk Disk `yaml:"disk"`

	System System `yaml:"system"`
}

type System struct {
	Memory  string `yaml:"memory"`
	Cores   string `yaml:"cores"`
	Qemubin string `yaml:"qemu_bin"`
	KVM     bool   `yaml:"kvm"`
}

type Disk struct {
	EFI  bool `yaml:"efi"`
	GCE  bool `yaml:"gce"`
	VHD  bool `yaml:"vhd"`
	BIOS bool `yaml:"bios"`
	// Partitions, when true, makes the EFI raw-disk build emit each partition as
	// a separate image file (efi.img, oem.img, recovery_partition.img) instead
	// of merging them into a single .raw disk. Intended for flashing workflows
	// such as Nvidia Jetson AGX Orin. Implies an EFI build and is mutually
	// exclusive with the gce/vhd cloud-image conversions.
	Partitions        bool   `yaml:"partitions"`
	MAAS              bool   `yaml:"maas"`
	Size              string `yaml:"size"`
	StateSize         string `yaml:"state_size"`
	RecoveryImageSize string `yaml:"recovery_image_size"`
	// BootActive pre-builds COS_STATE with active.img so the EFI raw disk boots straight into active.
	BootActive bool `yaml:"boot_active"`
	// StateSlots sizes COS_STATE for this many system images on boot-active disks (empty means the default of 3); 3 also ships passive.img.
	StateSlots string `yaml:"state_slots"`
	// NoRecovery skips the COS_RECOVERY partition on boot-active disks; requires state_slots=1 since upgrades need recovery.
	NoRecovery bool `yaml:"no_recovery"`
}

const (
	// MinStateSlots is one image: active only, no room for an in-place upgrade.
	MinStateSlots = 1
	// MaxStateSlots is active, passive and the upgrade transition image.
	MaxStateSlots = 3
)

type NetBoot struct {
	Cmdline string `yaml:"cmdline"`
}

type ISO struct {
	DataPath      string `yaml:"data"`
	Name          string `yaml:"name"` // Final artifact base name
	OverrideName  string `yaml:"override_name"`
	IncludeDate   bool   `yaml:"include_date"`
	OverlayISO    string `yaml:"overlay_iso"`
	OverlayRootfs string `yaml:"overlay_rootfs"`
	// ExtendLiveCmdline is appended to the kernel cmdline when booting from the live/installer ISO. Does not affect the installed system.
	ExtendLiveCmdline string `yaml:"extend-live-cmdline"`
	LiveConsole       string `yaml:"live_console"`
	// DefaultGrubEntry is the id (`--id`) of the live menu entry grub boots when the
	// timeout expires. It must be one of constants.LiveGrubEntries; the build
	// refuses any other value. Left empty, the ISO boots the interactive installer.
	DefaultGrubEntry string `yaml:"default_grub_entry"`
	// ExtensionsCatalogs are searched in order when resolving an extension
	// name: the first catalog publishing the name wins. Left empty, the
	// build reads extensions.DefaultCatalog.
	ExtensionsCatalogs []string `yaml:"extensions_catalogs"`
	// Extensions are the system extensions every artifact this build produces
	// carries. A name is resolved against the catalogs; a name carrying the
	// extensions.FileScheme prefix is a .raw image on the build host, which is
	// copied in as it is and needs no catalog.
	Extensions []extensions.Request `yaml:"extensions"`
}

// HandleDeprecations checks for deprecated ISO options and migrates them.
// iso.data is deprecated in favor of iso.overlay_iso. If iso.data is set,
// its value is moved to iso.overlay_iso (unless overlay_iso is already set)
// and a deprecation warning is logged.
func (i *ISO) HandleDeprecations(log logger.KairosLogger) {
	if i.DataPath == "" {
		return
	}

	if i.OverlayISO == "" {
		log.Logger.Warn().Msg("'iso.data' is deprecated and will be removed in a future release. Use 'iso.overlay_iso' instead.")
		i.OverlayISO = i.DataPath
	} else {
		log.Logger.Warn().Msg("'iso.data' is deprecated and will be removed in a future release. Both 'iso.data' and 'iso.overlay_iso' are set; 'iso.data' will be ignored.")
	}
	i.DataPath = ""
}

// HandleDeprecations reconciles deprecated top-level config keys into their
// current equivalents. The old "insecure" key is migrated to
// "allow-insecure-registries" only when the new key is absent; if the new key
// is already set (to any value, including false) it is left untouched so that
// an explicit "allow-insecure-registries: false" is never overridden.
func (c *Config) HandleDeprecations(log logger.KairosLogger) {
	if c.DeprecatedInsecure {
		log.Logger.Warn().Msg("'insecure' is deprecated and will be removed in a future release. Use 'allow-insecure-registries' instead.")
		if c.AllowInsecureRegistries == nil {
			t := true
			c.AllowInsecureRegistries = &t
		}
		c.DeprecatedInsecure = false
	}
}

// AllowInsecureRegistriesBool returns the resolved boolean value of
// AllowInsecureRegistries, returning false when the field has not been set.
func (c *Config) AllowInsecureRegistriesBool() bool {
	if c.AllowInsecureRegistries == nil {
		return false
	}
	return *c.AllowInsecureRegistries
}

// Validate checks the configuration for unsupported combinations before a build
// starts, so we fail fast with a clear message instead of deep inside a build
// step.
func (c Config) Validate() error {
	if err := c.Disk.validateBIOS(); err != nil {
		return err
	}
	// Partition-image output skips the final merge into a single .raw disk, so
	// the gce/vhd conversions (which operate on that merged disk) have nothing
	// to convert. Reject the combination up front.
	if c.Disk.Partitions {
		if c.Disk.GCE {
			return fmt.Errorf("disk.partitions cannot be combined with disk.gce: partition-image output does not produce a merged disk to convert")
		}
		if c.Disk.VHD {
			return fmt.Errorf("disk.partitions cannot be combined with disk.vhd: partition-image output does not produce a merged disk to convert")
		}
	}
	return c.validateBootActive()
}

// validateBIOS rejects disk.bios next to an EFI-family output. Both raw-disk
// builds run in parallel and write the same kairos-<name>.raw, so one would
// corrupt the other.
func (d Disk) validateBIOS() error {
	if !d.BIOS {
		return nil
	}
	for _, o := range []struct {
		key string
		set bool
	}{{"efi", d.EFI}, {"partitions", d.Partitions}, {"gce", d.GCE}, {"vhd", d.VHD}, {"maas", d.MAAS}} {
		if o.set {
			return fmt.Errorf("disk.bios cannot be combined with disk.%s: both raw disks are built at the same time into the same file", o.key)
		}
	}
	return nil
}

// StateSlotsCount returns disk.state_slots as a number, 0 when unset.
func (d Disk) StateSlotsCount() (int, error) {
	if d.StateSlots == "" {
		return 0, nil
	}
	slots, err := strconv.Atoi(d.StateSlots)
	if err != nil || slots < MinStateSlots || slots > MaxStateSlots {
		return 0, fmt.Errorf("disk.state_slots must be a number between %d and %d, got %q", MinStateSlots, MaxStateSlots, d.StateSlots)
	}
	return slots, nil
}

func (c Config) validateBootActive() error {
	if !c.Disk.BootActive {
		if c.Disk.StateSlots != "" {
			return fmt.Errorf("disk.state_slots requires disk.boot_active")
		}
		if c.Disk.NoRecovery {
			return fmt.Errorf("disk.no_recovery requires disk.boot_active")
		}
		return nil
	}
	slots, err := c.Disk.StateSlotsCount()
	if err != nil {
		return err
	}
	if c.Disk.NoRecovery && slots != MinStateSlots {
		return fmt.Errorf("disk.no_recovery requires disk.state_slots=%d: kairos-agent upgrade needs a recovery partition", MinStateSlots)
	}
	if c.Disk.BIOS {
		return fmt.Errorf("disk.boot_active cannot be combined with disk.bios: only EFI raw disks are supported")
	}
	if c.Disk.Partitions {
		return fmt.Errorf("disk.boot_active cannot be combined with disk.partitions: only merged raw disks are supported")
	}
	if c.Disk.MAAS {
		return fmt.Errorf("disk.boot_active cannot be combined with disk.maas: MAAS images are not supported")
	}
	// Raw disks install bundled extensions from the first-boot reset, which boot-active disks never run
	if len(c.ISO.Extensions) > 0 {
		return fmt.Errorf("disk.boot_active cannot be combined with iso.extensions: bundled extensions are installed by the first-boot reset, which boot-active disks skip")
	}
	return nil
}

func (c Config) StateDir(s ...string) string {
	d := "/tmp"
	if c.State != "" {
		d = c.State
	}

	return filepath.Join(append([]string{d}, s...)...)
}
