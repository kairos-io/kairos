package values

import (
	"sort"
)

// Common Used for packages that are common to whatever key
const Common = "common"

type Architecture string

func (a Architecture) String() string {
	return string(a)
}

const (
	ArchAMD64   Architecture = "amd64"
	ArchARM64   Architecture = "arm64"
	ArchRiscV64 Architecture = "riscv64"
	ArchCommon  Architecture = "common"
)

type Distro string

func (d Distro) String() string {
	return string(d)
}

// Individual distros for when we need to be specific
const (
	Unknown            Distro = "unknown"
	Debian             Distro = "debian"
	Ubuntu             Distro = "ubuntu"
	RedHat             Distro = "rhel"
	RockyLinux         Distro = "rocky"
	AlmaLinux          Distro = "almalinux"
	OracleLinux        Distro = "ol"
	Fedora             Distro = "fedora"
	Arch               Distro = "arch"
	Alpine             Distro = "alpine"
	OpenSUSELeap       Distro = "opensuse-leap"
	OpenSUSETumbleweed Distro = "opensuse-tumbleweed"
	SLES               Distro = "sles"
	SLEMicroRancher    Distro = "sle-micro-rancher"
	Hadron             Distro = "hadron"
)

type Family string

func (f Family) String() string {
	return string(f)
}

// generic families that have things in common and we can apply to all of them
const (
	UnknownFamily Family = "unknown"
	DebianFamily  Family = "debian"
	RedHatFamily  Family = "redhat"
	ArchFamily    Family = "arch"
	AlpineFamily  Family = "alpine"
	SUSEFamily    Family = "suse"
	HadronFamily  Family = "hadron"
)

type Model string              // Model is the type of the system
func (m Model) String() string { return string(m) }

const (
	Generic Model = "generic"
	Rpi3    Model = "rpi3"
	Rpi4    Model = "rpi4"
	AgxOrin  Model = "nvidia-jetson-agx-orin"
	OrinNX   Model = "nvidia-jetson-orin-nx"
	Thor     Model = "nvidia-jetson-thor"
	DgxSpark Model = "nvidia-dgx-spark"
)

var SupportedModels = []Model{Generic, Rpi3, Rpi4, AgxOrin, OrinNX, Thor, DgxSpark}

// modelArch maps a Model to the target architecture that kairos-init must run under.
// Generic is architecture-agnostic and is intentionally omitted.
var modelArch = map[Model]Architecture{
	Rpi3:     ArchARM64,
	Rpi4:     ArchARM64,
	AgxOrin:  ArchARM64,
	OrinNX:   ArchARM64,
	Thor:     ArchARM64,
	DgxSpark: ArchARM64,
}

// RequiredArch returns the target architecture required for this model.
// Returns "" when the model has no architecture constraint (e.g. Generic).
func (m Model) RequiredArch() Architecture {
	return modelArch[m]
}

func SupportedModelStrings() []string {
	s := make([]string, len(SupportedModels))
	for i, m := range SupportedModels {
		s[i] = m.String()
	}
	return s
}

type System struct {
	Name    string
	Distro  Distro
	Family  Family
	Version string
	Arch    Architecture
}

// GetTemplateParams returns a map of parameters that can be used in a template
func GetTemplateParams(s System) map[string]string {
	return map[string]string{
		"distro":  s.Distro.String(),
		"version": s.Version,
		"arch":    s.Arch.String(),
		"family":  s.Family.String(),
	}
}

type StepInfo struct {
	Key   string
	Value string
}

