package mcp

import (
	"fmt"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	"gopkg.in/yaml.v3"
)

// renderCloudConfig builds the #cloud-config document an install runs with.
//
// The caller's own YAML is merged on top of the install block, so an agent can
// add users, SSH keys or yip stages without this package having to model them.
// It cannot, however, redirect the install: the device, source and finish
// action come from the tool arguments that were confirmed, and are written last
// so extra YAML cannot quietly move the install to another disk.
func renderCloudConfig(device, source, finishAction, extra string) (string, error) {
	merged := map[string]any{}

	if extra != "" {
		if err := yaml.Unmarshal([]byte(extra), &merged); err != nil {
			return "", fmt.Errorf("cloud_config is not valid YAML: %w", err)
		}
	}

	install := &sdkInstall.Install{Device: device, Source: source}
	switch finishAction {
	case FinishReboot:
		install.Reboot = true
	case FinishPoweroff:
		install.Poweroff = true
	}

	// No users were asked for and none can be added after the fact from here,
	// so say so explicitly rather than leaving the agent to fail a validation
	// it cannot see. An extra cloud_config that does create users overrides
	// this, because its own install block is merged first and this one wins
	// only on the keys it sets.
	if _, ok := merged["users"]; !ok && !hasStages(merged) {
		install.NoUsers = true
	}

	overlay, err := yamlMap(&sdkConfig.Config{Install: install})
	if err != nil {
		return "", err
	}
	// The install block is merged key by key, so a caller can still set
	// install options this tool does not model without losing the device.
	if existing, ok := merged["install"].(map[string]any); ok {
		if incoming, ok := overlay["install"].(map[string]any); ok {
			for k, v := range incoming {
				existing[k] = v
			}
			overlay["install"] = existing
		}
	}
	for k, v := range overlay {
		merged[k] = v
	}

	out, err := yaml.Marshal(merged)
	if err != nil {
		return "", err
	}

	return "#cloud-config\n" + string(out), nil
}

// hasStages reports whether the caller's YAML carries yip stages, which is
// where a cloud-config creates users when it does not use the top-level users
// key.
func hasStages(m map[string]any) bool {
	_, ok := m["stages"]
	return ok
}

func yamlMap(v any) (map[string]any, error) {
	dat, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}

	out := map[string]any{}
	if err := yaml.Unmarshal(dat, &out); err != nil {
		return nil, err
	}

	return out, nil
}
