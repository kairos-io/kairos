package main

import (
	"context"
	"net"
	"runtime"
	"time"

	"github.com/ipfs/go-log"
	"github.com/mudler/edgevpn/api"
	"github.com/mudler/edgevpn/pkg/config"
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
		token = qr.Reader(qrCodePath)
	}

	ctx := context.Background()
	nc := config.Config{
		NetworkToken:   token,
		Address:        c.String("address"),
		Libp2pLogLevel: "error",
		FrameTimeout:   "30s",
		LogLevel:       "debug",
		LowProfile:     true,
		VPNLowProfile:  true,
		Interface:      "c3os0",
		Concurrency:    runtime.NumCPU(),
		PacketMTU:      1420,
		InterfaceMTU:   1200,
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

	lvl, err := log.LevelFromString(nc.LogLevel)
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

	opts, err := vpn.Register(vpnOpts...)
	if err != nil {
		return err
	}

	e, err := node.New(append(o, opts...)...)
	if err != nil {
		return err
	}

	go api.API(ctx, c.String("api"), 5*time.Second, 20*time.Second, e, nil, false)

	return e.Start(ctx)
}
