package stages

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos-init/pkg/config"
	"github.com/kairos-io/kairos-init/pkg/kernel"

	semver "github.com/hashicorp/go-version"
	"github.com/kairos-io/kairos-init/pkg/values"
	"github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/mudler/go-pluggable"
	"github.com/mudler/yip/pkg/schema"
)

// This file contains the stages that are run during the init process

// GetInitrdStage Returns the initrd stage
// This stage cleans up any existing initrd files and creates a new one
// In the case of Trusted boot systems, we dont do anything but remove the initrd files as the initrd is created and
// signed during the build process
// If we have fips, we need to add the fips support to the initrd as well
func GetInitrdStage(_ values.System, logger logger.KairosLogger) ([]schema.Stage, error) {
	if config.ContainsSkipStep(values.InitrdStep) {
		logger.Logger.Warn().Msg("Skipping initrd generation stage")
		return []schema.Stage{}, nil
	}

	stage := []schema.Stage{
		{
			Name: "Remove all initrds",
			Commands: []string{
				"rm -f /boot/initrd*",
				"rm -f /boot/initramfs*",
			},
		},
	}

	// Thor (T264) display bring-up. NVIDIA activates the GPU/display driver at
	// first boot via nv-load-display-modules.service, which never runs in a
	// container build - so the display never initializes ("NVRM: Cannot
	// initialize DCE firmware RM", nvgpu clk-arb / IOMMU failures). Bake the
	// activation the service would do: select the openrm variant (Thor is
	// openrm-only), which via nv-modprobe-openrm-l4t-display.conf loads the GPU
	// power-gating module (nv-gpu-static-pg) before nvidia.ko and blocks nvgpu,
	// and load nvidia_drm at boot. The depmod below then picks up the override.
	// See kairos-io/kairos#4228.
	if values.Model(config.DefaultConfig.Model) == values.Thor {
		stage = append(stage, schema.Stage{
			Name: "Activate openrm display driver for Thor",
			Commands: []string{
				"mkdir -p /etc/depmod.d /etc/modprobe.d /etc/modules-load.d",
				"cp /opt/nvidia/nv-disp-module-configs/nv-depmod-openrm-l4t-display.conf /etc/depmod.d/nvidia-display.conf",
				"cp /opt/nvidia/nv-disp-module-configs/nv-modprobe-openrm-l4t-display.conf /etc/modprobe.d/nvidia-display.conf",
				"printf 'nvidia_drm\\n' > /etc/modules-load.d/nvidia-display.conf",
			},
		})
	}

	// If we are not using trusted boot we need to create a new initrd
	if !config.DefaultConfig.TrustedBoot {
		kernel, err := getLatestKernel(logger)
		if err != nil {
			logger.Logger.Error().Msgf("Failed to get the latest kernel: %s", err)
			return []schema.Stage{}, err
		}

		dracutCmd := fmt.Sprintf("dracut -f /boot/initrd %s", kernel)

		if logger.GetLevel() == 0 {
			dracutCmd = fmt.Sprintf("dracut -v -f /boot/initrd %s", kernel)
		}

		stage = append(stage, []schema.Stage{
			{
				Name:     "Create new initrd",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|SLES.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				Commands: []string{
					fmt.Sprintf("depmod -a %s", kernel),
					dracutCmd,
				},
			},
			{
				Name:     "Create new initrd for Alpine",
				OnlyIfOs: "Alpine.*",
				Commands: []string{
					fmt.Sprintf("depmod -a %s", kernel),
					fmt.Sprintf("mkinitfs -o /boot/initrd %s", kernel),
				},
			},
		}...)
	}

	return stage, nil
}

