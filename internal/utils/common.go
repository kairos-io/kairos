package utils

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go"
	"github.com/google/go-tpm/tpm2"
	"github.com/google/go-tpm/tpm2/transport"
	"github.com/joho/godotenv"
	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/kairos-sdk/state"
	"golang.org/x/term"
)

// BootStateToLabelDevice lets us know the device we need to mount sysroot on based on labels.
func BootStateToLabelDevice() string {
	runtime, err := state.NewRuntimeWithLogger(KLog.Logger)
	if err != nil {
		return ""
	}
	switch runtime.BootState {
	case state.Active:
		return filepath.Join("/dev/disk/by-label", "COS_ACTIVE")
	case state.Passive:
		return filepath.Join("/dev/disk/by-label", "COS_PASSIVE")
	case state.Recovery:
		return filepath.Join("/dev/disk/by-label", "COS_SYSTEM")
	default:
		return ""
	}
}

// GetRootDir returns the proper dir to mount all the stuff
// Useful if we want to move to a no-pivot boot.
func GetRootDir() string {
	cmdline, _ := os.ReadFile(GetHostProcCmdline())
	switch {
	case state.DetectUKIboot(string(cmdline)):
		return "/"
	default:
		// Default is sysroot for normal no-pivot boot
		return "/sysroot"
	}
}

// UniqueSlice removes duplicated entries from a slice.So dumb. Like really? Why not have a set which enforces uniqueness????
func UniqueSlice(slice []string) []string {
	keys := make(map[string]bool)
	var list []string
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

// ReadEnv will read an env file (key=value) and return a nice map.
func ReadEnv(file string) (map[string]string, error) {
	var envMap map[string]string
	var err error

	f, err := os.Open(file)
	if err != nil {
		return envMap, err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	envMap, err = godotenv.Parse(f)
	if err != nil {
		return envMap, err
	}

	return envMap, err
}

// CreateIfNotExists will check if a path exists and create it if needed.
func CreateIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, os.ModePerm)
	}

	return nil
}

// CleanupSlice will clean a slice of strings of empty items
// Typos can be made on writing the cos-layout.env file and that could introduce empty items
// In the lists that we need to go over, which causes bad stuff.
func CleanupSlice(slice []string) []string {
	var cleanSlice []string
	for _, item := range slice {
		if strings.Trim(item, " ") == "" {
			continue
		}
		cleanSlice = append(cleanSlice, item)
	}
	return cleanSlice
}

// GetTarget gets the target image and device to mount in /sysroot.
func GetTarget(dryRun bool) (string, string, error) {
	if IsUKI() {
		return "", "", nil
	}

	label := BootStateToLabelDevice()

	// If dry run, or we are disabled return whatever values, we won't go much further
	if dryRun || DisableImmucore() {
		return "fake", label, nil
	}

	imgs := CleanupSlice(ReadCMDLineArg("cos-img/filename="))

	// If no image just panic here, we cannot longer continue
	if len(imgs) == 0 {
		msg := "could not get the image name from cmdline (i.e. cos-img/filename=/cOS/active.img)"
		KLog.Logger.Error().Msg(msg)
		return "", "", errors.New(msg)
	}

	KLog.Logger.Debug().Str("what", imgs[0]).Msg("Target device")
	KLog.Logger.Debug().Str("what", label).Msg("Target label")
	return imgs[0], label, nil
}

// RebootOrWait halts the machine (or reboots it, if
// rd.immucore.rebootonfailure is on the cmdline) after logging msg + err.
// Used from UKI paths (SecureBoot missing, kcrypt unlock failed) where the
// operator does not need to fix anything at the cmdline — the machine is
// broken enough that rebooting is the sensible default. For missing-config
// halts that need to display an actionable message on the console, use
// HaltWithBanner instead.
//
// We deliberately do issue a real syscall.Reboot to the kernel. Just
// returning an error from immucore.service lets systemd move on to
// initrd-switch-root.target regardless (Conflicts= only stops immucore
// during the switch, it does not block the switch itself), so boot would
// continue into a userland where our mounts never happened. `select {}` used
// to be the "halt" path, but that only blocks the goroutine — same silent
// boot-continue outcome after systemd's DefaultTimeoutStartSec kicks in.
func RebootOrWait(msg string, err error) {
	if err != nil {
		KLog.Logger.Error().Err(err).Msg(msg)
	}
	syscall.Sync()
	if len(ReadCMDLineArg("rd.immucore.rebootonfailure")) > 0 {
		KLog.Logger.Warn().Msg(fmt.Sprintf("%s - Rebooting in 10 seconds", msg))
		time.Sleep(10 * time.Second)
		if rerr := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); rerr != nil {
			KLog.Logger.Err(rerr).Msg("reboot syscall failed; blocking to avoid boot continuation")
			select {}
		}
	}
	KLog.Logger.Warn().Msg(fmt.Sprintf("%s - Halting boot", msg))
	if herr := syscall.Reboot(syscall.LINUX_REBOOT_CMD_HALT); herr != nil {
		KLog.Logger.Err(herr).Msg("halt syscall failed; blocking to avoid boot continuation")
		select {}
	}
}

