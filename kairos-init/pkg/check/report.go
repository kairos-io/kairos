package check

import (
	"fmt"
	"io"
)

// Status is the verdict on one requirement.
type Status string

const (
	// Pass means the requirement is met, or it is left to the packages
	// kairos-init installs for the family.
	Pass Status = "pass"
	// Warn means Kairos works without it, but a feature or a boot mode is not
	// available.
	Warn Status = "warn"
	// Fail means kairos-init cannot turn this system into a Kairos system.
	Fail Status = "fail"
)

// Finding is the verdict on one requirement.
type Finding struct {
	Section string
	Status  Status
	Message string
}

// Report is what the check found.
type Report struct {
	Findings []Finding

	// Family and Distro are what kairos-init makes of the system. They are
	// empty when it knows neither.
	Family string
	Distro string
	// ID is the ID of os-release.
	ID string

	// installs is whether kairos-init installs packages for the family, and so
	// whether a tool that is missing now is left to them.
	installs bool
	// uefiMissing is whether the agent cannot find the files for a UEFI install.
	uefiMissing bool
	efiArch     string
	converted   bool

	fails         int
	warns         int
	identityFails int
}

// Fails is the number of requirements that are missing.
func (r *Report) Fails() int { return r.fails }

// Warns is the number of optional requirements that are missing.
func (r *Report) Warns() int { return r.warns }

// OK is whether nothing required is missing.
func (r *Report) OK() bool { return r.fails == 0 }

// Failures are the findings that make the system unfit.
func (r *Report) Failures() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Status == Fail {
			out = append(out, f)
		}
	}
	return out
}

// Summary is the closing text of the report.
func (r *Report) Summary() string {
	if r.fails > 0 {
		out := fmt.Sprintf("Result: this system cannot become a Kairos system as it is (%d fail, %d warn).\n", r.fails, r.warns)
		if r.installs && r.identityFails == r.fails {
			out += fmt.Sprintf("The %s family can. If this one has the same packages, all that stops it is that the Kairos tools do not recognize its name or version.\n", r.Family)
		}
		return out
	}
	var out string
	switch {
	case r.converted:
		out = fmt.Sprintf("Result: this system has what Kairos needs (%d warn).\n", r.warns)
	case r.Distro != "" && r.Distro != r.ID:
		out = fmt.Sprintf("Result: this system can become a Kairos system, as a derivative of %s (%d warn).\n", r.Distro, r.warns)
	default:
		out = fmt.Sprintf("Result: this system can become a Kairos system (%d warn).\n", r.warns)
	}
	if r.uefiMissing {
		out += "It cannot be installed on a UEFI machine.\n"
		if r.efiArch != archAMD64 {
			out += "Machines with this architecture have no BIOS, so only images for a board such as the Raspberry Pi boot.\n"
		}
	}
	if r.installs {
		out += "The package repositories were not checked. Only a build with kairos-init proves that the packages exist.\n"
	}
	return out
}

// Print writes the report: one line for each requirement, then the result. With
// quiet it leaves out the pass lines and the section titles.
func (r *Report) Print(w io.Writer, quiet bool) {
	section := ""
	for _, f := range r.Findings {
		if quiet && f.Status == Pass {
			continue
		}
		if !quiet && f.Section != section {
			_, _ = fmt.Fprintf(w, "\n%s\n", f.Section)
		}
		section = f.Section
		_, _ = fmt.Fprintf(w, "  %s  %s\n", f.Status, f.Message)
	}
	_, _ = fmt.Fprintf(w, "\n%s", r.Summary())
}
