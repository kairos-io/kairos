package wizard

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Overrides are the keys a frontend confirmed with the operator, which no
// text in the cloud-config may change.
type Overrides struct {
	// Device is the disk the operator confirmed. Empty keeps the document's.
	Device string
	// Source is the installer's --source. Empty keeps the document's, because
	// forcing an empty source in would override the one the agent resolves.
	Source string
	// FinishAction is written back even when empty, because a zero value has
	// to win too: `poweroff: true` left in edited YAML must not power off a
	// machine whose operator chose to do nothing.
	FinishAction string
	// DefaultNoUsers sets install.nousers when the document has no users key
	// and no stages. MCP wants it: its caller may add no users at all, and
	// the agent would fail a validation the caller cannot see. The wizard
	// does not: Render already writes nousers when no user was asked for, and
	// adding it to hand-edited YAML would hide a user the operator deleted.
	DefaultNoUsers bool
}

// Finalize is the last step before any install: it takes the cloud-config the
// install will run with, from whichever frontend, and writes back the keys
// the operator confirmed, last, so nothing in the text can move the install
// to another disk or end it another way. Every other key is kept.
//
// Comments and anchors do not survive the round trip. Nothing reads them:
// the agent parses the file, and it is deleted when the install ends.
func Finalize(cloudConfig string, o Overrides) (string, error) {
	doc := map[string]any{}
	if err := yaml.Unmarshal([]byte(cloudConfig), &doc); err != nil {
		return "", fmt.Errorf("cloud-config is not valid YAML: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}

	install := map[string]any{}
	if raw, ok := doc["install"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("cloud-config install must be a mapping, got %T", raw)
		}
		install = m
	}

	if o.Device != "" {
		install["device"] = o.Device
	}
	if o.Source != "" {
		install["source"] = o.Source
	}
	install["reboot"] = o.FinishAction == FinishReboot
	install["poweroff"] = o.FinishAction == FinishPoweroff

	if o.DefaultNoUsers {
		_, hasUsers := doc["users"]
		_, hasStages := doc["stages"]
		if !hasUsers && !hasStages {
			install["nousers"] = true
		}
	}

	doc["install"] = install

	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(out), nil
}