const (
	InitStage            = "init"             // Full init stage
	InstallStage         = "install"          // Full install stage
	InstallPackagesStep  = "installPackages"  // Installs the base system packages
	InstallKernelStep    = "installKernel"    // Installs the kernel packages
	InitrdStep           = "initrd"           // Generates the initrd
	KairosReleaseStep    = "kairosRelease"    // Creates and fills the /etc/kairos-release file
	WorkaroundsStep      = "workarounds"      // Applies workarounds for known issues
	CleanupStep          = "cleanup"          // Cleans up the system of unneeded packages and files
	ServicesStep         = "services"         // Creates and enables required services
	KernelStep           = "kernel"           // Installs the kernel
	KubernetesStep       = "kubernetes"       // Installs the kubernetes provider
	CloudconfigsStep     = "cloudconfigs"     // Installs the cloud-configs for the system
	BrandingStep         = "branding"         // Applies the branding for the system
	GrubStep             = "grub"             // Configures the grub bootloader
	KairosBinariesStep   = "kairosBinaries"   // Installs the kairos binaries
	ProviderBinariesStep = "providerBinaries" // Installs the kairos provider binaries for k8s
	BuildProviderStep    = "buildProvider"    // Builds the provider binaries
	InitramfsConfigsStep = "initramfsConfigs" // Configures the initramfs for the system
	MiscellaneousStep    = "miscellaneous"    // Applies miscellaneous configurations
	SshHardeningStep     = "sshHardening"     // Installs the sshd hardening drop-in and filters weak Diffie-Hellman moduli
)

// StepsInfo returns a slice of StepInfo containing the steps and their descriptions
func StepsInfo() []StepInfo {
	steps := map[string]string{
		InitStage:            "The full init stage, which includes kairosRelease, kubernetes, initrd, services, sshHardening, workarounds and cleanup steps",
		InstallStage:         "The full install stage, which includes installPackages, kubernetes, cloudconfigs, branding, grub, services, kairosBinaries, providerBinaries, initramfsConfigs and miscellaneous steps",
		InstallPackagesStep:  "installs the base system packages",
		InstallKernelStep:    "installs the kernel packages",
		InitrdStep:           "generates the initrd",
		KairosReleaseStep:    "creates and fills the /etc/kairos-release file",
		WorkaroundsStep:      "applies workarounds for known issues",
		CleanupStep:          "cleans up the system of unneeded packages and files",
		ServicesStep:         "creates and enables required services",
		KernelStep:           "installs the kernel",
		KubernetesStep:       "installs the kubernetes provider",
		CloudconfigsStep:     "installs the cloud-configs for the system",
		BrandingStep:         "applies the branding for the system",
		GrubStep:             "configures the grub bootloader",
		KairosBinariesStep:   "installs the kairos binaries",
		ProviderBinariesStep: "installs the kairos provider binaries for k8s",
		BuildProviderStep:    "builds the provider binaries",
		InitramfsConfigsStep: "configures the initramfs for the system",
		MiscellaneousStep:    "applies miscellaneous configurations",
		SshHardeningStep:     "installs the sshd hardening drop-in",
	}
	keys := make([]string, 0, len(steps))
	for k := range steps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]StepInfo, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, StepInfo{Key: k, Value: steps[k]})
	}
	return ordered
}

// GetStepNames returns a slice of step names
func GetStepNames() []string {
	stepsInfo := StepsInfo()
	steps := make([]string, 0, len(stepsInfo))
	for step := range stepsInfo {
		steps = append(steps, stepsInfo[step].Key)
	}
	return steps

}

// AllSuseRegex matches any SUSE-based distribution name.
// AllSuseButMicroRegex matches SLES, openSUSE, or SUSE Linux Enterprise Server names.
// AlpineRegex matches Alpine Linux distribution names.
// RHELFamilyRegex matches RHEL-family distributions such as Fedora, CentOS, Rocky, AlmaLinux, Oracle Linux, and Red Hat.
const (
	AllSuseRegex                 = "SLES.*|[Oo]penSUSE.*|SUSE.*"
	AllSuseButMicroRegex         = "^(?:SLES.*|[Oo]penSUSE.*|SUSE Linux Enterprise Server.*)$"
	AllSuseButMicroAndTumbleweed = "^(?:SLES.*|SUSE Linux Enterprise Server.*|[Oo]penSUSE Leap.*)$"
	OnlyMicroRegex               = "SUSE Linux Enterprise Micro for Rancher.*"
	AlpineRegex                  = "Alpine.*"
	RHELFamilyRegex              = "Fedora.*|CentOS.*|Rocky.*|AlmaLinux.*|Oracle\\sLinux.*|Red\\sHat.*"
)
