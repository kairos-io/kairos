package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	cnst "github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/kairos-io/immucore/pkg/op"
	"github.com/kairos-io/immucore/pkg/schema"
	"github.com/kairos-io/kairos-sdk/kcrypt"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/spectrocloud-labs/herd"
)

// Shared steps for all the workflows

// WriteSentinelDagStep sets the sentinel file to identify the boot mode.
// This is used by several things to know in which state they are, for example cloud configs.
func (s *State) WriteSentinelDagStep(g *herd.Graph, deps ...string) error {
	return g.Add(cnst.OpSentinel,
		herd.WithDeps(deps...),
		TimedCallback(cnst.OpSentinel, func(_ context.Context) error {
			var sentinel string

			internalUtils.KLog.Logger.Debug().Msg("Will now create /run/cos if not exists")
			err := internalUtils.CreateIfNotExists("/run/cos/")
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("failed to create /run/cos")
				return err
			}

			internalUtils.KLog.Logger.Debug().Msg("Will now create the runtime object")
			runtime, err := state.NewRuntimeWithLogger(internalUtils.KLog.Logger)
			if err != nil {
				return err
			}
			internalUtils.KLog.Logger.Debug().Msg("Bootstate: " + string(runtime.BootState))

			switch runtime.BootState {
			case state.Active:
				sentinel = "active_mode"
			case state.Passive:
				sentinel = "passive_mode"
			case state.Recovery:
				sentinel = "recovery_mode"
			case state.AutoReset:
				sentinel = "autoreset_mode"
			case state.LiveCD:
				sentinel = "live_mode"
			default:
				sentinel = string(state.Unknown)
			}

			internalUtils.KLog.Logger.Debug().Str("BootState", string(runtime.BootState)).Msg("The BootState was")

			internalUtils.KLog.Logger.Info().Str("to", sentinel).Msg("Setting sentinel file")
			err = os.WriteFile(filepath.Join("/run/cos/", sentinel), []byte("1"), os.ModePerm)
			if err != nil {
				return err
			}

			// In-RAM boots also get an additive sentinel so tooling can tell
			// them apart from a regular Active install. The BootState sentinel
			// above is already active_mode (kairos-sdk forces BootState=Active
			// when kairos.ram is present) so existing gates keep firing.
			if s.InRAM {
				internalUtils.KLog.Logger.Info().Str("to", cnst.InRAMSentinelName).Msg("Setting in-RAM sentinel file")
				if err = os.WriteFile(filepath.Join("/run/cos/", cnst.InRAMSentinelName), []byte("1"), os.ModePerm); err != nil {
					return err
				}
			}

			// Let's add a uki sentinel as well! In-RAM UKI boots come from
			// removable/network media but must behave like an installed
			// Active system, so they get uki_boot_mode, never
			// uki_install_mode (which would fire the installer stages).
cmdline, err := os.ReadFile(internalUtils.GetHostProcCmdline())
if err != nil {
	return fmt.Errorf("reading kernel cmdline: %w", err)
}
if strings.Contains(string(cmdline), "rd.immucore.uki") {
				ukiSentinel := internalUtils.UkiSentinel(state.EfiBootFromInstall(internalUtils.KLog.Logger), s.InRAM)
				internalUtils.KLog.Logger.Info().Str("to", ukiSentinel).Msg("Setting sentinel file")
				if err := os.WriteFile(filepath.Join("/run/cos/", ukiSentinel), []byte("1"), os.ModePerm); err != nil {
					return err
				}
			}

			return nil
		}))
}

// RootfsStageDagStep will add the rootfs stage.
func (s *State) RootfsStageDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpRootfsHook, append(opts, TimedCallback(cnst.OpRootfsHook, s.RunStageOp("rootfs")))...)
}

// InitramfsStageDagStep will add the rootfs stage.
func (s *State) InitramfsStageDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpInitramfsHook, append(opts, TimedCallback(cnst.OpInitramfsHook, s.RunStageOp("initramfs")))...)
}