// SystemdBooted reports whether systemd is PID 1, using the same check as
// sd_booted(3): /run/systemd/system/ exists only when systemd created it
// during early boot. Alpine (openrc) and other non-systemd inits lack it.
func SystemdBooted() bool {
	st, err := os.Stat("/run/systemd/system")
	return err == nil && st.IsDir()
}

// HaltWithBanner is the operator-facing halt used by boot-blocking
// configuration errors (render the screen with RenderFailureScreen). Unlike
// RebootOrWait, which just prints a one-line log message, HaltWithBanner
// takes over the console so the message actually stays put.
//
// On systemd systems it never returns:
//
//  1. Silences kernel + systemd console logging so the banner is not
//     clobbered ("A start job is running for ..." et al).
//  2. Paints banner once on every console derived from the cmdline's
//     console= stanzas, plus a footer explaining the reboot behavior.
//  3. Spawns raw-mode key listeners on the same consoles. Any single byte
//     triggers an immediate reboot.
//  4. If nobody presses anything within 90 seconds, starts systemd's own
//     systemd-reboot.service — a clean reboot through the normal machinery
//     so boot-assessment counters (boot attempts / systemd-bless-boot) see
//     the failed boot and can act on it.
//  5. rd.immucore.rebootonfailure short-circuits everything above and just
//     reboots after 10 seconds — useful for hands-off PXE fleet setups.
//
// On non-systemd systems (Alpine/openrc) there is no job engine holding
// boot for us and no reboot service to trigger: paint the banner, log, and
// RETURN so the caller can propagate a normal error — same behavior the DAG
// had before this feature existed.
//
// banner is the full pre-rendered screen (ANSI codes included). logMsg is a
// short one-liner for the structured log. err may be nil.
func HaltWithBanner(banner, logMsg string, err error) {
	if err != nil {
		KLog.Logger.Error().Err(err).Msg(logMsg)
	}
	syscall.Sync()

	consoleDevs := ConsoleDevices()
	consoles := openConsolesForWriting(consoleDevs...)

	if !SystemdBooted() {
		// Alpine & friends: no systemd to fight over the console and no
		// reboot service for boot assessment. Show the screen, return, let
		// the caller fail the step normally.
		paintBanner(consoles, banner)
		KLog.Logger.Warn().Msg(fmt.Sprintf("%s - non-systemd host, returning error to the boot flow", logMsg))
		return
	}

	if len(ReadCMDLineArg("rd.immucore.rebootonfailure")) > 0 {
		KLog.Logger.Warn().Msg(fmt.Sprintf("%s - Rebooting in 10 seconds", logMsg))
		time.Sleep(10 * time.Second)
		if rerr := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); rerr != nil {
			KLog.Logger.Err(rerr).Msg("reboot syscall failed; blocking to avoid boot continuation")
			select {}
		}
	}

	// Best-effort silencing stack. Failure of any single step is fine — this
	// is UX polish, not a correctness step.
	//
	//  1. Kernel console loglevel → EMERG-only. Kills `dmesg`-style noise.
	//  2. systemd's own logging → target null. Kills its journal-to-console
	//     bridge.
	//  3. systemd's PID 1 ShowStatus → off via SIGRTMIN+21 (the show-status
	//     OVERRIDE; the cylon job ticker re-enables the plain state on every
	//     tick so only the override sticks; D-Bus is not reachable from the
	//     initrd). See signalShowStatusOff for the musl/glibc signal-number
	//     dance.
	//  4. plymouth (if installed) re-enables status output on its own.
	//     Quit it defensively.
	_ = os.WriteFile("/proc/sys/kernel/printk", []byte("1 4 1 7\n"), 0o644)
	_ = exec.Command("systemctl", "log-target", "null").Run()
	signalShowStatusOff()
	_ = exec.Command("plymouth", "quit", "--retain-splash").Run()

	// Paint once after a short settle delay: the silencing calls above race
	// any status line systemd already has in flight, so give those a moment
	// to land before we draw over them. No periodic refresh — with the
	// ticker disabled nothing else writes to the console, and repainting
	// just flickers.
	const grace = 90 * time.Second
	footer := fmt.Sprintf("\n>>> Press any key to reboot now."+
		"\n>>> Without input the system reboots automatically in %d seconds.\n", int(grace.Seconds()))
	time.Sleep(1 * time.Second)
	paintBanner(consoles, banner+footer)

	keyPressed := make(chan struct{}, 1)
	for _, dev := range consoleDevs {
		go waitForKeyOn(dev, keyPressed)
	}

	select {
	case <-keyPressed:
		KLog.Logger.Warn().Msg("key pressed on console; rebooting")
	case <-time.After(grace):
		KLog.Logger.Warn().Msg("no key press within grace period; rebooting")
	}

	// Reboot through systemd's own service so the failed boot is visible to
	// boot-assessment (boot counting / bless-boot): a raw reboot(2) would
	// skip the shutdown machinery entirely.
	syscall.Sync()
	if cerr := exec.Command("systemctl", "start", "systemd-reboot.service").Run(); cerr != nil {
		KLog.Logger.Err(cerr).Msg("starting systemd-reboot.service failed; falling back to reboot(2)")
		if rerr := syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART); rerr != nil {
			KLog.Logger.Err(rerr).Msg("reboot syscall failed; blocking to avoid boot continuation")
		}
	}
	// Block while the reboot request is in flight so we never return into
	// the DAG and let boot proceed.
	select {}
}

