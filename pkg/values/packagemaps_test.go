package values

import (
	"testing"

	"github.com/kairos-io/kairos-init/pkg/config"
	"github.com/kairos-io/kairos-sdk/types/logger"
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
