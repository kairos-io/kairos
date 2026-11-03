// Tests that assert the multi-peer qemu legs ask for bridge networking.
// Like permissions_test.go these read the YAML under .github/workflows and
// never touch the network.
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

// bridgeLegs are the qemu test legs that need a second libp2p peer to be
// discovered, so they cannot run on QEMU user-mode networking:
//
//   - provider-qrcode-install pairs the runner with the VM over go-nodepair
//     (tests/provider_install_qrcode_test.go);
//   - provider-decentralized-k8s pairs the two VMs over edgevpn
//     (tests/provider_decentralized_test.go).
//
// Every other leg drives a single VM from the runner over the forwarded SSH
// port, which user-mode networking carries. Add a leg here when it grows a
// second peer, and give it `bridge: true` in the matrix.
var bridgeLegs = []string{
	"provider-decentralized-k8s",
	"provider-qrcode-install",
}

// matrixEntry is one `include:` row. Only the two keys this test reads are
// declared; a row carries others.
type matrixEntry struct {
	Test   string `yaml:"test"`
	Bridge bool   `yaml:"bridge"`
}

type bridgeJob struct {
	Uses    string
	With    map[string]yaml.Node
	Include []matrixEntry
}

// UnmarshalYAML keeps `with:` as raw nodes, because its values are GitHub
// expressions rather than YAML scalars of a known type, and tolerates a
// `matrix:` that is an expression rather than a literal: _build-flavors.yaml
// builds one with `fromJSON`, and this test has nothing to say about it.
func (b *bridgeJob) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Uses     string               `yaml:"uses"`
		With     map[string]yaml.Node `yaml:"with"`
		Strategy struct {
			Matrix yaml.Node `yaml:"matrix"`
		} `yaml:"strategy"`
	}
	if err := n.Decode(&raw); err != nil {
		return err
	}
	b.Uses = raw.Uses
	b.With = raw.With
	if raw.Strategy.Matrix.Kind != yaml.MappingNode {
		return nil
	}
	var matrix struct {
		Include yaml.Node `yaml:"include"`
	}
	if err := raw.Strategy.Matrix.Decode(&matrix); err != nil {
		return err
	}
	if matrix.Include.Kind == yaml.SequenceNode {
		if err := matrix.Include.Decode(&b.Include); err != nil {
			return err
		}
	}
	return nil
}

type bridgeWorkflow struct {
	On   yaml.Node            `yaml:"on"`
	Jobs map[string]bridgeJob `yaml:"jobs"`
}

func parseBridgeWorkflow(path string) (*bridgeWorkflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var w bridgeWorkflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &w, nil
}

var _ = Describe("qemu test legs that need a second libp2p peer", func() {
	var workflows map[string]*bridgeWorkflow

	BeforeEach(func() {
		entries, err := filepath.Glob(filepath.Join(workflowsDir, "*.y*ml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).ToNot(BeEmpty(), "found no workflow files to check")

		workflows = map[string]*bridgeWorkflow{}
		for _, e := range entries {
			w, err := parseBridgeWorkflow(e)
			Expect(err).ToNot(HaveOccurred())
			workflows[filepath.Base(e)] = w
		}
	})

	// reusable-qemu-test.yaml has carried `use_bridge_network` since the
	// input was added, and no caller ever set it. The two legs below then
	// ran on user-mode networking, where the rendezvous has to go out to
	// the public DHT and back, and reddened master intermittently: green
	// on 10-13, 10-14 and 10-18, red on 10-15 and 10-19, each red sitting
	// on a 600s `Eventually` with one peer in `Active nodes`.
	It("passes use_bridge_network to the reusable workflow", func() {
		var problems []string

		for name, w := range workflows {
			jobNames := make([]string, 0, len(w.Jobs))
			for jn := range w.Jobs {
				jobNames = append(jobNames, jn)
			}
			sort.Strings(jobNames)

			for _, jobName := range jobNames {
				j := w.Jobs[jobName]
				if filepath.Base(j.Uses) != "reusable-qemu-test.yaml" ||
					!strings.HasPrefix(j.Uses, "./.github/workflows/") {
					continue
				}

				want := map[string]bool{}
				for _, leg := range bridgeLegs {
					want[leg] = false
				}
				legHere := false
				for _, e := range j.Include {
					if _, ok := want[e.Test]; !ok {
						continue
					}
					legHere = true
					want[e.Test] = e.Bridge
				}
				if !legHere {
					continue
				}

				for _, leg := range bridgeLegs {
					if !want[leg] {
						problems = append(problems, fmt.Sprintf(
							"%s: job %s runs %s without `bridge: true` in its matrix entry",
							name, jobName, leg))
					}
				}
				if _, ok := j.With["use_bridge_network"]; !ok {
					problems = append(problems, fmt.Sprintf(
						"%s: job %s runs a leg that needs a bridge but its `with:` never sets use_bridge_network",
						name, jobName))
				}
			}
		}

		Expect(problems).To(BeEmpty(), strings.Join(problems, "\n"))
	})

	// The input is only worth setting if the callee still reads it. A
	// rename on the callee side would otherwise leave the callers setting
	// a key GitHub rejects, and the legs back on user-mode networking.
	It("is an input reusable-qemu-test.yaml declares and consumes", func() {
		path := filepath.Join(workflowsDir, "reusable-qemu-test.yaml")
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())

		var w struct {
			On struct {
				WorkflowCall struct {
					Inputs map[string]yaml.Node `yaml:"inputs"`
				} `yaml:"workflow_call"`
			} `yaml:"on"`
		}
		Expect(yaml.Unmarshal(data, &w)).To(Succeed())

		_, declared := w.On.WorkflowCall.Inputs["use_bridge_network"]
		Expect(declared).To(BeTrue(),
			"reusable-qemu-test.yaml no longer declares a use_bridge_network input")

		Expect(string(data)).To(ContainSubstring("USE_BRIDGE_NETWORK: ${{ inputs.use_bridge_network"),
			"reusable-qemu-test.yaml declares use_bridge_network but no longer passes it to the suite")
	})

	// At least one caller has to set it. This is the state the fix found:
	// the input existed, the setup step existed, the suite read the
	// environment variable, and nothing anywhere turned it on.
	It("has at least one caller that sets it", func() {
		callers := []string{}
		for name, w := range workflows {
			if name == "reusable-qemu-test.yaml" {
				continue
			}
			for jobName, j := range w.Jobs {
				if _, ok := j.With["use_bridge_network"]; ok {
					callers = append(callers, name+":"+jobName)
				}
			}
		}
		sort.Strings(callers)
		Expect(callers).ToNot(BeEmpty(),
			"use_bridge_network is declared but no workflow sets it, so every leg runs on user-mode networking")
	})
})
