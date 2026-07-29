package utils

import (
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/mudler/yip/pkg/console"
	"github.com/mudler/yip/pkg/executor"
	"github.com/twpayne/go-vfs/v4"
	"gopkg.in/yaml.v3"
)

func RunStage(stage string) error {
	var allErrors, err error

	// Set debug logger
	yip := executor.NewExecutor(executor.WithLogger(KLog))
	c := ImmucoreConsole{}

	stageBefore := fmt.Sprintf("%s.before", stage)
	stageAfter := fmt.Sprintf("%s.after", stage)

	// Run all stages for each of the default cloud config paths + extra cloud config paths
	for _, s := range []string{stageBefore, stage, stageAfter} {
		err = yip.Run(s, vfs.OSFS, c, constants.GetCloudInitPaths()...)
		if err != nil {
			allErrors = multierror.Append(allErrors, err)
		}
	}

	// Kairos-owned cmdline stanzas (kairos.config_url= and legacy bare cos.setup=)
	// resolve to a config source URI which we hand to yip as an additional stage source.
	// Parsing is delegated to kairos-sdk/machine so immucore, kairos-agent and any other
	// consumer stay in lockstep. See kairos-sdk PR #812.
	if uri := KairosConfigURIFromCmdline(); uri != "" {
		for _, s := range []string{stageBefore, stage, stageAfter} {
			err = yip.Run(s, vfs.OSFS, c, uri)
			if err != nil {
				allErrors = multierror.Append(allErrors, err)
			}
		}
	}

	// Enable dot notation via the SDK parser so that arbitrary dot-nested cmdline tokens
	// (e.g. stages.initramfs[0].name=foo) become yip stages, while Kairos-owned prefixes
	// (kairos.config=, kairos.config_url=, cos.setup=) are skipped and handled above.
	yip.Modifier(SDKDotNotationModifier)

	// Read and parse the cmdline looking for yip config in there
	cmdLineOut, err := os.ReadFile(GetHostProcCmdline())
	if err == nil {
		for _, s := range []string{stageBefore, stage, stageAfter} {
			err = yip.Run(s, vfs.OSFS, console.NewStandardConsole(), string(cmdLineOut))
			if err != nil {
				allErrors = checkYAMLError(allErrors, err)
			}
		}
	}

	// Set back the modifier to nil
	yip.Modifier(nil)

	// Not doing anything with the errors yet, need to know which ones are permissible (no metadata, marshall errors, etc..)
	return nil
}

// KairosConfigURIFromCmdline reads the kernel cmdline (via GetHostProcCmdline so tests
// can mock it) and returns the config source URI extracted from any Kairos-owned stanza
// (kairos.config_url=URI or legacy bare cos.setup=URI). Returns empty when no such stanza
// is present or when the parsed YAML contains only nested key=value payloads (which are
// consumed by kairos-agent, not immucore). Parsing goes through kairos-sdk/machine so
// immucore stays consistent with kairos-agent's config collector.
func KairosConfigURIFromCmdline() string {
	dat, err := os.ReadFile(GetHostProcCmdline())
	if err != nil {
		return ""
	}
	return KairosConfigURIFromString(string(dat))
}

// KairosConfigURIFromString is the string-based counterpart of KairosConfigURIFromCmdline.
// Exposed so tests can exercise every cmdline variant without touching the filesystem.
func KairosConfigURIFromString(s string) string {
	yml, err := machine.KairosCmdlineYAMLFromString(s)
	if err != nil || len(yml) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(yml, &m); err != nil {
		return ""
	}
	if v, ok := m["config_url"].(string); ok {
		return v
	}
	return ""
}

// SDKDotNotationModifier converts dot-nested KEY=VALUE cmdline tokens into YAML via
// kairos-sdk/machine. Unlike yip's built-in schema.DotNotationModifier it skips
// Kairos-owned prefixes (kairos.config=, kairos.config_url=, cos.setup=) so those
// tokens are not double-processed here — they flow through KairosConfigURIFromCmdline.
func SDKDotNotationModifier(s []byte) ([]byte, error) {
	return machine.DotStringToYAML(string(s))
}

// RunYipStageInline runs a yip stage whose full YAML document is passed as a
// string, rather than loaded from the default cloud-init directories. Used by
// steps that need to synthesize a one-shot stage (e.g. ensure-partitions
// building a layout: block on the fly). The stage name inside yamlBody must
// match stageName. Returns the raw yip error so the caller can wrap it.
func RunYipStageInline(stageName, yamlBody string) error {
	yip := executor.NewExecutor(executor.WithLogger(KLog))
	return yip.Run(stageName, vfs.OSFS, ImmucoreConsole{}, yamlBody)
}

func onlyYAMLPartialErrors(er error) bool {
	if merr, ok := errors.AsType[*multierror.Error](er); ok {
		for _, e := range merr.Errors {
			// Skip partial unmarshalling errors
			// TypeError is throwed when it is possible to read the yaml partially
			// XXX: Seems errors.Is and errors.As are not working as expected here.
			// Even if the underlying type is yaml.TypeError.
			var d *yaml.TypeError
			if fmt.Sprintf("%T", e) != fmt.Sprintf("%T", d) {
				return false
			}
		}
	}
	return true
}

func checkYAMLError(allErrors, err error) error {
	if !onlyYAMLPartialErrors(err) {
		// here we absorb errors only if are related to YAML unmarshalling
		// As cmdline is parsed out as a yaml file
		allErrors = multierror.Append(allErrors, err)
	}
	return allErrors
}
