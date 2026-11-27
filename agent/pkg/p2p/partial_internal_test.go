package p2p

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/miekg/dns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/time/rate"
)

const ichunk = 256 << 10

func iserver(data []byte) (*Server, string, *Manifest) {
	p := filepath.Join(GinkgoT().TempDir(), "x.img")
	Expect(os.WriteFile(p, data, 0o600)).To(Succeed())
	m, err := BuildManifest(p, "active", ichunk)
	Expect(err).ToNot(HaveOccurred())
	f, err := os.Open(p)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(f.Close)
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
	srv := newServer(l, NetConfig{Allow: allow}, nil)
	DeferCleanup(srv.Close)
	srv.setShare("active", m, f)
	return srv, l.Addr().String(), m
}

func randomData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

var _ = Describe("shares", func() {
	It("serves ranges of a complete image, and the whole of it", func() {
		data := randomData(4 * ichunk)
		_, addr, _ := iserver(data)
		get := func(rng string) (int, []byte) {
			req, _ := http.NewRequest("GET", "http://"+addr+"/active/image", nil)
			if rng != "" {
				req.Header.Set("Range", rng)
			}
			resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close() //nolint:errcheck
			b, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, b
		}
		code, b := get("bytes=262144-524287")
		Expect(code).To(Equal(206))
		Expect(b).To(Equal(data[ichunk : 2*ichunk]))
		code, b = get("")
		Expect(code).To(Equal(200))
		Expect(b).To(Equal(data))
	})

	// P2P-04: a client that stops reading loses its stream slot after the
	// write stall timeout, it does not hold it for minutes.
	It("cuts a client that does not read and frees its slot", func() {
		old := writeStallTimeout
		writeStallTimeout = time.Second
		DeferCleanup(func() { writeStallTimeout = old })
		data := randomData(48 << 20) // more than the socket buffers hold
		p := filepath.Join(GinkgoT().TempDir(), "big.img")
		Expect(os.WriteFile(p, data, 0o600)).To(Succeed())
		m, err := BuildManifest(p, "active", maxChunkSize)
		Expect(err).ToNot(HaveOccurred())
		f, _ := os.Open(p)
		DeferCleanup(f.Close)
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		srv := newServer(l, NetConfig{Allow: allow}, nil)
		DeferCleanup(srv.Close)
		srv.setShare("active", m, f)

		c, err := net.Dial("tcp", l.Addr().String())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(c.Close)
		_, _ = c.Write([]byte("GET /active/image HTTP/1.1\r\nHost: x\r\nRange: bytes=0-33554431\r\n\r\n"))
		inflight := func() int { srv.ipMu.Lock(); defer srv.ipMu.Unlock(); return len(srv.streams) }
		Eventually(inflight, 3*time.Second, 50*time.Millisecond).Should(Equal(1))
		Eventually(inflight, 6*time.Second, 100*time.Millisecond).Should(BeZero())
		Expect(len(srv.sem)).To(BeZero())
	})

	It("limits the streams one address can hold", func() {
		old := writeStallTimeout
		writeStallTimeout = 30 * time.Second
		DeferCleanup(func() { writeStallTimeout = old })
		data := randomData(48 << 20)
		p := filepath.Join(GinkgoT().TempDir(), "big.img")
		Expect(os.WriteFile(p, data, 0o600)).To(Succeed())
		m, _ := BuildManifest(p, "active", maxChunkSize)
		f, _ := os.Open(p)
		DeferCleanup(f.Close)
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		srv := newServer(l, NetConfig{Allow: allow, MaxConnsPerIP: 20}, nil)
		DeferCleanup(srv.Close)
		srv.setShare("active", m, f)
		var conns []net.Conn
		for i := 0; i < 12; i++ {
			c, err := net.Dial("tcp", l.Addr().String())
			Expect(err).ToNot(HaveOccurred())
			conns = append(conns, c)
			_, _ = c.Write([]byte("GET /active/image HTTP/1.1\r\nHost: x\r\nRange: bytes=0-33554431\r\n\r\n"))
		}
		DeferCleanup(func() {
			for _, c := range conns {
				_ = c.Close()
			}
		})
		Eventually(func() int { srv.ipMu.Lock(); defer srv.ipMu.Unlock(); return len(srv.streams["127.0.0.1"]) }, 3*time.Second).Should(Equal(maxInflightPerIP))
		Consistently(func() int { srv.ipMu.Lock(); defer srv.ipMu.Unlock(); return len(srv.streams["127.0.0.1"]) }, time.Second).Should(Equal(maxInflightPerIP))
	})
})

