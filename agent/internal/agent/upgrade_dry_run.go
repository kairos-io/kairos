package agent

import (
	"fmt"
	"io"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// writeUpgradeSummary prints what an upgrade with the given spec would do.
// Every value comes from the resolved spec, so it reflects the cloud-config
// and the partitions detected on this host, not only the command line.
func writeUpgradeSummary(w io.Writer, s *spec.UpgradeSpec) {
	target := "active"
	img := s.Active
	part := s.Partitions.State
	partName := "State partition"
	if s.RecoveryUpgrade() {
		target = constants.BootEntryRecovery
		img = s.Recovery
		part = s.Partitions.Recovery
		partName = "Recovery partition"
	}

	fmt.Fprintln(w, "Upgrade dry run, nothing was changed.")
	fmt.Fprintf(w, "  Target:              %s\n", target)
	fmt.Fprintf(w, "  Source:              %s\n", sourceString(img.Source))
	fmt.Fprintf(w, "  Transition image:    %s\n", img.File)
	fmt.Fprintf(w, "  Image size:          %d MiB\n", img.Size)
	if img.FS != "" {
		fmt.Fprintf(w, "  Image filesystem:    %s\n", img.FS)
	}
	if img.Label != "" {
		fmt.Fprintf(w, "  Image label:         %s\n", img.Label)
	}
	fmt.Fprintf(w, "  %-20s %s\n", partName+":", partitionString(part))
	if len(s.ExcludedPaths) > 0 {
		fmt.Fprintf(w, "  Excluded paths:      %s\n", strings.Join(s.ExcludedPaths, ", "))
	}
	writeSourceNotes(w, img.Source, s.AllowInsecureRegistries)
	fmt.Fprintln(w, "Not checked: the image layers are not pulled, and the free space on the target partition is not compared with the image size.")
}

// writeUkiUpgradeSummary prints what a trusted boot upgrade with the given spec would do.
func writeUkiUpgradeSummary(w io.Writer, s *spec.UpgradeUkiSpec) {
	target := "active"
	if s.Entry != "" {
		target = s.Entry
	}

	fmt.Fprintln(w, "Upgrade dry run, nothing was changed.")
	fmt.Fprintf(w, "  Target:              %s\n", target)
	fmt.Fprintf(w, "  Source:              %s\n", sourceString(s.Active.Source))
	fmt.Fprintf(w, "  Image size:          %d MiB\n", s.Active.Size)
	fmt.Fprintf(w, "  EFI partition:       %s\n", partitionString(s.EfiPartition))
	writeSourceNotes(w, s.Active.Source, s.AllowInsecureRegistries)
	fmt.Fprintln(w, "Not checked: the image layers are not pulled.")
}

func writeSourceNotes(w io.Writer, src *sdkImages.ImageSource, allowInsecure bool) {
	if src == nil {
		return
	}
	switch {
	case src.IsDocker():
		fmt.Fprintln(w, "  Registry:            reachable, image manifest resolved")
		if allowInsecure {
			fmt.Fprintln(w, "  Insecure registries: allowed")
		}
	case src.IsDir():
		fmt.Fprintln(w, "  Note:                the size of a directory source is measured now and can change before a real upgrade")
	}
}

func sourceString(src *sdkImages.ImageSource) string {
	if src == nil || src.IsEmpty() {
		return "none"
	}
	return src.String()
}

func partitionString(p *sdkPartitions.Partition) string {
	if p == nil {
		return "not found"
	}
	device := p.Path
	if device == "" {
		device = "unknown device"
	}
	if p.MountPoint == "" {
		return device
	}
	return fmt.Sprintf("%s mounted at %s", device, p.MountPoint)
}
