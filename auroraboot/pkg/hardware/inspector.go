package hardware

import (
	"context"
	"fmt"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

// Requirements defines the minimum hardware requirements for deployment.
type Requirements struct {
	MinMemoryGiB     int
	MinStorageGiB    int
	MinCPUs          int
	RequiredFeatures []string
}

// systemInspector is the subset of the Redfish Deployer the inspector needs. It
// keeps gofish types out of pkg/hardware (only our own redfish.SystemInfo crosses
// the boundary) and lets tests supply a fake.
type systemInspector interface {
	Inspect(ctx context.Context) (*redfish.SystemInfo, error)
}

// Inspector handles hardware inspection and validation.
type Inspector struct {
	client systemInspector
}

// NewInspector creates a new hardware inspector backed by a Redfish Deployer (or
// any value implementing the inspection contract).
func NewInspector(client systemInspector) *Inspector {
	return &Inspector{
		client: client,
	}
}

// InspectSystem performs a comprehensive hardware inspection.
func (i *Inspector) InspectSystem(ctx context.Context) (*SystemInfo, error) {
	sysInfo, err := i.client.Inspect(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting system info: %w", err)
	}

	// Convert to our internal SystemInfo type. Memory/CPU now arrive populated
	// from the Deployer's typed ComputerSystem read (no longer 0/0).
	info := &SystemInfo{
		MemoryGiB:      sysInfo.MemoryGiB,
		ProcessorCount: sysInfo.ProcessorCount,
		Model:          sysInfo.Model,
		Manufacturer:   sysInfo.Manufacturer,
		SerialNumber:   sysInfo.SerialNumber,
		Features:       sysInfo.Features,
	}

	return info, nil
}

// SystemInfo represents the inspected system information.
type SystemInfo struct {
	MemoryGiB      int
	ProcessorCount int
	Model          string
	Manufacturer   string
	SerialNumber   string
	// Features are the capabilities the Redfish Deployer positively detected for
	// this system (keyed by feature name; value always true). A feature absent
	// from this map was NOT detected and ValidateRequirements fails closed on it.
	Features map[string]bool
}

// knownFeatures is the set of feature names AuroraBoot understands and can
// detect. A required feature outside this set is reported as unknown (rather than
// merely unsupported) so the operator gets an actionable error instead of a
// silent pass. Compared case-insensitively.
var knownFeatures = map[string]struct{}{
	strings.ToUpper(redfish.FeatureUEFI):       {},
	strings.ToUpper(redfish.FeatureSecureBoot): {},
}

// ValidateRequirements checks if the system meets the minimum requirements.
func (i *Inspector) ValidateRequirements(info *SystemInfo, reqs *Requirements) error {
	if info.MemoryGiB < reqs.MinMemoryGiB {
		return fmt.Errorf("insufficient memory: %d GiB (minimum: %d GiB)",
			info.MemoryGiB, reqs.MinMemoryGiB)
	}

	if info.ProcessorCount < reqs.MinCPUs {
		return fmt.Errorf("insufficient CPUs: %d (minimum: %d)",
			info.ProcessorCount, reqs.MinCPUs)
	}

	for _, feature := range reqs.RequiredFeatures {
		name := strings.TrimSpace(feature)
		if name == "" {
			continue
		}
		// Fail closed on a feature AuroraBoot cannot reason about: requiring it
		// would otherwise pass silently, which is exactly the bug this gate fixes.
		if !isKnownFeature(name) {
			return fmt.Errorf("required feature %q is not known to AuroraBoot (cannot be verified); known features: UEFI, SecureBoot", name)
		}
		if !i.hasFeature(info, name) {
			return fmt.Errorf("required feature %q is not supported by this system", name)
		}
	}

	return nil
}

// isKnownFeature reports whether name is a feature AuroraBoot can detect.
func isKnownFeature(name string) bool {
	_, ok := knownFeatures[strings.ToUpper(name)]
	return ok
}

// hasFeature reports whether the inspected system advertises the given feature.
// It consults the capability set the Redfish Deployer detected (see
// pkg/redfish/features.go), matching case-insensitively. A feature the Deployer
// did not detect is reported as absent — the gate fails closed rather than
// assuming support.
func (i *Inspector) hasFeature(info *SystemInfo, feature string) bool {
	for name, present := range info.Features {
		if present && strings.EqualFold(name, feature) {
			return true
		}
	}
	return false
}
