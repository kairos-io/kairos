package validation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/joho/godotenv"
	"github.com/kairos-io/kairos-init/pkg/config"
	"github.com/kairos-io/kairos-init/pkg/kernel"
	"github.com/kairos-io/kairos-init/pkg/system"
	"github.com/kairos-io/kairos-init/pkg/values"
	"github.com/kairos-io/kairos-sdk/types/logger"
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

// TODO: Validate fips, if enabled, check go binaries for boringcrypto

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
			for _, binary := range []string{"immucore", "kairos-agent"} {
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
