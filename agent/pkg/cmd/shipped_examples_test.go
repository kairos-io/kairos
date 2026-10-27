package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/schema"
)

// auroraBootExamplesDir holds the cloud-config templates the AuroraBoot
// reference hands out by raw.githubusercontent.com URL, so a reader passes one
// to --cloud-config without ever opening it. Read off disk rather than through
// an AuroraBoot package: AuroraBoot imports this module, not the other way
// around, and what it downloads is the file as committed anyway.
const auroraBootExamplesDir = "../../../examples/auroraboot"

// A template is a cloud config until AuroraBoot substitutes into it, and
// `kairos-agent validate` is what we tell a user to run on a cloud config. The
// shipped master-template.yaml spelled its placeholders with the two-bracket
// delimiters AuroraBoot stopped using in 2023, and YAML reads `[[x]]` as a
// nested sequence: `passwd` was a list, no --set reached it, and the file
// described no user at all (#5437). Valid is not installable either: the admin
// user rule is enforced at install time, outside the schema, and the fixed
// template still failed it until its user joined the admin group.
func TestAuroraBootExamplesValidate(t *testing.T) {
	entries, err := os.ReadDir(auroraBootExamplesDir)
	if err != nil {
		t.Fatalf("reading %s: %v", auroraBootExamplesDir, err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
			continue
		}
		checked++

		t.Run(name, func(t *testing.T) {
			path := filepath.Join(auroraBootExamplesDir, name)
			if err := schema.Validate(path); err != nil {
				t.Errorf("%s is not a valid cloud config: %v", name, err)
			}

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("opening %s: %v", path, err)
			}
			defer f.Close()
			c, err := agentConfig.ScanNoLogs(collector.Readers(f))
			if err != nil {
				t.Fatalf("scanning %s: %v", name, err)
			}
			if err := agentConfig.CheckConfigForUsers(c); err != nil {
				t.Errorf("%s would not install: %v", name, err)
			}
		})
	}

	// Without this the guard reports success over an empty directory, which is
	// what a rename of examples/auroraboot would turn it into.
	if checked == 0 {
		t.Fatalf("no cloud config found in %s, the guard checked nothing", auroraBootExamplesDir)
	}
}