// RunStageOp runs elemental run-stage stage. If its rootfs its special as it needs som symlinks
// If its uki we don't symlink as we already have everything in the sysroot.
func (s *State) RunStageOp(stage string) func(context.Context) error {
	return func(_ context.Context) error {
		switch stage {
		case "rootfs":
			if !internalUtils.IsUKI() {
				if _, err := os.Stat("/system"); os.IsNotExist(err) {
					err = os.Symlink("/sysroot/system", "/system")
					if err != nil {
						internalUtils.KLog.Logger.Err(err).Msg("creating symlink")
					}
				}
				if _, err := os.Stat("/oem"); os.IsNotExist(err) {
					err = os.Symlink("/sysroot/oem", "/oem")
					if err != nil {
						internalUtils.KLog.Logger.Err(err).Msg("creating symlink")
					}
				}
			}

			internalUtils.KLog.Logger.Info().Msg("Running rootfs stage")
			_ = internalUtils.RunStage("rootfs")
			return nil
		case "initramfs":
			// Not sure if it will work under UKI where the s.Rootdir is the current root already
			internalUtils.KLog.Logger.Info().Msg("Running initramfs stage")
			if internalUtils.IsUKI() {
				_ = internalUtils.RunStage("initramfs")
				return nil
			} else {
				chroot := internalUtils.NewChroot(s.Rootdir)
				return chroot.RunCallback(func() error {
					_ = internalUtils.RunStage("initramfs")
					return nil
				})
			}

		default:
			return errors.New("no stage that we know off")
		}
	}
}

// LoadEnvLayoutDagStep will add the stage to load from cos-layout.env and fill the proper CustomMounts, OverlayDirs and BindMounts.
func (s *State) LoadEnvLayoutDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpLoadConfig,
		append(opts, herd.WithDeps(cnst.OpRootfsHook),
			TimedCallback(cnst.OpLoadConfig, func(_ context.Context) error {
				if s.CustomMounts == nil {
					s.CustomMounts = map[string]string{}
				}

				env, err := internalUtils.ReadEnv("/run/cos/cos-layout.env")
				if err != nil {
					internalUtils.KLog.Logger.Err(err).Msg("Reading env")
					return err
				}
				// populate from env here
				s.OverlayDirs = internalUtils.CleanupSlice(strings.Split(env["RW_PATHS"], " "))
				// Append default RW_Paths if list is empty, otherwise we won't boot properly
				if len(s.OverlayDirs) == 0 {
					s.OverlayDirs = cnst.DefaultRWPaths()
				}

				// Remove any duplicates
				s.OverlayDirs = internalUtils.UniqueSlice(internalUtils.CleanupSlice(s.OverlayDirs))

				s.BindMounts = strings.Split(env["PERSISTENT_STATE_PATHS"], " ")
				// Add custom bind mounts
				s.BindMounts = append(s.BindMounts, strings.Split(env["CUSTOM_BIND_MOUNTS"], " ")...)

				// Same but with distro specific ones
				specificEnv, err := internalUtils.ReadEnv("/run/cos/extra-layout.env")
				if err != nil && os.IsNotExist(err) {
					// Just log the error, we don't care if it does not exist
					internalUtils.KLog.Logger.Debug().Msg("No extra env from /run/cos/extra-layout.env")
				} else {
					// If the specific env has PERSISTENT_STATE_PATHS, we append it to the BindMounts
					internalUtils.KLog.Logger.Debug().Str("specific_env", specificEnv["PERSISTENT_STATE_PATHS"]).Msg("Reading specific env for persistent state paths")
					s.BindMounts = append(s.BindMounts, internalUtils.CleanupSlice(strings.Split(specificEnv["PERSISTENT_STATE_PATHS"], " "))...)
				}

				// Remove any duplicates
				s.BindMounts = internalUtils.UniqueSlice(internalUtils.CleanupSlice(s.BindMounts))

				// Load Overlay config
				overlayConfig := env["OVERLAY"]
				if overlayConfig != "" {
					s.OverlayBase = overlayConfig
				}

				s.StateDir = env["PERSISTENT_STATE_TARGET"]
				if s.StateDir == "" {
					s.StateDir = cnst.PersistentStateTarget
				}

				addLine := func(d string) {
					dat := strings.Split(d, ":")
					if len(dat) == 2 {
						disk := dat[0]
						path := dat[1]
						s.CustomMounts[disk] = path
					}
				}
				// Parse custom mounts also from cmdline (rd.cos.mount=)
				// Parse custom mounts also from cmdline (rd.immucore.mount=)
				// Parse custom mounts also from env file (VOLUMES)

				for _, v := range append(append(internalUtils.ReadCMDLineArg("rd.cos.mount="), internalUtils.ReadCMDLineArg("rd.immucore.mount=")...), strings.Split(env["VOLUMES"], " ")...) {
					addLine(internalUtils.ParseMount(v))
				}

				return nil
			}))...)
}