// GetKairosReleaseStage Returns the kairos-release stage which creates the /etc/kairos-release file
// This file is very important as severals other pieces of Kairos refer to it.
// For example, for upgrading the version its taken from here
// During boot, grub checks this file to know things about the system and enable or disable stuff, like console for rpi images
func GetKairosReleaseStage(sis values.System, log logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.KairosReleaseStep) {
		log.Logger.Warn().Msg("Skipping /etc/kairos-release generation stage")
		return []schema.Stage{}
	}
	// TODO: Expand tis as this doesn't cover all the current fields
	// Current missing fields
	/*
			KAIROS_VERSION_ID="v3.2.4-36-g24ca209-v1.32.0-k3s1"
			KAIROS_GITHUB_REPO="kairos-io/kairos"
			KAIROS_IMAGE_REPO="quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.2.4-36-g24ca209-k3sv1.32.0-k3s1"
			KAIROS_ARTIFACT="kairos-ubuntu-24.04-standard-amd64-generic-v3.2.4-36-g24ca209-k3sv1.32.0+k3s1"
			KAIROS_PRETTY_NAME="kairos-standard-ubuntu-24.04 v3.2.4-36-g24ca209-v1.32.0-k3s1"

		VERSION_ID and VERSION are the same, needed ?
		RELEASE is the short version of VERSION and VERSION_ID, the version without the k3s version needed?
		GITHUB_REPO is the repo where the image is stored, not really needed?
		PRETTY_NAME is the same as the ID_LIKE but different? needed?

	*/

	idLike := fmt.Sprintf("kairos-%s-%s-%s", config.DefaultConfig.Variant, sis.Distro.String(), sis.Version)
	flavor := sis.Distro.String()
	flavorRelease := sis.Version

	// TODO: Check if this affects sles versions? I don't think so as they are set like registry.suse.com/bci/bci-micro:15.6
	if strings.Contains(flavor, "opensuse") {
		// We store the suse version under the flavorRelease for some reason
		// So opensuse-leap:15.5 will be stored as `leap-15.5` with flavor being plain `opensuse`
		// Its a bit iffy IMHO but this is done so all opensuse stuff goes under the same repo instead of having
		// a repo for opensuse-leap and a repo for opensuse-tumbleweed
		flavorSplitted := strings.Split(flavor, "-")
		if len(flavorSplitted) == 2 {
			flavor = flavorSplitted[0]
			flavorRelease = fmt.Sprintf("%s-%s", flavorSplitted[1], sis.Version)
		} else {
			log.Debugf("Failed to split the flavor %s", flavor)
		}
	}

	// Back compat with old images
	// Before this we enforced the version to be vX.Y.Z
	// But now the version just cant be whatever semver version
	// The problem is that the upgrade checker uses a semver parses that marks anything without a v an invalid version :sob:
	// So now we need to enforce this forever
	release := config.DefaultConfig.KairosVersion.String()
	if release[0] != 'v' {
		release = fmt.Sprintf("v%s", release)
	}

	env := map[string]string{
		"KAIROS_ID":             "kairos", // What for?
		"KAIROS_ID_LIKE":        idLike,   // What for?
		"KAIROS_NAME":           idLike,   // What for? Same as ID_LIKE
		"KAIROS_VERSION":        release,
		"KAIROS_ARCH":           sis.Arch.String(),
		"KAIROS_TARGETARCH":     sis.Arch.String(), // What for? Same as ARCH
		"KAIROS_FLAVOR":         flavor,            // This should be in os-release as ID
		"KAIROS_FLAVOR_RELEASE": flavorRelease,     // This should be in os-release as VERSION_ID
		"KAIROS_FAMILY":         sis.Family.String(),
		"KAIROS_MODEL":          config.DefaultConfig.Model,            // NEEDED or it breaks boot!
		"KAIROS_VARIANT":        config.DefaultConfig.Variant.String(), // TODO: Fully drop variant
		"KAIROS_BUG_REPORT_URL": "https://github.com/kairos-io/kairos/issues",
		"KAIROS_HOME_URL":       "https://github.com/kairos-io/kairos",
		"KAIROS_RELEASE":        release,
		"KAIROS_FIPS":           fmt.Sprintf("%t", config.DefaultConfig.Fips),        // Was the image built with FIPS support?
		"KAIROS_TRUSTED_BOOT":   fmt.Sprintf("%t", config.DefaultConfig.TrustedBoot), // Was the image built with Trusted Boot support?
		"KAIROS_INIT_VERSION":   values.GetVersion(),                                 // The version of the kairos-init binary
	}

	// Todo: Change this to allow getting info from several providers? No idea how to store it, the current below is only for k3s/k0s I think
	versionInfo, err := getProviderInfo(log)
	if err == nil && versionInfo.Provider != "" && versionInfo.Version != "" {
		env["KAIROS_SOFTWARE_VERSION"] = versionInfo.Version
		env["KAIROS_SOFTWARE_VERSION_PREFIX"] = versionInfo.Provider
	}

	log.Logger.Debug().Interface("env", env).Msg("Kairos release stage")

	return []schema.Stage{
		{
			Name:            "Write kairos-release",
			Environment:     env,
			EnvironmentFile: "/etc/kairos-release",
		},
	}
}

func getProviderInfo(logger logger.KairosLogger) (bus.ProviderInstalledVersionPayload, error) {
	logger.Logger.Info().Msg("Triggering provider info event")
	versionInfo := bus.ProviderInstalledVersionPayload{}
	manager := bus.NewBus(bus.InitProviderInfo)
	manager.Initialize(bus.WithLogger(&logger))

	if len(manager.Plugins) == 0 {
		logger.Logger.Info().Msg("No plugins found, skipping provider install event")
		return versionInfo, nil
	}

	manager.Response(bus.InitProviderInfo, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		logger.Logger.Debug().Str("at", p.Executable).Interface("resp", resp).Msg("Received info event from provider")
		if resp.Errored() {
			logger.Logger.Error().Msgf("Provider info event failed: %s", resp.Error)
			return
		}
		if resp.State == bus.EventResponseNotApplicable {
			logger.Logger.Info().Msg("Provider info event is non-applicable, skipping")
			return
		}
		if err := json.Unmarshal([]byte(resp.Data), &versionInfo); err != nil {
			logger.Logger.Error().Msgf("Failed to unmarshal provider info event: %s", err)
			return
		}
		if resp.State == bus.EventResponseSuccess {
			logger.Logger.Info().Msg("Provider info event succeeded")
		}
	})
	_, err := manager.Publish(bus.InitProviderInfo, nil)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to publish provider info event: %s", err)
		return versionInfo, err
	}
	return versionInfo, nil
}

