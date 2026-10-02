package validation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/joho/godotenv"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/kernel"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/system"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// Default systemd search paths in order of precedence
var defaultSystemdSearchPaths = []string{
	"/usr/lib/systemd/system",
	"/lib/systemd/system",
	"/etc/systemd/system",
	"/run/systemd/system",
}

type Validator struct {
	Log    logger.KairosLogger
	System values.System
}

func NewValidator(logger logger.KairosLogger) *Validator {
	sis := system.DetectSystem(logger)
	return &Validator{Log: logger, System: sis}
}

// TODO: Validate FIPS. The build-time metadata check lives in the verify-fips
// make target; what remains here is the runtime check -- asking the installed
// binary whether the FIPS module is actually active.
//
// Key it off KAIROS_FIPS in /etc/kairos-release, not config.DefaultConfig.Fips:
// --fips is a local flag on the root command (main.go), so it is not inherited
// by the validate subcommand, and no caller passes it to validate anyway
// (see kairos-init/Dockerfile.test). Inside Validate() that field is always
// false. The release file is the only source of truth available here.

// nolint:gocyclo // Validate walks every distro/arch/model/version constraint and reports each independently; the shape is intentionally one branch per rule so failures point at the exact clause that tripped.
func (v *Validator) Validate() error {
	var multi *multierror.Error

	binaries := []string{
		"immucore",
		"kairos-agent",
		"sudo",
		"less",
		"kcrypt-discovery-challenger",
	}
	// Why do we check for "mount.nfs" ?? Is it that required somehow? Do we consider part of the requirements
	if v.System.Family != values.HadronFamily {
		binaries = append(binaries, "mount.nfs")
	}

	if config.DefaultConfig.Variant == "standard" {
		binaries = append(binaries, "agent-provider-kairos", "kairos", "edgevpn")
	}

	vals, err := godotenv.Read("/etc/kairos-release")
	if err == nil {
		provider := vals["KAIROS_SOFTWARE_VERSION"]
		switch provider {
		case "k3s":
			binaries = append(binaries, "k3s")
		case "k0s":
			binaries = append(binaries, "k0s")
		}
	}

	// Alter path to include our providers path
	originalPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", fmt.Sprintf("%s:%s:%s", "/system/providers/", "/system/discovery/", originalPath))
	// Check binaries
	for _, binary := range binaries {
		path, err := exec.LookPath(binary)
		if err != nil {
			multi = multierror.Append(multi, fmt.Errorf("[BINARIES] could not find binary %s", binary))
		} else {
			v.Log.Logger.Info().Str("path", path).Str("binary", binary).Msg("[BINARIES] Found binary")
			// Check if the binary is executable
			info, err := os.Stat(path)
			if err != nil {
				multi = multierror.Append(multi, fmt.Errorf("[BINARIES] could not stat binary %s: %s", binary, err))
			}
			if info.Mode()&0111 == 0 {
				multi = multierror.Append(multi, fmt.Errorf("[BINARIES] binary %s is not executable", binary))
			} else {
				v.Log.Logger.Info().Str("binary", binary).Msg("[BINARIES] Binary is executable")
			}
		}
	}

	// Restore the path
	_ = os.Setenv("PATH", originalPath)

	checkFiles := []string{"/boot/vmlinuz"}
	if !config.DefaultConfig.TrustedBoot {
		checkFiles = append(checkFiles, "/boot/initrd")
	}
	for _, f := range checkFiles {
		s, err := os.Lstat(f)
		if err != nil {
			multi = multierror.Append(multi, fmt.Errorf("[FILES] file missing %s", f))
			continue
		}
		v.Log.Logger.Info().Str("file", f).Msg("Found file")
		// Check if its a symlink in the vmlinuz case
		if s != nil && s.Mode()&os.ModeSymlink != 0 && f == "/boot/vmlinuz" {
			if err := validateBootFileSymlink(f); err != nil {
				multi = multierror.Append(multi, err)
				continue
			}
			v.Log.Logger.Info().Str("file", f).Msg("File is a symlink and resolves as expected")
		} else {
			v.Log.Logger.Info().Str("file", f).Msg("File is not a symlink")
		}
	}

	// Validate all needed keys are stored in kairos-release
	keys := []string{
		"KAIROS_ID",
		"KAIROS_ID_LIKE", // Maybe not critical? Same as name below
		"KAIROS_NAME",
		"KAIROS_VERSION",
		"KAIROS_ARCH",
		"KAIROS_FIPS",
		"KAIROS_TARGETARCH", // Not critical, same as ARCH above
		"KAIROS_FLAVOR",
		"KAIROS_FLAVOR_RELEASE",
		"KAIROS_FAMILY",
		"KAIROS_MODEL",
		"KAIROS_VARIANT",
		"KAIROS_BUG_REPORT_URL", // Not critical
		"KAIROS_HOME_URL",       // Not critical
		"KAIROS_RELEASE",
	}

	vals, err = godotenv.Read("/etc/kairos-release")
	if err != nil {
		multi = multierror.Append(multi, fmt.Errorf("[RELEASE] could not open kairos-release file"))
	} else {
		for _, key := range keys {
			if vals[key] == "" {
				multi = multierror.Append(multi, fmt.Errorf("[RELEASE] key %s not found or empty in kairos-release", key))
			}
		}
	}

	if config.DefaultConfig.Variant == "standard" {
		if vals["KAIROS_VARIANT"] != "standard" {
			multi = multierror.Append(multi, fmt.Errorf("[RELEASE] KAIROS_VARIANT is not standard"))
		}
		if vals["KAIROS_SOFTWARE_VERSION"] == "" {
			multi = multierror.Append(multi, fmt.Errorf("[RELEASE] KAIROS_SOFTWARE_VERSION is empty"))
		}
		if vals["KAIROS_SOFTWARE_VERSION_PREFIX"] == "" {
			multi = multierror.Append(multi, fmt.Errorf("[RELEASE] KAIROS_SOFTWARE_VERSION_PREFIX is empty"))
		}
	}

	ExpectedDirs := []string{"/var/lock"}

	for _, dir := range ExpectedDirs {
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			multi = multierror.Append(multi, fmt.Errorf("[DIRS] directory %s does not exist", dir))
		}
	}

	// Check if initrd contains the necessary binaries
	// Do it at the ends as its the slowest check
	if !config.DefaultConfig.TrustedBoot {
		// check dracut
		if _, err := exec.LookPath("lsinitrd"); err != nil {
			v.Log.Logger.Warn().Msg("[INITRD] lsinitrd not found, cannot check initrd contents")
		} else {
			v.Log.Logger.Info().Msg("Checking initrd contents")
			out, err := exec.Command("lsinitrd", "/boot/initrd").CombinedOutput()
			if err != nil {
				multi = multierror.Append(multi, fmt.Errorf("[INITRD] failed checking initrd contents: %s", err))
			}
			// Only immucore runs from the initrd (its immucore.service is
			// what the 28immucore dracut module wires in). kairos-agent is a
			// post-switch-root userland tool, driven by the cloud-config
			// systemd units in 02_agent.yaml and 09_systemd_services.yaml,
			// and the dracut module does not install a kairos-agent name
			// into the initrd's PATH, so lsinitrd will never surface one.
			for _, binary := range []string{"immucore"} {
				if !strings.Contains(string(out), binary) {
					multi = multierror.Append(multi, fmt.Errorf("[INITRD] did not find %s in the initrd", binary))
				} else {
					v.Log.Logger.Info().Str("binary", binary).Msg("Found binary in the initrd")
				}
			}

			// Verify kernel modules that we force-include via dracut add_drivers made it in.
			// Warn-only: some kernels (minimal builds, non-x86 arches) simply don't ship the
			// module, in which case dracut silently drops the add_drivers directive. That's
			// not a build failure — but on kernels that DO ship it, a miss here means the
			// dracut config was never applied and BMC virtual media boots would break on
			// affected server generations (HPE iLO, Dell iDRAC, Supermicro BMC) — the
			// dependency is on the BMC controller hardware/firmware, not the server chipset.
			// Check both the canonical underscored name and the dashed variant since
			// lsinitrd may render the module filename with either form.
			for _, module := range []string{"xhci_pci_renesas"} {
				dashed := strings.ReplaceAll(module, "_", "-")
				found := ""
				if strings.Contains(string(out), module) {
					found = module
				} else if strings.Contains(string(out), dashed) {
					found = dashed
				}
				if found == "" {
					v.Log.Logger.Warn().Str("module", module).Msg("[INITRD] kernel module not found in initrd (may be absent from kernel package)")
				} else {
					v.Log.Logger.Info().Str("module", found).Msg("Found kernel module in the initrd")
				}
			}

			if err := validateSplashInInitrd(bundled.SplashBinaryPath, string(out)); err != nil {
				multi = multierror.Append(multi, err)
			} else {
				v.Log.Logger.Info().Msg("Boot splash initrd check passed")
			}
		}
	}

	// Check if there are any ssh host keys in /etc/ssh
	matches, err := filepath.Glob("/etc/ssh/ssh_host_*_key")
	if err != nil {
		multi = multierror.Append(multi, fmt.Errorf("[SSH] error checking for SSH host keys: %s", err))
	}
	if len(matches) > 0 {
		multi = multierror.Append(multi, fmt.Errorf("[SSH] found SSH host keys in the system: %v", matches))
	} else {
		v.Log.Logger.Info().Msg("No SSH host keys found bundled in the system")
	}

	// Check service validations
	if err := v.ValidateServices(); err != nil {
		multi = multierror.Append(multi, err)
	}

	// Validate exactly one kernel is installed
	if err := v.ValidateKernel(); err != nil {
		multi = multierror.Append(multi, err)
	}

	if multi.ErrorOrNil() == nil {
		v.Log.Logger.Info().Msg("System validation passed")
	}

	return multi.ErrorOrNil()
}

