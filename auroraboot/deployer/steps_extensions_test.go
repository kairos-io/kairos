package deployer

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
)

// A bundled extension the artifact does not contain cannot be installed from
// it, so every artifact AuroraBoot writes has to carry the extensions the
// build asked for. kairos-io/kairos#5040 was exactly that failure: the netboot
// tree dropped every extension and nothing turned red, because each artifact
// type was tested on its own and no test asked whether the list of artifact
// types was complete.
//
// The census below is that missing question. It reads the ops the deployer
// actually registers out of the herd DAG, not out of the source, and requires
// every one of them to be classified: either it cuts its content from the ISO,
// or it cuts it from the raw disk, or it writes no artifact at all. A new
// output added to RegisterAll fails the census by name until someone decides
// which of the three it is, which is the decision that was skipped for
// netboot.

// artifactLeg names the pipeline an artifact-producing op takes its content
// from. There are only two, and each has a single extension payload: the ISO
// leg reads iso.extensions directly, and the raw-disk leg reads it through
// Deployer.diskExtensions. An op in neither leg writes nothing a user installs
// from.
type artifactLeg string

const (
	legISO     artifactLeg = "iso"
	legRawDisk artifactLeg = "raw-disk"
	legNone    artifactLeg = ""
)

// opLegs classifies every op RegisterAll adds. legNone means the op writes no
// installable artifact: it prepares a directory, copies the cloud config,
// unpacks or downloads the source, or serves what the other ops produced.
var opLegs = map[string]artifactLeg{
	constants.OpPrepareDirs:     legNone,
	constants.OpCopyCloudConfig: legNone,
	constants.OpDumpSource:      legNone,
	constants.OpDownloadISO:     legNone,
	constants.OpInjectCC:        legNone,
	constants.OpStartHTTPServer: legNone,
	constants.OpStartNetboot:    legNone,
	constants.OpGenISO:          legISO,
	constants.OpExtractNetboot:  legISO,
	constants.OpGenEFIRawDisk:   legRawDisk,
	constants.OpGenBIOSRawDisk:  legRawDisk,
	constants.OpConvertGCE:      legRawDisk,
	constants.OpConvertVHD:      legRawDisk,
	constants.OpConvertMAAS:     legRawDisk,
}

// extensionRequest is the one entry every config here asks for. A file://
// source is what lets a test name an extension with no catalog and no
// registry.
func extensionRequest() extensions.Request {
	return extensions.Request{Name: extensions.FileScheme + "/tmp/work.sysext.raw"}
}

// dagOps returns the names of the ops in the deployer's DAG. RegisterAll adds
// every op unconditionally and marks the ones the config did not ask for as
// ignored, so onlyEnabled is what separates "this op exists" from "this build
// runs it".
func dagOps(t *testing.T, c schema.Config, onlyEnabled bool) []string {
	t.Helper()
	d := NewDeployer(c, schema.ReleaseArtifact{ContainerImage: "quay.io/kairos/fixture:latest"})
	if err := RegisterAll(d); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	var names []string
	for _, layer := range d.Analyze() {
		for _, op := range layer {
			if onlyEnabled && op.Ignored {
				continue
			}
			names = append(names, op.Name)
		}
	}
	sort.Strings(names)
	return names
}

// isoBuild and diskBuild are the two shapes a build takes. They are separate
// configs because they are mutually exclusive: StepGenISO and
// StepExtractNetboot are both disabled once a raw disk is requested
// (rawDiskIsSet), so no single config runs every artifact op.
func isoBuild(t *testing.T) schema.Config {
	return schema.Config{
		State: t.TempDir(),
		ISO:   schema.ISO{Extensions: []extensions.Request{extensionRequest()}},
	}
}

func diskBuild(t *testing.T) schema.Config {
	return schema.Config{
		State: t.TempDir(),
		ISO:   schema.ISO{Extensions: []extensions.Request{extensionRequest()}},
		Disk:  schema.Disk{EFI: true, GCE: true, VHD: true, MAAS: true},
	}
}

// biosDiskBuild is the raw-disk shape disk.bios takes on its own, since it
// cannot be combined with disk.efi or the outputs cut from it.
func biosDiskBuild(t *testing.T) schema.Config {
	return schema.Config{
		State: t.TempDir(),
		ISO:   schema.ISO{Extensions: []extensions.Request{extensionRequest()}},
		Disk:  schema.Disk{BIOS: true},
	}
}

// TestEveryRegisteredOpIsClassified is the census. An op the deployer
// registers that opLegs does not know about is an output whose extension
// payload nobody has decided on.
func TestEveryRegisteredOpIsClassified(t *testing.T) {
	for _, op := range dagOps(t, isoBuild(t), false) {
		if _, known := opLegs[op]; !known {
			t.Fatalf("op %q is registered but not classified in opLegs. "+
				"Decide whether it writes an artifact a user installs from: if it does, "+
				"wire the build's iso.extensions into it and record its leg; if it does not, "+
				"record it as legNone. See kairos-io/kairos#5040 for what the unclassified case costs.", op)
		}
	}
}