// GetWorkaroundsStage Returns the workarounds stage
// It applies some workarounds to the system to fix up inconsistent things or issues on the system
// For ubuntu + trusted boot we need to download the linux-modules-extra package, save the nvdimm modules
// and then clean it up so http uki boot works out of the box. By default the nvdimm modules needed are in that package
// We could just install the package but its a 100+MB  package and we need just 4 or 5 modules.
// Canonical deprecated linux-modules-extra starting with the 6.15 development kernel and the split is gone
// on newer HWE kernels (e.g. linux-hwe-7.0 on noble). When the package is not available in apt the nvdimm
// modules are already shipped inside linux-modules (installed as a dep of the kernel image), so we skip the
// whole workaround via an apt-cache guard rather than version-gating (the 6.17 noble HWE backport still had
// modules-extra while 6.17 in questing did not, so a plain version check would misfire).
func GetWorkaroundsStage(_ values.System, l logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.WorkaroundsStep) {
		l.Logger.Warn().Msg("Skipping workarounds stage")
		return []schema.Stage{}
	}
	stages := []schema.Stage{
		{
			Name: "Link grub-editenv to grub2-editenv",
			//OnlyIfOs: "Ubuntu.*|Alpine.*", // Maybe not needed and just checking if the file exists is enough
			// test if the file exists and if the link does not exist
			If: "test -f /usr/bin/grub-editenv && ! test -e /usr/bin/grub2-editenv",
			Commands: []string{
				"ln -s /usr/bin/grub-editenv /usr/bin/grub2-editenv",
			},
		},
		{
			Name:     "Fixup sudo perms",
			OnlyIfOs: "Ubuntu.*|Debian.*",
			Commands: []string{
				"chown root:root /usr/bin/sudo",
				"chmod 4755 /usr/bin/sudo",
			},
		},
		{
			Name:     "Create snap dir in rootfs", // Very special as its on teh rootfs so we need to create it now just in case
			OnlyIfOs: "Ubuntu.*|Debian.*",
			Directories: []schema.Directory{
				{
					Path:        "/snap",
					Permissions: 0755,
					Owner:       0,
					Group:       0,
				},
			},
		},
		{
			Name:     "Disable SELinux for Red Hat",
			OnlyIfOs: "Red.*Hat.*",
			Files: []schema.File{
				{
					Path:    "/etc/selinux/config",
					Content: `SELINUX=disabled`,
				},
			},
		},
		{
			Name: "Link nvidia-smi into the expected place",
			// Check and only do if the link/binary is not there
			If: "test -f /usr/sbin/nvidia-smi && ! test -e /usr/bin/nvidia-smi",
			Commands: []string{
				"ln -s /usr/sbin/nvidia-smi /usr/bin/nvidia-smi",
			},
		},
	}

	if config.DefaultConfig.TrustedBoot {
		// This looks like its out of its place as we would expect this modules to be in the initrd but this is for Trusted Boot
		// so the initrd is creating during artifact build and contains the rootfs, so this is ok to be in here
		kernel, err := getLatestKernel(l)
		if err != nil {
			l.Logger.Error().Msgf("Failed to get the latest kernel: %s", err)
			return stages
		}
		// 25.10 is the first version where this workaround is not needed. On 24.04 with newer HWE kernels
		// (linux-hwe-7.0 and up) Canonical dropped the modules-extra split too, so the apt-cache guard
		// skips the stage in that case (nvdimm modules already live in linux-modules).
		stages = append(stages, []schema.Stage{
			{
				Name:            "Download linux-modules-extra for nvdimm modules",
				OnlyIfOs:        "Ubuntu.*",
				OnlyIfOsVersion: `2[0-4]\..*`,
				If:              fmt.Sprintf("apt-cache show linux-modules-extra-%s >/dev/null 2>&1", kernel),
				Commands: []string{
					fmt.Sprintf("apt-get download linux-modules-extra-%s", kernel),
					fmt.Sprintf("dpkg-deb -x linux-modules-extra-%s_*.deb /tmp/modules", kernel),
					fmt.Sprintf("mkdir -p /usr/lib/modules/%s/kernel/drivers/nvdimm", kernel),
					fmt.Sprintf("mv /tmp/modules/lib/modules/%[1]s/kernel/drivers/nvdimm/* /usr/lib/modules/%[1]s/kernel/drivers/nvdimm/", kernel),
					fmt.Sprintf("depmod -a %s", kernel),
					"rm -rf /tmp/modules",
					"rm /*.deb",
				},
			},
		}...)
	}

	return stages
}

