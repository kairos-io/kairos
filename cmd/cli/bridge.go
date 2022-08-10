package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/c3os-io/c3os/pkg/config"
	"github.com/c3os-io/c3os/pkg/utils"
	"github.com/ipfs/go-log"
	"github.com/mudler/edgevpn/api"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/edgevpn/pkg/services"
	"github.com/mudler/edgevpn/pkg/vpn"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/urfave/cli"
)

// bridge is just starting a VPN with edgevpn to the given network token.
func bridge(c *cli.Context) error {
	qrCodePath := ""
	fromQRCode := false
	var serviceUUID, sshPassword string

	if c.String("qr-code-image") != "" {
		qrCodePath = c.String("qr-code-image")
		fromQRCode = true
	}
	if c.Bool("qr-code-snapshot") {
		qrCodePath = ""
		fromQRCode = true
	}

	token := c.String("network-token")

	if fromQRCode {
		recoveryToken := qr.Reader(qrCodePath)
		data := utils.DecodeRecoveryToken(recoveryToken)
		if len(data) != 3 {
			fmt.Println("Token not decoded correctly")
			return fmt.Errorf("invalid token")
		}
		token = data[0]
		serviceUUID = data[1]
		sshPassword = data[2]
		if serviceUUID == "" || sshPassword == "" || token == "" {
			return fmt.Errorf("decoded invalid values")
		}
	}

	ctx := context.Background()

	nc := config.Network(token, c.String("address"), c.String("log-level"), "c3os0")

	lvl, err := log.LevelFromString(nc.LogLevel)
	if err != nil {
		lvl = log.LevelError
	}
	llger := logger.New(lvl)

	o, vpnOpts, err := nc.ToOpts(llger)
	if err != nil {
		llger.Fatal(err.Error())
	}

	opts := []node.Option{}

	if !fromQRCode {
		// We just connect to a VPN token
		o = append(o,
			services.Alive(
				time.Duration(20)*time.Second,
				time.Duration(10)*time.Second,
				time.Duration(10)*time.Second)...)

		if c.Bool("dhcp") {
			// Adds DHCP server
			address, _, err := net.ParseCIDR(c.String("address"))
			if err != nil {
				return err
			}
			nodeOpts, vO := vpn.DHCP(llger, 15*time.Minute, c.String("lease-dir"), address.String())
			o = append(o, nodeOpts...)
			vpnOpts = append(vpnOpts, vO...)
		}

		opts, err = vpn.Register(vpnOpts...)
		if err != nil {
			return err
		}
	} else {
		// We hook into a service
		llger.Info("Connecting to service", serviceUUID)
		llger.Info("SSH access password is", sshPassword)
		llger.Info("SSH server reachable at 127.0.0.1:2200")
		opts = append(opts, node.WithNetworkService(
			services.ConnectNetworkService(
				30*time.Second,
				serviceUUID,
				"127.0.0.1:2200",
			),
		))
		llger.Info("To connect, keep this terminal open and run in another terminal 'ssh 127.0.0.1 -p 2200' the password is ", sshPassword)
		llger.Info("Note: the connection might not be available instantly and first attempts will likely fail.")
		llger.Info("      Few attempts might be required before establishing a tunnel to the host.")
	}

	e, err := node.New(append(o, opts...)...)
	if err != nil {
		return err
	}

	go api.API(ctx, c.String("api"), 5*time.Second, 20*time.Second, e, nil, false) //nolint:errcheck

	return e.Start(ctx)
}