// signalShowStatusOff sends SIGRTMIN+21 to PID 1, which makes systemd set
// show_status_overridden=no. The OVERRIDE part is what matters: the cylon
// ticker ("A start job is running for X (Ys / no limit)") re-enables plain
// show_status on every tick via manager_flip_auto_status(m, true, "delay"),
// so anything that only flips the non-override state (like the
// systemd.show_status kernel param at runtime) gets immediately undone. Only
// the override — set by SIGRTMIN+20/21 or the SetShowStatus D-Bus method —
// survives. D-Bus is not reachable from the initrd (no dbus-daemon; busctl
// does not speak the /run/systemd/private socket), so the signal is the only
// initrd-safe channel.
//
// The signal NUMBER is libc-dependent, because systemd computes SIGRTMIN+21
// with the libc it was built against:
//   - glibc reserves 32-33 for NPTL      → SIGRTMIN=34 → +21 = 55
//   - musl  reserves 32-34 internally    → SIGRTMIN=35 → +21 = 56
//
// Sending the wrong one is actively harmful: 55 on a musl-built systemd is
// SIGRTMIN+20, which ENABLES status messages. Detect the libc by looking for
// musl's dynamic loader.
func signalShowStatusOff() {
	sigrtmin := 34 // glibc
	if matches, _ := filepath.Glob("/lib/ld-musl-*"); len(matches) > 0 {
		sigrtmin = 35 // musl
	}
	if err := syscall.Kill(1, syscall.Signal(sigrtmin+21)); err != nil {
		KLog.Logger.Err(err).Int("signal", sigrtmin+21).Msg("could not send show-status-off signal to PID 1")
	}
}

// ConsoleDevices returns the console device paths the operator can actually
// see, derived from the console= entries on the kernel cmdline (e.g.
// console=ttyS0,115200 → /dev/ttyS0). The kernel accepts multiple console=
// stanzas and mirrors output to all of them, so we honor every one. When the
// cmdline names no console at all, fall back to the conventional trio:
// video console, first serial, and /dev/console (whatever the kernel picked
// as primary).
func ConsoleDevices() []string {
	var out []string
	for _, v := range CleanupSlice(ReadCMDLineArg("console=")) {
		// Strip options after the device name (console=ttyS0,115200n8).
		dev := strings.SplitN(v, ",", 2)[0]
		if dev == "" {
			continue
		}
		out = append(out, "/dev/"+dev)
	}
	if len(out) == 0 {
		return []string{"/dev/tty1", "/dev/ttyS0", "/dev/console"}
	}
	// Always include /dev/console too — it aliases the last console= entry
	// from the kernel's point of view, and catches setups where the device
	// node for a named console is not populated in the initrd yet.
	return UniqueSlice(append(out, "/dev/console"))
}