// GetCleanupStage Returns the cleanup stage
// This stage is mainly about cleaning up the system and removing unneeded packages
// As some of the software installed can mess with he system and we dont want to have it in an inconsistent state
// I also removes some packages that are no longer needed, like dracut and dependant packages as once
// we have build the initramfs we dont need them anymore
// TODO: Remove package cache for all distros
func GetCleanupStage(_ values.System, l logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.CleanupStep) {
		l.Logger.Warn().Msg("Skipping cleanup stage")
		return []schema.Stage{}
	}

	stages := []schema.Stage{
		{
			Name: "Remove dbus machine-id",
			If:   "test -f /var/lib/dbus/machine-id",
			Commands: []string{
				"rm -f /var/lib/dbus/machine-id",
			},
		},
		{
			Name: "truncate machine-id",
			If:   "test -f /etc/machine-id",
			Commands: []string{
				"truncate -s 0 /etc/machine-id",
			},
		},
		{
			Name: "truncate hostname",
			If:   "test -f /etc/hostname",
			Commands: []string{
				"truncate -s 0 /etc/hostname",
			},
		},
		{
			Name: "Remove host ssh keys",
			If:   "test -d /etc/ssh",
			Commands: []string{
				"rm -f /etc/ssh/ssh_host_*_key*",
			},
		},
		{
			Name:     "Cleanup",
			OnlyIfOs: "Ubuntu.*|Debian.*",
			Commands: []string{
				"apt-get clean",
				"rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*",
			},
		},
		{
			Name:     "Cleanup",
			OnlyIfOs: "Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*",
			Commands: []string{
				"dnf clean all",
				"rm -rf /var/cache/dnf/* /tmp/* /var/tmp/*",
			},
		},
		{
			Name:     "Cleanup",
			OnlyIfOs: values.AlpineRegex,
			Commands: []string{
				"rm -rf /var/cache/apk/* /tmp/* /var/tmp/*",
			},
		},
		{
			Name:     "Cleanup",
			OnlyIfOs: values.AllSuseRegex,
			Commands: []string{
				"zypper clean -a",
				"rm -rf /var/cache/zypp/* /tmp/* /var/tmp/*",
			},
		},
	}

	return stages
}

// GetServicesStage Returns the services stage
// This stage is about configuring the services to be run on the system. Either enabling or disabling them.
func GetServicesStage(_ values.System, l logger.KairosLogger) []schema.Stage {
	if config.ContainsSkipStep(values.ServicesStep) {
		l.Logger.Warn().Msg("Skipping services stage")
		return []schema.Stage{}
	}
	return []schema.Stage{
		{
			Name:                 "Configure default systemd services",
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Mask: []string{
					"systemd-firstboot.service",
				},
				Overrides: []schema.SystemctlOverride{
					{
						Service: "systemd-networkd-wait-online",
						Content: bundled.SystemdNetworkOnlineWaitOverride,
					},
				},
			},
		},
		{
			Name:                 "Enable fail2ban service for RHEL family",
			OnlyIfServiceManager: "systemd",
			If:                   "test -f /usr/bin/fail2ban-server",
			OnlyIfOs:             "CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"fail2ban",
				},
			},
		},
		{
			Name:                 "Enable fail2ban service",
			OnlyIfServiceManager: "systemd",
			OnlyIfOs:             "Ubuntu.*|Debian.*|SLES.*|[Oo]penSUSE.*|Fedora.*", // RHEL family has it optionally installed
			Systemctl: schema.Systemctl{
				Enable: []string{
					"fail2ban",
				},
			},
		},
		{
			Name:                 "Enable timesyncd service",
			OnlyIfServiceManager: "systemd",
			OnlyIfOs:             "Ubuntu.*|Debian.*|SLES.*|[Oo]penSUSE.*|Hadron.*", // RHEL family and Fedora use chronyd instead
			Systemctl: schema.Systemctl{
				Enable: []string{
					"systemd-timesyncd",
				},
			},
		},
		{
			Name:                 "Enable chronyd service for RHEL family and Fedora",
			OnlyIfServiceManager: "systemd",
			OnlyIfOs:             "Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"chronyd",
				},
			},
		},
		{
			Name:                 "Enable services for Debian family",
			OnlyIfOs:             "Ubuntu.*|Debian.*",
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"ssh",
					"systemd-networkd",
				},
			},
		},
		{
			Name:                 "Disable Wicked for SUSE family (excluding SLE Micro Rancher/Tumbleweed)", // Collides with systemd-networkd
			OnlyIfOs:             values.AllSuseButMicroAndTumbleweed,
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Disable: []string{
					"wicked",
				},
				Mask: []string{
					"wicked",
				},
			},
		},
		{
			Name:                 "Enable services for SUSE family (excluding SLE Micro Rancher)",
			OnlyIfOs:             values.AllSuseButMicroRegex,
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"sshd",
					"systemd-networkd",
					"systemd-resolved",
				},
			},
		},
		{
			Name:                 "Disable services for SLE Micro Rancher",
			OnlyIfOs:             values.OnlyMicroRegex,
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Disable: []string{
					"NetworkManager",
				},
			},
		},
		{
			Name:                 "Enable services for SLE Micro Rancher",
			OnlyIfOs:             values.OnlyMicroRegex,
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"sshd",
					"systemd-networkd",
					"systemd-resolved",
				},
			},
		},
		{
			Name:                 "Enable services for RHEL family",
			OnlyIfOs:             "Fedora.*|CentOS.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*",
			OnlyIfServiceManager: "systemd",
			Commands: []string{
				"systemctl unmask getty.target",   // Unmask getty.target to allow login on ttys as it comes masked by default
				"systemctl unmask systemd-udevd",  // Unmask systemd-udevd as it comes masked by default
				"systemctl unmask systemd-logind", // Unmask systemd-logind as it comes masked by default
			},
			Systemctl: schema.Systemctl{
				Enable: []string{
					"sshd",
					"systemd-resolved",
				},
				Disable: []string{
					"dnf-makecache",
					"dnf-makecache.timer",
				},
			},
		},
		{
			Name:                 "Enable services for RHEL",
			OnlyIfOs:             "Red\\sHat.*",
			OnlyIfServiceManager: "systemd",
			Commands: []string{
				"systemctl unmask getty.target",         // Unmask getty.target to allow login on ttys as it comes masked by default
				"systemctl unmask console-getty",        // Unmask console-getty to allow console login on ttys as it comes masked by default
				"systemctl unmask systemd-udevd",        // Unmask systemd-udevd as it comes masked by default
				"systemctl unmask systemd-udev-trigger", // Unmask systemd-udev-trigger as it comes masked by default
				"systemctl unmask systemd-logind",       // Unmask systemd-logind as it comes masked by default
				"systemctl unmask systemd-random-seed",  // Unmask systemd-random-seed as it comes masked by default
				"systemctl unmask systemd-remount-fs",   // Unmask systemd-remount-fs as it comes masked by default
			},
			Systemctl: schema.Systemctl{
				Enable: []string{
					"sshd",
					"systemd-resolved",
					"getty@tty1",
					"getty@tty2",
					"getty@tty3",
					"tmp.mount",
					"proc-sys-fs-binfmt_misc.mount",
				},
				Disable: []string{
					"dnf-makecache",
					"dnf-makecache.timer",
					"selinux-autorelabel-mark",
				},
			},
		},
		{
			Name:                 "Enable networkd for RHEL family if binary is available",
			OnlyIfOs:             values.RHELFamilyRegex,
			OnlyIfServiceManager: "systemd",
			If:                   "test -f /usr/lib/systemd/systemd-networkd",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"systemd-networkd",
				},
			},
		},
		{
			Name:                 "Enable NetworkManager for RHEL if binary is available",
			OnlyIfOs:             values.RHELFamilyRegex,
			OnlyIfServiceManager: "systemd",
			If:                   "test -f /usr/sbin/NetworkManager",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"NetworkManager",
				},
			},
		},
		{
			Name:                 "Enable services for Alpine family",
			OnlyIfOs:             values.AlpineRegex,
			OnlyIfServiceManager: "openrc",
			Commands: []string{
				"rc-update add sshd boot",
				"rc-update add fail2ban boot",
				"rc-update add connman boot",
				"rc-update add acpid boot",
				"rc-update add hwclock boot",
				"rc-update add syslog boot",
				"rc-update add udev sysinit",
				"rc-update add udev-trigger sysinit",
				"rc-update add cgroups sysinit",
				"rc-update add ntpd boot",
				"rc-update add crond",
			},
		},
		{
			Name:                 "Enable services for Hadron",
			OnlyIfOs:             "Hadron.*",
			OnlyIfServiceManager: "systemd",
			Systemctl: schema.Systemctl{
				Enable: []string{
					"sshd",
					"systemd-networkd",
					"systemd-resolved",
				},
			},
		},
	}
}