// TestEachBuildShapeEnablesItsArtifactLeg keeps the census honest. The census
// passes as long as every name is classified, including in a hypothetical
// future where no artifact op is enabled at all, so pin the two shapes to the
// leg each of them is supposed to run.
func TestEachBuildShapeEnablesItsArtifactLeg(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config schema.Config
		want   artifactLeg
		absent artifactLeg
	}{
		{"an iso build runs the iso leg", isoBuild(t), legISO, legRawDisk},
		{"a disk build runs the raw-disk leg", diskBuild(t), legRawDisk, legISO},
		{"a bios disk build runs the raw-disk leg", biosDiskBuild(t), legRawDisk, legISO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[artifactLeg]bool{}
			for _, op := range dagOps(t, tc.config, true) {
				seen[opLegs[op]] = true
			}
			if !seen[tc.want] {
				t.Fatalf("no op of the %q leg is enabled, so this build produces no artifact that could carry an extension", tc.want)
			}
			if seen[tc.absent] {
				t.Fatalf("an op of the %q leg is enabled, which this build shape does not produce; the legs are no longer mutually exclusive and the census needs a third shape", tc.absent)
			}
		})
	}
}

// TestBothArtifactLegsCarryTheOneExtensionSpec pins the two payloads to the
// single iso.extensions spec. The raw-disk leg reads it through its own
// accessor, so nothing but this stops the two from drifting and leaving a disk
// build with no extensions while an ISO build of the same config has them.
func TestBothArtifactLegsCarryTheOneExtensionSpec(t *testing.T) {
	want := extensionRequest()
	c := diskBuild(t)
	c.ISO.ExtensionsCatalogs = []string{"catalog.yaml"}
	d := NewDeployer(c, schema.ReleaseArtifact{})

	isoLeg := d.Config.ISO.Extensions
	if len(isoLeg) != 1 || isoLeg[0] != want {
		t.Fatalf("the ISO leg carries %v, want exactly %v", isoLeg, want)
	}

	diskLeg := d.diskExtensions()
	if len(diskLeg.Requests) != 1 || diskLeg.Requests[0] != want {
		t.Fatalf("the raw-disk leg carries %v, want exactly %v", diskLeg.Requests, want)
	}
	if len(diskLeg.Catalogs) != 1 || diskLeg.Catalogs[0] != "catalog.yaml" {
		t.Fatalf("the raw-disk leg resolves against %v, want the configured catalogs %v",
			diskLeg.Catalogs, c.ISO.ExtensionsCatalogs)
	}
}

// TestACloudImageOnlyBuildStillCarriesTheExtensions covers the shape that is
// easiest to get wrong: disk.gce with no disk.efi. The cloud image is cut from
// a raw disk the caller never named, so if that implied raw build lost the
// extension payload the only artifact produced would be extension-free.
func TestACloudImageOnlyBuildStillCarriesTheExtensions(t *testing.T) {
	c := schema.Config{
		State: t.TempDir(),
		ISO:   schema.ISO{Extensions: []extensions.Request{extensionRequest()}},
		Disk:  schema.Disk{GCE: true},
	}

	enabled := dagOps(t, c, true)
	for _, want := range []string{constants.OpGenEFIRawDisk, constants.OpConvertGCE} {
		if !contains(enabled, want) {
			t.Fatalf("a gce-only build does not run %q, so the cloud image is cut from nothing: %v", want, enabled)
		}
	}

	d := NewDeployer(c, schema.ReleaseArtifact{})
	if got := d.diskExtensions().Requests; len(got) != 1 {
		t.Fatalf("the implied raw disk of a gce-only build carries %v, want the one configured extension", got)
	}
}

// TestTheStepsThatTakeAnExtensionPayloadReceiveTheBuildSpec is the behavioural
// half of the census. The classification above says which ops carry
// extensions; this runs the ones the deployer hands a payload to and proves
// the payload that arrives is the build's.
//
// Each of these three resolves its extensions before it touches a disk, so a
// spec naming an image that is not there makes the op fail by that image's
// name, and nothing else in the op can produce that name. A call site rewired
// to pass an empty payload resolves nothing, gets past that point, and fails
// on the real build work instead, which is the regression this catches.
func TestTheStepsThatTakeAnExtensionPayloadReceiveTheBuildSpec(t *testing.T) {
	const absent = "absent-fixture.sysext.raw"
	c := schema.Config{
		State: t.TempDir(),
		ISO:   schema.ISO{Extensions: []extensions.Request{{Name: extensions.FileScheme + filepath.Join(t.TempDir(), absent)}}},
	}

	// Every op that is handed a DiskExtensions or an schema.ISO carrying the
	// requests. The derived outputs (netboot, the cloud-image conversions) are
	// deliberately absent: they re-read what these produced and take no
	// payload of their own.
	for _, name := range []string{constants.OpGenISO, constants.OpGenEFIRawDisk, constants.OpGenBIOSRawDisk} {
		t.Run(name, func(t *testing.T) {
			// disk.efi and disk.bios cannot be combined, so each raw-disk op
			// gets a config that enables only its own disk.
			c := c
			c.Disk = schema.Disk{EFI: name != constants.OpGenBIOSRawDisk, BIOS: name == constants.OpGenBIOSRawDisk}
			d := NewDeployer(c, schema.ReleaseArtifact{ContainerImage: "quay.io/kairos/fixture:latest"})
			if err := RegisterAll(d); err != nil {
				t.Fatalf("RegisterAll: %v", err)
			}
			entry := d.State(name)
			if len(entry.Callback) == 0 {
				t.Fatalf("op %q has no callback to run", name)
			}
			var err error
			for _, cb := range entry.Callback {
				if err = cb(context.Background()); err != nil {
					break
				}
			}
			if err == nil {
				t.Fatalf("op %q succeeded with an extension image that is not on disk, so it never resolved the build's extensions", name)
			}
			if !strings.Contains(err.Error(), absent) {
				t.Fatalf("op %q failed with %q, which does not name %s: the step did not receive the build's extension spec",
					name, err, absent)
			}
		})
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
