package utils

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A UKI initrd is the whole rootfs cpio'd into the .initrd section of the EFI
// binary, with /init pointing at immucore. There is no dracut hook chain in
// there, so none of the rd.* network options do anything and no interface is
// ever configured before the switch to the real root. That is a problem only
// for the steps that run before it, and today there is exactly one: unlocking
// an encrypted partition against a remote KMS.
//
// The rootfs does ship systemd-networkd, and it is the only DHCP client in the
// image (there is no dhclient, no dhcpcd, and busybox is built without the
// applet), so we run it directly. It does not need systemd as PID 1: it talks
// to the kernel over netlink and reports through files under /run/systemd/netif.

// networkdBinaries lists the paths systemd-networkd can live at, most likely
// first.
var networkdBinaries = []string{
	"/usr/lib/systemd/systemd-networkd",
	"/lib/systemd/systemd-networkd",
	"/usr/libexec/systemd/systemd-networkd",
}

const (
	// networkdRuntimeUnitDir is the runtime drop-in directory networkd reads
	// configuration from. It ranks below /etc, so anything the image ships
	// keeps winning.
	networkdRuntimeUnitDir = "/run/systemd/network"
	// networkdFallbackUnit is deliberately numbered last: networkd applies the
	// first matching file in lexical order, so a real configuration in
	// /etc/systemd/network takes precedence over this one.
	networkdFallbackUnit = "99-immucore-dhcp.network"
	// networkdStateFile is where networkd publishes the manager state,
	// including the DNS servers it learned.
	networkdStateFile = "/run/systemd/netif/state"
	// machineIDFile holds the machine id networkd derives its default DHCP
	// identifiers from. A Kairos image ships without one on purpose: it is
	// created on first boot of the real root, per node.
	machineIDFile = "/etc/machine-id"
)

// networkConfigDirs are the directories that count as somebody having
// configured the network already. /usr/lib/systemd/network is deliberately
// absent: see hasNetworkConfig.
var networkConfigDirs = []string{"/etc/systemd/network", "/run/systemd/network"}

// fallbackNetworkUnit is a DHCP-on-every-wired-interface configuration, used
// only when the image does not configure the interface itself.
const fallbackNetworkUnit = `# Written by immucore for early-boot networking in UKI mode.
# Numbered 99 on purpose: any configuration shipped in /etc/systemd/network
# matches first and wins.
[Match]
Type=ether

[Network]
DHCP=yes
IPv6AcceptRA=yes

[DHCPv4]
UseDNS=yes
UseDomains=yes
# Both identifiers below are derived from the interface's hardware address.
# networkd's defaults are derived from /etc/machine-id instead, and a Kairos
# image ships without one: the id is created on first boot of the real root,
# so in the initrd there is none. networkd starts either way, but it cannot
# configure a DHCP client whose identifier it cannot compute, so the link
# stays down and the only thing the boot shows is immucore's own timeout.
ClientIdentifier=mac

[DHCPv6]
DUIDType=link-layer
`

// FallbackNetworkUnit returns the networkd configuration immucore falls back
// to when the image ships none.
func FallbackNetworkUnit() string {
	return fallbackNetworkUnit
}

// findNetworkd returns the path to the systemd-networkd binary, or an empty
// string when the image does not ship one.
func findNetworkd(root string) string {
	for _, rel := range networkdBinaries {
		p := filepath.Join(root, rel)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// hasNetworkConfig reports whether this image configures the interface
// itself, in which case immucore must not add a configuration of its own.
//
// Only /etc and /run count. /usr/lib/systemd/network is systemd's own vendor
// drop-in directory and is never empty: hadron-trusted v0.5.2 ships 18 files
// there, all of them for containers, tunnels and ad-hoc wifi (80-container-*,
// 80-6rd-tunnel, 80-wifi-adhoc, 99-default.link). None of them match a plain
// wired NIC, so counting them as configuration would suppress the fallback and
// leave the interface down.
func hasNetworkConfig(root string, dirs ...string) bool {
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch filepath.Ext(e.Name()) {
			case ".network", ".netdev", ".link":
				return true
			}
		}
	}
	return false
}

