package p2p

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
)

// MDNSService is the DNS-SD service type nodes announce.
const MDNSService = "_kairos-p2p._tcp"

func ifaceOf(ip string) (*net.Interface, error) {
	want := net.ParseIP(ip)
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for i := range ifs {
		addrs, _ := ifs[i].Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(want) {
				return &ifs[i], nil
			}
		}
	}
	return nil, fmt.Errorf("no interface owns %s", ip)
}

// Announce publishes this node on the local network with mDNS/DNS-SD so
// clients without a static peer list can find it. Same L2 segment only.
// Discovery is unauthenticated: the allowlist and the sha256 pin are what
// protect the downloader. role is "seed" or "leech".
func Announce(bindIP string, port int, role string) (stop func(), err error) {
	ip := net.ParseIP(bindIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid bind address %q", bindIP)
	}
	iface, err := ifaceOf(bindIP)
	if err != nil {
		return nil, err
	}
	instance := instanceName()
	svc, err := mdns.NewMDNSService(instance, MDNSService, "", "", port, []net.IP{ip}, []string{"role=" + role})
	if err != nil {
		return nil, err
	}
	srv, err := mdns.NewServer(&mdns.Config{Zone: svc, Iface: iface, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		return nil, err
	}
	return func() { _ = srv.Shutdown() }, nil
}

// Discover looks for announced peers for up to timeout and returns host:port
// pairs. An announcement is only trusted for its port: the address is the one
// the answer came from, it must be inside the allowlist, answers that are not
// responses are ignored, and at most limit peers are returned (never
// bindIP itself). Discovery is unauthenticated, a host on the segment can still
// announce itself: what it serves has to pass the manifest authentication.
func Discover(ctx context.Context, bindIP string, timeout time.Duration, allow Allowlist, limit int) ([]string, error) {
	iface, err := ifaceOf(bindIP)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(bindIP)})
	if err != nil {
		return nil, err
	}
	defer conn.Close() //nolint:errcheck
	pc := ipv4.NewPacketConn(conn)
	if err := pc.SetMulticastInterface(iface); err != nil {
		return nil, err
	}
	_ = pc.SetMulticastLoopback(true)

	q := new(dns.Msg)
	q.SetQuestion(MDNSService+".local.", dns.TypePTR)
	q.Question[0].Qclass |= 1 << 15 // QU: ask for a unicast reply
	buf, err := q.Pack()
	if err != nil {
		return nil, err
	}
	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	deadline := time.Now().Add(timeout)
	found := map[string]struct{}{} // one peer per address, the first port it announces
	var peers []string
	lastSend := time.Time{}
	rb := make([]byte, 65536)
	for time.Now().Before(deadline) && ctx.Err() == nil && len(peers) < limit {
		if time.Since(lastSend) > time.Second {
			if _, err := conn.WriteTo(buf, group); err != nil {
				return nil, err
			}
			lastSend = time.Now()
		}
		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, from, err := conn.ReadFromUDP(rb)
		if err != nil {
			continue
		}
		if from.IP.Equal(net.ParseIP(bindIP)) || (len(allow) > 0 && !allow.Allows(from.IP)) {
			continue
		}
		var m dns.Msg
		if m.Unpack(rb[:n]) != nil {
			continue
		}
		for _, hp := range peersFromMessage(&m, from.IP) {
			if _, ok := found[from.IP.String()]; !ok && len(peers) < limit {
				found[from.IP.String()] = struct{}{}
				peers = append(peers, hp)
			}
		}
	}
	return peers, nil
}

// peersFromMessage extracts the peers an mDNS answer announces. Only the port
// is taken from the announcement: the address is the one the datagram came
// from, whatever the records say. Questions and other opcodes are ignored.
func peersFromMessage(m *dns.Msg, from net.IP) []string {
	if !m.Response || m.Opcode != dns.OpcodeQuery {
		return nil
	}
	var out []string
	for _, rr := range append(append([]dns.RR{}, m.Answer...), m.Extra...) {
		srv, ok := rr.(*dns.SRV)
		if !ok || !strings.HasSuffix(srv.Hdr.Name, MDNSService+".local.") || srv.Port == 0 {
			continue
		}
		out = append(out, net.JoinHostPort(from.String(), strconv.Itoa(int(srv.Port))))
	}
	return out
}

// instanceName is the DNS-SD instance a node announces: random, the host name
// is not published.
func instanceName() string {
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	return fmt.Sprintf("kairos-p2p-%x", rnd)
}
