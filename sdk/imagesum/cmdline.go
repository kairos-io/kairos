package imagesum

import (
	"fmt"
	"strconv"
	"strings"
)

// The kernel cmdline spelling of each key is the dot notation of the config key,
// the format every Kairos config key has on the cmdline. There is one spelling
// per key and no alternates.
const (
	CmdlineCreate  = "image-checksum.create"
	CmdlineVerify  = "image-checksum.verify"
	CmdlineRequire = "image-checksum.require"
)

// Overrides are the values found on the kernel cmdline. A nil field was not
// given. They win over the cloud config and over the policy file.
type Overrides struct {
	Create  *bool
	Verify  *Action
	Require *bool
	// Problems lists values that could not be parsed. Such a value is ignored,
	// so the next source applies, and it is up to the caller to log it.
	Problems []string
}

// ParseCmdline reads the three keys from the text of a kernel cmdline. When a key
// is given more than once the last one wins.
func ParseCmdline(cmdline string) Overrides {
	var o Overrides
	for _, tok := range strings.Fields(cmdline) {
		key, val, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"`)
		switch key {
		case CmdlineCreate, CmdlineRequire:
			b, err := strconv.ParseBool(val)
			if err != nil {
				o.Problems = append(o.Problems, fmt.Sprintf("%s=%s: want true or false", key, val))
				continue
			}
			if key == CmdlineCreate {
				o.Create = &b
			} else {
				o.Require = &b
			}
		case CmdlineVerify:
			a, ok := ParseAction(val)
			if !ok {
				o.Problems = append(o.Problems, fmt.Sprintf("%s=%s: want none, warn or fail", key, val))
				continue
			}
			o.Verify = &a
		}
	}
	return o
}

// Apply overwrites the fields of c that the cmdline set.
func (c *Config) Apply(o Overrides) {
	if o.Create != nil {
		c.Create = o.Create
	}
	if o.Verify != nil {
		c.Verify = *o.Verify
	}
	if o.Require != nil {
		c.Require = o.Require
	}
}
