package schema

import (
	"strings"
	"unicode"
)

// ValidHeaders are the first-line comments that mark a document as a Kairos
// cloud-config. #cloud-config is the one to write. The other two are legacy
// and are still read.
var ValidHeaders = []string{"#cloud-config", "#kairos-config", "#node-config"}

// HasValidHeader reports whether the document carries a Kairos cloud-config
// header.
//
// The header does not have to be the first line. cloud-init allows a comment
// block, and a jinja template declaration, above it, so the first nine lines
// are searched. Trailing whitespace on the header line is ignored. See
// https://cloudinit.readthedocs.io/en/latest/explanation/instancedata.html#example-cloud-config-with-instance-data
//
// The line has to be the header and nothing else, so that #cloud-configuration
// is not read as #cloud-config.
//
// This is the rule that decides whether a file under /oem, or the body behind
// config_url, is applied, so it is also the rule the validator has to apply:
// a document the system boots with has to validate, and one it skips has to
// fail.
func HasValidHeader(data string) bool {
	for _, line := range strings.SplitN(data, "\n", 10) {
		// Check the prefix first, so that a very long line is not trimmed for
		// nothing.
		if !strings.HasPrefix(line, "#") {
			continue
		}

		header := strings.TrimRightFunc(line, unicode.IsSpace)
		for _, valid := range ValidHeaders {
			if header == valid {
				return true
			}
		}
	}

	return false
}
