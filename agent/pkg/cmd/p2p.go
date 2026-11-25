package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/agent/internal/agent"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const p2pBanner = "EXPERIMENTAL: kairos agent p2p is alpha software, opt-in, and only meant for isolated, trusted networks."

type p2pNetFlags struct {
	experimental bool
	bind         string
	port         int
	allow        []string
	uploadKiB    int
	maxPeers     int
	insecure     bool
}

func (f *p2pNetFlags) register(fl *pflag.FlagSet) {
	fl.BoolVar(&f.experimental, "experimental", false, "acknowledge that this feature is experimental (or set KAIROS_P2P_EXPERIMENTAL=1, or p2p-source.experimental: true in the cloud-config)")
	fl.StringVar(&f.bind, "bind", "", "local IP address to listen on (default: p2p-source.bind from the cloud-config, else the first non-loopback IPv4 address)")
	fl.IntVar(&f.port, "port", 0, "port to listen on (seeder default 42069)")
	fl.StringSliceVar(&f.allow, "allow-cidr", nil, "network peers must belong to, repeatable (default: the listed peers, or the local subnet)")
	fl.IntVar(&f.uploadKiB, "upload-limit", 0, "upload limit in KiB/s, 0 = the default, 65536")
	fl.IntVar(&f.maxPeers, "max-peers", 0, "max image streams served at the same time")
	fl.BoolVar(&f.insecure, "insecure", false, "DANGEROUS: allow a source without a ?pin= or ?sha256=")
}

// withConfig fills unset flags from the cloud-config p2p-source block.
func (f *p2pNetFlags) withConfig(cfg images.P2POptions) {
	f.experimental = f.experimental || cfg.Experimental
	if f.bind == "" {
		f.bind = cfg.Bind
	}
	if f.port == 0 {
		f.port = cfg.Port
	}
	if len(f.allow) == 0 {
		f.allow = cfg.Allow
	}
	if f.uploadKiB == 0 {
		f.uploadKiB = cfg.UploadLimit
	}
	if f.maxPeers == 0 {
		f.maxPeers = cfg.MaxPeers
	}
	f.insecure = f.insecure || cfg.Insecure
}

func (f *p2pNetFlags) auth(cfg images.P2POptions) (p2p.Auth, error) {
	o := images.P2POptions{Insecure: f.insecure, MaxImageSize: cfg.MaxImageSize}
	return p2p.AuthFromOptions(&o)
}

func (f *p2pNetFlags) net() (p2p.NetConfig, error) {
	if err := p2p.RequireExperimental(f.experimental); err != nil {
		return p2p.NetConfig{}, err
	}
	var allow p2p.Allowlist
	if len(f.allow) > 0 {
		var err error
		if allow, err = p2p.ParseAllowlist(f.allow); err != nil {
			return p2p.NetConfig{}, err
		}
	}
	return p2p.NetConfig{ListenHost: f.bind, ListenPort: f.port, Allow: allow, UploadBytesPerSec: f.uploadKiB * 1024, MaxPeers: f.maxPeers}, nil
}

func newP2PCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "p2p",
		Short: "EXPERIMENTAL: share and fetch the OS images with other nodes of a closed network",
		Long: p2pBanner + `

A node can serve its active, passive and recovery images (read only) to peers in
an isolated network. A node installing, upgrading or resetting pulls the image
from several peers at once, in chunks that are each verified:

  kairos agent p2p seed          # prints the source, with its pin, to use on the other nodes
  kairos agent upgrade --experimental-p2p --source 'p2p://10.0.0.2,10.0.0.3/active?pin=<pin printed by seed>'
  kairos agent upgrade --experimental-p2p --source 'p2p://active?pin=<pin>'   # peers found with mDNS

A source needs a pin: without one the download is refused (p2p-source.insecure: true
allows it, and then any host on the network can make the node install anything).
  ?pin=<hash>     the manifest of the image; what "seed" and "pin" print, use this one
  ?sha256=<hash>  the sha256 of the image (plus an optional &size=<bytes>); weaker: a
                  rogue peer can make the node waste time before the hash is checked

p2p:// sources also work in the cloud-config (install.system.source,
upgrade.system.source, reset.system.source) once p2p-source.experimental is set.

See https://kairos.io/docs/upgrade/p2p-experimental/ (experimental).`,
	}
	cmd.AddCommand(newP2PSeedCmd(), newP2PFetchCmd(), newP2PPinCmd())
	return cmd
}

