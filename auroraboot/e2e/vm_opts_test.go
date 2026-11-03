package e2e_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud/peg/pkg/machine/types"
)

var _ = Describe("vmOpts", Label("e2e"), func() {
	var stateDir string

	BeforeEach(func() {
		stateDir = GinkgoT().TempDir()
	})

	build := func(spec vmSpec) *types.MachineConfig {
		cfg := types.DefaultMachineConfig()
		for _, o := range vmOpts(stateDir, spec) {
			Expect(o(cfg)).To(Succeed())
		}
		return cfg
	}

	It("builds the options without starting a TPM", func() {
		build(vmSpec{ISO: "/x.iso"})

		Expect(filepath.Join(stateDir, "tpm")).NotTo(BeADirectory())
	})

	It("boots the ISO it is given", func() {
		GinkgoT().Setenv("ISO", "")
		Expect(build(vmSpec{ISO: "/x.iso"}).ISO).To(Equal("/x.iso"))
	})

	It("uses the memory and CPUs it is given over the environment", func() {
		GinkgoT().Setenv("MEMORY", "6000")
		GinkgoT().Setenv("CPUS", "8")
		cfg := build(vmSpec{ISO: "/x.iso", Memory: "4000", CPUs: "2"})
		Expect(cfg.Memory).To(Equal("4000"))
		Expect(cfg.CPU).To(Equal("2"))
	})

	It("falls back to MEMORY and CPUS", func() {
		GinkgoT().Setenv("MEMORY", "5000")
		GinkgoT().Setenv("CPUS", "3")
		cfg := build(vmSpec{ISO: "/x.iso"})
		Expect(cfg.Memory).To(Equal("5000"))
		Expect(cfg.CPU).To(Equal("3"))
	})

	It("falls back to the default memory and CPUs", func() {
		GinkgoT().Setenv("MEMORY", "")
		GinkgoT().Setenv("CPUS", "")
		cfg := build(vmSpec{ISO: "/x.iso"})
		Expect(cfg.Memory).To(Equal("6000"))
		Expect(cfg.CPU).To(Equal("4"))
	})
})

// idMachine is a machine that only knows its ID.
type idMachine struct {
	types.Machine
	id string
}

func (m idMachine) Config() types.MachineConfig { return types.MachineConfig{ID: m.id} }

var _ = Describe("vmLogDir", Label("e2e"), func() {
	It("keeps each VM in its own directory under VM_LOG_DIR", func() {
		GinkgoT().Setenv("VM_LOG_DIR", "/base")
		Expect(vmLogDir(testVM{machine: idMachine{id: "a"}})).To(Equal("/base/a"))
		Expect(vmLogDir(testVM{machine: idMachine{id: "b"}})).To(Equal("/base/b"))
	})

	It("defaults the base to logs", func() {
		GinkgoT().Setenv("VM_LOG_DIR", "")
		Expect(vmLogDir(testVM{machine: idMachine{id: "a"}})).To(Equal("logs/a"))
	})
})