// ParseNetworkdDNS pulls the DNS server list out of the contents of
// /run/systemd/netif/state. networkd writes them space separated on a single
// DNS= line, and an address can carry an interface or port suffix which the
// resolver does not understand, so those are trimmed.
func ParseNetworkdDNS(state string) []string {
	var servers []string
	seen := map[string]bool{}
	for _, line := range strings.Split(state, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "DNS=")
		if !ok {
			continue
		}
		for _, field := range strings.Fields(value) {
			// networkd can write "1.1.1.1#cloudflare-dns.com" or
			// "fe80::1%eth0"; resolv.conf wants the bare address.
			addr := field
			if i := strings.IndexAny(addr, "#%"); i >= 0 {
				addr = addr[:i]
			}
			if net.ParseIP(addr) == nil || seen[addr] {
				continue
			}
			seen[addr] = true
			servers = append(servers, addr)
		}
	}
	return servers
}

// RenderResolvConf renders a minimal resolv.conf for the given servers.
func RenderResolvConf(servers []string) string {
	var b strings.Builder
	b.WriteString("# Written by immucore for early-boot networking in UKI mode.\n")
	for _, s := range servers {
		fmt.Fprintf(&b, "nameserver %s\n", s)
	}
	return b.String()
}

// ResolvConfTarget returns the file that has to be written for
// /etc/resolv.conf to resolve to it. Images that enable systemd-resolved ship
// /etc/resolv.conf as a symlink into /run/systemd/resolve/, and resolved is not
// running in the initrd, so writing the link path itself would only replace the
// symlink; the target is what has to exist. Returns the link path when the link
// escapes root, so we never write outside the initrd.
func ResolvConfTarget(root string) (string, error) {
	link := filepath.Join(root, "/etc/resolv.conf")
	target, err := os.Readlink(link)
	if err != nil {
		// Not a symlink (or missing): write the file itself.
		return link, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	} else {
		target = filepath.Join(root, target)
	}
	resolved := filepath.Clean(target)
	prefix := filepath.Clean(root)
	if prefix != "/" && !strings.HasPrefix(resolved, prefix+string(os.PathSeparator)) {
		return link, fmt.Errorf("resolv.conf symlink %q escapes %q", target, root)
	}
	return resolved, nil
}

// hasRoutableAddress reports whether any interface other than loopback carries
// a global unicast address. This is the condition the KMS call actually needs,
// and reading it from the kernel avoids depending on networkd's own readiness
// reporting, which changes shape between systemd versions.
var hasRoutableAddress = func() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			// IsGlobalUnicast already rules out 169.254.0.0/16 and fe80::/10,
			// which is what we want: a self-assigned address cannot reach a KMS.
			if ipnet.IP.IsGlobalUnicast() {
				return true
			}
		}
	}
	return false
}

// hasMachineID reports whether the initrd carries a usable machine id.
// An empty file counts as absent, which is how systemd itself reads it.
func hasMachineID(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, machineIDFile))
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(raw)) > 0
}

// maxNetworkdLogLine caps how much output is held while waiting for a
// newline, so a binary that writes none cannot grow the initrd's memory.
const maxNetworkdLogLine = 8 << 10

// networkdLogWriter forwards systemd-networkd's own output into the immucore
// log, one message per line. It exists because the first version of this step
// discarded that output: when networkd could not configure a DHCP client the
// boot showed nothing but immucore's 30s timeout, and the cause had to be
// found by rebuilding the image.
type networkdLogWriter struct {
	mu   sync.Mutex
	rest []byte
}

func (w *networkdLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rest = append(w.rest, p...)
	for {
		i := bytes.IndexByte(w.rest, '\n')
		if i < 0 {
			break
		}
		logNetworkdLine(w.rest[:i])
		w.rest = w.rest[i+1:]
	}
	if len(w.rest) > maxNetworkdLogLine {
		logNetworkdLine(w.rest)
		w.rest = nil
	}
	return len(p), nil
}

// Flush logs a trailing line that never got its newline, which is what a
// binary exiting mid-message leaves behind.
func (w *networkdLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	logNetworkdLine(w.rest)
	w.rest = nil
}

func logNetworkdLine(line []byte) {
	msg := strings.TrimRight(string(line), "\r\n\t ")
	if msg == "" {
		return
	}
	KLog.Logger.Info().Str("bin", "systemd-networkd").Msg(msg)
}

