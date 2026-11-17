package deployer

import (
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
)

// rawDiskIsSet must treat a MAAS output the same as the other raw-disk outputs
// so the ISO-only steps are correctly skipped when a MAAS image is requested.
func TestRawDiskIsSetWithMAAS(t *testing.T) {
	d := NewDeployer(schema.Config{Disk: schema.Disk{MAAS: true}}, schema.ReleaseArtifact{})
	if !d.rawDiskIsSet() {
		t.Fatal("rawDiskIsSet should be true when Disk.MAAS is set")
	}
}

// opEnabled reports whether the named op is registered in the DAG and enabled
// (not ignored by its EnableIf predicate).
func opEnabled(d *Deployer, name string) (found, enabled bool) {
	for _, layer := range d.Analyze() {
		for _, op := range layer {
			if op.Name == name {
				return true, !op.Ignored
			}
		}
	}
	return false, false
}

// With MAAS requested, the raw-disk generation step and the MAAS conversion step
// must both be registered and enabled in the DAG.
func TestStepConvertMAASEnabledForMAAS(t *testing.T) {
	d := NewDeployer(schema.Config{Disk: schema.Disk{MAAS: true}}, schema.ReleaseArtifact{})
	if err := RegisterAll(d); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	if _, enabled := opEnabled(d, constants.OpGenEFIRawDisk); !enabled {
		t.Errorf("%s should be enabled when Disk.MAAS is set", constants.OpGenEFIRawDisk)
	}
	if found, enabled := opEnabled(d, constants.OpConvertMAAS); !found || !enabled {
		t.Errorf("%s should be registered and enabled when Disk.MAAS is set (found=%v enabled=%v)", constants.OpConvertMAAS, found, enabled)
	}
}

// Without MAAS (and no other raw-disk output), the MAAS conversion step must be
// registered but disabled so it does not run for plain ISO builds.
func TestStepConvertMAASDisabledWithoutMAAS(t *testing.T) {
	d := NewDeployer(schema.Config{}, schema.ReleaseArtifact{})
	if err := RegisterAll(d); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	found, enabled := opEnabled(d, constants.OpConvertMAAS)
	if !found {
		t.Fatalf("%s should be registered", constants.OpConvertMAAS)
	}
	if enabled {
		t.Errorf("%s should be disabled when Disk.MAAS is not set", constants.OpConvertMAAS)
	}
}
