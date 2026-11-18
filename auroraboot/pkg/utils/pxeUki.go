package utils

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	nbConstants "github.com/kairos-io/netboot/constants"
	"github.com/kairos-io/netboot/dhcp4"
)

// Very simple as this is http booting
// We setup a dhcp server that only returns a ProxyDHCP offer
// with the HTTP server address and the filename to boot from.
// This is used by the EFI client to find the HTTP server and the file to boot from.
// The HTTP server serves the ISO file for all requests.
// A proxyDHCP server is a DHCP server that only responds to DHCP requests with a ProxyDHCP offer,
// which is a DHCP offer that contains the address of an HTTP server and the filename to boot from.
// No real DHCP configuration is done, as the client will not use the DHCP server to configure its network settings.
// This requires an existing DHCP server on the network to provide the necessary network configuration to the client.

// ServeUkiPXE starts a HTTP+DHCP server that serves the UKI ISO file over HTTP.
func ServeUkiPXE(isoFile string, log logger.KairosLogger) error {
	log.Logger.Info().Msgf("Start pixiecore for UKI")
	dhcp, err := dhcp4.NewSnooperConn(fmt.Sprintf("%s:%d", "0.0.0.0", nbConstants.PortDHCP))
	if err != nil {
		return err
	}

	// 2 buffer slots, one for each goroutine
	errs := make(chan error, 2)

	internal.Log.Logger.Debug().Str("subsystem", "Init").Msgf("Starting Pixiecore goroutines")

	// DHCP helps clients searching for HTTP Boot servers find the right one with the right filename
	// HTTP is used for plain HTTP requests serving the ISO file.

	go func() { errs <- serveDHCP(dhcp, log) }()
	go func() { errs <- serveHTTP(isoFile, log) }()
	err = <-errs
	dhcp.Close()

	return err
}

// serveDHCP listens for DHCP requests and responds with a ProxyDHCP offer so http booting can jump to the HTTP server.
func serveDHCP(conn *dhcp4.Conn, log logger.KairosLogger) error {
	log.Logger.Info().Str("subsystem", "DHCP").Msgf("Listening for requests on :%d", nbConstants.PortDHCP)
	for {
		pkt, intf, err := conn.RecvDHCP()
		if err != nil {
			return fmt.Errorf("receiving DHCP packet: %s", err)
		}
		if intf == nil {
			return fmt.Errorf("received DHCP packet with no interface information (this is a violation of dhcp4.Conn's contract, please file a bug)")
		}

		if !strings.Contains(strings.ToLower(string(pkt.Options[dhcp4.OptVendorIdentifier])), "httpclient") {
			log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Ignoring packet from %s on %s: not a HTTPClient request", pkt.HardwareAddr, intf.Name)
			continue
		}

		log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Received DHCP packet from %s on %s", pkt.HardwareAddr, intf.Name)

		if err = isBootDHCP(pkt); err != nil {
			log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Ignoring packet from %s: %s", pkt.HardwareAddr, err)
			continue
		}

		// Machine should be booted.
		serverIP, err := interfaceIP(intf)
		if err != nil {
			log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Want to boot %s on %s, but couldn't get a source address: %s", pkt.HardwareAddr, intf.Name, err)
			continue
		}

		resp, err := offerDhcpPackage(pkt, serverIP, log)
		if err != nil {
			log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Failed to construct ProxyDHCP offer for %s: %s", pkt.HardwareAddr, err)
			continue
		}

		if err = conn.SendDHCP(resp, intf); err != nil {
			log.Logger.Debug().Str("subsystem", "DHCP").Msgf("Failed to send ProxyDHCP offer for %s: %s", pkt.HardwareAddr, err)
			continue
		}
	}
}

// isBootDHCP checks if the given DHCP packet is a PXE boot request.
func isBootDHCP(pkt *dhcp4.Packet) error {
	if pkt.Type != dhcp4.MsgDiscover {
		return fmt.Errorf("packet is %s, not %s", pkt.Type, dhcp4.MsgDiscover)
	}

	if pkt.Options[dhcp4.OptClientSystem] == nil {
		return errors.New("not a PXE boot request (missing option 93)")
	}

	return nil
}

// interfaceIP returns a usable unicast IP address from the given network interface.
func interfaceIP(intf *net.Interface) (net.IP, error) {
	addrs, err := intf.Addrs()
	if err != nil {
		return nil, err
	}

	// Try to find an IPv4 address to use, in the following order:
	// global unicast (includes rfc1918), link-local unicast,
	// loopback.
	fs := []func(net.IP) bool{
		net.IP.IsGlobalUnicast,
		net.IP.IsLinkLocalUnicast,
		net.IP.IsLoopback,
	}
	for _, f := range fs {
		for _, a := range addrs {
			ipaddr, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipaddr.IP.To4()
			if ip == nil {
				continue
			}
			if f(ip) {
				return ip, nil
			}
		}
	}

	return nil, errors.New("no usable unicast address configured on interface")
}

// offerDhcpPackage constructs a ProxyDHCP offer packet with the given parameters.
func offerDhcpPackage(pkt *dhcp4.Packet, serverIP net.IP, log logger.KairosLogger) (resp *dhcp4.Packet, err error) {
	resp = &dhcp4.Packet{
		Type:           dhcp4.MsgOffer,
		TransactionID:  pkt.TransactionID,
		HardwareAddr:   pkt.HardwareAddr,
		ClientAddr:     pkt.ClientAddr,
		RelayAddr:      pkt.RelayAddr,
		ServerAddr:     serverIP,
		BootServerName: serverIP.String(),
		BootFilename:   fmt.Sprintf("http://%s/kairos.iso", serverIP),
		Options: dhcp4.Options{
			dhcp4.OptServerIdentifier: serverIP,
			dhcp4.OptVendorIdentifier: []byte("HTTPClient"),
		},
	}
	if pkt.Options[dhcp4.OptUidGuidClientIdentifier] != nil {
		resp.Options[dhcp4.OptUidGuidClientIdentifier] = pkt.Options[dhcp4.OptUidGuidClientIdentifier]
	}

	log.Logger.Debug().Str("BootServerName", resp.BootServerName).Str("BootFilename", resp.BootFilename).Msgf("Sending ProxyDHCP offer to %s on %s", pkt.HardwareAddr, serverIP)
	return resp, nil
}

// isoServeMux returns a private mux that serves isoFile for all requests. It is
// deliberately a fresh http.NewServeMux per call rather than the global
// http.DefaultServeMux: registering "/" on the global mux panics on a second
// call ("multiple registrations for /") and leaks the handler across servers.
func isoServeMux(isoFile string, log logger.KairosLogger) *http.ServeMux {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Logger.Info().Str("method", r.Method).Str("url", r.URL.Path).Msg("Serving kairos.iso for all requests")
		http.ServeFile(w, r, isoFile)
	})
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	return mux
}

// serveHTTP starts an HTTP server that serves the specified ISO file for all requests.
func serveHTTP(isoFile string, log logger.KairosLogger) error {
	srv := &http.Server{Addr: ":80", Handler: isoServeMux(isoFile, log)}
	internal.WarnUnauthenticatedServe(":80", "UKI PXE ISO server")
	log.Logger.Info().Str("subsystem", "HTTP").Msg("Listening for requests on :80")
	err := srv.ListenAndServe()
	if err != nil {
		log.Logger.Error().Err(err).Msg("Error starting HTTP server")
	}
	return err
}
