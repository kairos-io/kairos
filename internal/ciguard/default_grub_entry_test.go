// The live ISO boots the interactive installer by default
// (kairos-io/AuroraBoot#869), but ten specs in the qemu acceptance suite
// call expectDefaultService after a plain types.WithISO() boot, which the
// interactive installer does not satisfy. Those builds pin the unattended
// entry through the default_grub_entry input, which reusable-factory.yaml
// turns into auroraboot's --default-grub-entry.
//
// Asserting on the YAML text would only prove the file says what it says.
// These specs run the step's own `run:` script under bash, with the
// ${{ }} expressions substituted the way Actions substitutes them, and
// read the auroraboot command line it builds.
package ciguard_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const (
	factoryWorkflow  = workflowsDir + "/reusable-factory.yaml"
	buildISOWorkflow = workflowsDir + "/_build-iso.yaml"

	// isoStep is the step that assembles the auroraboot command line.
	isoStep = "Generate ISO artifact"

	// unattendedEntry is the --id of the live GRUB entry that still runs
	// kairos-installer.service unattended. It has to match
	// constants.LiveGrubEntryUnattended in AuroraBoot; a rename there
	// makes `set default` fall back to the first entry silently, so the
	// value is spelled out here rather than derived.
	unattendedEntry = "kairos-install"
)

// runScript is the `run:` of one step, addressed by step name.
func runScript(workflow, step string) string {
	GinkgoHelper()

	raw, err := os.ReadFile(workflow)
	Expect(err).ToNot(HaveOccurred())

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	Expect(yaml.Unmarshal(raw, &wf)).To(Succeed())

	for _, job := range wf.Jobs {
		for _, s := range job.Steps {
			if s.Name == step {
				return s.Run
			}
		}
	}

	Fail(fmt.Sprintf("no step named %q in %s", step, workflow))
	return ""
}

// expression matches a single ${{ ... }} template, which is what Actions
// substitutes before bash ever sees the script.
var expression = regexp.MustCompile(`\$\{\{([^}]*)\}\}`)

// substitute replaces every ${{ ... }} with the value bound to its
// trimmed contents. An expression with no binding is a mistake in the
// test table, not an empty string, so it fails loudly: an unbound
// `inputs.default_grub_entry` would otherwise read as "flag not passed"
// and the spec would pass for the wrong reason.
func substitute(script string, values map[string]string) string {
	GinkgoHelper()

	var missing []string
	out := expression.ReplaceAllStringFunc(script, func(m string) string {
		key := strings.TrimSpace(expression.FindStringSubmatch(m)[1])
		v, ok := values[key]
		if !ok {
			missing = append(missing, key)
			return ""
		}
		return v
	})
	Expect(missing).To(BeEmpty(), "unbound expressions in the step script")

	return out
}

// aurorabootCmd runs the ISO step under bash with the flags Actions uses
// for `shell: bash`, and returns the command line it echoes before
// running it. docker is stubbed, so nothing is pulled or built.
func aurorabootCmd(values map[string]string) string {
	GinkgoHelper()

	dir := GinkgoT().TempDir()

	// The step ends by listing artifacts/*.iso and failing when there is
	// none. The stub docker writes nothing, so seed the file.
	Expect(os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "artifacts", "kairos.iso"), nil, 0o644)).To(Succeed())

	bin := filepath.Join(dir, "bin")
	Expect(os.MkdirAll(bin, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())

	script := filepath.Join(dir, "step.sh")
	Expect(os.WriteFile(script, []byte(substitute(runScript(factoryWorkflow, isoStep), values)), 0o644)).To(Succeed())

	// `shell: bash` in Actions is `bash --noprofile --norc -e -o pipefail`.
	cmd := exec.Command("bash", "--noprofile", "--norc", "-e", "-o", "pipefail", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_OUTPUT="+filepath.Join(dir, "github_output"),
	)

	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "step script failed:\n%s", out)

	for _, line := range strings.Split(string(out), "\n") {
		if _, cmdline, found := strings.Cut(line, "Executing: "); found {
			return cmdline
		}
	}

	Fail(fmt.Sprintf("step printed no auroraboot command line:\n%s", out))
	return ""
}