// MountOemDagStep will add mounting COS_OEM partition under s.Rootdir + /oem .
func (s *State) MountOemDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpMountOEM,
		append(opts,
			TimedCallback(cnst.OpMountOEM, func(ctx context.Context) error {
				runtime, _ := state.NewRuntimeWithLogger(internalUtils.KLog.Logger)
				if runtime.BootState == state.LiveCD {
					internalUtils.KLog.Logger.Debug().Msg("Livecd mode detected, won't mount OEM")
					return nil
				}
				if internalUtils.GetOemLabel() == "" {
					internalUtils.KLog.Logger.Debug().Msg("OEM label from cmdline empty, won't mount OEM")
					return nil
				}
				operation := func(_ context.Context) error {
					fstab, err := op.MountOPWithFstab(
						fmt.Sprintf("/dev/disk/by-label/%s", internalUtils.GetOemLabel()),
						s.path("/oem"),
						internalUtils.DiskFSType(fmt.Sprintf("/dev/disk/by-label/%s", internalUtils.GetOemLabel())),
						[]string{
							"rw",
							"suid",
							"dev",
							"exec",
							"async",
						}, time.Duration(internalUtils.GetOemTimeout())*time.Second)
					for _, f := range fstab {
						s.fstabs = append(s.fstabs, f)
					}
					return err
				}
				return operation(ctx)
			}))...)
}

// MountBaseOverlayDagStep will add mounting /run/overlay as an overlay dir
// Requires the config-load step because some parameters can come from there.
func (s *State) MountBaseOverlayDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpMountBaseOverlay,
		append(opts, herd.WithDeps(cnst.OpLoadConfig),
			TimedCallback(cnst.OpMountBaseOverlay,
				func(_ context.Context) error {
					operation, err := op.BaseOverlay(schema.Overlay{
						Base:        "/run/overlay",
						BackingBase: s.OverlayBase,
					})
					if err != nil {
						return err
					}
					err2 := operation.Run()
					// No error, add fstab
					if err2 == nil {
						s.fstabs = append(s.fstabs, &operation.FstabEntry)
						return nil
					}
					// Error but its already mounted error, dont add fstab but dont return error
					if err2 != nil && errors.Is(err2, cnst.ErrAlreadyMounted) {
						return nil
					}

					return err2
				},
			),
		)...)
}

// MountCustomOverlayDagStep will add mounting s.OverlayDirs under /run/overlay .
func (s *State) MountCustomOverlayDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpOverlayMount,
		append(opts, herd.WithDeps(cnst.OpLoadConfig, cnst.OpMountBaseOverlay),
			TimedCallback(cnst.OpOverlayMount,
				func(_ context.Context) error {
					var multierr *multierror.Error
					internalUtils.KLog.Logger.Debug().Strs("dirs", s.OverlayDirs).Msg("Mounting overlays")
					for _, p := range s.OverlayDirs {
						internalUtils.KLog.Logger.Debug().Str("what", p).Msg("Overlay mount start")
						operation := op.MountWithBaseOverlay(p, s.Rootdir, "/run/overlay")
						err := operation.Run()
						// A path that is missing from the OS image cannot be mounted, but it
						// must not take the whole step down with it: OpMountBind depends on
						// this one, so failing here silently skips every persistent state
						// bind (/etc/systemd, /etc/sysconfig, /home, /var/lib/rancher, ...)
						// and the node boots with a pristine image /etc. Losing one ephemeral
						// RW path is far less damaging, so warn loudly and carry on.
						if errors.Is(err, cnst.ErrMountTargetMissing) {
							internalUtils.KLog.Logger.Warn().Err(err).Str("what", p).
								Msg("Skipping ephemeral overlay: path is not in the OS image. Writes to it will fail against the read-only rootfs. Create the directory at image build time or drop it from ephemeral_mounts/RW_PATHS.")
							continue
						}
						// Append to errors only if it's not an already mounted error
						if err != nil && !errors.Is(err, cnst.ErrAlreadyMounted) {
							internalUtils.KLog.Logger.Err(err).Msg("overlay mount")
							multierr = multierror.Append(multierr, err)
							continue
						}
						s.fstabs = append(s.fstabs, &operation.FstabEntry)
						internalUtils.KLog.Logger.Debug().Str("what", p).Msg("Overlay mount done")
					}
					return multierr.ErrorOrNil()
				},
			),
		)...)
}

