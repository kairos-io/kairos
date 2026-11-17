package values

import (
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// The after-install QSPI firmware check reads the bootloader version and stages
// the capsule payload, both of which are shipped by nvidia-l4t-bootloader.
// Without it in the Thor image the hook aborts every Thor install and there is
// no capsule to stage. Cover the real Thor package-map resolution path.
// See kairos-io/kairos#4228.
func TestThorKernelPackagesIncludeBootloader(t *testing.T) {
	prev := config.DefaultConfig.Model
	config.DefaultConfig.Model = Thor.String()
	t.Cleanup(func() { config.DefaultConfig.Model = prev })

	l := logger.NewKairosLogger("test", "error", false)
	pkgs, err := GetKernelPackages(System{
		Distro:  Ubuntu,
		Arch:    ArchARM64,
		Version: "22.04",
	}, l)
	if err != nil {
		t.Fatalf("GetKernelPackages: %v", err)
	}

	for _, p := range pkgs {
		if p == "nvidia-l4t-bootloader" {
			return
		}
	}
	t.Fatalf("Thor kernel packages must include nvidia-l4t-bootloader "+
		"(needed for the QSPI capsule payload and version); got %v", pkgs)
}

func TestBasePackagesExclude(t *testing.T) {
	cases := []struct {
		name    string
		model   Model
		system  System
		missing string
	}{
		{
			name:    "opensuse leap without tmux",
			model:   Generic,
			system:  System{Distro: OpenSUSELeap, Family: SUSEFamily, Arch: ArchAMD64, Version: "15.6"},
			missing: "tmux",
		},
		{
			name:    "opensuse tumbleweed without tmux",
			model:   Generic,
			system:  System{Distro: OpenSUSETumbleweed, Family: SUSEFamily, Arch: ArchARM64, Version: "20250101"},
			missing: "tmux",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := config.DefaultConfig.Model
			config.DefaultConfig.Model = tc.model.String()
			t.Cleanup(func() { config.DefaultConfig.Model = prev })

			l := logger.NewKairosLogger("test", "error", false)
			pkgs, err := GetPackages(tc.system, l)
			if err != nil {
				t.Fatalf("GetPackages: %v", err)
			}
			if len(pkgs) == 0 {
				t.Fatalf("GetPackages returned no packages for %+v", tc.system)
			}
			for _, p := range pkgs {
				if p == tc.missing {
					t.Fatalf("%s must not be in the base packages for %+v; got %v", tc.missing, tc.system, pkgs)
				}
			}
		})
	}
}

// The SELinux stack is installed on openSUSE and SLES but not on SLE Micro
// for Rancher 5.4 and 5.5: there the Leap 15.5 OSS repo workaround removes
// policycoreutils 3.4 and zypper then cannot install the 3.1 one.
// See kairos-io/kairos#6101.
func TestSUSEBasePackagesSELinux(t *testing.T) {
	selinuxPkgs := []string{"policycoreutils", "selinux-policy-targeted"}
	cases := []struct {
		name   string
		system System
		want   bool
	}{
		{"sle micro rancher 5.4", System{Distro: SLEMicroRancher, Family: SUSEFamily, Arch: ArchAMD64, Version: "5.4"}, false},
		{"sle micro rancher 5.5", System{Distro: SLEMicroRancher, Family: SUSEFamily, Arch: ArchAMD64, Version: "5.5"}, false},
		{"opensuse leap 15.6", System{Distro: OpenSUSELeap, Family: SUSEFamily, Arch: ArchAMD64, Version: "15.6"}, true},
		{"opensuse leap 16.0", System{Distro: OpenSUSELeap, Family: SUSEFamily, Arch: ArchARM64, Version: "16.0"}, true},
		{"opensuse tumbleweed", System{Distro: OpenSUSETumbleweed, Family: SUSEFamily, Arch: ArchAMD64, Version: "20250101"}, true},
		{"sles 15.6", System{Distro: SLES, Family: SUSEFamily, Arch: ArchAMD64, Version: "15.6"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := config.DefaultConfig.Model
			config.DefaultConfig.Model = Generic.String()
			t.Cleanup(func() { config.DefaultConfig.Model = prev })

			l := logger.NewKairosLogger("test", "error", false)
			pkgs, err := GetPackages(tc.system, l)
			if err != nil {
				t.Fatalf("GetPackages: %v", err)
			}
			if len(pkgs) == 0 {
				t.Fatalf("GetPackages returned no packages for %+v", tc.system)
			}
			for _, want := range selinuxPkgs {
				got := false
				for _, p := range pkgs {
					if p == want {
						got = true
						break
					}
				}
				if got != tc.want {
					t.Errorf("%s in the base packages for %+v: got %v, want %v", want, tc.system, got, tc.want)
				}
			}
		})
	}
}