// GetKernelStage Returns the kernel stage
// This stage is about configuring the kernel to be used on the system. Mainly we already have a kernel
// but all things kairos look for the /boot/vmlinuz file to be there
// So this creates a link to the actual kernel, no matter the version so we can boot the same everywhere
// This stage also cleans up the old kernels and initrd files that are no longer needed.
// This is a bit of a complex one, as every distro has its own way of doing things but we make it work here
func GetKernelStage(_ values.System, logger logger.KairosLogger) ([]schema.Stage, error) {
	if config.ContainsSkipStep(values.KernelStep) {
		logger.Logger.Warn().Msg("Skipping kernel stage")
		return []schema.Stage{}, nil
	}
	kernel, err := getLatestKernel(logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the latest kernel: %s", err)
		return []schema.Stage{}, err
	}

	return []schema.Stage{
		{
			Name: "Create dir if not exists",
			If:   "test ! -d /boot",
			Directories: []schema.Directory{
				{
					Path:        "/boot",
					Permissions: 0644,
					Owner:       0,
					Group:       0,
				},
			},
		},
		{
			Name: "Clean current kernel link",
			If:   "test -L /boot/vmlinuz",
			Commands: []string{
				"rm /boot/vmlinuz",
			},
		},
		{
			Name: "Clean current kernel link if its a symlink",
			If:   "test -L /boot/Image",
			Commands: []string{
				"rm /boot/Image",
			},
		},
		{
			Name: "Clean old kernel link",
			If:   "test -f /boot/vmlinuz.old",
			Commands: []string{
				"rm /boot/vmlinuz.old",
			},
		},
		{
			// On debian riscv machine, the kernel is named like this so only remove it on other arches
			Name:       "Clean debug kernel",
			If:         fmt.Sprintf("test -f /boot/vmlinux-%s", kernel),
			OnlyIfArch: "amd64",
			Commands: []string{
				fmt.Sprintf("rm /boot/vmlinux-%s", kernel),
			},
		},
		{
			// On debian riscv machine, the kernel is named like this so only remove it on other arches
			Name:       "Clean debug kernel",
			If:         fmt.Sprintf("test -f /boot/vmlinux-%s", kernel),
			OnlyIfArch: "arm64",
			Commands: []string{
				fmt.Sprintf("rm /boot/vmlinux-%s", kernel),
			},
		},
		{
			Name: "Link kernel for Nvidia AGX Orin",              // Nvidia AGX Orin has the kernel in the Image file directly
			If:   "test -e /boot/Image && test ! -L /boot/Image", // If its not a symlink then its the kernel so link it to our expected location
			Commands: []string{
				"ln -s /boot/Image /boot/vmlinuz",
			},
		},
		{ // On RHEL family, if we don't have grub2 installed, it wont copy the kernel and rename it to the /boot dir, so we need to do it manually
			Name:     "Copy kernel for Trusted Boot",
			OnlyIfOs: values.RHELFamilyRegex,
			If:       fmt.Sprintf("test ! -f /boot/vmlinuz-%s && test -f /usr/lib/modules/%s/vmlinuz", kernel, kernel),
			Commands: []string{
				fmt.Sprintf("cp /usr/lib/modules/%s/vmlinuz /boot/vmlinuz-%s", kernel, kernel),
			},
		},
		{
			Name: "Link kernel",
			If:   fmt.Sprintf("test -f /boot/vmlinuz-%s", kernel),
			Commands: []string{
				fmt.Sprintf("ln -s /boot/vmlinuz-%s /boot/vmlinuz", kernel),
			},
		},
		{
			// On debian riscv machine, the kernel is named like this
			Name:       "Link kernel",
			If:         fmt.Sprintf("test -f /boot/vmlinux-%s", kernel),
			OnlyIfArch: "riscv64",
			Commands: []string{
				fmt.Sprintf("ln -sf vmlinux-%s /boot/vmlinuz", kernel),
			},
		},
		{
			Name: "Remove any existing hmac symlinks",
			If:   "test -L /boot/.vmlinuz.hmac",
			Commands: []string{
				"rm /boot/.vmlinuz.hmac",
			},
		},
		{
			// hmac files are used under FIPS. We ship them along but because dracut will use the kernel file name
			// to search for the companion hmac file, we need to also link it to the name :)
			Name: "Link .hmac if any",
			If:   fmt.Sprintf("test -f /boot/.vmlinuz-%s.hmac", kernel),
			Commands: []string{
				fmt.Sprintf("ln -s /boot/.vmlinuz-%s.hmac /boot/.vmlinuz.hmac", kernel),
			},
		},
		{
			Name: "Link kernel",
			If:   fmt.Sprintf("test -f /boot/Image-%s", kernel), // On suse arm64 kernel starts with Image
			Commands: []string{
				fmt.Sprintf("ln -s /boot/Image-%s /boot/vmlinuz", kernel),
			},
		},
		{
			Name: "Link kernel for Alpine",
			If:   "test -f /boot/vmlinuz-lts",
			Commands: []string{
				"ln -s /boot/vmlinuz-lts /boot/vmlinuz",
			},
		},
		{
			Name: "Link kernel for Alpine RPI",
			If:   "test -f /boot/vmlinuz-rpi",
			Commands: []string{
				"ln -s /boot/vmlinuz-rpi /boot/vmlinuz",
			},
		},
	}, nil
}

