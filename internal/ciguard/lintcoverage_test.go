// Tests that the lint jobs this repository publishes actually reach
// every file they are meant to lint. They run the `find` command out of
// the workflow itself against this tree, so a selector that silently
// matches nothing is a failure rather than a green job.
package ciguard_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// repoRoot is relative to this file: internal/ciguard -> repo root.
const repoRoot = "../.."

// reusableLinting and compositeLinting are the two shapes the same lint
// steps ship in. README.md says external consumers may use either, so a
// selector fixed in one and not the other leaves half the callers blind.
const (
	reusableLinting  = repoRoot + "/.github/workflows/reusable-linting.yaml"
	compositeLinting = repoRoot + "/linting-composite-action/action.yml"
)

// yamldirsForThisRepo is what _lint.yaml passes to reusable-linting.yaml.
// The find command is written against an input, so the spec has to supply
// the same value the repository's own caller does.
const yamldirsForThisRepo = ".github/workflows/"

// yamldirsEnvVar is the env var reusable-linting.yaml binds the yamldirs
// input to, so the run block never interpolates the input into the shell.
const yamldirsEnvVar = "INPUTS_YAMLDIRS"

type lintStep struct {
	Name string            `yaml:"name"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

// lintFile covers both shapes at once: a reusable workflow puts its steps
// under jobs.<job>.steps, a composite action under runs.steps.
type lintFile struct {
	Jobs map[string]struct {
		Steps []lintStep `yaml:"steps"`
	} `yaml:"jobs"`
	Runs struct {
		Steps []lintStep `yaml:"steps"`
	} `yaml:"runs"`
}

// lintSteps maps every named step in the file to the step itself, so a
// spec can read both its `run:` body and the `env:` the body reads from.
func lintSteps(path string) map[string]lintStep {
	GinkgoHelper()

	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())

	var f lintFile
	Expect(yaml.Unmarshal(data, &f)).To(Succeed(), "%s is not parseable", filepath.Base(path))

	out := map[string]lintStep{}
	add := func(steps []lintStep) {
		for _, s := range steps {
			if s.Name != "" && s.Run != "" {
				out[s.Name] = s
			}
		}
	}
	for _, j := range f.Jobs {
		add(j.Steps)
	}
	add(f.Runs.Steps)
	return out
}

// lintStepNamed returns the named step, failing the spec when the file
// declares no such step with a run block.
func lintStepNamed(path, stepName string) lintStep {
	GinkgoHelper()

	s, ok := lintSteps(path)[stepName]
	Expect(ok).To(BeTrue(), "%s declares no step named %q with a run block", filepath.Base(path), stepName)
	return s
}

// findCommand pulls the `find ... -print` prefix out of the named step's
// `run:` line. Everything from the first pipe on is the xargs and docker
// half, which needs a daemon; the selector is the part under test.
func findCommand(path, stepName string) string {
	GinkgoHelper()

	run := lintStepNamed(path, stepName).Run

	pipe := strings.Index(run, "|")
	Expect(pipe).To(BeNumerically(">", 0), "%s: step %q does not pipe its find into a linter", filepath.Base(path), stepName)

	cmd := strings.TrimSpace(run[:pipe])
	Expect(cmd).To(HavePrefix("find "), "%s: step %q does not start with find", filepath.Base(path), stepName)
	return cmd
}

// matched runs the extracted selector from the repository root and returns
// the paths it printed, relative to that root, so the result can be
// compared with a filepath.WalkDir listing.
func matched(cmd string) []string {
	GinkgoHelper()

	out, err := exec.Command("sh", "-c", "cd "+repoRoot+" && "+cmd).Output()
	Expect(err).ToNot(HaveOccurred(), "running %q failed", cmd)

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Clean(line)))
	}
	sort.Strings(paths)
	return paths
}

// walk lists the files under root, relative to prefix, that keep returns
// true for. Directories named in skip are not descended into, which is how
// the `-path "./examples" -prune` half of each selector is expressed here.
func walk(root, prefix string, skip []string, keep func(name string) bool) []any {
	GinkgoHelper()

	var found []any
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// .git holds no lintable file and is not part of a
			// checkout's content, but find walks into it.
			if rel == ".git" {
				return fs.SkipDir
			}
			for _, s := range skip {
				if rel == s {
					return fs.SkipDir
				}
			}
			return nil
		}
		if keep(d.Name()) {
			found = append(found, filepath.ToSlash(filepath.Join(prefix, rel)))
		}
		return nil
	})
	Expect(err).ToNot(HaveOccurred())
	sort.Slice(found, func(i, j int) bool { return found[i].(string) < found[j].(string) })
	return found
}

func isDockerfile(name string) bool {
	return name == "Dockerfile" || strings.HasPrefix(name, "Dockerfile.")
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

var _ = Describe("lint job coverage", func() {
	// `find . -name "Dockerfile.*"` matches Dockerfile.img and
	// Dockerfile.test and nothing else, so images/Dockerfile,
	// kairos-init/Dockerfile, kcrypt/Dockerfile and
	// cmd/kcrypt-challenger/Dockerfile had never been linted. The job
	// still reported success, because a selector that matches nothing is
	// not an error. AGENTS.md tells contributors "hadolint runs in CI",
	// so the gap is invisible from the contributor's side too.
	DescribeTable("hadolint sees every Dockerfile in the tree",
		func(path string) {
			want := walk(repoRoot, "", []string{"examples"}, isDockerfile)
			Expect(want).ToNot(BeEmpty(), "this spec proves nothing if the tree holds no Dockerfile")
			Expect(want).To(ContainElement(HaveSuffix("/Dockerfile")),
				"this spec proves nothing unless the tree holds a Dockerfile with no suffix")

			Expect(matched(findCommand(path, "hadolint"))).To(ConsistOf(want...),
				"the hadolint selector in %s does not cover every Dockerfile", filepath.Base(path))
		},
		Entry("reusable workflow", reusableLinting),
		Entry("composite action", compositeLinting),
	)

	// `find DIR -name "*.yml" -or -name "*.yaml" -print` binds -print to
	// the last -name only, so a .yml file matches the first branch, the
	// -o short-circuits, and it is never printed. Three workflow files
	// here are .yml and all three were skipped.
	It("yamllint sees every workflow file, .yml as well as .yaml", func() {
		// The step takes the directory through an env var rather than
		// interpolating the input straight into the shell, so the spec
		// has to follow the same hop to know what it is testing.
		step := lintStepNamed(reusableLinting, "yamllint")
		Expect(step.Env).To(HaveKeyWithValue(yamldirsEnvVar, "${{ inputs.yamldirs }}"),
			"the yamllint step no longer binds %s to the yamldirs input", yamldirsEnvVar)

		cmd := findCommand(reusableLinting, "yamllint")
		Expect(cmd).To(ContainSubstring("${"+yamldirsEnvVar+"}"),
			"the yamllint selector no longer reads the yamldirs input, so this spec is testing the wrong directory")
		cmd = strings.ReplaceAll(cmd, "${"+yamldirsEnvVar+"}", yamldirsForThisRepo)

		want := walk(filepath.Join(repoRoot, yamldirsForThisRepo), yamldirsForThisRepo, nil, isYAML)
		Expect(want).To(ContainElement(HaveSuffix(".yml")),
			"this spec proves nothing unless the directory holds at least one .yml file")

		Expect(matched(cmd)).To(ConsistOf(want...),
			"the yamllint selector in %s does not cover every workflow file", filepath.Base(reusableLinting))
	})

	// The composite action hardcodes the directory rather than taking an
	// input, so it is checked against the whole tree.
	It("the composite action's yamllint sees every .yml in the tree", func() {
		want := walk(repoRoot, "", []string{"examples"}, isYAML)
		Expect(want).To(ContainElement(HaveSuffix(".yml")),
			"this spec proves nothing unless the tree holds at least one .yml file")

		Expect(matched(findCommand(compositeLinting, "yamllint"))).To(ConsistOf(want...),
			"the yamllint selector in %s does not cover every yaml file", filepath.Base(compositeLinting))
	})
})
