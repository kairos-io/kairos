// Tests that keep pr-fork-checks.yaml honest. That workflow runs on
// the plain `pull_request` trigger, so a fork PR starts it with no
// maintainer approval. Everything it reaches therefore has to be
// harmless to run on a stranger's code: no repository secret, no
// permission above `contents: read`, and no self-hosted runner.
//
// The `authorize` gate in pr.yaml is what enforces that rule for the
// rest of the pipeline. This file is what enforces it here, because
// nothing else does: adding a job to pr-fork-checks.yaml that calls a
// reusable needing a registry login or a `kvm` runner is a one-line
// edit that CI would otherwise accept.
package ciguard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const forkChecksFile = "pr-fork-checks.yaml"

// secretRef finds every `secrets.NAME` lookup in a workflow's text.
// Reading the raw file rather than the parsed tree is deliberate: a
// secret can be referenced from a `with:` value, an `env:` block, a
// `run:` script or an `if:`, and the point is to find all of them.
var secretRef = regexp.MustCompile(`secrets\.([A-Za-z_][A-Za-z0-9_]*)`)

// githubHostedRunner matches the runner labels GitHub provides. A
// self-hosted label (kairos uses `kvm` for the qemu suites) is a
// machine the project owns, so an ungated fork PR must not reach one.
var githubHostedRunner = regexp.MustCompile(`^(ubuntu|windows|macos)-`)

// forkJob is the slice of a job this file cares about. It is separate
// from permissions_test.go's `job` because it needs `secrets:` and
// `runs-on:`, which that file does not read.
type forkJob struct {
	Uses    string    `yaml:"uses"`
	Secrets yaml.Node `yaml:"secrets"`
	RunsOn  yaml.Node `yaml:"runs-on"`
}

type forkWorkflow struct {
	Jobs map[string]forkJob `yaml:"jobs"`
}

func readForkWorkflow(path string) (forkWorkflow, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return forkWorkflow{}, "", err
	}
	var w forkWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return forkWorkflow{}, "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return w, string(data), nil
}

// runnerLabels flattens a `runs-on:` node, which GitHub accepts as a
// scalar, a sequence, or a mapping with `labels:`.
func runnerLabels(n yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			out = append(out, c.Value)
		}
		return out
	case yaml.MappingNode:
		var m struct {
			Labels []string `yaml:"labels"`
		}
		if err := n.Decode(&m); err == nil {
			return m.Labels
		}
	}
	return nil
}

