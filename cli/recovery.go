package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/c3os-io/c3os/cli/utils"
	"github.com/ipfs/go-log"
	"github.com/urfave/cli"

	machine "github.com/c3os-io/c3os/cli/machine"
	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/mudler/edgevpn/pkg/config"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/edgevpn/pkg/services"
	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/pterm/pterm"
)

const recoveryAddr = "127.0.0.1:2222"

func networkConfig(token, address, loglevel, i string) *config.Config {
	return &config.Config{
		NetworkToken:   token,
		Address:        address,
		Libp2pLogLevel: "error",
		FrameTimeout:   "30s",
		BootstrapIface: true,
		LogLevel:       loglevel,
		LowProfile:     true,
		VPNLowProfile:  true,
		Interface:      i,
		Concurrency:    runtime.NumCPU(),
		PacketMTU:      1420,
		InterfaceMTU:   1200,
		Ledger: config.Ledger{
			AnnounceInterval: time.Duration(30) * time.Second,
			SyncInterval:     time.Duration(30) * time.Second,
		},
		NAT: config.NAT{
			Service:           true,
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
			RelayV1: true,

			AutoRelay:      true,
			MaxConnections: 100,
			MaxStreams:     100,
			HolePunch:      true,
		},
	}
}

func startRecoveryService(ctx context.Context, token, name, address, loglevel string) error {

	nc := networkConfig(token, "", loglevel, "c3osrecovery0")

	lvl, err := log.LevelFromString(loglevel)
	if err != nil {
		lvl = log.LevelError
	}
	llger := logger.New(lvl)

	o, _, err := nc.ToOpts(llger)
	if err != nil {
		llger.Fatal(err.Error())
	}

	o = append(o,
		services.Alive(
			time.Duration(20)*time.Second,
			time.Duration(10)*time.Second,
			time.Duration(10)*time.Second)...)

	// opts, err := vpn.Register(vpnOpts...)
	// if err != nil {
	// 	return err
	// }
	o = append(o, services.RegisterService(llger, time.Duration(5*time.Second), name, address)...)

	e, err := node.New(o...)
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

	serviceUUID := utils.RandStringRunes(10)
	generatedPassword := utils.RandStringRunes(7)

	startRecoveryService(ctx, tk, serviceUUID, recoveryAddr, "fatal")

	pterm.DefaultBox.WithTitle("Recovery").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		`Welcome to c3os recovery mode!
p2p device recovery mode is starting.
A QR code with a generated network token will be displayed below that can be used to connect 
over with "c3os bridge --qr-code-image /path/to/image.jpg" from another machine, 
further instruction will appear on the bridge CLI to connect over via SSH.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).`)

	pterm.Info.Println("Press any key to abort recovery. To restart the process run 'c3os recovery'.")

	time.Sleep(5 * time.Second)

	pterm.Info.Printfln(
		"starting ssh server on '%s', password: '%s' service: '%s' ", recoveryAddr, generatedPassword, serviceUUID)

	qr.Print(encodeRecoveryToken(tk, serviceUUID, generatedPassword))

	go sshServer(recoveryAddr, generatedPassword)

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

const sep = "_CREDENTIALS_"

func encodeRecoveryToken(data ...string) string {
	return strings.Join(data, sep)
}

func decodeRecoveryToken(recoverytoken string) []string {
	return strings.Split(recoverytoken, sep)
}

func sshServer(listenAdddr, password string) {
	ssh.Handle(func(s ssh.Session) {
		cmd := exec.Command("bash")
		ptyReq, winCh, isPty := s.Pty()
		if isPty {
			cmd.Env = append(cmd.Env, fmt.Sprintf("TERM=%s", ptyReq.Term))
			f, err := pty.Start(cmd)
			if err != nil {
				pterm.Warning.Println("Failed reserving tty")
			}
			go func() {
				for win := range winCh {
					setWinsize(f, win.Width, win.Height)
				}
			}()
			go func() {
				io.Copy(f, s) // stdin
			}()
			io.Copy(s, f) // stdout
			cmd.Wait()
		} else {
			io.WriteString(s, "No PTY requested.\n")
			s.Exit(1)
		}
	})

	pterm.Info.Println(ssh.ListenAndServe(listenAdddr, nil, ssh.PasswordAuth(func(ctx ssh.Context, pass string) bool {
		return pass == password
	}),
	))
}