// getLatestKernel returns the latest kernel version installed on the system.
func getLatestKernel(l logger.KairosLogger) (string, error) {
	return kernel.GetLatest(config.DefaultConfig.Model, l)
}

// GetKairosInitramfsFilesStage installs the kairos initramfs files
// This stage is used to install the initramfs files that are needed for the system to boot
func GetKairosInitramfsFilesStage(sis values.System, l logger.KairosLogger) ([]schema.Stage, error) {
	if config.ContainsSkipStep(values.InitramfsConfigsStep) {
		l.Logger.Warn().Msg("Skipping installing initramfs configs stage")
		return []schema.Stage{}, nil
	}
	var data []schema.Stage
	if config.DefaultConfig.TrustedBoot {
		l.Logger.Info().Msg("Skipping installing initramfs files stage for trusted boot")
		return data, nil
	}

	if sis.Family.String() == "alpine" {
		immucoreFiles, err := bundled.EmbeddedAlpineInit.ReadFile("alpineInit/immucore.files")
		if err != nil {
			l.Logger.Error().Err(err).Str("file", "immucore.files").Msg("Failed to read embedded file")
			return nil, err
		}
		initramfsInit, err := bundled.EmbeddedAlpineInit.ReadFile("alpineInit/initramfs-init")
		if err != nil {
			l.Logger.Error().Err(err).Str("file", "initramfs-init").Msg("Failed to read embedded file")
			return nil, err
		}
		mkinitfsConf, err := bundled.EmbeddedAlpineInit.ReadFile("alpineInit/mkinitfs.conf")
		if err != nil {
			l.Logger.Error().Err(err).Str("file", "mkinitfs.conf").Msg("Failed to read embedded file")
			return nil, err
		}
		tpmModules, err := bundled.EmbeddedAlpineInit.ReadFile("alpineInit/tpm.modules")
		if err != nil {
			l.Logger.Error().Err(err).Str("file", "tpm.modules").Msg("Failed to read embedded file")
			return nil, err
		}

		data = append(data, []schema.Stage{
			{
				Name: "Install reconcile script",
				Files: []schema.File{
					{
						Path:        "/usr/sbin/cos-setup-reconcile",
						Permissions: 0755,
						Owner:       0,
						Group:       0,
						Content:     bundled.ReconcileScript,
					},
				},
			},
			{
				Name: "Install Alpine initrd scripts",
				Files: []schema.File{
					{
						Path:        "/etc/mkinitfs/features.d/immucore.files",
						Permissions: 0644,
						Owner:       0,
						Group:       0,
						Content:     string(immucoreFiles),
					},
					{
						Path:        "/etc/mkinitfs/features.d/tpm.modules",
						Permissions: 0644,
						Owner:       0,
						Group:       0,
						Content:     string(tpmModules),
					},
					{
						Path:        "/etc/mkinitfs/mkinitfs.conf",
						Permissions: 0644,
						Owner:       0,
						Group:       0,
						Content:     string(mkinitfsConf),
					},
					{
						Path:        "/usr/share/mkinitfs/initramfs-init",
						Permissions: 0755,
						Owner:       0,
						Group:       0,
						Content:     string(initramfsInit),
					},
				},
			},
		}...)
	} else {
		// Add proper network and systemd-sysext if needed
		// We default to systemd-networkd+network-legacy and sysext enabled
		// If its ubuntu <= 22.04 we need to disable sysext
		// If its ubuntu <= 20.04 we need to use the plain network module
		// network-legacy is needed for ipxe as it comes up very fast which makes the livenet stuff work properly
		// otherwise systemd-networkd does not trigger the dracut hooks to let it know that its up and running
		// https://github.com/dracutdevs/dracut/issues/1822
		networkModule := "systemd-networkd network-legacy"
		sysextModule := true

		if sis.Distro == values.Ubuntu {
			ver, err := semver.NewVersion(sis.Version)
			if err != nil {
				l.Logger.Error().Msgf("Failed to parse the version %s: %s", sis.Version, err)
				return []schema.Stage{}, err
			}
			constraint, _ := semver.NewConstraint("<=22.04")
			// If its <= 22.04 we need to use the plain network module and disable sysext
			if constraint.Check(ver) {
				l.Logger.Debug().Str("distro", string(sis.Distro)).Str("version", sis.Version).Msg("Disabling sysext")
				sysextModule = false
				constraint, _ = semver.NewConstraint("<=20.04")
				// If its <= 20.04 we need to use the plain network module
				if constraint.Check(ver) {
					l.Logger.Debug().Str("distro", string(sis.Distro)).Str("version", sis.Version).Msg("Using the plain network module")
					networkModule = "network"
				}
			}
			constraint, _ = semver.NewConstraint(">=24.04")
			// If its >= 24.04 we need to append resolved to the network module
			if constraint.Check(ver) {
				networkModule += " systemd-resolved"
			}
			constraint, _ = semver.NewConstraint(">=26.04")
			if constraint.Check(ver) {
				// For 26.04+, network-legacy is merged into systemd-networkd and removed
				networkModule = "systemd-networkd systemd-resolved"
			}
		}

		if sis.Family == values.RedHatFamily {
			// Check sysext first
			ver, err := semver.NewVersion(sis.Version)
			if err != nil {
				l.Logger.Error().Msgf("Failed to parse the version %s: %s", sis.Version, err)
				return []schema.Stage{}, err
			}
			constraint, _ := semver.NewConstraint("<9.0")
			// If its < 9.0 we need to disable sysext
			if constraint.Check(ver) {
				l.Logger.Debug().Str("distro", string(sis.Distro)).Str("version", sis.Version).Msg("Disabling sysext")
				sysextModule = false
			}

			// Now network
			// we default to NetworkManager
			// if systemd-network is available we use it instead
			// depending on the version we might add network-legacy
			// Start from scratch
			networkModule = ""
			// Do we have NetworkManager? Then add it and skip the rest of checks
			if _, err := os.Stat("/usr/sbin/NetworkManager"); err == nil {
				networkModule = "network-manager"
			} else {
				// Nothing seems to ship networkd modules for dracut in the RHEL+clones so only add them under Fedora
				if sis.Distro == values.Fedora {
					// Do we have systemd-networkd?
					if _, err := os.Stat("/usr/lib/systemd/systemd-networkd"); err == nil {
						networkModule = "systemd-networkd"
						// Systemd resolved modules only make sense if networkd is used alongside
						// Otherwise other modules provide their own resolvers
						// Do we have systemd-resolved?
						if _, err := os.Stat("/usr/lib/systemd/systemd-resolved"); err == nil {
							networkModule += " systemd-resolved"
						}
					} else {
						// Fallback: if neither NetworkManager nor systemd-networkd is available on Fedora,
						// add either network or network-legacy based on the version, same as other distros.
						// network-legacy was dropped from 10.0 onwards
						constraint, _ = semver.NewConstraint("<10")
						if constraint.Check(ver) {
							networkModule = "network-legacy"
						} else {
							networkModule = "network"
						}
					}
				} else {
					// On other distros add either network or network-legacy
					// network-legacy was dropped from 10.0 onwards
					constraint, _ = semver.NewConstraint("<10")
					if constraint.Check(ver) {
						networkModule = "network-legacy"
					} else {
						networkModule = "network"
					}
				}
			}
		}

		// Hadron uses the full systemd network stuff
		if sis.Distro == values.Hadron {
			networkModule = "systemd-networkd systemd-resolved"
		}

		l.Logger.Debug().Str("networkModule", networkModule).Bool("sysextModule", sysextModule).Msg("Adding dracut modules to initramfs")

		// Add support for pmem modules to support HTTP EFI boot automatically mounting the served ISO as a livecd
		// This means the UEFI firmware will expose the loaded HTTP Iso memory as a block device for the kernel
		// to find it and mount it as if it was a regular disk
		// Then dracut will find the label and mount it in the proper places
		// Add the dmsquash-live module to the initramfs so we can use it
		// Add network module to the initramfs so we can use it
		// Add immucore module to the initramfs so we can use it
		data = append(data, []schema.Stage{
			{
				Name:     "Add pmem modules to initramfs",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*",
				Files: []schema.File{
					{
						Path:        bundled.DracutPmemPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutPmemConfig,
					},
				},
			},
			{
				Name:     "Add xhci_pci_renesas module to initramfs",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				Files: []schema.File{
					{
						Path:        bundled.DracutXhciRenesasPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutXhciRenesasConfig,
					},
				},
			},
			{
				Name:     "Add sysext module to initramfs",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				If:       strconv.FormatBool(sysextModule),
				Files: []schema.File{
					{
						Path:        bundled.DracutSysextPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutSysextConfig,
					},
				},
			},
			{
				Name:     "Add network module to initramfs",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				Files: []schema.File{
					{
						Path:        bundled.DracutNetworkPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     fmt.Sprintf(bundled.DracutNetworkConfig, networkModule),
					},
				},
			},
			{
				Name:     "Add immucore module to initramfs",
				OnlyIfOs: "Ubuntu.*|Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				Files: []schema.File{
					{
						Path:        bundled.DracutConfigPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.ImmucoreConfigDracut,
					},
					{
						Path:        bundled.DracutImmucoreModuleSetupPath,
						Owner:       0,
						Group:       0,
						Permissions: 0755,
						Content:     bundled.ImmucoreModuleSetupDracut,
					},
					{
						Path:        bundled.DracutImmucoreGeneratorPath,
						Owner:       0,
						Group:       0,
						Permissions: 0755,
						Content:     bundled.ImmucoreGeneratorDracut,
					},
					{
						Path:        bundled.DracutImmucoreServicePath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.ImmucoreServiceDracut,
					},
				},
			},
			// Ubuntu 20.04 does not support the dracut multipath module
			// therefore we don't support multipath for Ubuntu 20.04 and below
			{
				Name:     "Add Multipath module to initramfs for Ubuntu 21.04 and above",
				OnlyIfOs: "Ubuntu.*",
				// Skips the multipath module for Ubuntu 20.04 and below
				// This uses a regex expression and Go does not support lookaheads
				// so we have to hackily use a or condition
				OnlyIfOsVersion: bundled.UbuntuSupportedMultipathVersions,
				Files: []schema.File{
					{
						Path:        bundled.DracutMultipathPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutMultipathConfig,
					},
				},
			},
			{
				Name:     "Add Multipath module to initramfs",
				OnlyIfOs: "Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
				Files: []schema.File{
					{
						Path:        bundled.DracutMultipathPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutMultipathConfig,
					},
				},
			},
			{
				Name: "Disable ISCSI for NVIDIA devices",
				If:   fmt.Sprintf(`[ "%[1]s" = "nvidia-jetson-agx-orin" ] || [ "%[1]s" = "nvidia-jetson-orin-nx" ]`, config.DefaultConfig.Model),
				Files: []schema.File{
					{
						Path:        bundled.DracutSkipScsiPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutSkipIscsi,
					},
				},
				Commands: []string{
					// iscsid causes delays on the login shell, and we don't need it, so we'll disable it
					"systemctl disable iscsi open-iscsi iscsid.socket || true",
				},
			},
			{
				Name: "Omit nvidia drivers loading in the initramfs",
				If:   fmt.Sprintf(`[ "%s" = "nvidia-jetson-thor" ]`, config.DefaultConfig.Model),
				Files: []schema.File{
					{
						Path:        bundled.DracutSkipNvidiaDriversPath,
						Owner:       0,
						Group:       0,
						Permissions: 0644,
						Content:     bundled.DracutSkipNvidiaDrivers,
					},
				},
			},
		}...)

		if config.DefaultConfig.Fips {
			// Add dracut fips support
			data = append(data, []schema.Stage{
				{
					Name:     "Add fips support to initramfs",
					OnlyIfOs: "Debian.*|Fedora.*|CentOS.*|Red\\sHat.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|[Oo]penSUSE.*|SUSE.*|Hadron.*",
					Files: []schema.File{
						{
							Path:        bundled.DracutFipsPath,
							Owner:       0,
							Group:       0,
							Permissions: 0644,
							Content:     bundled.DracutFipsConfig,
						},
					},
				},
			}...)
		}

	}

	return data, nil
}
