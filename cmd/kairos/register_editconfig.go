package main

import (
	editconfig "github.com/kairos-io/kairos/v4/editconfig/pkg/cmd"
)

func init() {
	// edit-config is a top-level sub-tool rather than nested under `agent` or
	// `provider`. Its concern -- editing and validating a cloud-init file --
	// is orthogonal to whatever provider is (or isn't) installed and to the
	// agent's install / upgrade lifecycle, so hiding it behind either would
	// misrepresent when a user is expected to reach for it.
	register("edit-config", editconfig.Run)
}