// MountCustomMountsDagStep will add mounting s.CustomMounts .
func (s *State) MountCustomMountsDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpCustomMounts, append(opts, herd.WithDeps(cnst.OpLoadConfig),
		TimedCallback(cnst.OpCustomMounts, func(_ context.Context) error {
			var err *multierror.Error
			internalUtils.KLog.Logger.Debug().Interface("mounts", s.CustomMounts).Msg("Mounting custom mounts")

			for what, where := range s.CustomMounts {
				internalUtils.KLog.Logger.Debug().Str("what", what).Str("where", where).Msg("Custom mount start")
				// TODO: scan for the custom mount disk to know the underlying fs and set it proper
				fstype := "ext4"
				mountOptions := []string{"ro"}
				// TODO: Are custom mounts always rw?ro?depends? Clarify.
				// Persistent needs to be RW
				if strings.Contains(what, "COS_PERSISTENT") {
					mountOptions = []string{"rw"}
				}
				fstab, err2 := op.MountOPWithFstab(
					what,
					s.path(where),
					fstype,
					mountOptions,
					3*time.Second,
				)
				for _, f := range fstab {
					s.fstabs = append(s.fstabs, f)
				}

				// If its COS_OEM and it fails then we can safely ignore, as it's not mandatory to have COS_OEM
				if err2 != nil && !strings.Contains(what, "COS_OEM") {
					err = multierror.Append(err, err2)
				}
				internalUtils.KLog.Logger.Debug().Str("what", what).Str("where", where).Msg("Custom mount done")
			}
			internalUtils.KLog.Logger.Warn().Err(err.ErrorOrNil()).Send()

			return err.ErrorOrNil()
		}),
	)...)
}

// MountCustomBindsDagStep will add mounting s.BindMounts
// mount state is defined over a custom mount (/usr/local/.state for instance, needs to be mounted over a device).
func (s *State) MountCustomBindsDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpMountBind,
		append(opts, herd.WithDeps(cnst.OpOverlayMount, cnst.OpCustomMounts, cnst.OpLoadConfig),
			TimedCallback(cnst.OpMountBind,
				func(_ context.Context) error {
					var err *multierror.Error
					internalUtils.KLog.Logger.Debug().Strs("mounts", s.BindMounts).Msg("Mounting binds")

					for _, p := range s.SortedBindMounts() {
						internalUtils.KLog.Logger.Debug().Str("what", p).Msg("Bind mount start")
						operation := op.MountBind(p, s.Rootdir, s.StateDir)
						err2 := operation.Run()
						if err2 == nil {
							// Only append to fstabs if there was no error, otherwise we will try to mount it after switch_root
							s.fstabs = append(s.fstabs, &operation.FstabEntry)
						}
						// Append to errors only if it's not an already mounted error
						if err2 != nil && !errors.Is(err2, cnst.ErrAlreadyMounted) {
							internalUtils.KLog.Logger.Err(err2).Send()
							err = multierror.Append(err, err2)
						}
						internalUtils.KLog.Logger.Debug().Str("what", p).Msg("Bind mount end")
					}
					internalUtils.KLog.Logger.Warn().Err(err.ErrorOrNil()).Send()
					return err.ErrorOrNil()
				},
			),
		)...)
}

