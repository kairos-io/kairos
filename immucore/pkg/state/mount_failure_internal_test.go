package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("mount failure policy", func() {
	var ranStages []string
	var stageErr error
	var failure error

	// stubStage stands in for the yip run. Recording the stage name is what
	// lets a spec say the hook was offered its turn without a cloud-init tree
	// on disk. Same shape as the breakpointHook stub in
	// breakpoint_internal_test.go.
	stubStage := func() {
		old := runMountFailureStage
		runMountFailureStage = func(stage string) error {
			ranStages = append(ranStages, stage)

			return stageErr
		}
		DeferCleanup(func() { runMountFailureStage = old })
	}

	// cmdline writes a /proc/cmdline for this spec and points immucore's
	// reader at it. ReadCMDLineArg goes through GetHostProcCmdline, which
	// honours HOST_PROC_CMDLINE, so the real reader is under test here rather
	// than a stub of it.
	cmdline := func(content string) {
		path := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
		GinkgoT().Setenv("HOST_PROC_CMDLINE", path)
	}

	BeforeEach(func() {
		internalUtils.KLog = logger.NewNullLogger()
		ranStages = nil
		stageErr = nil
		failure = errors.New("extra volume has no such label")
		stubStage()
		cmdline("BOOT_IMAGE=/boot/vmlinuz root=LABEL=COS_STATE\n")
	})

	// The default is the whole point of the change this file backs: a node
	// that could not be given its persistent state must not boot to a login
	// prompt pretending it has one.
	It("returns the failure, so the step stays fatal, when the cmdline is silent", func() {
		Expect(onMountFailure(cnst.OpMountBind, failure)).To(MatchError(failure))
	})

	It("runs the mount failure stage before deciding", func() {
		Expect(onMountFailure(cnst.OpMountBind, failure)).To(HaveOccurred())
		Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}))
	})

	// The override is what the maintainer asked for: the old carry-on
	// behaviour, available to an operator who would rather have the node than
	// the diagnosis.
	It("swallows the failure when the cmdline asks it to", func() {
		cmdline("root=LABEL=COS_STATE " + cnst.CmdlineIgnoreMountFailure + "\n")

		Expect(onMountFailure(cnst.OpMountBind, failure)).To(Succeed())
	})

	// The stage is the hook for the state, not for the halt. A distribution
	// that set the override still has to get its turn to recover.
	It("runs the stage on the override path too", func() {
		cmdline("root=LABEL=COS_STATE " + cnst.CmdlineIgnoreMountFailure + "\n")

		Expect(onMountFailure(cnst.OpCustomMounts, failure)).To(Succeed())
		Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}))
	})

	// A `commands:` block in the stage has to be able to branch on what broke
	// without parsing a log, so both values are in the environment by the time
	// the stage runs, not after it.
	It("exports the step and the error to the stage environment", func() {
		var stepAtStageTime, errAtStageTime string
		old := runMountFailureStage
		runMountFailureStage = func(_ string) error {
			stepAtStageTime = os.Getenv(cnst.EnvMountFailureStep)
			errAtStageTime = os.Getenv(cnst.EnvMountFailure)

			return nil
		}
		DeferCleanup(func() { runMountFailureStage = old })
		DeferCleanup(func() {
			Expect(os.Unsetenv(cnst.EnvMountFailureStep)).To(Succeed())
			Expect(os.Unsetenv(cnst.EnvMountFailure)).To(Succeed())
		})

		Expect(onMountFailure(cnst.OpCustomMounts, failure)).To(HaveOccurred())

		Expect(stepAtStageTime).To(Equal(cnst.OpCustomMounts))
		Expect(errAtStageTime).To(Equal("extra volume has no such label"))
	})

	// A hook that is itself broken must not become the reason the boot
	// stopped, and must not rescue a boot that was going to stop anyway.
	It("keeps the mount error when the stage itself fails", func() {
		stageErr = errors.New("the hook exited 1")

		Expect(onMountFailure(cnst.OpMountBind, failure)).To(MatchError(failure))
	})

	It("still honours the override when the stage itself fails", func() {
		stageErr = errors.New("the hook exited 1")
		cmdline("root=LABEL=COS_STATE " + cnst.CmdlineIgnoreMountFailure + "\n")

		Expect(onMountFailure(cnst.OpMountBind, failure)).To(Succeed())
	})

	// ReadCMDLineArg matches on a prefix, so a stanza that merely starts with
	// the same letters must not turn the policy off by accident.
	It("is not switched on by an unrelated stanza", func() {
		cmdline("root=LABEL=COS_STATE rd.immucore.debug rd.immucore.break=mount-root\n")

		Expect(onMountFailure(cnst.OpMountBind, failure)).To(MatchError(failure))
	})

	// The specs above decide what the policy does. These two decide that the
	// bind step asks it, which is a separate claim and the one a refactor is
	// most likely to drop.
	//
	// The bind is made to fail without privilege and without touching a real
	// device, by pointing Rootdir at a regular file: op.MountBind's prepare
	// callback does a MkdirAll under it, which is ENOTDIR for root and
	// non-root alike, and MountOperation.Run returns a prepare error before it
	// reaches mount(2).
	Describe("the bind step", func() {
		var g *herd.Graph
		var s *State

		BeforeEach(func() {
			notADir := filepath.Join(GinkgoT().TempDir(), "rootdir-is-a-file")
			Expect(os.WriteFile(notADir, []byte("not a directory"), 0o600)).To(Succeed())

			g = herd.DAG()
			s = &State{
				Rootdir:    notADir,
				StateDir:   GinkgoT().TempDir(),
				BindMounts: []string{"/kairos-mount-failure-spec"},
			}
			for _, dep := range []string{cnst.OpLoadConfig, cnst.OpCustomMounts, cnst.OpOverlayMount} {
				Expect(g.Add(dep)).To(Succeed())
			}
			Expect(s.MountCustomBindsDagStep(g)).To(Succeed())
		})

		It("stops the boot on a bind that did not mount", func() {
			Expect(g.Run(context.Background())).ToNot(Succeed())
			Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}),
				"the step has to go through the policy, not return the error itself")
		})

		It("boots on when the cmdline overrides the policy", func() {
			cmdline("root=LABEL=COS_STATE " + cnst.CmdlineIgnoreMountFailure + "\n")

			Expect(g.Run(context.Background())).To(Succeed())
			Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}))
		})
	})

	// Same claim for the other step. A by-label custom mount naming a label
	// no disk carries fails in resolveMountSource and never reaches mount(2),
	// which is the shape kairos#4782 was reported as: one bad VOLUMES entry
	// taking the whole step down and with it every persistent bind.
	Describe("the custom mounts step", func() {
		var g *herd.Graph
		var s *State

		BeforeEach(func() {
			g = herd.DAG()
			s = &State{
				Rootdir: GinkgoT().TempDir(),
				CustomMounts: map[string]string{
					"/dev/disk/by-label/KAIROS_NO_SUCH_LABEL": "/var/lib/spec",
				},
			}
			Expect(g.Add(cnst.OpLoadConfig)).To(Succeed())
			Expect(s.MountCustomMountsDagStep(g)).To(Succeed())
		})

		// This step is not fatal on its own: it is a hard dependency of the
		// fatal OpMountBind, so what stops the boot is herd refusing to run a
		// node whose dependency errored. The claim here is therefore about
		// what the step returns, which is what herd reads.
		stepError := func() error {
			for _, layer := range g.Analyze() {
				for _, e := range layer {
					if e.Name == cnst.OpCustomMounts {
						return e.Error
					}
				}
			}

			Fail("no graph entry named " + cnst.OpCustomMounts)

			return nil
		}

		It("errors the step on a custom mount that did not resolve", func() {
			Expect(g.Run(context.Background())).To(Succeed(), "the step is not fatal on its own")
			Expect(stepError()).To(HaveOccurred())
			Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}),
				"the step has to go through the policy, not return the error itself")
		})

		It("leaves the step clean when the cmdline overrides the policy", func() {
			cmdline("root=LABEL=COS_STATE " + cnst.CmdlineIgnoreMountFailure + "\n")

			Expect(g.Run(context.Background())).To(Succeed())
			Expect(stepError()).ToNot(HaveOccurred())
			Expect(ranStages).To(Equal([]string{cnst.StageMountFailure}))
		})
	})
})