// isoInputs is the binding for a plain GRUB ISO build. Specs copy it and
// change only what they are about.
func isoInputs() map[string]string {
	return map[string]string{
		"inputs.iso":                       "true",
		"inputs.model":                     "generic",
		"inputs.trusted_boot":              "false",
		"inputs.keys_dir":                  "",
		"inputs.sysext_dir":                "",
		"inputs.cloud_config":              "",
		"inputs.single_efi_cmdline":        "",
		"inputs.default_grub_entry":        "",
		"inputs.allow_insecure_registries": "false",
		"inputs.auroraboot_version":        "v0.27.1",
		"steps.setup.outputs.image_tag":    "ttl.sh/kairos:test",
	}
}

var _ = Describe("the live GRUB default entry", func() {
	Describe("reusable-factory.yaml, Generate ISO artifact", func() {
		It("passes the entry to build-iso when one is pinned", func() {
			in := isoInputs()
			in["inputs.default_grub_entry"] = unattendedEntry

			cmd := aurorabootCmd(in)

			Expect(cmd).To(ContainSubstring(" build-iso"))
			Expect(cmd).To(ContainSubstring("--default-grub-entry " + unattendedEntry))
		})

		It("passes no entry when none is pinned, so the ISO keeps auroraboot's default", func() {
			cmd := aurorabootCmd(isoInputs())

			Expect(cmd).To(ContainSubstring(" build-iso"))
			Expect(cmd).ToNot(ContainSubstring("--default-grub-entry"))
		})

		// build-uki has no --default-grub-entry: a trusted boot ISO boots
		// a UKI from systemd-boot and writes no grub.cfg. Leaking the flag
		// onto that branch would abort the build on an unknown flag rather
		// than do nothing, so the guard is tested from both sides.
		It("passes no entry to build-uki even when one is pinned", func() {
			in := isoInputs()
			in["inputs.trusted_boot"] = "true"
			in["inputs.keys_dir"] = "tests/assets/keys"
			in["inputs.default_grub_entry"] = unattendedEntry

			cmd := aurorabootCmd(in)

			Expect(cmd).To(ContainSubstring(" build-uki"))
			Expect(cmd).ToNot(ContainSubstring("--default-grub-entry"))
		})
	})

	// The flag is only reachable from a release or qemu-test build if
	// _build-iso.yaml hands its own input down. Nothing in the step script
	// can catch that wire being cut.
	Describe("_build-iso.yaml", func() {
		It("forwards default_grub_entry to reusable-factory.yaml", func() {
			raw, err := os.ReadFile(buildISOWorkflow)
			Expect(err).ToNot(HaveOccurred())

			var wf struct {
				On struct {
					WorkflowCall struct {
						Inputs map[string]yaml.Node `yaml:"inputs"`
					} `yaml:"workflow_call"`
				} `yaml:"on"`
				Jobs map[string]struct {
					Uses string            `yaml:"uses"`
					With map[string]string `yaml:"with"`
				} `yaml:"jobs"`
			}
			Expect(yaml.Unmarshal(raw, &wf)).To(Succeed())

			Expect(wf.On.WorkflowCall.Inputs).To(HaveKey("default_grub_entry"),
				"_build-iso.yaml must accept the input its callers pin")

			var forwarded bool
			for _, job := range wf.Jobs {
				if !strings.Contains(job.Uses, "reusable-factory.yaml") {
					continue
				}
				Expect(job.With).To(HaveKeyWithValue("default_grub_entry", "${{ inputs.default_grub_entry }}"))
				forwarded = true
			}
			Expect(forwarded).To(BeTrue(), "no job calling reusable-factory.yaml")
		})
	})
})
