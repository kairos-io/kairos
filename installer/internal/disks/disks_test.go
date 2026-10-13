package disks

import "testing"

func TestExcluded(t *testing.T) {
	// Virtual devices are never installation targets. A frontend that offered
	// one would let a person, or an agent, install onto a ramdisk.
	for _, name := range []string{"loop0", "loop12", "ram0", "sr0", "zram0"} {
		if !excluded(name) {
			t.Errorf("%s is offered as an installation target", name)
		}
	}

	for _, name := range []string{"sda", "nvme0n1", "vda", "mmcblk0"} {
		if excluded(name) {
			t.Errorf("%s is not offered as an installation target", name)
		}
	}
}

// ghw says "unknown" rather than nothing when the kernel does not report a
// model, which reads as a disk whose model is called "unknown".
func TestModelDropsGHWsUnknown(t *testing.T) {
	for _, in := range []string{"unknown", "Unknown", " unknown ", ""} {
		if got := model(in); got != "" {
			t.Errorf("model(%q) = %q, want empty", in, got)
		}
	}

	if got := model(" Samsung SSD 990 PRO "); got != "Samsung SSD 990 PRO" {
		t.Errorf("model = %q, want the trimmed model", got)
	}
}

func TestHumanSize(t *testing.T) {
	if got := HumanSize(20 << 30); got != "20.00 GiB" {
		t.Errorf("HumanSize = %q, want 20.00 GiB", got)
	}
}
