package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/internal/version"
	"github.com/kairos-io/immucore/pkg/dag"
	"github.com/kairos-io/immucore/pkg/state"
	"github.com/spectrocloud-labs/herd"
	"github.com/urfave/cli/v2"
)

// Apply Immutability profiles.
func main() {
	app := cli.NewApp()
	app.Version = version.GetVersion()
	app.Authors = []*cli.Author{{Name: "Kairos authors"}}
	app.Copyright = "kairos authors"
	app.Action = func(c *cli.Context) (err error) {
		var targetDevice, targetImage string
		var st *state.State

		utils.MountBasic()
		utils.SetLogger()

		v := version.Get()
		utils.KLog.Logger.Info().Str("commit", v.GitCommit).Str("compiled_with", v.GoVersion).Str("version", v.Version).Msg("Immucore")

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
	app.Commands = []*cli.Command{
		{
			Name:  "version",
			Usage: "version",
			Action: func(_ *cli.Context) error {
				utils.SetLogger()
				v := version.Get()
				utils.KLog.Logger.Info().Str("commit", v.GitCommit).Str("compiled_with", v.GoVersion).Str("version", v.Version).Msg("Immucore")
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
