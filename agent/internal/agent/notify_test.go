package agent_test

import (
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/v4/agent/internal/agent"
	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/pkg/cmd"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// notifyProvider records that it ran, next to its own executable, so the spec
// can tell which directory the provider that answered was loaded from.
const notifyProvider = `#!/bin/bash
echo "ran" >> "$(dirname "$0")/ran.log"
echo "{}"
`

var _ = Describe("Notify", func() {
	var providerDir, configDir string

	BeforeEach(func() {
		var err error
		providerDir, err = os.MkdirTemp("", "notify-providers")
		Expect(err).ToNot(HaveOccurred())

		configDir, err = os.MkdirTemp("", "notify-config")
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(filepath.Join(providerDir, "agent-provider-notify"),
			[]byte(notifyProvider), 0777)
		Expect(err).ToNot(HaveOccurred())

		// providerDir is none of the default provider directories, so the only
		// way the provider in it can answer is through providers.paths.
		err = os.WriteFile(filepath.Join(configDir, "providers.yaml"), []byte(`#cloud-config
providers:
  paths:
    - `+providerDir+"\n"), 0644)
		Expect(err).ToNot(HaveOccurred())

		// Start from the bus a fresh process has.
		bus.Manager = bus.NewBus()
	})

	AfterEach(func() {
		_ = os.RemoveAll(providerDir)
		_ = os.RemoveAll(configDir)
		bus.Manager = bus.NewBus()
	})

	It("asks the providers in providers.paths", func() {
		Expect(Notify("agent.bootstrap", []string{configDir})).To(Succeed())

		Expect(bus.Manager.LoadedPaths()).To(Equal([]string{providerDir}))
		Expect(filepath.Join(providerDir, "ran.log")).To(BeAnExistingFile())
	})

	It("still asks them after the command line was parsed", func() {
		// The whole bug: the command line is parsed by cmd.Run, which used to
		// initialize the bus from the default directories before any command
		// could say which directories it wanted. By the time notify asked, the
		// bus was already registered and providers.paths was dropped.
		args := os.Args
		defer func() { os.Args = args }()
		os.Args = []string{"kairos-agent", "--help"}

		Expect(cmd.Run()).To(Equal(0))

		Expect(Notify("agent.bootstrap", []string{configDir})).To(Succeed())

		Expect(bus.Manager.LoadedPaths()).To(Equal([]string{providerDir}))
		Expect(filepath.Join(providerDir, "ran.log")).To(BeAnExistingFile())
	})
})