// EnableSysAndConfExtensions softlinks extensions for the running state from /var/lib/kairos/extensions/$STATE to /run/extensions.
// So when initramfs stage runs and enables systemd-sysext it can load the extensions for a given bootentry.
// Works for both sysext and confext.
func (s *State) EnableSysAndConfExtensions(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpUkiCopySysExtensions, append(opts, TimedCallback(cnst.OpUkiCopySysExtensions, func(_ context.Context) error {
		// If uki and we are not booting from install media then do nothing
		if internalUtils.IsUKI() {
			if !state.EfiBootFromInstall(internalUtils.KLog.Logger) {
				internalUtils.KLog.Logger.Debug().Msg("Not copying sysextensions as we think we are booting from removable media")
				return nil
			}
		}

		// Not that while we are using the source the /sysroot by using s.path
		// the destination dir is actually /run/extensions without any sysroot path appended
		// This is because after initramfs finishes it will be moved into the final sysroot automatically
		// and the one under /sysroot/run will be shadowed
		// create the /run/extensions dir if it does not exist
		if _, err := os.Stat(cnst.DestSysExtDir); os.IsNotExist(err) {
			err = os.MkdirAll(cnst.DestSysExtDir, 0755)
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Creating sysext dir")
				return err
			}
		}

		// Create the confext dir as well
		if _, err := os.Stat(cnst.DestConfExtDir); os.IsNotExist(err) {
			err = os.MkdirAll(cnst.DestConfExtDir, 0755)
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Creating confext dir")
				return err
			}
		}

		err := validateAndEnableSysConfExtensions(s, cnst.SysExt)
		if err != nil {
			return err
		}
		err = validateAndEnableSysConfExtensions(s, cnst.ConfExt)
		if err != nil {
			return err
		}

		return nil
	}))...)
}

// bootStateToExtensionSubDir returns the sub-directory of the extension source
// that a boot state installs from. The second value is false for a boot state
// that installs no extensions. AutoReset runs the recovery image, so it reads
// the recovery directory.
func bootStateToExtensionSubDir(b state.Boot) (string, bool) {
	switch b {
	case state.Active:
		return "active", true
	case state.Passive:
		return "passive", true
	case state.Recovery, state.AutoReset:
		return "recovery", true
	default:
		return "", false
	}
}

