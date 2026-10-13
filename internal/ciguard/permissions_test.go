// Tests that assert invariants about this repository's own GitHub
// Actions workflows. They read the YAML under .github/workflows and
// never touch the network, so they run in the ordinary unit-test pass
// rather than needing a job of their own.
package ciguard_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// workflowsDir is relative to this file: internal/ciguard -> repo root.
const workflowsDir = "../../.github/workflows"

// levels ranks a permission scope's value. GitHub rejects a run at
// startup when a reusable workflow asks for a higher level than the
// job calling it was granted, so "covers" is a per-scope >= test.
var levels = map[string]int{"none": 0, "read": 1, "write": 2}

// permissions is one `permissions:` block. GitHub accepts three shapes:
// a mapping of scope to level, the `read-all`/`write-all` shorthands,
// and `{}` (an empty mapping, which grants nothing).
type permissions struct {
	declared bool
	// blanket is "read" or "write" for the -all shorthands, empty otherwise.
	blanket string
	scopes  map[string]string
}

// level reports the granted level for one scope. A declared block is
// exhaustive: any scope it does not name is granted `none`.
func (p permissions) level(scope string) int {
	if p.blanket != "" {
		return levels[p.blanket]
	}
	if v, ok := p.scopes[scope]; ok {
		return levels[v]
	}
	return 0
}

