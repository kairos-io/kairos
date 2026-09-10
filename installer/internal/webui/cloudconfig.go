package webui

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// renderCloudConfig returns the YAML handed to kairos-agent: the operator's
// own cloud-config with install.device set to the device the form named.
//
// The device is written last, and unconditionally, because it is the one key
// this form owns. An install.device left in the pasted YAML must not quietly
// win over the device the operator typed into the field next to it and
// submitted. The reboot and poweroff checkboxes are not merged here: they
// reach the agent as the --reboot/--poweroff flags agentrun.Command builds,
// which the agent reads after the config file.
//
// This round-trips the document through yaml, so comments and anchors in the
// pasted config do not survive into the temporary file. Nothing downstream
// reads them: the agent parses the file, it is deleted when the install ends,
// and the text the operator typed is still in their browser.
func renderCloudConfig(cloudConfig, device string) (string, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal([]byte(cloudConfig), &doc); err != nil {
		return "", fmt.Errorf("cloud-config is not valid YAML: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}

	if device != "" {
		install, _ := doc["install"].(map[string]any)
		if install == nil {
			install = map[string]any{}
		}
		install["device"] = device
		doc["install"] = install
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(out), nil
}

// finishAction maps the two form checkboxes onto the single agentrun finish
// action. Reboot wins when both are ticked, because that is what the agent
// does with both flags set: hooks.Lifecycle checks ShouldReboot before
// ShouldShutdown and reboots before it can reach the power-off branch.
// Resolving it here keeps the browser and the agent naming the same outcome.
func finishAction(reboot, powerOff string) string {
	if checked(reboot) {
		return "reboot"
	}
	if checked(powerOff) {
		return "poweroff"
	}
	return ""
}

// checked reports whether an HTML checkbox came back ticked. A browser sends
// "on" for a box with no explicit value and omits the field entirely when it
// is clear, but the endpoint also takes JSON, so "true" and "1" are accepted.
func checked(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}