// reachable walks the local reusable workflows a file calls, depth
// first, and returns every file in the closure including the root.
// A `uses:` pointing at an action or another repository is not
// followed: an action cannot be handed a secret it was not passed,
// and this workflow passes none.
func reachable(root string) ([]string, error) {
	seen := map[string]bool{}
	var order []string

	var walk func(path string) error
	walk = func(path string) error {
		base := filepath.Base(path)
		if seen[base] {
			return nil
		}
		seen[base] = true
		order = append(order, path)

		w, _, err := readForkWorkflow(path)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(w.Jobs))
		for n := range w.Jobs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if callee := localCallee(w.Jobs[n].Uses); callee != "" {
				if err := walk(callee); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walk(root); err != nil {
		return nil, err
	}
	return order, nil
}

var _ = Describe("pr-fork-checks.yaml", func() {
	var (
		closure []string
		root    forkWorkflow
	)

	BeforeEach(func() {
		var err error
		root, _, err = readForkWorkflow(filepath.Join(workflowsDir, forkChecksFile))
		Expect(err).ToNot(HaveOccurred())
		Expect(root.Jobs).ToNot(BeEmpty(), "%s declares no jobs", forkChecksFile)

		closure, err = reachable(filepath.Join(workflowsDir, forkChecksFile))
		Expect(err).ToNot(HaveOccurred())
		// The root plus the five reusables it calls, plus
		// reusable-linting.yaml which _lint.yaml calls.
		Expect(len(closure)).To(BeNumerically(">=", 6),
			"the walk found only %d workflows, so it is not following `uses:`", len(closure))
	})

	// `secrets: inherit` is the line that would hand a fork's code the
	// registry credentials. pr.yaml carries it on every call because
	// its `authorize` gate runs first; nothing here may.
	It("passes no secrets to any job it calls", func() {
		var problems []string
		for name, j := range root.Jobs {
			if j.Secrets.Kind != 0 && j.Secrets.Tag != "!!null" {
				problems = append(problems, fmt.Sprintf(
					"job %q declares `secrets:`, which an ungated fork PR must not get", name))
			}
		}
		sort.Strings(problems)
		Expect(problems).To(BeEmpty(), "%s:\n  %s", forkChecksFile, strings.Join(problems, "\n  "))
	})

	// Withholding `secrets: inherit` is only half of it. A callee that
	// reads a named secret would fail, or worse silently degrade, so
	// the closure must not want one in the first place.
	// `secrets.GITHUB_TOKEN` is exempt: GitHub gives every called
	// workflow the caller's token without it being inherited, and
	// under `pull_request` from a fork that token is read-only.
	It("reaches no workflow that reads a repository secret", func() {
		var problems []string
		for _, path := range closure {
			_, text, err := readForkWorkflow(path)
			Expect(err).ToNot(HaveOccurred())

			named := map[string]bool{}
			for _, m := range secretRef.FindAllStringSubmatch(text, -1) {
				if m[1] == "GITHUB_TOKEN" {
					continue
				}
				named[m[1]] = true
			}
			for s := range named {
				problems = append(problems, fmt.Sprintf(
					"%s reads secrets.%s", filepath.Base(path), s))
			}
		}
		sort.Strings(problems)
		Expect(problems).To(BeEmpty(), "reachable from %s without approval:\n  %s",
			forkChecksFile, strings.Join(problems, "\n  "))
	})

	// The qemu and image suites run on the project's own `kvm`
	// runners. Those stay behind `authorize` in pr.yaml; an ungated
	// fork PR must never be able to schedule work onto one.
	It("reaches no job that runs on a self-hosted runner", func() {
		var problems []string
		for _, path := range closure {
			w, _, err := readForkWorkflow(path)
			Expect(err).ToNot(HaveOccurred())

			names := make([]string, 0, len(w.Jobs))
			for n := range w.Jobs {
				names = append(names, n)
			}
			sort.Strings(names)

			for _, n := range names {
				j := w.Jobs[n]
				if j.Uses != "" {
					// A job that calls a reusable has no `runs-on` of
					// its own; the callee is walked separately.
					continue
				}
				labels := runnerLabels(j.RunsOn)
				Expect(labels).ToNot(BeEmpty(),
					"%s: job %q declares neither `uses:` nor a readable `runs-on:`", filepath.Base(path), n)
				for _, l := range labels {
					if !githubHostedRunner.MatchString(l) {
						problems = append(problems, fmt.Sprintf(
							"%s: job %q runs on %q", filepath.Base(path), n, l))
					}
				}
			}
		}
		sort.Strings(problems)
		Expect(problems).To(BeEmpty(), "reachable from %s without approval:\n  %s",
			forkChecksFile, strings.Join(problems, "\n  "))
	})

	// Every job here has to be skipped on a same-repo PR, or pr.yaml
	// (which routes those straight through the `internal` environment)
	// and this workflow both run the same job on every maintainer and
	// renovate branch.
	It("skips every job on a same-repo pull request", func() {
		data, err := os.ReadFile(filepath.Join(workflowsDir, forkChecksFile))
		Expect(err).ToNot(HaveOccurred())

		var w struct {
			Jobs map[string]struct {
				If string `yaml:"if"`
			} `yaml:"jobs"`
		}
		Expect(yaml.Unmarshal(data, &w)).To(Succeed())

		const guard = "github.event.pull_request.head.repo.full_name != github.repository"
		var problems []string
		for name, j := range w.Jobs {
			if !strings.Contains(j.If, guard) {
				problems = append(problems, fmt.Sprintf("job %q is missing the fork-only guard", name))
			}
		}
		sort.Strings(problems)
		Expect(problems).To(BeEmpty(), "%s:\n  %s", forkChecksFile, strings.Join(problems, "\n  "))
	})

	// The whole point of the workflow. If a future edit drops one of
	// these, fork PRs quietly lose that feedback and nothing goes red.
	It("covers the secret-free half of pr.yaml", func() {
		called := map[string]bool{}
		for _, j := range root.Jobs {
			if c := localCallee(j.Uses); c != "" {
				called[filepath.Base(c)] = true
			}
		}
		for _, want := range []string{
			"_unit-tests.yaml",
			"_lint.yaml",
			"_build-kairos.yaml",
			"_build-kairos-init.yaml",
			"_build-kcrypt-challenger.yaml",
		} {
			Expect(called).To(HaveKey(want), "%s no longer runs %s on fork PRs", forkChecksFile, want)
		}
	})
})
