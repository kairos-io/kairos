package redfish

// This file exposes a few internal seams to the external redfish_test package so
// the fake-BMC-driven specs can exercise compiled quirk profiles end-to-end
// without making the quirks seam itself public. It is compiled only under `go
// test`.

// InstallProfileForTest compiles a YAML quirk profile and installs it on the
// Deployer, replacing whatever profile NewDeployer selected. It lets a spec drive
// a YAML-compiled profile through the same flow as the in-tree Go profiles, which
// is how the YAML-ilo ≡ Go-ilo equivalence is asserted.
func (d *Deployer) InstallProfileForTest(yamlProfile []byte) error {
	q, err := LoadProfile(yamlProfile)
	if err != nil {
		return err
	}
	d.quirks = q
	return nil
}

// InstallGoProfileForTest installs the named in-tree Go profile on the Deployer
// (e.g. "ilo"), so a spec can pin the Go producer regardless of VendorType
// selection. It reports whether the name was known.
func (d *Deployer) InstallGoProfileForTest(name string) bool {
	q, ok := quirksByName(name)
	if !ok {
		return false
	}
	d.quirks = q
	return true
}