// validateAndEnableSysConfExtensions is the common logic for validating and enabling both sys and conf extensions,
// as they work in a similar way, just different source and destination dirs and different validation for sys extensions.
func validateAndEnableSysConfExtensions(s *State, extType string) error {
	// At this point the extensions dir should be available
	r, err := state.NewRuntimeWithLogger(internalUtils.KLog.Logger)
	if err != nil {
		return err
	}
	var dir string
	var sourceDir string
	var destDir string

	switch extType {
	case cnst.SysExt:
		internalUtils.KLog.Logger.Debug().Msg("Enabling system extensions")
		sourceDir = cnst.SourceSysExtDir
		destDir = cnst.DestSysExtDir
	case cnst.ConfExt:
		internalUtils.KLog.Logger.Debug().Msg("Enabling config extensions")
		sourceDir = cnst.SourceConfExtDir
		destDir = cnst.DestConfExtDir
	default:
		return errors.New("unknown extension type")
	}

	subDir, known := bootStateToExtensionSubDir(r.BootState)
	if !known {
		internalUtils.KLog.Logger.Debug().Str("state", string(r.BootState)).Msg("Not copying sysextensions as we are not in a state that we know off")
		return nil
	}
	dir = filepath.Join(sourceDir, subDir)
	// move to use dir with the full path from here so its simpler
	entries, err := os.ReadDir(s.path(dir))
	// We don't care if the dir does not exist
	if err != nil && !os.IsNotExist(err) {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".raw" {
			// If the file is a raw file, lets softlink it
			if internalUtils.IsUKI() {
				// Verify the signature
				output, err := internalUtils.CommandWithPath(fmt.Sprintf("systemd-dissect --validate %s %s", cnst.SysextDefaultPolicy, s.path(filepath.Join(dir, entry.Name()))))
				if err != nil {
					// If the file didn't pass the validation, we don't copy it
					internalUtils.KLog.Logger.Warn().Str("src", s.path(filepath.Join(dir, entry.Name()))).Msgf("%s does not pass validation", extType)
					internalUtils.KLog.Logger.Debug().Err(err).Str("src", s.path(filepath.Join(dir, entry.Name()))).Str("output", output).Msgf("Validating %s", extType)
					continue
				}
			}
			// Check if it already exists with the same name
			// This is because as we have the common dir, there could be a point in which the common dir and the
			// specific boot state dir have the same file, and we dont want to fail at this point, just warn and continue
			if _, err := os.Stat(filepath.Join(destDir, entry.Name())); !os.IsNotExist(err) {
				// If it exists, we can just skip it
				internalUtils.KLog.Logger.Warn().Str("file", filepath.Join(destDir, entry.Name())).Msgf("Skipping %s as its already enabled", extType)
				continue
			}
			// it has to link to the final dir after initramfs, so we avoid setting s.path here for the target
			err = os.Symlink(filepath.Join(dir, entry.Name()), filepath.Join(destDir, entry.Name()))
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Creating symlink")
				return err
			}
			internalUtils.KLog.Logger.Debug().Str("what", entry.Name()).Msgf("Enabled %s", extType)
		}
	}

	// Common dir is always there for all states no matter what
	commonEntries, _ := os.ReadDir(s.path(filepath.Join(sourceDir, "common")))
	for _, entry := range commonEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".raw" {
			// If the file is a raw file, lets softlink it
			if internalUtils.IsUKI() {
				// Verify the signature
				output, err := internalUtils.CommandWithPath(fmt.Sprintf("systemd-dissect --validate %s %s", cnst.SysextDefaultPolicy, s.path(filepath.Join(sourceDir, "common", entry.Name()))))
				if err != nil {
					// If the file didn't pass the validation, we don't copy it
					internalUtils.KLog.Logger.Warn().Str("src", s.path(filepath.Join(sourceDir, "common", entry.Name()))).Msgf("%s does not pass validation", extType)
					internalUtils.KLog.Logger.Debug().Err(err).Str("src", s.path(filepath.Join(sourceDir, "common", entry.Name()))).Str("output", output).Msgf("Validating %s", extType)
					continue
				}
			}
			// Check if it already exists with the same name
			// This is because as we have the common dir, there could be a point in which the common dir and the
			// specific boot state dir have the same file, and we dont want to fail at this point, just warn and continue
			if _, err := os.Stat(filepath.Join(destDir, entry.Name())); !os.IsNotExist(err) {
				// If it exists, we can just skip it
				internalUtils.KLog.Logger.Warn().Str("file", filepath.Join(destDir, entry.Name())).Msgf("Skipping %s as its already enabled", extType)
				continue
			}
			// it has to link to the final dir after initramfs, so we avoid setting s.path here for the target
			err = os.Symlink(filepath.Join(sourceDir, "common", entry.Name()), filepath.Join(destDir, entry.Name()))
			if err != nil {
				internalUtils.KLog.Logger.Err(err).Msg("Creating symlink")
				return err
			}
			internalUtils.KLog.Logger.Debug().Str("what", entry.Name()).Msgf("Enabled %s", extType)
		}
	}
	return nil
}

// WriteFstabDagStep will add writing the final fstab file with all the mounts
// Depends on everything but weak, so it will still try to write.
func (s *State) WriteFstabDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpWriteFstab, append(opts, TimedCallback(cnst.OpWriteFstab, s.WriteFstab()))...)
}

// unlockEncryptedPartitions is the unified logic for unlocking encrypted partitions.
// It works for both UKI and non-UKI modes by using kcrypt.UnlockAllEncryptedPartitions()
// which automatically detects the appropriate encryption method and unlocks all partitions.
func unlockEncryptedPartitions() error {
	return kcrypt.UnlockAllEncryptedPartitions(internalUtils.KLog)
}