// named lists the scopes the block raises above `none`.
func (p permissions) named() []string {
	out := []string{}
	for s, v := range p.scopes {
		if levels[v] > 0 {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func parsePermissions(n yaml.Node) (permissions, error) {
	p := permissions{scopes: map[string]string{}}
	if n.Kind == 0 || n.Tag == "!!null" {
		return p, nil
	}
	p.declared = true

	switch n.Kind {
	case yaml.ScalarNode:
		switch n.Value {
		case "read-all":
			p.blanket = "read"
		case "write-all":
			p.blanket = "write"
		default:
			return p, fmt.Errorf("unknown permissions shorthand %q", n.Value)
		}
	case yaml.MappingNode:
		var raw map[string]string
		if err := n.Decode(&raw); err != nil {
			return p, err
		}
		for scope, value := range raw {
			if _, ok := levels[value]; !ok {
				return p, fmt.Errorf("unknown permission level %q for scope %q", value, scope)
			}
			p.scopes[scope] = value
		}
	default:
		return p, fmt.Errorf("unexpected permissions node kind %v", n.Kind)
	}
	return p, nil
}

type job struct {
	Uses        string    `yaml:"uses"`
	Permissions yaml.Node `yaml:"permissions"`
}

type workflow struct {
	Permissions yaml.Node      `yaml:"permissions"`
	Jobs        map[string]job `yaml:"jobs"`
}

type parsedWorkflow struct {
	topLevel permissions
	jobs     map[string]job
	jobPerms map[string]permissions
	// requires is the highest level this workflow asks for on each
	// scope, taken across its own top-level block and every job block.
	// It is what a caller has to cover.
	requires permissions
}

func parseWorkflow(path string) (*parsedWorkflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}

	top, err := parsePermissions(w.Permissions)
	if err != nil {
		return nil, fmt.Errorf("%s: top-level permissions: %w", filepath.Base(path), err)
	}

	pw := &parsedWorkflow{
		topLevel: top,
		jobs:     w.Jobs,
		jobPerms: map[string]permissions{},
		requires: permissions{declared: top.declared, blanket: top.blanket, scopes: map[string]string{}},
	}
	for scope, value := range top.scopes {
		pw.requires.scopes[scope] = value
	}

	for name, j := range w.Jobs {
		jp, err := parsePermissions(j.Permissions)
		if err != nil {
			return nil, fmt.Errorf("%s: job %s: %w", filepath.Base(path), name, err)
		}
		pw.jobPerms[name] = jp
		if !jp.declared {
			continue
		}
		pw.requires.declared = true
		if levels[jp.blanket] > levels[pw.requires.blanket] {
			pw.requires.blanket = jp.blanket
		}
		for scope, value := range jp.scopes {
			if levels[value] > levels[pw.requires.scopes[scope]] {
				pw.requires.scopes[scope] = value
			}
		}
	}
	return pw, nil
}

// localCallee maps a `uses:` value to a path under .github/workflows,
// or returns "" for anything this test cannot resolve: an action, or a
// reusable workflow in another repository.
func localCallee(uses string) string {
	if !strings.HasPrefix(uses, "./.github/workflows/") {
		return ""
	}
	return filepath.Join(workflowsDir, filepath.Base(uses))
}

var _ = Describe("GitHub Actions workflow permissions", func() {
	var parsed map[string]*parsedWorkflow

	BeforeEach(func() {
		entries, err := filepath.Glob(filepath.Join(workflowsDir, "*.y*ml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).ToNot(BeEmpty(), "found no workflow files to check")

		parsed = map[string]*parsedWorkflow{}
		for _, e := range entries {
			pw, err := parseWorkflow(e)
			Expect(err).ToNot(HaveOccurred())
			parsed[filepath.Base(e)] = pw
		}
	})

	// A reusable workflow runs on the token its caller was granted.
	// When the callee's `permissions:` asks for a scope above what the
	// calling job holds, GitHub does not fail that job: it refuses to
	// start the whole run, so every other job disappears with it and no
	// log survives to say why. #4569 narrowed the three pipelines'
	// top-level grant to `contents: read` and left test-uki on
	// `packages: read` while _uki-test.yaml declares `packages: write`,
	// which stopped every push to master and every pull request from
	// producing a single job.
	It("grants every reusable-workflow caller at least what its callee asks for", func() {
		var problems []string

		for name, pw := range parsed {
			jobNames := make([]string, 0, len(pw.jobs))
			for jn := range pw.jobs {
				jobNames = append(jobNames, jn)
			}
			sort.Strings(jobNames)

			for _, jobName := range jobNames {
				callee := localCallee(pw.jobs[jobName].Uses)
				if callee == "" {
					continue
				}
				calleeWorkflow, ok := parsed[filepath.Base(callee)]
				Expect(ok).To(BeTrue(), "%s: job %s calls %s, which does not exist", name, jobName, callee)

				if !calleeWorkflow.requires.declared {
					// The callee declares nothing, so it runs on
					// whatever the caller holds and cannot over-ask.
					continue
				}

				grant := pw.jobPerms[jobName]
				if !grant.declared {
					grant = pw.topLevel
				}
				if !grant.declared {
					// Neither the job nor the workflow declares a
					// block, so the run uses the repository default,
					// which is not visible from the tree.
					continue
				}

				// A `read-all`/`write-all` callee names no scope, so the
				// per-scope loop below would not see it. It has to be
				// matched by a caller grant at least as broad.
				if b := calleeWorkflow.requires.blanket; b != "" && levels[grant.blanket] < levels[b] {
					problems = append(problems, fmt.Sprintf(
						"%s: job %q does not grant %s-all, which %s asks for",
						name, jobName, b, filepath.Base(callee)))
				}

				for _, scope := range calleeWorkflow.requires.named() {
					need := calleeWorkflow.requires.level(scope)
					if grant.level(scope) >= need {
						continue
					}
					problems = append(problems, fmt.Sprintf(
						"%s: job %q grants %s:%s but %s asks for %s:%s",
						name, jobName, scope, levelName(grant.level(scope)),
						filepath.Base(callee), scope, levelName(need)))
				}
			}
		}

		sort.Strings(problems)
		Expect(problems).To(BeEmpty(), "these callers would be rejected at startup:\n  %s",
			strings.Join(problems, "\n  "))
	})
})

func levelName(l int) string {
	for name, v := range levels {
		if v == l {
			return name
		}
	}
	return "unknown"
}
