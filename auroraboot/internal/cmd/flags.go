package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

// AllowInsecureRegistriesFlag is shared by every command that pulls container
// images, so the flag is defined once and behaves consistently. urfave/cli v2
// has no persistent-flag mechanism; defining it as a global app flag would force
// it to be passed before the subcommand (e.g.
// "auroraboot --allow-insecure-registries build-iso ..."), breaking the natural
// "build-iso --allow-insecure-registries ..." form, so we share a single flag
// value across the commands that need it instead.
//
// The legacy "insecure" name is kept as a deprecated alias so scripts written
// against v0.22.0 keep working.
var AllowInsecureRegistriesFlag = &cli.BoolFlag{
	Name:    "allow-insecure-registries",
	Aliases: []string{"insecure"},
	Usage:   "Allow pulling container images from registries over plain HTTP or with untrusted/self-signed TLS certificates. The --insecure alias is deprecated; use --allow-insecure-registries instead",
}

// wholeStringSliceFlag is a repeatable string flag that keeps every value as
// given. cli.StringSliceFlag splits each value on commas, which cuts kernel
// cmdline arguments such as console=ttyS0,115200n8 apart. Read the values with
// wholeStringSliceValues, not ctx.StringSlice.
type wholeStringSliceFlag struct {
	cli.StringSliceFlag
}

type wholeStringSlice struct {
	values []string
}

// wholeStringSliceSerialPrefix marks a serialized wholeStringSlice. cli copies
// a flag value between its names by serializing it and calling Set again. The
// prefix starts with a NUL byte, which no command line argument can hold.
const wholeStringSliceSerialPrefix = "\x00wholeStringSlice\x00"

func (s *wholeStringSlice) Set(value string) error {
	if serialized, ok := strings.CutPrefix(value, wholeStringSliceSerialPrefix); ok {
		return json.Unmarshal([]byte(serialized), &s.values)
	}
	s.values = append(s.values, strings.TrimSpace(value))
	return nil
}

func (s *wholeStringSlice) Serialize() string {
	serialized, _ := json.Marshal(s.values)
	return wholeStringSliceSerialPrefix + string(serialized)
}

func (s *wholeStringSlice) String() string {
	return fmt.Sprintf("%s", s.values)
}

// Apply registers a fresh value on every run, so values from one run do not
// leak into the next.
func (f *wholeStringSliceFlag) Apply(set *flag.FlagSet) error {
	value := &wholeStringSlice{}
	for _, name := range f.Names() {
		set.Var(value, name, f.Usage)
	}
	return nil
}

func wholeStringSliceValues(ctx *cli.Context, name string) []string {
	if value, ok := ctx.Generic(name).(*wholeStringSlice); ok {
		return value.values
	}
	return nil
}