// validateSplashInInitrd checks that an image carrying the boot splash binary
// also has the splash dracut module inside the initrd.
//
// The splash is optional, so the binary on the rootfs is what decides whether
// the module is expected. installSplashBinary writes nothing at
// bundled.SplashBinaryPath when there is no multi-call binary to point it at,
// and the 50kairos-splash module's check() then drops the module. That is an
// image built without a splash, not a broken one, so there is nothing to
// report.
//
// When the binary is on the rootfs and the module is not in the initrd, the
// initrd was built before the binary was installed, or a cached initrd was
// reused. Both ship an image whose animation never draws in the initramfs
// half of the boot, and both leave every other check green: the units, the
// dracut config and the module source are all written to the rootfs by an
// earlier stage, so inspecting the rootfs cannot tell the two cases apart.
// Only the initrd can.
//
// The path is an argument so a test can exercise this against a temporary
// tree; the caller passes the real one.
func validateSplashInInitrd(splashBinaryPath, lsinitrdOutput string) error {
	// Lstat, not Stat: the default install is a symlink to the multi-call
	// binary, and Stat on a dangling one would report the splash as absent
	// and skip a check that should run.
	if _, err := os.Lstat(splashBinaryPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("[INITRD] failed checking for the boot splash binary at %s: %s", splashBinaryPath, err)
	}

	// What the initrd should hold is always the real installed paths, never
	// splashBinaryPath: that argument says where to look on the rootfs, and
	// a test points it at a temporary tree, but the module copies the binary
	// in under its own name either way.
	//
	// Two entries, not one substring: the module installs the executable and
	// the unit separately, and an initrd with only one of them is a splash
	// that cannot start. lsinitrd prints paths without a leading slash, so
	// the binary is matched on its path without one.
	//
	// The service file name carries no directory, because the module
	// installs it under dracut's systemdsystemunitdir, which differs between
	// base images.
	want := []string{
		strings.TrimPrefix(bundled.SplashBinaryPath, "/"),
		filepath.Base(bundled.DracutSplashServicePath),
	}

	var missing []string
	for _, entry := range want {
		if !strings.Contains(lsinitrdOutput, entry) {
			missing = append(missing, entry)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("[INITRD] %s is installed but the boot splash dracut module is not in the initrd, missing: %s", splashBinaryPath, strings.Join(missing, ", "))
	}

	return nil
}

// ValidateServices performs comprehensive service validations for all systemd-based flavors
func (v *Validator) ValidateServices() error {
	var multi *multierror.Error

	// Check RHEL family specific service validations
	if err := v.ValidateRHELServices(); err != nil {
		multi = multierror.Append(multi, err)
	}

	// Check getty service validations for all systemd-based flavors
	if err := v.ValidateGettyServices(); err != nil {
		multi = multierror.Append(multi, err)
	}

	return multi.ErrorOrNil()
}

// lookupSystemdServiceFile checks if a service file exists in the provided systemd search paths
// Returns the path where the service was found, or an error if not found
func (v *Validator) lookupSystemdServiceFile(serviceName string, searchPaths []string) (string, error) {
	for _, path := range searchPaths {
		servicePath := filepath.Join(path, serviceName)
		if _, err := os.Stat(servicePath); err == nil {
			return servicePath, nil
		}
	}

	return "", fmt.Errorf("service %s not found in systemd search paths", serviceName)
}

// ValidateRHELServices checks that critical systemd services are not masked on RHEL family systems
func (v *Validator) ValidateRHELServices() error {
	return v.ValidateRHELServicesWithPaths(defaultSystemdSearchPaths)
}

// ValidateRHELServicesWithPaths checks that critical systemd services exist and are not masked on RHEL family systems
// This method is used for testing by allowing custom systemd search paths
func (v *Validator) ValidateRHELServicesWithPaths(searchPaths []string) error {
	var multi *multierror.Error

	if v.System.Family != values.RedHatFamily {
		// Not a RHEL family system, skip validation
		return nil
	}

	v.Log.Logger.Info().Msg("Checking RHEL family service validations")
	services := []string{"systemd-udevd", "systemd-logind"}

	for _, service := range services {
		serviceName := fmt.Sprintf("%s.service", service)

		// Check if the service exists in the systemd search path
		servicePath, err := v.lookupSystemdServiceFile(serviceName, searchPaths)
		if err != nil {
			multi = multierror.Append(multi, fmt.Errorf("[SERVICES] service %s does not exist on RHEL family system", service))
			continue
		}

		// Check if the service is masked (symlink to /dev/null)
		if target, err := os.Readlink(servicePath); err == nil && target == "/dev/null" {
			multi = multierror.Append(multi, fmt.Errorf("[SERVICES] service %s is masked on RHEL family system", service))
		} else {
			v.Log.Logger.Info().Str("service", service).Msg("Service exists and is not masked")
		}
	}

	return multi.ErrorOrNil()
}

// ValidateGettyServices checks that getty.target is not masked on systemd-based flavors
func (v *Validator) ValidateGettyServices() error {
	return v.ValidateGettyServicesWithPaths(defaultSystemdSearchPaths)
}

// ValidateKernel checks that the kernel chooser can find a valid kernel under /lib/modules.
func (v *Validator) ValidateKernel() error {
	return v.ValidateKernelWithPath("/lib/modules", config.DefaultConfig.Model)
}

// ValidateKernelWithPath checks that the kernel chooser can find a valid kernel in the given
// modules path for the specified model.  It uses the same selection logic as the init kernel
// step so that the validation and the actual kernel selection stay in sync.
// model is the machine model string (e.g. "rpi4", "generic"), used for model-specific logic.
func (v *Validator) ValidateKernelWithPath(modulesPath, model string) error {
	kernelVersion, err := kernel.GetLatestFromPath(modulesPath, model, v.Log)
	if err != nil {
		return fmt.Errorf("[KERNEL] %w", err)
	}
	v.Log.Logger.Info().Str("kernel", kernelVersion).Msg("[KERNEL] Found kernel")
	return nil
}

// validateBootFileSymlink checks that a boot symlink resolves to an existing file.
// Relative targets (e.g. Debian riscv64 vmlinux-* links) are resolved from the
// symlink directory, not the process working directory.
func validateBootFileSymlink(linkPath string) error {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return fmt.Errorf("%s symlink is not a valid symlink", linkPath)
	}

	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return fmt.Errorf("[FILES] symlink %s points to a non-existent file %s", linkPath, target)
	}

	if _, err = os.Stat(resolved); os.IsNotExist(err) {
		return fmt.Errorf("[FILES] symlink %s points to a non-existent file %s", linkPath, target)
	}

	return nil
}