// openConsolesForWriting opens each path for writing. Missing / unopenable
// devices are silently skipped — headless setups without a serial line are a
// valid state, and the banner just goes to whichever consoles do exist.
// Caller is responsible for keeping the files alive; we deliberately do not
// close them because the paint loop reuses the same fds for the lifetime of
// the halt.
func openConsolesForWriting(paths ...string) []*os.File {
	out := make([]*os.File, 0, len(paths))
	for _, p := range paths {
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err == nil {
			out = append(out, f)
		}
	}
	return out
}

// paintBanner writes banner to every open console, ignoring per-fd errors so
// a briefly-hung serial does not stop us from repainting on video.
//
// Newlines are expanded to \r\n before writing: the key-listener goroutines
// put these same ttys into raw mode (term.MakeRaw clears OPOST/ONLCR), which
// stops the kernel from translating \n into carriage-return + line-feed on
// output. Without the expansion every line starts where the previous one
// ended, staircasing the whole banner across the screen.
func paintBanner(consoles []*os.File, banner string) {
	out := strings.ReplaceAll(banner, "\n", "\r\n")
	for _, f := range consoles {
		_, _ = f.WriteString(out)
	}
}

// waitForKeyOn opens dev, puts it in raw mode so we accept any single byte
// (not just Enter), and pushes to done on the first successful read. Errors
// on any of open / raw-mode / read are silent — a headless server without a
// serial line or a machine without a video console is a valid state; the halt
// syscall is the other path and does not depend on this succeeding.
func waitForKeyOn(dev string, done chan<- struct{}) {
	f, err := os.OpenFile(dev, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	fd := int(f.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fall back to cooked-mode read — user has to press Enter but the
		// reboot still works.
		var b [1]byte
		if _, rerr := f.Read(b[:]); rerr == nil {
			select {
			case done <- struct{}{}:
			default:
			}
		}
		return
	}
	defer func() { _ = term.Restore(fd, oldState) }()
	var b [1]byte
	if _, rerr := f.Read(b[:]); rerr == nil {
		select {
		case done <- struct{}{}:
		default:
		}
	}
}

// DisableImmucore identifies if we need to be disabled
// We disable if we boot from CD, netboot, squashfs recovery or have the rd.cos.disable stanza in cmdline.
func DisableImmucore() bool {
	cmdline, _ := os.ReadFile(GetHostProcCmdline())
	cmdlineS := string(cmdline)

	return strings.Contains(cmdlineS, "live:LABEL") || strings.Contains(cmdlineS, "live:CDLABEL") ||
		strings.Contains(cmdlineS, "netboot") || strings.Contains(cmdlineS, "rd.cos.disable") ||
		strings.Contains(cmdlineS, "rd.immucore.disable")
}

// RootRW tells us if the mode to mount root.
func RootRW() string {
	if len(ReadCMDLineArg("rd.cos.debugrw")) > 0 || len(ReadCMDLineArg("rd.immucore.debugrw")) > 0 {
		KLog.Logger.Warn().Msg("Mounting root as RW")
		return "rw"
	}
	return "ro"
}

// GetState returns the disk-by-label of the state partition to mount
// This is only valid for either active/passive or normal recovery.
func GetState() string {
	var label string

	err := retry.Do(
		func() error {
			r, err := state.NewRuntimeWithLogger(KLog.Logger)
			if err != nil {
				return err
			}
			switch r.BootState {
			case state.Active, state.Passive:
				label = "COS_STATE"
			case state.Recovery:
				label = "COS_RECOVERY"
			default:
				return errors.New("could not get label")
			}
			return nil
		},
		retry.Delay(1*time.Second),
		retry.Attempts(10),
		retry.DelayType(retry.FixedDelay),
		retry.OnRetry(func(n uint, _ error) {
			KLog.Logger.Debug().Uint("try", n).Msg("Cannot get state label, retrying")
		}),
	)
	if err != nil {
		KLog.Logger.Panic().Err(err).Msg("Could not get state label")
	}

	KLog.Logger.Debug().Str("what", label).Msg("Get state label")
	return filepath.Join("/dev/disk/by-label/", label)
}

func IsUKI() bool {
	cmdline, err := os.ReadFile(GetHostProcCmdline())
	if err != nil {
		KLog.Logger.Warn().Err(err).Msg("Error reading /proc/cmdline file " + err.Error())
		return false
	}

	return state.DetectUKIboot(string(cmdline))
}

// UkiSentinel returns the UKI sentinel name for the current boot. Booting
// from an installed system always means boot mode. Removable-media boots are
// install media by default — except under the in-RAM workflow, where the UKI
// is PXE/ISO-served on purpose and the system must behave like an installed
// Active one (installer cloud-init stages key off uki_install_mode and must
// NOT fire).
func UkiSentinel(bootedFromInstall, inRAM bool) string {
	if bootedFromInstall || inRAM {
		return "uki_boot_mode"
	}
	return "uki_install_mode"
}

// SkipUKIUnlock decides whether the UKI unlock step should skip unlocking
// partitions. Removable-media boots normally have nothing to unlock — but the
// in-RAM workflow boots from removable/network media by design and still owns
// encrypted OEM/persistent partitions on disk.
func SkipUKIUnlock(bootedFromInstall, inRAM bool) bool {
	return !bootedFromInstall && !inRAM
}

// BootInRAM returns true when the kernel cmdline enables the in-RAM workflow
// (kairos.ram token). Wraps kairos-sdk's DetectInRAM and honors the
// HOST_PROC_CMDLINE seam so tests can drive it. Used only for dispatch in
// main.go; every step in the DAG should read State.InRAM instead.
func BootInRAM() bool {
	cmdline, err := os.ReadFile(GetHostProcCmdline())
	if err != nil {
		KLog.Logger.Warn().Err(err).Msg("Error reading /proc/cmdline file " + err.Error())
		return false
	}
	return state.DetectInRAM(string(cmdline))
}

// CommandWithPath runs a command adding the usual PATH to environment
// Useful under UKI as there is nothing setting the PATH.
func CommandWithPath(c string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Env = os.Environ()
	pathAppend := "/usr/bin:/usr/sbin:/bin:/sbin"
	// try to extract any existing path from the environment
	for _, env := range cmd.Env {
		splitted := strings.Split(env, "=")
		if splitted[0] == "PATH" {
			pathAppend = fmt.Sprintf("%s:%s", pathAppend, splitted[1])
		}
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("PATH=%s", pathAppend))
	o, err := cmd.CombinedOutput()
	return string(o), err
}

// PrepareCommandWithPath prepares a cmd with the proper env
// For running under yip.
func PrepareCommandWithPath(c string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Env = os.Environ()
	pathAppend := constants.PathAppend
	// try to extract any existing path from the environment
	for _, env := range cmd.Env {
		splitted := strings.Split(env, "=")
		if splitted[0] == constants.PATH {
			pathAppend = fmt.Sprintf("%s:%s", pathAppend, splitted[1])
		}
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", constants.PATH, pathAppend))
	return cmd
}

// GetHostProcCmdline returns the path to /proc/cmdline
// Mainly used to override the cmdline during testing.
func GetHostProcCmdline() string {
	proc := os.Getenv("HOST_PROC_CMDLINE")
	if proc == "" {
		return "/proc/cmdline"
	}
	return proc
}

// RenderFailureSummary builds a human-readable boot failure summary.
// It is intentionally separate from the shell-exec logic so it can be unit
// tested without exec-ing anything. reason is the caller-provided context
// describing which operation failed (may be empty). logDir is the directory the
// summary points operators at for logs; pass the same dir given to
// WriteFailureSummary so the displayed path matches where the summary lands.
func RenderFailureSummary(reason, logDir string) string {
	var b strings.Builder

	reason = normalizeFailureReason(reason)

	// Read the kernel cmdline best-effort; do not fail the summary if it errors.
	cmdline, err := os.ReadFile(GetHostProcCmdline())
	cmdlineStr := strings.TrimSpace(string(cmdline))
	if err != nil || cmdlineStr == "" {
		cmdlineStr = "(unavailable)"
	}

	b.WriteString("========================================\n")
	b.WriteString("        IMMUCORE BOOT FAILED\n")
	b.WriteString("========================================\n")
	fmt.Fprintf(&b, "Reason:  %s\n", reason)
	fmt.Fprintf(&b, "Cmdline: %s\n", cmdlineStr)
	fmt.Fprintf(&b, "Logs:    %s\n", logDir)
	b.WriteString("----------------------------------------\n")
	b.WriteString("Inspect logs above, then exit this shell to retry or reboot.\n")
	b.WriteString("========================================\n")

	return b.String()
}

// normalizeFailureReason returns a human-readable reason, substituting a
// placeholder when the caller did not provide one. Shared so the structured
// log and the rendered/persisted summary stay consistent.
func normalizeFailureReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "unknown failure (no reason provided)"
	}
	return reason
}

