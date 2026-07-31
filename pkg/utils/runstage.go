/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"gopkg.in/yaml.v3"
)

// RunstageAnalyze
func RunStageAnalyze(cfg *sdkConfig.Config, stage string) error {
	return runstage(cfg, stage, true)
}

// RunStage will run yip
func RunStage(cfg *sdkConfig.Config, stage string) error {
	return runstage(cfg, stage, false)
}

func runstage(cfg *sdkConfig.Config, stage string, analyze bool) error {
	var allErrors error
	var cloudInitPaths []string

	cloudInitPaths = append(constants.GetCloudInitPaths(), cfg.CloudInitPaths...)
	cfg.Logger.Debugf("Cloud-init paths set to %v", cloudInitPaths)
	if analyze {
		cfg.Logger.Info("Analyze mode, showing DAG")
	}

	// Make sure cloud init path specified are existing in the system
	for _, cp := range cloudInitPaths {
		err := fsutils.MkdirAll(cfg.Fs, cp, constants.DirPerm)
		if err != nil {
			cfg.Logger.Debugf("Failed creating cloud-init config path: %s %s", cp, err.Error())
		}
	}

	stageBefore := fmt.Sprintf("%s.before", stage)
	stageAfter := fmt.Sprintf("%s.after", stage)

	// Read the kernel cmdline. Every Kairos-owned stanza (kairos.config_url=,
	// kairos.config=, legacy bare cos.setup=) is parsed via kairos-sdk/machine
	// so immucore, kairos-agent and any other consumer stay in lockstep with a
	// single implementation. See kairos-sdk PR #812.
	cmdLineOut, err := cfg.Fs.ReadFile("/proc/cmdline")
	if err != nil {
		allErrors = multierror.Append(allErrors, err)
	}

	cmdLineYipURI := KairosConfigURIFromString(string(cmdLineOut))
	if cmdLineYipURI != "" {
		cfg.Logger.Debugf("Found Kairos config URI on cmdline with value %s", cmdLineYipURI)
		// Templated config_url (e.g. http://d/?u={{ .Values.product.uuid }})
		// must be resolved against the sysinfo-derived context BEFORE yip's
		// FromUrl fetches it verbatim. A rendering failure aborts the stage:
		// silently proceeding would fetch a mangled URL or 404 endpoint.
		rendered, rerr := collector.RenderConfigURL(cmdLineYipURI)
		if rerr != nil {
			return fmt.Errorf("rendering config_url %q from cmdline: %w", cmdLineYipURI, rerr)
		}
		if rendered != cmdLineYipURI {
			cfg.Logger.Debugf("Rendered Kairos config URI to %s", rendered)
			cmdLineYipURI = rendered
		}
	}

	// Run all stages for each of the default cloud config paths + extra cloud config paths
	for _, s := range []string{stageBefore, stage, stageAfter} {
		if analyze {
			cfg.CloudInitRunner.Analyze(s, cloudInitPaths...)
		} else {
			err = cfg.CloudInitRunner.Run(s, cloudInitPaths...)
			if err != nil {
				allErrors = multierror.Append(allErrors, err)
			}
		}
	}

	// Run the stages if cmdline contains the cos.setup stanza
	if cmdLineYipURI != "" {
		cmdLineArgs := []string{cmdLineYipURI}
		for _, s := range []string{stageBefore, stage, stageAfter} {
			if analyze {
				cfg.CloudInitRunner.Analyze(s, cloudInitPaths...)
			} else {
				err = cfg.CloudInitRunner.Run(s, cmdLineArgs...)
				if err != nil {
					allErrors = multierror.Append(allErrors, err)
				}
			}
		}
	}

	// Run stages encoded from /proc/cmdline. The SDK-backed modifier converts
	// generic dot-nested KEY=VALUE tokens (e.g. stages.foo[0].name=bar) to YAML
	// while skipping Kairos-owned prefixes (kairos.config=, kairos.config_url=,
	// cos.setup=) so those tokens are not double-processed here.
	cfg.CloudInitRunner.SetModifier(SDKDotNotationModifier)

	for _, s := range []string{stageBefore, stage, stageAfter} {
		if analyze {
			cfg.CloudInitRunner.Analyze(s, cloudInitPaths...)
		} else {
			err = cfg.CloudInitRunner.Run(s, string(cmdLineOut))
			if err != nil {
				// Best-effort: /proc/cmdline is a legacy dot-notation source
				// for yip stages, not a real config channel. Yip errors here
				// (yaml.TypeError, "unexpected token", "function not defined",
				// ...) are all products of arbitrary kernel-cmdline garbage
				// rather than a broken cloud-config. Demote them to debug in
				// non-strict mode so a random cmdline token cannot escalate a
				// clean run into a warn-level "some errors found" surface.
				if cfg.Strict {
					allErrors = multierror.Append(allErrors, err)
				} else {
					cfg.Logger.Debugf("/proc/cmdline yip parsing returned errors, ignoring on non-strict run: %s", err)
				}
			}
		}

	}

	cfg.CloudInitRunner.SetModifier(nil)

	// We return error here only if we have been running in strict mode.
	// Cloud configs are being loaded and executed on a best-effort, so every step/config
	// gets a chance to be executed and error is being appended and reported.
	if allErrors != nil && !cfg.Strict {
		cfg.Logger.Info("Some errors found but were ignored. Enable --strict mode to fail on those or --debug to see them in the log")
		cfg.Logger.Warn(allErrors)
		return nil
	}

	return allErrors
}

// KairosConfigURIFromString extracts a Kairos config source URI from a cmdline
// string. It routes through kairos-sdk/machine so kairos.config_url= and the
// legacy bare cos.setup=URI form both resolve consistently. Returns empty when
// the cmdline contains no such stanza or when the parsed YAML only carries
// nested key=value payloads (which are consumed by the config collector, not
// by RunStage).
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

// SDKDotNotationModifier is a yip schema.Modifier backed by
// machine.DotStringToYAML. Unlike yip's built-in schema.DotNotationModifier it
// skips Kairos-owned prefixes so those tokens flow exclusively through the
// KairosCmdlineYAML path instead of being double-processed as generic
// dot-nested keys.
func SDKDotNotationModifier(s []byte) ([]byte, error) {
	return machine.DotStringToYAML(string(s))
}
