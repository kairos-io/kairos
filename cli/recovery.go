package main

import (
	"context"
	"io/ioutil"
	"runtime"
	"time"

	"github.com/c3os-io/c3os/cli/utils"
	"github.com/ipfs/go-log"
	"github.com/urfave/cli"

	machine "github.com/c3os-io/c3os/cli/machine"
	"github.com/mudler/edgevpn/pkg/config"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/edgevpn/pkg/services"
	"github.com/mudler/edgevpn/pkg/vpn"
	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/pterm/pterm"
)

func startRecoveryVPN(ctx context.Context, token, address, loglevel string) error {

	nc := config.Config{
		NetworkToken:   token,
		Address:        address,
		Libp2pLogLevel: "error",
		FrameTimeout:   "30s",

		LogLevel:      loglevel,
		LowProfile:    true,
		VPNLowProfile: true,
		Interface:     "c3osrecovery0",
		Concurrency:   runtime.NumCPU(),
		PacketMTU:     1420,
		InterfaceMTU:  1200,
		Ledger: config.Ledger{
			AnnounceInterval: time.Duration(30) * time.Second,
			SyncInterval:     time.Duration(30) * time.Second,
		},
		NAT: config.NAT{
			Service:           false,
			Map:               true,
			RateLimit:         true,
			RateLimitGlobal:   10,
			RateLimitPeer:     10,
			RateLimitInterval: time.Duration(10) * time.Second,
		},
		Discovery: config.Discovery{
			DHT:      true,
			MDNS:     true,
			Interval: time.Duration(120) * time.Second,
		},
		Connection: config.Connection{
			AutoRelay:      true,
			MaxConnections: 100,
			MaxStreams:     100,
			HolePunch:      true,
		},
	}

	lvl, err := log.LevelFromString(loglevel)
	if err != nil {
		lvl = log.LevelError
	}
	llger := logger.New(lvl)
	o, vpnOpts, err := nc.ToOpts(llger)
	if err != nil {
		llger.Fatal(err.Error())
	}

	o = append(o,
		services.Alive(
			time.Duration(20)*time.Second,
			time.Duration(10)*time.Second,
			time.Duration(10)*time.Second)...)

	opts, err := vpn.Register(vpnOpts...)
	if err != nil {
		return err
	}

	e, err := node.New(append(o, opts...)...)
	if err != nil {
		return err
	}

	return e.Start(ctx)
}

func recovery(c *cli.Context) error {

	utils.PrintBanner(banner)
	tk := nodepair.GenerateToken()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startRecoveryVPN(ctx, tk, "10.1.20.20/24", "fatal")
	pterm.DefaultBox.WithTitle("Recovery").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		`Welcome to c3os recovery mode!
p2p device recovery mode is starting.
A QR code with a generated network token will be displayed below that can be used to connect 
over with "c3os bridge --qr-code-image /path/to/image.jpg" from another machine.
The machine will have the 10.1.20.20 ip in the VPN.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).`)

	pterm.Info.Println("Press any key to abort recovery. To restart the process run 'c3os recovery'.")

	pterm.Info.Println("Starting in 5 seconds...")
	pterm.Print("\n\n") // Add two new lines as spacer.

	time.Sleep(5 * time.Second)

	qr.Print(tk)

	if d, err := ioutil.ReadFile("/run/recovery_pass"); err == nil {
		pterm.Info.Println("SSH username: c3os password: " + string(d))
	}

	// Wait for user input and go back to shell
	utils.Prompt("")
	cancel()
	// give tty1 back
	svc, err := machine.Getty(1)
	if err == nil {
		svc.Start()
	}

	return nil
}
