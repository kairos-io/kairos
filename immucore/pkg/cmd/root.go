// Package cmd is the importable entrypoint of immucore. It exposes the CLI app
// so both the standalone immucore binary and the multi-call kairos binary can
// dispatch into it.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/dag"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/internal/version"
	"github.com/spectrocloud-labs/herd"
	"github.com/urfave/cli/v2"
)

// NewApp returns the immucore urfave/cli app, ready to be Run.
func NewApp() *cli.App {
	app := cli.NewApp()
	app.Name = "immucore"
	app.Version = version.Version
	app.Authors = []*cli.Author{{Name: "Kairos authors"}}
	app.Copyright = "kairos authors"
	app.Action = func(c *cli.Context) (err error) {
		var targetDevice, targetImage string
		var st *state.State

		utils.MountBasic()
		utils.SetLogger()

		utils.KLog.Logger.Info().Str("version", version.Version).Str("go", runtime.Version()).Msg("Immucore")

		cmdline, _ := os.ReadFile(utils.GetHostProcCmdline())
		utils.KLog.Logger.Debug().Str("content", string(cmdline)).Msg("cmdline")
		g := herd.DAG(herd.EnableInit)

		// Get targets and state
		targetImage, targetDevice, err = utils.GetTarget(c.Bool("dry-run"))
		if err != nil {
			return err
		}

		st = &state.State{
			Rootdir:       utils.GetRootDir(),
			TargetDevice:  targetDevice,
			TargetImage:   targetImage,
			RootMountMode: utils.RootRW(),
			OverlayBase:   utils.GetOverlayBase(),
			InRAM:         utils.BootInRAM(),
		}

		// normalBoot tracks whether we took the full active/passive/recovery mount
		// pipeline. Only that path should drop to an emergency shell on failure:
		// live media intentionally disables immucore, and UKI already drops to a
		// shell from inside its own steps.
		var normalBoot bool
		switch {
		case st.InRAM && utils.IsUKI():
			// Trusted boot in-RAM: the UKI is already the whole system in
			// RAM, so the regular UKI DAG applies — RegisterUKI keys off
			// st.InRAM to add partition provisioning (encrypted with the TPM
			// policy) and to skip the removable-media unlock/sentinel gates.
			utils.KLog.Logger.Info().Msg("UKI booting in-RAM (kairos.ram) with OEM+persistent from disk!")
			err = dag.RegisterUKI(st, g)
		case st.InRAM:
			// kairos.ram must win over DisableImmucore below: an in-RAM boot
			// still has live:LABEL / netboot on the cmdline (that is how the
			// squashfs got copied into RAM in the first place), and we do NOT
			// want the live-media no-op path — we still need to mount OEM +
			// persistent + apply cloud-init from disk.
			utils.KLog.Logger.Info().Msg("Booting in-RAM (kairos.ram) with OEM+persistent from disk.")
			normalBoot = true
			err = dag.RegisterInRAMBoot(st, g)
		case utils.DisableImmucore():
			utils.KLog.Logger.Info().Msg("Stanza rd.cos.disable/rd.immucore.disable on the cmdline or booting from CDROM/Netboot/Squash recovery. Disabling immucore.")
			err = dag.RegisterLiveMedia(st, g)
		case utils.IsUKI():
			utils.KLog.Logger.Info().Msg("UKI booting!")
			err = dag.RegisterUKI(st, g)
		default:
			utils.KLog.Logger.Info().Msg("Booting on active/passive/recovery.")
			normalBoot = true
			err = dag.RegisterNormalBoot(st, g)
		}

		if err != nil {
			return err
		}

		utils.KLog.Logger.Info().Msg(st.WriteDAG(g))

		// Once we print the dag we can exit already
		if c.Bool("dry-run") {
			return nil
		}

		// A signal that arrives while the DAG is still running means the mount
		// graph never completed. Exiting quietly here is what leaves a booted
		// system with no binds, no /etc overlay and no /usr/local, so take over
		// the console instead of letting the boot continue.
		stopWatching := watchForTermination(normalBoot, haltTerminated)
		defer stopWatching()

		err = g.Run(context.Background())
		utils.KLog.Logger.Info().Msg(st.WriteDAG(g))
		// Emit the boot timeline (slowest-first) to the log and a machine-readable
		// trace file under constants.LogDir for diagnosing slow/hung boots.
		utils.KLog.Logger.Info().Msg(state.RenderTimeline())
		state.LogTimeline(constants.LogDir)

		// On a normal-boot failure, print and persist a failure summary so the
		// operator can see which DAG step broke and where the logs are, then return
		// the error. We do NOT exec our own shell here: normal boot runs immucore as
		// a dracut hook (not as init), so dracut owns the emergency shell. Exec-ing a
		// shell ourselves would either hang (no console) or continue booting into the
		// broken system on shell exit. UKI drops to a shell from inside its own steps
		// because there immucore is the init; live media is intentionally disabled.
		if err != nil && normalBoot {
			summary := utils.RenderFailureSummary(st.FailureReason(g), constants.LogDir)
			fmt.Fprint(os.Stderr, summary)
			if _, werr := utils.WriteFailureSummary(constants.LogDir, summary); werr != nil {
				utils.KLog.Logger.Err(werr).Msg("writing failure summary")
			}
		}
		return err
	}
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name: "dry-run",
		},
	}
	return app
}

// Run runs the immucore CLI with os.Args and returns a process exit code.
func Run() int {
	if err := NewApp().Run(os.Args); err != nil {
		fmt.Println(err)
		return 1
	}
	return 0
}

// watchForTermination calls onSignal if immucore is signalled before its mount
// DAG finishes. It returns a function that stops the watch, to be deferred by
// the caller once the DAG has run. Production passes haltTerminated.
//
// Only a normal boot is guarded. Live media runs a no-op DAG, and UKI makes
// immucore the init, where a signal is not a mid-mount interruption of a
// system that is about to switch root.
//
// The oneshot unit keeps RemainAfterExit=yes, so on a healthy boot the process
// has already exited by the time systemd acts on Conflicts= during the switch
// to the real root. Nothing is left to signal, and this watch never fires.
func watchForTermination(normalBoot bool, onSignal func(os.Signal)) func() {
	if !normalBoot {
		return func() {}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	stop := watchSignals(signals, onSignal)

	return func() {
		signal.Stop(signals)
		stop()
	}
}

// watchSignals calls onSignal with the first signal to arrive on ch. The
// returned function ends the watch; calling it after a signal has already been
// handled is harmless.
func watchSignals(ch <-chan os.Signal, onSignal func(os.Signal)) func() {
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			onSignal(sig)
		case <-done:
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// haltTerminated takes over the console and stops the boot. Nothing mounted
// the real root, so there is no system worth continuing into.
func haltTerminated(sig os.Signal) {
	utils.HaltWithBanner(
		utils.RenderTerminatedMessage(sig.String(), constants.LogDir),
		fmt.Sprintf("terminated by %s before the mount DAG completed", sig),
		fmt.Errorf("immucore terminated by %s", sig),
	)
	// Reached only on non-systemd hosts (Alpine/openrc), where HaltWithBanner
	// paints the screen and returns. There is no boot left to protect at that
	// point, so stop the process rather than letting a half-mounted root come
	// up.
	os.Exit(1)
}