// WriteFailureSummary writes the rendered summary to a file under dir,
// creating dir if needed. Returns the path written.
func WriteFailureSummary(dir, summary string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil { // #nosec G301 -- log dir, world readable is fine
		return "", err
	}
	// 0600: the summary embeds the kernel cmdline, which can carry secrets
	// (tokens, KMS passwords, keys). Keep it root-only so non-root readers on the
	// booted system cannot harvest them from the persisted file.
	path := filepath.Join(dir, "boot_failure.log")
	if err := os.WriteFile(path, []byte(summary), 0600); err != nil {
		return "", err
	}
	return path, nil
}

// DropToEmergencyShellWithError prints a structured failure summary to stderr,
// persists it under the log dir, and then drops to the emergency shell.
// reason describes the operation that failed.
func DropToEmergencyShellWithError(reason string) {
	reason = normalizeFailureReason(reason)
	summary := RenderFailureSummary(reason, constants.LogDir)

	// Always print to stderr so it is visible even if logging is misconfigured.
	// RenderFailureSummary already ends in a newline; use Fprint to avoid a
	// trailing blank line.
	_, _ = fmt.Fprint(os.Stderr, summary)

	// Mirror into the structured log.
	KLog.Logger.Error().Str("reason", reason).Msg("immucore boot failed, dropping to emergency shell")

	// Best-effort persist to the log dir.
	if path, err := WriteFailureSummary(constants.LogDir, summary); err != nil {
		KLog.Logger.Warn().Err(err).Msg("could not write boot failure summary to log dir")
	} else {
		KLog.Logger.Info().Str("path", path).Msg("wrote boot failure summary")
	}

	DropToEmergencyShell()
}