// ValidateGettyServicesWithPaths checks that getty.target is not masked on systemd-based flavors
// This method is used for testing by allowing custom systemd search paths
func (v *Validator) ValidateGettyServicesWithPaths(searchPaths []string) error {
	var multi *multierror.Error

	// Only validate on systemd-based flavors (skip Alpine which uses OpenRC)
	if v.System.Family == values.AlpineFamily {
		// Alpine uses OpenRC, not systemd, so skip validation
		return nil
	}

	v.Log.Logger.Info().Msg("Checking getty service validations")
	services := []string{"getty.target"}

	for _, service := range services {
		// Check if the service exists in the systemd search path
		servicePath, err := v.lookupSystemdServiceFile(service, searchPaths)
		if err != nil {
			multi = multierror.Append(multi, fmt.Errorf("[SERVICES] service %s does not exist on systemd-based system", service))
			continue
		}

		// Check if the service is masked (symlink to /dev/null)
		if target, err := os.Readlink(servicePath); err == nil && target == "/dev/null" {
			multi = multierror.Append(multi, fmt.Errorf("[SERVICES] service %s is masked on systemd-based system", service))
		} else {
			v.Log.Logger.Info().Str("service", service).Msg("Service exists and is not masked")
		}
	}

	return multi.ErrorOrNil()
}