func newP2PSeedCmd() *cobra.Command {
	var nf p2pNetFlags
	var stateDir, recoveryDir, cacheDir string
	var poll time.Duration
	var noAnnounce bool
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "serve the active, passive and recovery images (read only) until interrupted",
		Long: p2pBanner + `

Serves whichever of active.img, passive.img and recovery.img exist, read only,
over HTTP on one port (a manifest with chunk hashes, and byte ranges). Images are
re-checked periodically: a changed image (an upgrade) is dropped and served again
once it is stable. Run it from the unit the agent installs when the cloud-config
has p2p-source.enabled: true and p2p-source.experimental: true.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agent.LoadP2POptionsFromDirs(constants.GetUserConfigDirs())
			if err != nil {
				fmt.Fprintf(os.Stderr, "p2p: ignoring unreadable cloud-config: %v\n", err)
			}
			nf.withConfig(cfg)
			nc, err := nf.net()
			if err != nil {
				return err
			}
			if nc.ListenHost == "" {
				if nc.ListenHost, err = p2p.AutoBind(); err != nil {
					return err
				}
			}
			if len(nc.Allow) == 0 {
				if nc.Allow, err = p2p.DefaultAllowlist(nil, nc.ListenHost); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "p2p: no allowlist configured, allowing the local subnet %v\n", nc.Allow)
			}
			fmt.Fprintln(os.Stderr, p2pBanner)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			state, recovery := agent.DefaultSeedDirs()
			if stateDir != "" {
				state = stateDir
			}
			if recoveryDir != "" {
				recovery = recoveryDir
			}
			s, err := p2p.StartService(ctx, p2p.ServiceConfig{
				NetConfig: nc, StateDir: state, RecoveryDir: recovery, CacheDir: cacheDir,
				PollInterval: poll, Announce: !noAnnounce && !cfg.DisableAnnounce,
				Logf: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
			})
			if err != nil {
				return err
			}
			defer s.Close() //nolint:errcheck
			fmt.Printf("p2p seeder listening on %s:%d, serving %v\n", nc.ListenHost, s.Port(), s.Seeding())
			fmt.Println("use these sources on the other nodes (the ?pin= is the hash of the image's manifest, the strongest pin; prefer it to ?sha256=):")
			for _, u := range s.Pins(nc.ListenHost) {
				fmt.Println("  " + u)
			}
			<-ctx.Done()
			return nil
		},
	}
	nf.register(cmd.Flags())
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "dir holding cOS/active.img and cOS/passive.img")
	cmd.Flags().StringVar(&recoveryDir, "recovery-dir", "", "dir holding cOS/recovery.img or recovery.squashfs")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "/usr/local/.kairos/p2p-cache", "where computed manifests are cached (keyed by size+mtime+inode)")
	cmd.Flags().DurationVar(&poll, "poll-interval", 10*time.Second, "how often the images are re-checked")
	cmd.Flags().BoolVar(&noAnnounce, "no-announce", false, "do not announce the node with mDNS")
	return cmd
}

func newP2PFetchCmd() *cobra.Command {
	var nf p2pNetFlags
	var source, output string
	var linger time.Duration
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "download an image from the swarm into a file and verify it",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, _ := agent.LoadP2POptionsFromDirs(constants.GetUserConfigDirs())
			nf.withConfig(cfg)
			nc, err := nf.net()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, p2pBanner)
			t, err := images.ParseP2PURI(source)
			if err != nil {
				return err
			}
			auth, err := nf.auth(cfg)
			if err != nil {
				return err
			}
			fc := p2p.FetchConfig{
				NetConfig: nc, Auth: auth, Target: t, DestPath: output, Linger: linger, Serve: linger > 0,
				Logf: func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) },
			}
			last := time.Now()
			fc.OnProgress = func(done, total int64, peers int) {
				if time.Since(last) < 2*time.Second && done != total {
					return
				}
				last = time.Now()
				fmt.Fprintf(os.Stderr, "%d/%d MiB (%d%%) peers=%d\n", done>>20, total>>20, done*100/max(total, 1), peers)
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			res, err := p2p.Fetch(ctx, fc)
			if err != nil {
				return err
			}
			fmt.Printf("downloaded sha256 %s (%d bytes, %d chunks resumed), checked against: %s\n", res.SHA256, res.Size, res.Resumed, res.Trust)
			for _, p := range res.Peers {
				fmt.Printf("  served by %-15s %10d bytes  %4d chunks  %d failures\n", p.Addr, p.Bytes, p.Chunks, p.Failures)
			}
			return nil
		},
	}
	nf.register(cmd.Flags())
	cmd.Flags().StringVar(&source, "source", "", "p2p://[peer[,peer]/]active|passive|recovery?pin=<hex>")
	cmd.Flags().StringVar(&output, "output", "", "where to write the image")
	cmd.Flags().DurationVar(&linger, "linger", 0, "after the download, offer the finished image to other nodes for this long (off by default)")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newP2PPinCmd() *cobra.Command {
	var image, kind, bind string
	var port int
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "print the source, with its pin, that fetches an image of this node",
		Long: p2pBanner + `

Hashes an image file and prints the p2p:// source to use on the other nodes. Its
?pin= is the hash of the image's manifest: a node checks every manifest against
it before it downloads a single chunk of the image, so a rogue peer cannot waste
its time. "kairos agent p2p seed" prints the same for the images it serves.`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if !images.ValidP2PImage(kind) {
				return fmt.Errorf("--kind must be one of %v", images.P2PImages)
			}
			if image == "" {
				state, recovery := agent.DefaultSeedDirs()
				switch kind {
				case images.P2PImageRecovery:
					image = filepath.Join(recovery, "cOS", "recovery.img")
				default:
					image = filepath.Join(state, "cOS", kind+".img")
				}
			}
			m, err := p2p.BuildManifest(image, kind, p2p.DefaultChunkSize)
			if err != nil {
				return err
			}
			if bind == "" {
				if bind, err = p2p.AutoBind(); err != nil {
					return err
				}
			}
			fmt.Println(p2p.PinURI(bind, port, kind, m.Root()))
			return nil
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "the image file (default: the one of this node)")
	cmd.Flags().StringVar(&kind, "kind", "active", "active, passive or recovery")
	cmd.Flags().StringVar(&bind, "bind", "", "address the other nodes reach this node at (default: the only interface)")
	cmd.Flags().IntVar(&port, "port", images.DefaultP2PPort, "port of the seeder")
	return cmd
}