func DropToEmergencyShell() {
	env := os.Environ()
	// try to extract any existing path from the environment
	pathAppend := constants.PathAppend
	for _, e := range env {
		splitted := strings.Split(e, "=")
		if splitted[0] == constants.PATH {
			pathAppend = fmt.Sprintf("%s:%s", pathAppend, splitted[1])
		}
	}
	env = append(env, fmt.Sprintf("%s=%s", constants.PATH, pathAppend))
	if err := syscall.Exec("/bin/bash", []string{"/bin/bash"}, env); err != nil {
		if err := syscall.Exec("/bin/sh", []string{"/bin/sh"}, env); err != nil {
			if err := syscall.Exec("/sysroot/bin/bash", []string{"/sysroot/bin/bash"}, env); err != nil {
				if err := syscall.Exec("/sysroot/bin/sh", []string{"/sysroot/bin/sh"}, env); err != nil {
					KLog.Logger.Fatal().Msg("Could not drop to emergency shell")
				}
			}
		}
	}
}

// PCRExtend extends the given pcr with the give data.
func PCRExtend(pcr int, data []byte) error {
	t, err := transport.OpenTPM()
	if err != nil {
		return err
	}
	defer func(t transport.TPMCloser) {
		_ = t.Close()
	}(t)
	digest := sha256.Sum256(data)
	pcrHandle := tpm2.PCRExtend{
		PCRHandle: tpm2.AuthHandle{
			Handle: tpm2.TPMHandle(pcr),
			Auth:   tpm2.PasswordAuth(nil),
		},
		Digests: tpm2.TPMLDigestValues{
			Digests: []tpm2.TPMTHA{
				{
					HashAlg: tpm2.TPMAlgSHA256,
					Digest:  digest[:],
				},
			},
		},
	}

	if _, err = pcrHandle.Execute(t); err != nil {
		return err
	}

	return nil
}

// Copy copies src to dst like the cp command.
func Copy(src, dst string) error {
	if dst == src {
		return os.ErrInvalid
	}

	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	info, err := srcF.Stat()
	if err != nil {
		return err
	}

	dstF, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstF.Close()

	if _, err := io.Copy(dstF, srcF); err != nil {
		return err
	}
	return nil
}