// networkdCommand builds the command that runs systemd-networkd, with both of
// its streams pointed at the returned writer. There is no journal in the
// initrd, so networkd's own diagnostics only exist if immucore carries them,
// and both streams share one writer to keep the order networkd wrote them in.
func networkdCommand(bin string) (*exec.Cmd, *networkdLogWriter) {
	out := &networkdLogWriter{}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/usr/sbin:/bin:/sbin")
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd, out
}

// SetupInitrdNetwork brings up networking inside the UKI initrd by running
// systemd-networkd directly and waiting for an address, then publishing the
// DNS servers it learned to resolv.conf.
//
// It never fails the boot: every problem is logged and reported, and the caller
// runs the unlock anyway so the failure the user sees is the real one (the KMS
// call) rather than a step that ran too early.
func SetupInitrdNetwork(ctx context.Context, timeout time.Duration) error {
	if hasRoutableAddress() {
		KLog.Logger.Info().Msg("Network is already up, not starting systemd-networkd")
		return nil
	}

	networkd := findNetworkd("/")
	if networkd == "" {
		return fmt.Errorf("no systemd-networkd in the image, cannot configure early-boot networking")
	}

	if hasNetworkConfig("/", networkConfigDirs...) {
		KLog.Logger.Info().Msg("Image ships networkd configuration, using it as is")
		if !hasMachineID("/") {
			// The fallback unit works around the missing id with
			// hardware-address identifiers. A unit the image ships gets no
			// such treatment, so say so rather than letting it look like a
			// plain DHCP timeout.
			KLog.Logger.Warn().Str("file", machineIDFile).
				Msg("No machine id in the initrd: networkd's default DHCP identifiers are derived from it, so the image's own units may get no address. Set ClientIdentifier=mac and DUIDType=link-layer in them")
		}
	} else {
		if err := os.MkdirAll(networkdRuntimeUnitDir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", networkdRuntimeUnitDir, err)
		}
		unit := filepath.Join(networkdRuntimeUnitDir, networkdFallbackUnit)
		if err := os.WriteFile(unit, []byte(fallbackNetworkUnit), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", unit, err)
		}
		KLog.Logger.Info().Str("unit", unit).Msg("Wrote fallback DHCP configuration")
	}

	cmd, out := networkdCommand(networkd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", networkd, err)
	}
	KLog.Logger.Info().Str("bin", networkd).Int("pid", cmd.Process.Pid).Msg("Started systemd-networkd")
	// immucore is PID 1 here, so the child has to be reaped or it stays a
	// zombie for the rest of the initrd.
	go func() {
		err := cmd.Wait()
		// Wait has drained both streams by now, so anything still buffered is
		// a last message with no newline.
		out.Flush()
		if err != nil {
			KLog.Logger.Warn().Err(err).Msg("systemd-networkd exited")
		}
	}()

	if err := waitForAddress(ctx, timeout); err != nil {
		return err
	}

	writeResolvConf()
	return nil
}

// waitForAddress polls until an interface carries a global unicast address, or
// the timeout expires.
func waitForAddress(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if hasRoutableAddress() {
			KLog.Logger.Info().Msg("Early-boot network is up")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("no interface got an address within %s", timeout)
			}
		}
	}
}

// writeResolvConf publishes the DNS servers networkd learned. A missing
// resolver only breaks a KMS address given as a hostname, so this is reported
// and not fatal.
func writeResolvConf() {
	raw, err := os.ReadFile(networkdStateFile)
	if err != nil {
		KLog.Logger.Warn().Err(err).Str("file", networkdStateFile).Msg("Cannot read networkd state, leaving resolv.conf alone")
		return
	}
	servers := ParseNetworkdDNS(string(raw))
	if len(servers) == 0 {
		KLog.Logger.Warn().Msg("networkd reported no DNS servers, leaving resolv.conf alone")
		return
	}
	target, err := ResolvConfTarget("/")
	if err != nil {
		KLog.Logger.Warn().Err(err).Msg("Cannot resolve resolv.conf target")
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		KLog.Logger.Warn().Err(err).Str("file", target).Msg("Cannot create resolv.conf directory")
		return
	}
	if err := os.WriteFile(target, []byte(RenderResolvConf(servers)), 0644); err != nil {
		KLog.Logger.Warn().Err(err).Str("file", target).Msg("Cannot write resolv.conf")
		return
	}
	KLog.Logger.Info().Str("file", target).Strs("servers", servers).Msg("Wrote resolv.conf")
}