var _ = Describe("file descriptor limit", func() {
	DescribeTable("plans the limit and the connection cap",
		func(cur, max uint64, conns int, wantCur uint64, wantConns int) {
			gotCur, gotConns := planFDLimit(cur, max, conns)
			Expect(gotCur).To(Equal(wantCur))
			Expect(gotConns).To(Equal(wantConns))
		},
		Entry("plenty", uint64(65536), uint64(65536), 128, uint64(65536), 128),
		Entry("raises the soft limit when it can", uint64(1024), uint64(1<<20), 4096, uint64(8256), 4096),
		Entry("lowers the cap when the hard limit is small", uint64(256), uint64(256), 1000, uint64(256), 96),
		Entry("never goes below 8", uint64(70), uint64(70), 1000, uint64(70), 8),
	)
})

var _ = Describe("mDNS answers", func() {
	service := MDNSService + ".local."
	answer := func(port uint16, a net.IP) *dns.Msg {
		m := new(dns.Msg)
		m.Response = true
		m.Answer = append(m.Answer, &dns.PTR{Hdr: dns.RR_Header{Name: service, Rrtype: dns.TypePTR, Class: dns.ClassINET}, Ptr: "x." + service})
		m.Extra = append(m.Extra,
			&dns.SRV{Hdr: dns.RR_Header{Name: "x." + service, Rrtype: dns.TypeSRV, Class: dns.ClassINET}, Port: port, Target: "h.local."},
			&dns.A{Hdr: dns.RR_Header{Name: "h.local.", Rrtype: dns.TypeA, Class: dns.ClassINET}, A: a})
		return m
	}
	from := net.ParseIP("10.0.0.7")

	It("takes the address from the datagram and only the port from the announcement", func() {
		Expect(peersFromMessage(answer(42069, net.ParseIP("169.254.169.254")), from)).To(Equal([]string{"10.0.0.7:42069"}))
	})
	It("ignores questions, port 0 and other services", func() {
		q := answer(42069, from)
		q.Response = false
		Expect(peersFromMessage(q, from)).To(BeEmpty())
		Expect(peersFromMessage(answer(0, from), from)).To(BeEmpty())
		other := answer(42069, from)
		other.Extra[0].(*dns.SRV).Hdr.Name = "x._http._tcp.local."
		Expect(peersFromMessage(other, from)).To(BeEmpty())
		notQuery := answer(42069, from)
		notQuery.Opcode = dns.OpcodeNotify
		Expect(peersFromMessage(notQuery, from)).To(BeEmpty())
	})
	It("announces a random name, never the host name", func() {
		host, _ := os.Hostname()
		a, b := instanceName(), instanceName()
		Expect(a).ToNot(Equal(b))
		Expect(strings.Contains(a, host) && host != "").To(BeFalse())
		Expect(a).To(HavePrefix("kairos-p2p-"))
	})
})

