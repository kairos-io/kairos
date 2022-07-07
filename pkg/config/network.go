package config

import (
	"runtime"
	"time"

	"github.com/mudler/edgevpn/pkg/config"
)

func Network(token, address, loglevel, i string) *config.Config {
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
