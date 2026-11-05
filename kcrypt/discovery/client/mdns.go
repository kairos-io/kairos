package client

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

const (
	MDNSServiceType = "_kcrypt._tcp"
	MDNSTimeout     = 15 * time.Second
)

// Variables so tests can swap them.
var (
	mdnsTimeout = MDNSTimeout
	// mdnsQueryWindow is how long one mdns query listens before the query is
	// sent again.
	mdnsQueryWindow = time.Second
	mdnsQuery       = mdns.QueryContext
)

// queryMDNS will make an mdns query on local network to find a kcrypt challenger server
// instance. If none is found, the original URL is returned and no additional headers.
// If a response is received, the IP address and port from the response will be returned
// and an additional "Host" header pointing to the original host.
func queryMDNS(originalURL string, logger loggerpkg.KairosLogger) (string, map[string]string, error) {
	additionalHeaders := map[string]string{}

	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		return originalURL, additionalHeaders, fmt.Errorf("parsing the original host: %w", err)
	}

	host := parsedURL.Host
	// Extract hostname without port for .local check
	hostname := host
	if hostPort := strings.Split(host, ":"); len(hostPort) > 1 {
		hostname = hostPort[0]
	}
	if !strings.HasSuffix(hostname, ".local") { // sanity check
		return "", additionalHeaders, fmt.Errorf("domain should end in \".local\" when using mdns")
	}

	mdnsIP, mdnsPort := discoverMDNSServer(hostname, logger)
	if mdnsIP == "" { // no reply
		logger.Debugf("no reply from mdns after %v timeout", MDNSTimeout)
		// For .local domains, mDNS is required - don't fall back to DNS
		return "", additionalHeaders, fmt.Errorf("mDNS resolution failed: no mDNS server found for %s (searched for service type %s)", hostname, MDNSServiceType)
	}

	// Set Host header to original hostname (for virtual hosting)
	additionalHeaders["Host"] = host

	// Build new URL with discovered IP address
	// Use the port from mDNS response if available, otherwise use original port
	// An IPv6 address needs its brackets in a URL host, which JoinHostPort adds.
	newHost := mdnsIP
	if strings.Contains(mdnsIP, ":") {
		newHost = "[" + mdnsIP + "]"
	}
	if mdnsPort != "" {
		newHost = net.JoinHostPort(mdnsIP, mdnsPort)
	} else if parsedURL.Port() != "" {
		// If mDNS didn't provide a port, keep the original port
		newHost = net.JoinHostPort(mdnsIP, parsedURL.Port())
	}

	// Reconstruct URL with new host
	parsedURL.Host = newHost
	newURL := parsedURL.String()

	logger.Debugf("mDNS resolved %s to %s", originalURL, newURL)
	return newURL, additionalHeaders, nil
}

// discoverMDNSServer performs an mDNS query to discover any running kcrypt challenger
// servers on the same network that matches the given hostname.
// If a response is received, the IP address (v4 or v6) and the Port from the response are returned.
func discoverMDNSServer(hostname string, logger loggerpkg.KairosLogger) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), mdnsTimeout)

	// Entries are only ever sent by the query goroutine below, which has
	// exited before this function returns, so the channel is never closed.
	entriesCh := make(chan *mdns.ServiceEntry, 4)

	logger.Debugf("Will now wait for some mdns server to respond")
	// A single mdns query only listens for mdnsQueryWindow and sends one
	// packet, so a challenger (or network link) that comes up later would
	// never be seen. Keep re-querying until the whole timeout is used.
	done := make(chan struct{})
	go func() {
		defer close(done)
		warned := false
		for ctx.Err() == nil {
			params := mdns.DefaultParams(MDNSServiceType)
			params.Entries = entriesCh
			params.Timeout = mdnsQueryWindow
			params.Logger = log.New(io.Discard, "", 0)
			if err := mdnsQuery(ctx, params); err != nil && ctx.Err() == nil {
				// No query could be sent (no usable interface yet, say).
				// Retry after a pause, the link may still be coming up.
				if !warned {
					logger.Warnf("mdns query failed, will keep retrying until timeout: %v", err)
					warned = true
				}
				select {
				case <-ctx.Done():
				case <-time.After(mdnsQueryWindow):
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-done
	}()

	expectedHost := hostname + "." // FQDN
	// Wait until a matching server is found or we reach a timeout
	for {
		select {
		case entry := <-entriesCh:
			logger.Debugf("mdns response received")
			if entry.Host == expectedHost {
				logger.Debugf("%s matches %s", entry.Host, expectedHost)
				addr := entryAddress(entry)
				if addr == "" {
					logger.Debugf("%s answered without an address, ignoring it", entry.Host)
					continue
				}
				return addr, strconv.Itoa(entry.Port)
			}
			logger.Debugf("%s didn't match %s", entry.Host, expectedHost)
		case <-ctx.Done():
			logger.Debugf("timed out waiting for mdns")
			return "", ""
		}
	}
}

// entryAddress returns the address to reach an mDNS answer on: the IPv4 one
// when there is one, else the IPv6 one (with its zone for a link-local
// address). It returns "" when the answer carries no address at all.
func entryAddress(entry *mdns.ServiceEntry) string {
	if entry.AddrV4 != nil {
		return entry.AddrV4.String()
	}
	if entry.AddrV6IPAddr != nil && entry.AddrV6IPAddr.IP != nil {
		return entry.AddrV6IPAddr.String()
	}
	if entry.AddrV6 != nil {
		return entry.AddrV6.String()
	}
	return ""
}