var _ = Describe("bind choice", func() {
	It("refuses to guess between several addresses", func() {
		_, err := chooseBind([]string{"10.0.0.2 (eth0)", "192.168.1.9 (eth1)"})
		Expect(err).To(MatchError(ContainSubstring("eth0")))
		Expect(err).To(MatchError(ContainSubstring("p2p-source.bind")))
		ip, err := chooseBind([]string{"10.0.0.2 (eth0)"})
		Expect(err).ToNot(HaveOccurred())
		Expect(ip).To(Equal("10.0.0.2"))
		_, err = chooseBind(nil)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("free space preflight", func() {
	It("refuses an image that does not fit, before creating the file", func() {
		old := statfsFn
		statfsFn = func(_ string, st *syscall.Statfs_t) error {
			st.Bsize = 4096
			st.Bavail = (100 << 20) / 4096
			return nil
		}
		DeferCleanup(func() { statfsFn = old })
		dest := filepath.Join(GinkgoT().TempDir(), "out", "dl.img")
		err := checkSpace(dest, 200<<20)
		Expect(err).To(MatchError(ContainSubstring("not enough free space")))
		Expect(checkSpace(dest, 10<<20)).To(Succeed())
		// the reserve is kept free
		Expect(checkSpace(dest, 90<<20)).ToNot(Succeed())
	})

	It("does not create the file in a Fetch that cannot fit", func() {
		old := statfsFn
		statfsFn = func(_ string, st *syscall.Statfs_t) error { st.Bsize = 4096; st.Bavail = 10; return nil }
		DeferCleanup(func() { statfsFn = old })
		dir := GinkgoT().TempDir()
		img := filepath.Join(dir, "active.img")
		Expect(os.WriteFile(img, randomData(2*ichunk), 0o600)).To(Succeed())
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		s, err := StartService(context.Background(), ServiceConfig{NetConfig: NetConfig{ListenHost: "127.0.0.1", ListenPort: port, Allow: allow}, Paths: map[string]string{"active": img}, ChunkSize: ichunk, PollInterval: time.Hour})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(s.Close)
		dst := filepath.Join(dir, "out", "dl.img")
		_, err = Fetch(context.Background(), FetchConfig{NetConfig: NetConfig{ListenHost: "127.0.0.2", Allow: allow}, Auth: Auth{Insecure: true},
			Target: images.P2PTarget{Peers: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}, Image: "active"}, DestPath: dst})
		Expect(err).To(MatchError(ContainSubstring("not enough free space")))
		Expect(dst).ToNot(BeAnExistingFile())
	})
})

// N8: the manifest and the chunk map are counted against the upload limit.
var _ = Describe("upload limit on small responses", func() {
	It("spends the limiter on the manifest, the chunk map and the pin", func() {
		data := randomData(64 * ichunk)
		pth := filepath.Join(GinkgoT().TempDir(), "x.img")
		Expect(os.WriteFile(pth, data, 0o600)).To(Succeed())
		m, err := BuildManifest(pth, "active", ichunk)
		Expect(err).ToNot(HaveOccurred())
		f, _ := os.Open(pth)
		DeferCleanup(f.Close)
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		srv := newServer(l, NetConfig{Allow: allow, UploadBytesPerSec: 1000}, nil) // 1 KB/s: no refill to speak of
		DeferCleanup(srv.Close)
		srv.setShare("active", m, f)
		for _, path := range []string{"manifest", "have", "pin"} {
			before := srv.limiter.Tokens()
			resp, err := http.Get("http://" + l.Addr().String() + "/active/" + path)
			Expect(err).ToNot(HaveOccurred())
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			after := srv.limiter.Tokens()
			Expect(before-after).To(BeNumerically(">=", float64(len(b))*0.9), path)
		}
	})
})

// N9 residual: a big manifest is not free, even when the upload is unlimited.
var _ = Describe("small responses have a per address allowance", func() {
	It("slows an address that keeps asking for a megabyte manifest", func() {
		m := &Manifest{Version: 1, Image: "active", Size: int64(maxChunks) * ichunk, ChunkSize: ichunk, SHA256: strings.Repeat("a", 64)}
		for i := 0; i < maxChunks; i++ {
			m.Chunks = append(m.Chunks, strings.Repeat("b", 64))
		}
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		srv := newServer(l, NetConfig{Allow: allow}, nil) // no upload limit
		DeferCleanup(srv.Close)
		srv.setShare("active", m, nil)
		start := time.Now()
		var total int
		for i := 0; i < 8; i++ { // about 8.6 MB, the burst is 4 MiB at 1 MiB/s afterwards
			resp, err := http.Get("http://" + l.Addr().String() + "/active/manifest")
			Expect(err).ToNot(HaveOccurred())
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			total += len(b)
		}
		Expect(total).To(BeNumerically(">", 8<<20))
		Expect(time.Since(start)).To(BeNumerically(">=", 3*time.Second))
	})
})

// W8: the seeder has a ceiling out of the box, and the small responses of all
// the addresses together are budgeted, not only each address.
var _ = Describe("default and global limits", func() {
	It("limits the upload by default and budgets small responses globally", func() {
		pth := filepath.Join(GinkgoT().TempDir(), "x.img")
		Expect(os.WriteFile(pth, randomData(8*ichunk), 0o600)).To(Succeed())
		m, err := BuildManifest(pth, "active", ichunk)
		Expect(err).ToNot(HaveOccurred())
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		srv := newServer(l, NetConfig{Allow: allow}, nil) // UploadBytesPerSec left at 0
		DeferCleanup(srv.Close)
		srv.setShare("active", m, nil)
		Expect(float64(srv.limiter.Limit())).To(BeEquivalentTo(DefaultUploadBytesPerSec))
		// a tiny global budget: the second and third manifest have to wait for it,
		// each address has its own (large) allowance and is not the limit here
		srv.smallAll = rate.NewLimiter(1000, 1000)
		start := time.Now()
		for i := 0; i < 3; i++ {
			resp, err := http.Get("http://" + l.Addr().String() + "/active/manifest")
			Expect(err).ToNot(HaveOccurred())
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			Expect(len(b)).To(BeNumerically("<", 1000))
		}
		Expect(time.Since(start)).To(BeNumerically(">=", 600*time.Millisecond))
	})
})

// X1: per-peer speed tracking, without any timing: the rates are put in by hand.
var _ = Describe("slow peer demotion", func() {
	newFetcher := func(chunks int) (*fetcher, *peerState, *peerState) {
		m := &Manifest{Version: 1, Image: "active", Size: int64(chunks) * ichunk, ChunkSize: ichunk}
		for i := 0; i < chunks; i++ {
			m.Chunks = append(m.Chunks, strings.Repeat("a", 64))
		}
		full := NewBitmap(chunks)
		for i := 0; i < chunks; i++ {
			full.Set(i)
		}
		slow := &peerState{addr: "10.0.0.1:1", have: full}
		fast := &peerState{addr: "10.0.0.2:1", have: full}
		ft := &fetcher{m: m, done: NewBitmap(chunks), logf: func(string, ...any) {},
			flight: map[int]int{}, flightBy: map[int]map[string]bool{}, failedBy: map[int]map[string]bool{},
			cancels: map[int]map[string]context.CancelFunc{}, peers: map[string]*peerState{slow.addr: slow, fast.addr: fast}}
		return ft, slow, fast
	}
	It("demotes a peer far slower than the best one, and lets another peer take its chunks", func() {
		ft, slow, fast := newFetcher(4)
		rng := rand.New(rand.NewSource(1))
		// the slow peer holds every chunk, the fast one has nothing to do
		for i := 0; i < 4; i++ {
			ft.flight[i] = 1
			ft.flightBy[i] = map[string]bool{slow.addr: true}
		}
		slow.flights = 4
		slow.rate, slow.busy = 10<<10, slowSeconds
		fast.rate, fast.busy = 10<<20, 1
		ft.sampleSpeeds()
		Expect(slow.slow).To(BeTrue())
		Expect(fast.slow).To(BeFalse())
		got := map[int]bool{}
		for k := 0; k < 4; k++ {
			i, ok := ft.pick(fast, rng)
			Expect(ok).To(BeTrue())
			got[i] = true
		}
		Expect(got).To(HaveLen(4)) // all four taken from the slow peer's hands
	})
	It("lets a demoted peer hold one chunk, and only a few demoted peers hold any", func() {
		ft, slow, _ := newFetcher(8)
		rng := rand.New(rand.NewSource(2))
		slow.slow = true
		_, ok := ft.pick(slow, rng)
		Expect(ok).To(BeTrue())
		_, ok = ft.pick(slow, rng)
		Expect(ok).To(BeFalse()) // one at a time
		// maxSlowHolders demoted peers already hold chunks: another one gets none
		for k := 0; k < maxSlowHolders; k++ {
			p := &peerState{addr: fmt.Sprintf("10.1.0.%d:1", k), have: slow.have, slow: true, flights: 1}
			ft.peers[p.addr] = p
		}
		other := &peerState{addr: "10.2.0.1:1", have: slow.have, slow: true}
		ft.peers[other.addr] = other
		_, ok = ft.pick(other, rng)
		Expect(ok).To(BeFalse())
	})
	It("does not demote when nobody is much faster", func() {
		ft, slow, fast := newFetcher(4)
		slow.flights, fast.flights = 1, 1
		slow.busy, fast.busy = slowSeconds, slowSeconds
		slow.rate, fast.rate = 100<<10, 150<<10
		ft.sampleSpeeds()
		Expect(slow.slow).To(BeFalse())
	})
})

// The slot of an evicted stream changes hands at once, without waiting for the
// victim's handler: no timing is involved.
var _ = Describe("stream eviction", func() {
	It("hands the slot of the heaviest address to the newcomer, deterministically", func() {
		allow, _ := ParseAllowlist([]string{"127.0.0.0/8"})
		l, _ := net.Listen("tcp4", "127.0.0.1:0")
		srv := newServer(l, NetConfig{Allow: allow, MaxPeers: 4}, nil)
		DeferCleanup(srv.Close)
		var victims []net.Conn
		var peers []net.Conn
		var held []*stream
		for i := 0; i < 4; i++ { // one greedy address takes all 4 slots
			a, b := net.Pipe()
			victims, peers = append(victims, a), append(peers, b)
			st, ok := srv.acquireStream("10.0.0.1", a)
			Expect(ok).To(BeTrue())
			held = append(held, st)
		}
		a, b := net.Pipe()
		DeferCleanup(a.Close)
		DeferCleanup(b.Close)
		st, ok := srv.acquireStream("10.0.0.2", a)
		Expect(ok).To(BeTrue(), "the newcomer must get a slot at once")
		// the oldest stream of the greedy address was cut
		_, err := peers[0].Write([]byte("x"))
		Expect(err).To(HaveOccurred())
		Expect(held[0].evicted.Load()).To(BeTrue())
		Expect(len(srv.sem)).To(Equal(4)) // still four tokens in use
		// the victim's handler finishing frees nothing twice
		srv.releaseStream("10.0.0.1", held[0])
		Expect(len(srv.sem)).To(Equal(4))
		srv.releaseStream("10.0.0.1", held[1])
		Expect(len(srv.sem)).To(Equal(3))
		srv.releaseStream("10.0.0.2", st)
		Expect(len(srv.sem)).To(Equal(2))
		// an address that holds as much as the heaviest evicts nobody
		for _, h := range held[2:] {
			srv.releaseStream("10.0.0.1", h)
		}
		for i := 0; i < 4; i++ {
			c, d := net.Pipe()
			DeferCleanup(c.Close)
			DeferCleanup(d.Close)
			_, ok := srv.acquireStream(fmt.Sprintf("10.0.1.%d", i), c)
			Expect(ok).To(BeTrue())
		}
		c, d := net.Pipe()
		DeferCleanup(c.Close)
		DeferCleanup(d.Close)
		_, ok = srv.acquireStream("10.0.9.9", c)
		Expect(ok).To(BeFalse(), "nobody holds more than the newcomer: it is told to come back later")
		_ = victims
	})
})
