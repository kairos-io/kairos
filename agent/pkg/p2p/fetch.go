package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

const (
	// workersPerPeer chunks are requested from one peer at the same time.
	workersPerPeer = 4
	// maxBadChunks a peer may serve before it is dropped for good.
	maxBadChunks = 2
	// maxConsecutiveFailures network errors drop a peer for peerCooldown.
	maxConsecutiveFailures = 5
	peerCooldown           = 10 * time.Second
	haveRefresh            = 2 * time.Second
	stateSuffix            = ".p2p-state"
	noPeersTimeout         = 30 * time.Second
	// maxPeers bounds the peers one download talks to, however many are
	// listed or announced.
	maxPeers = 16
	// maxPinnedPeers is the same bound for a source with a manifest pin: there a
	// rogue peer costs one small manifest, so more of them can be looked at, and
	// the honest ones are not crowded out of the first 16.
	maxPinnedPeers = 64
	// maxWasteFactor bounds the bytes that may go into manifests that fail (the
	// download stalls, is too slow, or ends with a wrong image) to this many
	// times the image size, before the fetch gives up.
	maxWasteFactor = 4
	// minThroughput is the speed a peer must reach for a chunk, bytes per second.
	minThroughput = 128 << 10
	// minGroupSpeed is the average speed, bytes per second, a download from one
	// manifest must keep once its grace period is over, or it is dropped.
	minGroupSpeed = 64 << 10
	// etaSpeed is the speed the "expected duration" of a download is measured
	// against: an image is expected to arrive in size/etaSpeed, at least 5 minutes.
	etaSpeed = 1 << 20
	// freeSpaceReserve is left free on the file system after the download.
	freeSpaceReserve = 64 << 20
)

// groupGrace is the time a download from one manifest gets before its average
// speed is held against it.
var groupGrace = 60 * time.Second

// etaGrace is how long a download runs before its projected duration is judged.
// manifestTimeout is how long one peer gets to answer for its manifest.
var manifestTimeout = 5 * time.Second

var etaGrace = 15 * time.Second

// etaFloor is the least time an image is expected to take, however small.
var etaFloor = 5 * time.Minute

// chunkTimeout scales with the chunk: a peer slower than minThroughput loses it
// to another peer instead of holding the tail of the download.
func chunkTimeout(l int64) time.Duration {
	return 10*time.Second + time.Duration(l/minThroughput)*time.Second
}

// FetchConfig configures a download. The fetch as a whole is bounded by
// Auth.Timeout (6 hours by default) and by ctx, a download from one manifest that
// makes no progress for Auth.StallTimeout is dropped, and so is one that is
// slower than 64 KiB/s on average after a minute: the next manifest is tried.
type FetchConfig struct {
	NetConfig
	Auth
	// Target says which image to fetch and from which peers (none = mDNS).
	Target images.P2PTarget
	// DestPath is where the image is written (mode 0600). A partial file plus
	// DestPath+".p2p-state" let a rerun continue where this one stopped. The
	// content is only valid if Fetch returns nil.
	DestPath string
	// Serve offers the finished image to other nodes (during Linger) and
	// announces the node with mDNS. It only happens once the whole image was
	// verified: chunks that were only checked against a manifest a peer
	// supplied are never handed on while downloading.
	Serve bool
	// Linger keeps serving the finished image for this long.
	Linger time.Duration
	// DiscoveryTimeout bounds mDNS discovery (default 5s).
	DiscoveryTimeout time.Duration
	// MetadataTimeout bounds getting the manifests from the peers (default 30s).
	MetadataTimeout time.Duration
	// OnProgress is called about once per second.
	OnProgress func(done, total int64, peers int)
	// Logf receives log lines (warnings included).
	Logf func(string, ...any)
}

// PeerStat tells what a given peer contributed.
type PeerStat struct {
	Addr     string
	Bytes    int64
	Chunks   int
	Failures int
	// Dropped is set for a peer that served bad data or a different image.
	Dropped bool
}

// Result is what Fetch reports.
type Result struct {
	Size   int64
	SHA256 string
	// Trust says what the image was checked against: "image sha256 pin" or
	// "none (insecure mode)".
	Trust string
	// Resumed is the number of chunks that were already on disk.
	Resumed int
	Peers   []PeerStat
}

type verdict struct {
	level int
	how   string
}

// Trust levels of a manifest.
const (
	trustNone     = iota // nothing vouches for it (insecure mode only)
	trustImage           // the image hash is pinned: the finished image is checked, the chunk list is not
	trustManifest        // the manifest root is pinned: every chunk hash is authentic before a chunk is fetched
)

// evaluate decides if a manifest from a peer is acceptable: it must be valid,
// of the right image, not larger than allowed, and match every pin the source
// has (the root of the manifest, the image hash, the size).
func (a Auth) evaluate(t images.P2PTarget, m *Manifest) (verdict, error) {
	if err := m.Validate(); err != nil {
		return verdict{}, err
	}
	if m.Image != t.Image {
		return verdict{}, fmt.Errorf("it is the manifest of %q, not %q", m.Image, t.Image)
	}
	max := a.MaxImageSize
	if max == 0 {
		max = DefaultMaxImageSize
	}
	if m.Size > max {
		return verdict{}, fmt.Errorf("the image is %d bytes, more than the %d accepted (p2p-source.max-image-size)", m.Size, max)
	}
	if t.Size > 0 && m.Size != t.Size {
		return verdict{}, fmt.Errorf("it serves an image of %d bytes, the source pins %d", m.Size, t.Size)
	}
	if t.SHA256 != "" && m.SHA256 != t.SHA256 {
		return verdict{}, fmt.Errorf("it serves an image with sha256 %s, the source pins %s", m.SHA256, t.SHA256)
	}
	if t.Pin != "" {
		if r := m.Root(); r != t.Pin {
			return verdict{}, fmt.Errorf("its manifest root is %s, the source pins %s", r, t.Pin)
		}
		return verdict{trustManifest, "manifest pin"}, nil
	}
	if t.SHA256 != "" {
		return verdict{trustImage, "image sha256 pin"}, nil
	}
	if !a.Insecure {
		return verdict{}, fmt.Errorf("the source has no pin and p2p-source.insecure is not set")
	}
	return verdict{trustNone, "none (insecure mode)"}, nil
}

// prepared holds what a fetch or a probe needs before talking to any peer.
type prepared struct {
	net   NetConfig
	peers []string
	hc    *http.Client
	logf  func(string, ...any)
}

func prepare(ctx context.Context, n NetConfig, t images.P2PTarget, discoveryTimeout time.Duration, logf func(string, ...any)) (*prepared, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if n.ListenHost == "" {
		b, err := AutoBind()
		if err != nil {
			return nil, err
		}
		n.ListenHost = b
		logf("p2p: binding to %s", b)
	}
	if len(n.Allow) == 0 {
		a, err := DefaultAllowlist(t.Peers, n.ListenHost)
		if err != nil {
			return nil, err
		}
		n.Allow = a
		if t.Discover() {
			logf("p2p: allowing the local subnet %v", a)
		}
	}
	if err := n.validate(); err != nil {
		return nil, err
	}
	peers := t.Peers
	if t.Discover() {
		to := discoveryTimeout
		if to == 0 {
			to = 5 * time.Second
		}
		logf("p2p: discovering peers with mDNS (%s)", to)
		found, err := Discover(ctx, n.ListenHost, to, n.Allow, peerCap(t))
		if err != nil {
			return nil, fmt.Errorf("mDNS discovery failed: %w", err)
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no p2p peers found with mDNS on %s (same L2 segment only): list them instead, e.g. p2p://10.0.0.2/%s", n.ListenHost, t.Image)
		}
		peers = found
		logf("p2p: discovered %v", found)
	}
	if c := peerCap(t); len(peers) > c {
		logf("p2p: %d peers given, using the first %d", len(peers), c)
		peers = peers[:c]
	}
	d := &net.Dialer{Timeout: 5 * time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(n.ListenHost)}}
	hc := &http.Client{
		Transport: &http.Transport{DialContext: d.DialContext, MaxIdleConnsPerHost: workersPerPeer, IdleConnTimeout: 30 * time.Second, Proxy: nil},
		// A peer must not be able to send this node anywhere else, whatever
		// the allowlist says: redirects are answered, never followed.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &prepared{net: n, peers: peers, hc: hc, logf: logf}, nil
}

// allowed resolves a peer address and checks it against the allowlist.
func (p *prepared) allowed(peer string) (string, error) {
	host, port, err := net.SplitHostPort(peer)
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ips, lerr := net.LookupIP(host)
		if lerr != nil || len(ips) == 0 {
			return "", fmt.Errorf("cannot resolve %s", host)
		}
		for _, c := range ips {
			if c.To4() != nil {
				ip = c
				break
			}
		}
	}
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("%s has no IPv4 address", host)
	}
	if !p.net.Allow.Allows(ip) {
		return "", fmt.Errorf("%s is not in the allowlist", ip)
	}
	if ip.Equal(net.ParseIP(p.net.ListenHost)) {
		return "", fmt.Errorf("that is this node")
	}
	return net.JoinHostPort(ip.String(), port), nil
}

// countingReader adds what it reads to a counter (nil: none).
type countingReader struct {
	r  io.Reader
	rx []*atomic.Int64
}

func (c countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	for _, rx := range c.rx {
		if rx != nil {
			rx.Add(int64(n))
		}
	}
	return n, err
}

// get fetches a small body, or a chunk. rx, when given, counts every byte that
// arrives, so a slow or a stalled peer can be told from a busy link.
func (p *prepared) get(ctx context.Context, addr, path, rangeHdr string, limit int64, rx ...*atomic.Int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, resp.StatusCode, fmt.Errorf("redirect to %q not followed", resp.Header.Get("Location"))
	}
	b, err := io.ReadAll(io.LimitReader(countingReader{resp.Body, rx}, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("response larger than %d bytes", limit)
	}
	return b, resp.StatusCode, nil
}

// group is a set of peers that serve the same, acceptable manifest.
const (
	// manifestParallel is how many peers are asked for their manifest at a time.
	manifestParallel = 16
)

type group struct {
	// fallback: its address had already given another manifest, tried last
	fallback bool
	m        *Manifest
	id       string
	v        verdict
	peers    []string
}

// manifestsFrom asks every peer (in parallel, a few at a time) for its manifest
// and groups the acceptable ones by manifest id. The unacceptable ones are
// skipped with a reason: they cannot poison the download of the others.
func (p *prepared) manifestsFrom(ctx context.Context, t images.P2PTarget, auth Auth) ([]*group, error) {
	type result struct {
		peer string
		m    *Manifest
		v    verdict
		err  error
	}
	results := make([]result, len(p.peers))
	// 16 peers are asked at a time, 5 s each: 64 peers that accept and then say
	// nothing take 20 s, inside the metadata budget (30 s). No peer is skipped
	// because others answered first: a rogue can copy the real manifest, so the
	// first answers are not necessarily the honest ones.
	sem := make(chan struct{}, manifestParallel)
	var wg sync.WaitGroup
	for i, peer := range p.peers {
		wg.Add(1)
		go func(i int, peer string) {
			defer wg.Done()
			r := result{peer: peer}
			defer func() { results[i] = r }()
			sem <- struct{}{}
			defer func() { <-sem }()
			addr, err := p.allowed(peer)
			if err != nil {
				r.err = err
				return
			}
			r.peer = addr
			rctx, cancel := context.WithTimeout(ctx, manifestTimeout)
			defer cancel()
			b, status, err := p.get(rctx, addr, "/"+t.Image+"/manifest", "", maxManifestBytes)
			if err != nil || status != http.StatusOK {
				r.err = fmt.Errorf("status %d, %v", status, err)
				return
			}
			var m Manifest
			if err := json.Unmarshal(b, &m); err != nil {
				r.err = fmt.Errorf("bad manifest: %v", err)
				return
			}
			if r.v, r.err = auth.evaluate(t, &m); r.err == nil {
				r.m = &m
			}
		}(i, peer)
	}
	wg.Wait()
	byID := map[string]*group{}
	var order, extra []*group
	var errs []string
	// One peer per address: a host that announces many ports, or many manifests,
	// is still one voice. The first manifest an address gives is the one that
	// counts; its other manifests are only kept as fallbacks, tried after every
	// other address was tried (a rogue port listed first must not hide the honest
	// manifest of the same address).
	ipUsed := map[string]bool{}
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.peer, r.err))
			p.logf("p2p: ignoring the manifest of %s: %v", r.peer, r.err)
			continue
		}
		ip := hostOf(r.peer)
		id := r.m.ID()
		if ipUsed[ip] {
			if byID[id] != nil {
				continue // the same manifest again: no new voice
			}
			p.logf("p2p: %s gave a second, different manifest: kept as a fallback", r.peer)
			var fb *group
			for _, e := range extra {
				if e.id == id {
					fb = e
				}
			}
			if fb == nil {
				fb = &group{m: r.m, id: id, v: r.v, fallback: true}
				extra = append(extra, fb)
			}
			fb.peers = append(fb.peers, r.peer)
			continue
		}
		ipUsed[ip] = true
		g := byID[id]
		if g == nil {
			g = &group{m: r.m, id: id, v: r.v}
			byID[id] = g
			order = append(order, g)
		}
		g.peers = append(g.peers, r.peer)
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("no peer served an acceptable %s manifest: %s", t.Image, strings.Join(errs, "; "))
	}
	// The manifest that more different addresses serve goes first (a rogue
	// host is one address), ties keep the order the peers were listed in: the
	// nodes the user named come before the ones found with mDNS.
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].v.level != order[j].v.level {
			return order[i].v.level > order[j].v.level
		}
		return len(order[i].peers) > len(order[j].peers)
	})
	for _, e := range extra {
		if byID[e.id] == nil {
			order = append(order, e)
		}
	}
	return order, nil
}

// agreedGroup returns the index of the group whose size the most different
// addresses agree on, the largest size on a tie.
func agreedGroup(gs []*group) int {
	votes := map[int64]int{}
	for _, g := range gs {
		votes[g.m.Size] += len(g.peers)
	}
	best := 0
	for i, g := range gs {
		b := gs[best]
		if votes[g.m.Size] > votes[b.m.Size] || votes[g.m.Size] == votes[b.m.Size] && g.m.Size > b.m.Size {
			best = i
		}
	}
	return best
}

// Probe returns the manifest of an image without downloading it, for the size
// the partitions are planned with. The size comes from the source when it is
// pinned there, else it is the one the most different addresses agree on (ties:
// the order the peers were listed in). No single peer decides it, and the
// download checks later that the real image fits.
func Probe(ctx context.Context, n NetConfig, a Auth, t images.P2PTarget, logf func(string, ...any)) (*Manifest, error) {
	if err := a.CheckTarget(t); err != nil {
		return nil, err
	}
	p, err := prepare(ctx, n, t, 0, logf)
	if err != nil {
		return nil, err
	}
	gs, err := p.manifestsFrom(ctx, t, a)
	if err != nil {
		return nil, err
	}
	// With the manifest pinned there is one manifest, and with the size pinned
	// one size: nobody is voting.
	if t.Pin != "" || t.Size > 0 {
		m := *gs[0].m
		return &m, nil
	}
	m := *gs[agreedGroup(gs)].m
	return &m, nil
}

type peerState struct {
	addr string
	mu   sync.Mutex
	have *Bitmap
	// stats
	bytes, chunks int64
	failures      int
	consec        int
	bad           int
	droppedUntil  time.Time
	dropped       bool // permanently
	haveAt        time.Time
	noImage       bool

	// speed tracking (rx is atomic, the rest is guarded by the fetcher's mu)
	rx      atomic.Int64 // bytes received from this peer, chunks that fail included
	lastRx  int64
	rate    float64 // bytes/s while it was busy, smoothed
	busy    int     // seconds it was seen busy
	flights int     // chunks it holds right now
	slow    bool    // far slower than the best peer: demoted
}

type fetcher struct {
	p         *prepared
	g         *group
	m         *Manifest
	id        string
	kind      string
	f         *os.File
	done      *Bitmap
	logf      func(string, ...any)
	cfg       FetchConfig
	mu        sync.Mutex
	flight    map[int]int
	flightBy  map[int]map[string]bool
	failedBy  map[int]map[string]bool
	cancels   map[int]map[string]context.CancelFunc
	peers     map[string]*peerState
	sinceSave int
	lastSave  time.Time
	fatal     chan error
	rx        atomic.Int64 // bytes received from the peers, chunks that fail included
}

func (ft *fetcher) saveState(force bool) {
	ft.mu.Lock()
	ft.sinceSave++
	if !force && ft.sinceSave < 16 && time.Since(ft.lastSave) < 2*time.Second {
		ft.mu.Unlock()
		return
	}
	ft.sinceSave = 0
	ft.lastSave = time.Now()
	ft.mu.Unlock()
	_ = ft.f.Sync() // data first: a bit in the state must never get ahead of the bytes
	st := resumeState{ID: ft.id, Size: ft.m.Size, ChunkSize: ft.m.ChunkSize, Done: ft.done.Encode()}
	b, _ := json.Marshal(st)
	tmp := ft.cfg.DestPath + stateSuffix + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, ft.cfg.DestPath+stateSuffix)
	}
}

type resumeState struct {
	ID        string `json:"id"`
	Size      int64  `json:"size"`
	ChunkSize int64  `json:"chunkSize"`
	Done      string `json:"done"`
}

// loadResume returns the number of chunks that are on disk and verified. A
// state that belongs to another manifest, or a bit whose bytes no longer match,
// is discarded.
// stateIsOurs tells whether the state file next to the image is of this
// manifest root (or there is none).
func (ft *fetcher) stateIsOurs() bool {
	b, err := os.ReadFile(ft.cfg.DestPath + stateSuffix)
	if err != nil {
		return true
	}
	var st resumeState
	return json.Unmarshal(b, &st) == nil && st.ID == ft.id
}

func removePartial(dest string) {
	_ = os.Remove(dest)
	_ = os.Remove(dest + stateSuffix)
}

func (ft *fetcher) loadResume() int {
	b, err := os.ReadFile(ft.cfg.DestPath + stateSuffix)
	if err != nil {
		return 0
	}
	var st resumeState
	if json.Unmarshal(b, &st) != nil || st.ID != ft.id || st.Size != ft.m.Size || st.ChunkSize != ft.m.ChunkSize {
		return 0
	}
	bm, err := DecodeBitmap(st.Done, ft.m.NumChunks())
	if err != nil {
		return 0
	}
	buf := make([]byte, ft.m.ChunkSize)
	n := 0
	for i := 0; i < ft.m.NumChunks(); i++ {
		if !bm.Has(i) {
			continue
		}
		l := ft.m.ChunkLen(i)
		if _, err := ft.f.ReadAt(buf[:l], int64(i)*ft.m.ChunkSize); err != nil || chunkHash(buf[:l]) != ft.m.Chunks[i] {
			continue
		}
		ft.done.Set(i)
		n++
	}
	return n
}

func chunkHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// errBadImage means the downloaded image does not match its manifest or pin:
// the next manifest is worth a try, a rerun of this one is not.
var errBadImage = errors.New("downloaded image does not match")

// errTransient marks a failure a rerun can resume.
type errTransient struct{ err error }

func (e errTransient) Error() string { return e.err.Error() }
func (e errTransient) Unwrap() error { return e.err }

// allocatedBytes returns what a file already occupies on disk.
func allocatedBytes(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return sys.Blocks * 512
	}
	return st.Size()
}

// statfsFn is a variable so tests can pretend the disk is small.
var statfsFn = syscall.Statfs

// checkSpace refuses a download that cannot fit, before anything is allocated.
func checkSpace(dest string, size int64) error {
	var st syscall.Statfs_t
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := statfsFn(dir, &st); err != nil {
		return nil // cannot tell: the write errors will
	}
	avail := int64(st.Bavail) * int64(st.Bsize)
	need := size - allocatedBytes(dest)
	if need < 0 {
		need = 0
	}
	if avail < need+freeSpaceReserve {
		return fmt.Errorf("not enough free space in %s: the image needs %d MiB, %d MiB are free (and %d MiB are kept in reserve)", dir, need>>20, avail>>20, freeSpaceReserve>>20)
	}
	return nil
}

// Fetch downloads the image from the swarm, verifies it and returns which peer
// served what. The manifests of the peers are first checked against the pin. Every chunk is then checked against the
// manifest and a bad or failed one is retried from another peer. At the end the
// sha256 of the whole image is checked.
func Fetch(ctx context.Context, cfg FetchConfig) (*Result, error) {
	logf := cfg.Logf
	if logf == nil {
		logf = func(string, ...any) {}
		cfg.Logf = logf
	}
	t := cfg.Target
	if err := cfg.CheckTarget(t); err != nil {
		return nil, err
	}
	if t.Pin == "" && t.SHA256 != "" {
		logf("p2p: note: this source only has a ?sha256= pin, a few rogue peers can keep it from getting the image; use the ?pin= source that \"kairos agent p2p seed\" prints")
	}
	if cfg.Insecure && t.SHA256 == "" && t.Pin == "" {
		logf("p2p: WARNING insecure mode: the source has no sha256 pin, any host that can reach this node could make it install a different image")
	}
	p, err := prepare(ctx, cfg.NetConfig, t, cfg.DiscoveryTimeout, logf)
	if err != nil {
		return nil, err
	}
	cfg.NetConfig = p.net
	mt := cfg.MetadataTimeout
	if mt == 0 {
		mt = 30 * time.Second
	}
	mctx, cancel := context.WithTimeout(ctx, mt)
	groups, err := p.manifestsFrom(mctx, t, cfg.Auth)
	cancel()
	if err != nil {
		return nil, err
	}
	// Every way a manifest can fail (no space, no progress, too slow, a wrong
	// image) drops that manifest and tries the next one; only the caller's
	// cancellation and the overall deadline end the fetch early, and so does the
	// bound on the bytes wasted in manifests that failed.
	to := cfg.Timeout
	if to == 0 {
		to = DefaultFetchTimeout
	}
	fctx, fcancel := context.WithTimeout(ctx, to)
	defer fcancel()
	ref := refSize(t, groups)
	bound := time.Duration(ref/etaSpeed) * time.Second
	if bound < etaFloor {
		bound = etaFloor
	}
	var lastErr error
	var wasted int64
	var slow []slowGroup
	// try downloads from one manifest. eta is the projected duration beyond which
	// a group is judged too slow (0: no judgement, the last resort).
	try := func(g *group, eta time.Duration) (*Result, error, bool) {
		logf("p2p: using the %s manifest %s (%d bytes in %d chunks, image checked against: %s), from %d peer(s)",
			t.Image, g.id, g.m.Size, g.m.NumChunks(), g.v.how, len(g.peers))
		res, got, err := fetchGroup(fctx, cfg, p, g, eta)
		if err == nil {
			return res, nil, true
		}
		var tr errTransient
		if errors.As(err, &tr) {
			return nil, errTransient{fetchDeadline(ctx, to, tr.err)}, true
		}
		wasted += got
		logf("p2p: %v: dropping that manifest (%d MiB wasted so far), trying the next one if there is one", err, wasted>>20)
		var sl errTooLong
		if errors.As(err, &sl) {
			slow = append(slow, slowGroup{g: g, rate: sl.rate})
		}
		lastErr = err
		return nil, err, false
	}
	for _, g := range groups {
		if err := fctx.Err(); err != nil {
			return nil, errTransient{fetchDeadline(ctx, to, err)}
		}
		res, err, done := try(g, bound)
		if res != nil {
			return res, nil
		}
		if done { // the caller gave up or the deadline passed
			return nil, err
		}
		// The bound is on what failed manifests cost, measured against the size the
		// source pins or the addresses agree on: never a manifest's own claim.
		if wasted > maxWasteFactor*ref {
			removePartial(cfg.DestPath)
			return nil, fmt.Errorf("gave up after wasting %d MiB on manifests that failed (more than %d times the image): %w", wasted>>20, maxWasteFactor, err)
		}
	}
	// Last resort: the manifests that were dropped only for the look of their
	// projected duration (a slow link is not a rogue), fastest first, without that
	// judgement. The stall, minimum speed and overall deadline still apply.
	sort.SliceStable(slow, func(i, j int) bool { return slow[i].rate > slow[j].rate })
	for _, sg := range slow {
		if err := fctx.Err(); err != nil {
			return nil, errTransient{fetchDeadline(ctx, to, err)}
		}
		logf("p2p: nothing else worked, trying the slow manifest %s again without the duration limit", sg.g.id)
		res, err, done := try(sg.g, 0)
		if res != nil {
			return res, nil
		}
		if done {
			return nil, err
		}
	}
	removePartial(cfg.DestPath)
	return nil, lastErr
}

// slowGroup is a manifest that was dropped for its projected duration.
type slowGroup struct {
	g    *group
	rate float64
}

// errTooLong says the projected duration of a download is beyond the bound.
type errTooLong struct {
	rate     float64
	projects time.Duration
	bound    time.Duration
}

func (e errTooLong) Error() string {
	return fmt.Sprintf("at %d KiB/s the download would take %s, more than the %s expected", int(e.rate)>>10, e.projects.Round(time.Minute), e.bound.Round(time.Minute))
}

// refSize is the size the cost of failed manifests is measured against: the
// size in the source if it has one, else the size most addresses agree on. It
// is never the size one manifest claims for itself.
func refSize(t images.P2PTarget, groups []*group) int64 {
	if t.Size > 0 {
		return t.Size
	}
	return groups[agreedGroup(groups)].m.Size
}

// fetchDeadline names what ended the fetch: the caller, or the overall deadline.
func fetchDeadline(parent context.Context, to time.Duration, err error) error {
	if parent.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("the download took more than %s (p2p-source.timeout): %w", to, err)
	}
	return err
}

// fetchGroup downloads the image from the peers of one manifest. wasted is the
// number of bytes it fetched, whatever came of them.
//
// nolint:gocyclo // one linear download pipeline (space check, resume, workers, verify, finish) where every step can fail and each failure picks its own cleanup; splitting it would scatter the cleanup rules.
func fetchGroup(ctx context.Context, cfg FetchConfig, p *prepared, g *group, etaBound time.Duration) (res *Result, wasted int64, err error) {
	logf := cfg.Logf
	t := cfg.Target
	m := g.m
	if err := checkSpace(cfg.DestPath, m.Size); err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(cfg.DestPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close() //nolint:errcheck
	// Nothing a failed download leaves behind may stay unless a rerun can
	// resume it (only the caller giving up or the overall deadline): a
	// mismatching, stalled or abandoned image is removed.
	// The one exception is a drop for the projected duration: the verified
	// chunks stay, the same manifest is retried at the end and continues there
	// (Fetch removes them if it gives up, a different manifest removes them).
	defer func() {
		var tr errTransient
		var tl errTooLong
		if err != nil && !errors.As(err, &tr) && !errors.As(err, &tl) {
			removePartial(cfg.DestPath)
		}
	}()
	if err := f.Truncate(m.Size); err != nil {
		return nil, 0, err
	}
	ft := &fetcher{p: p, g: g, m: m, id: g.id, kind: t.Image, f: f, done: NewBitmap(m.NumChunks()), logf: logf, cfg: cfg,
		flight: map[int]int{}, flightBy: map[int]map[string]bool{}, failedBy: map[int]map[string]bool{}, cancels: map[int]map[string]context.CancelFunc{},
		peers: map[string]*peerState{}, fatal: make(chan error, 1)}
	if !ft.stateIsOurs() {
		// what is on disk belongs to another manifest: none of it is kept
		if err := f.Truncate(0); err != nil {
			return nil, 0, err
		}
		_ = os.Remove(cfg.DestPath + stateSuffix)
		if err := f.Truncate(m.Size); err != nil {
			return nil, 0, err
		}
	}
	resumed := ft.loadResume()
	if resumed > 0 {
		logf("p2p: resuming, %d of %d chunks are already on disk and verified", resumed, m.NumChunks())
	}
	defer func() { wasted = ft.rx.Load() }()

	var srv *Server
	stopAnn := func() {}
	defer func() {
		stopAnn()
		if srv != nil {
			_ = srv.Close()
		}
	}()

	wctx, wcancel := context.WithCancel(ctx)
	defer wcancel()
	var wg sync.WaitGroup
	ipSeen := map[string]bool{}
	addPeers := func(list []string) {
		for _, peer := range list {
			addr, err := p.allowed(peer)
			if err != nil {
				continue
			}
			ft.mu.Lock()
			// one peer per address, however many ports it announces
			if ipSeen[hostOf(addr)] || len(ft.peers) >= peerCap(t) {
				ft.mu.Unlock()
				continue
			}
			ipSeen[hostOf(addr)] = true
			ps := &peerState{addr: addr}
			ft.peers[addr] = ps
			ft.mu.Unlock()
			for w := 0; w < workersPerPeer; w++ {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					ft.worker(wctx, ps, w)
				}(w)
			}
		}
	}
	addPeers(g.peers)
	if t.Discover() {
		wg.Add(1)
		go func() { // peers that start later (other nodes downloading) join the swarm
			defer wg.Done()
			for {
				select {
				case <-wctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				if found, err := Discover(wctx, cfg.ListenHost, 2*time.Second, cfg.Allow, peerCap(t)); err == nil {
					addPeers(found)
				}
			}
		}()
	}

	stall := cfg.StallTimeout
	if stall == 0 {
		stall = DefaultStallTimeout
	}
	start := time.Now()
	lastProgress, lastRx := start, ft.rx.Load()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	lastPeer := time.Now()
	ticks := 0
	for ft.done.Count() < m.NumChunks() {
		select {
		case <-ctx.Done():
			ft.saveState(true)
			return nil, 0, errTransient{ctx.Err()}
		case err := <-ft.fatal:
			ft.saveState(true)
			return nil, 0, err
		case <-tick.C:
			if ticks++; ticks%10 != 0 {
				continue
			}
			ft.sampleSpeeds()
			live := ft.liveCount()
			if cfg.OnProgress != nil {
				cfg.OnProgress(ft.bytesDone(), m.Size, live)
			}
			now := time.Now()
			if r := ft.rx.Load(); r > lastRx {
				lastRx, lastProgress = r, now
			}
			if live > 0 {
				lastPeer = now
			} else if now.Sub(lastPeer) > noPeersTimeout {
				return nil, 0, fmt.Errorf("no usable peer left for %s, %d of %d chunks are on disk", noPeersTimeout, ft.done.Count(), m.NumChunks())
			}
			if now.Sub(lastProgress) > stall {
				return nil, 0, fmt.Errorf("no data arrived for %s, %d of %d chunks are on disk", stall.Round(time.Second), ft.done.Count(), m.NumChunks())
			}
			el := now.Sub(start)
			if el > groupGrace {
				if rate := float64(ft.rx.Load()) / el.Seconds(); rate < minGroupSpeed {
					return nil, 0, fmt.Errorf("the download is too slow (%d KiB/s, at least %d KiB/s are needed)", int(rate)>>10, minGroupSpeed>>10)
				}
			}
			if etaBound > 0 && el > etaGrace {
				if rx := ft.rx.Load(); rx > 0 {
					rate := float64(rx) / el.Seconds()
					remaining := float64(m.Size - ft.bytesDone())
					if proj := el + time.Duration(remaining/rate*float64(time.Second)); proj > etaBound {
						ft.saveState(true)
						return nil, 0, errTooLong{rate: rate, projects: proj, bound: etaBound}
					}
				}
			}
		}
	}
	wcancel()
	wg.Wait()
	if cfg.OnProgress != nil {
		cfg.OnProgress(m.Size, m.Size, ft.liveCount())
	}
	ft.saveState(true)
	if err := f.Sync(); err != nil {
		return nil, 0, err
	}

	res = &Result{Size: m.Size, Trust: g.v.how, Resumed: resumed}
	if res.SHA256, err = SHA256File(cfg.DestPath); err != nil {
		return nil, 0, err
	}
	if res.SHA256 != m.SHA256 || (t.SHA256 != "" && res.SHA256 != t.SHA256) {
		return nil, 0, fmt.Errorf("%w: downloaded sha256 %s, manifest %s, pin %q", errBadImage, res.SHA256, m.SHA256, t.SHA256)
	}
	_ = os.Remove(cfg.DestPath + stateSuffix)
	ft.mu.Lock()
	for _, ps := range ft.peers {
		ps.mu.Lock()
		res.Peers = append(res.Peers, PeerStat{Addr: hostOf(ps.addr), Bytes: ps.bytes, Chunks: int(ps.chunks), Failures: ps.failures, Dropped: ps.dropped})
		ps.mu.Unlock()
	}
	ft.mu.Unlock()
	sort.Slice(res.Peers, func(i, j int) bool { return res.Peers[i].Bytes > res.Peers[j].Bytes })

	// Only when asked for (p2p fetch --linger), and only now: the whole image
	// is verified, nothing partial is ever offered.
	if cfg.Serve && cfg.Linger > 0 {
		if l, err := net.Listen("tcp4", fmt.Sprintf("%s:%d", cfg.ListenHost, cfg.ListenPort)); err != nil {
			logf("p2p: cannot offer the image to other nodes: %v", err)
		} else {
			srv = newServer(l, cfg.NetConfig, logf)
			srv.setShare(t.Image, m, f)
			if stop, err := Announce(cfg.ListenHost, srv.Port(), "leech"); err != nil {
				logf("p2p: mDNS announce failed: %v", err)
			} else {
				stopAnn = stop
			}
			select {
			case <-time.After(cfg.Linger):
			case <-ctx.Done():
			}
		}
	}
	return res, 0, nil
}

func (ft *fetcher) liveCount() int {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	n := 0
	now := time.Now()
	for _, ps := range ft.peers {
		ps.mu.Lock()
		if !ps.dropped && now.After(ps.droppedUntil) && !ps.noImage {
			n++
		}
		ps.mu.Unlock()
	}
	return n
}

func (ft *fetcher) bytesDone() int64 {
	var b int64
	for i := 0; i < ft.m.NumChunks(); i++ {
		if ft.done.Has(i) {
			b += ft.m.ChunkLen(i)
		}
	}
	return b
}

// refreshHave asks the peer which chunks it holds.
func (ft *fetcher) refreshHave(ctx context.Context, ps *peerState) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, status, err := ft.p.get(rctx, ps.addr, "/"+ft.kind+"/have", "", 4<<20)
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.haveAt = time.Now()
	if err != nil || status != http.StatusOK {
		ps.noImage = status == http.StatusNotFound
		if err != nil {
			ft.failLocked(ps, err)
		}
		return
	}
	ps.noImage = false
	var hr haveResponse
	if json.Unmarshal(b, &hr) != nil || hr.Chunks != ft.m.NumChunks() {
		ft.dropLocked(ps, "sent a malformed chunk map")
		return
	}
	if hr.ID != ft.id {
		ft.dropLocked(ps, "serves a different image than the authenticated manifest")
		return
	}
	bm, err := DecodeBitmap(hr.Have, hr.Chunks)
	if err != nil {
		ft.dropLocked(ps, "sent a malformed chunk map")
		return
	}
	ps.have = bm
	ps.consec = 0
}

func (ft *fetcher) dropLocked(ps *peerState, why string) {
	if !ps.dropped {
		ft.logf("p2p: dropping peer %s: %s", ps.addr, why)
	}
	ps.dropped = true
}

func (ft *fetcher) failLocked(ps *peerState, err error) {
	ps.failures++
	ps.consec++
	if ps.consec >= maxConsecutiveFailures {
		ps.droppedUntil = time.Now().Add(peerCooldown)
		ps.consec = 0
		ft.logf("p2p: peer %s failed %d times in a row (%v), pausing it for %s", ps.addr, maxConsecutiveFailures, err, peerCooldown)
	}
}

// Speed tracking: every second the bytes each busy peer delivered are turned into
// a smoothed rate. A peer that, after slowSeconds of being busy, delivers less
// than 1/slowFraction of what the best peer does is demoted: it may hold one
// chunk at a time, only while fewer than maxSlowHolders demoted peers hold
// chunks, and a chunk that only demoted peers hold is taken by any peer that is
// not demoted, at once.
const (
	slowSeconds    = 3
	slowFraction   = 4
	maxSlowHolders = 4
)

// sampleSpeeds is called once a second.
func (ft *fetcher) sampleSpeeds() {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	var best float64
	for _, ps := range ft.peers {
		rx := ps.rx.Load()
		delta := rx - ps.lastRx
		ps.lastRx = rx
		if ps.flights == 0 {
			continue // idle: it keeps what it measured
		}
		ps.busy++
		if ps.busy == 1 {
			ps.rate = float64(delta)
		} else {
			ps.rate = 0.5*ps.rate + 0.5*float64(delta)
		}
	}
	for _, ps := range ft.peers {
		// a peer that finished its chunks in under a second still counts
		ps.mu.Lock()
		served := ps.chunks > 0
		ps.mu.Unlock()
		if ps.rate > best && (ps.busy >= 1 || served) {
			best = ps.rate
		}
	}
	for _, ps := range ft.peers {
		was := ps.slow
		switch {
		case ps.busy >= slowSeconds && best > 0 && ps.rate*slowFraction < best:
			ps.slow = true
		case ps.rate*2 >= best:
			ps.slow = false
		}
		if ps.slow && !was {
			ft.logf("p2p: peer %s is much slower than the best peer (%d KiB/s against %d KiB/s): demoted", ps.addr, int(ps.rate)>>10, int(best)>>10)
		}
	}
}

// slowHoldersLocked counts the demoted peers that hold a chunk.
func (ft *fetcher) slowHoldersLocked() int {
	n := 0
	for _, ps := range ft.peers {
		if ps.slow && ps.flights > 0 {
			n++
		}
	}
	return n
}

// pick returns a chunk this peer holds, that is neither done, in flight, nor
// already failed from this peer. The start is random so peers spread out. Chunks
// held only by demoted peers are taken by peers that are not demoted. Near the
// end the chunks in flight at other (maybe slow) peers are requested from this
// one too: the first answer wins.
//
// nolint:gocyclo // the selection rules (done, in flight, failed, demoted, endgame) are one scan over the chunks and read best together.
func (ft *fetcher) pick(ps *peerState, rng *rand.Rand) (int, bool) {
	ps.mu.Lock()
	have := ps.have
	ps.mu.Unlock()
	if have == nil {
		return 0, false
	}
	n := ft.m.NumChunks()
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if ps.slow && (ps.flights >= 1 || ft.slowHoldersLocked() >= maxSlowHolders) {
		return 0, false
	}
	start := rng.Intn(n)
	remaining := n - ft.done.Count()
	endgame := remaining <= 2*maxInt(len(ft.peers), 1)
	onlySlow := func(i int) bool {
		if len(ft.flightBy[i]) == 0 {
			return false
		}
		for a := range ft.flightBy[i] {
			if h := ft.peers[a]; h != nil && !h.slow {
				return false
			}
		}
		return true
	}
	for pass := 0; pass < 4; pass++ {
		if pass == 1 && (!endgame || ps.slow) {
			continue
		}
		if pass >= 2 && ps.slow {
			break
		}
		if pass == 3 && ps.flights > 0 {
			break
		}
		for k := 0; k < n; k++ {
			i := (start + k) % n
			if ft.done.Has(i) || !have.Has(i) || ft.failedBy[i][ps.addr] {
				continue
			}
			switch pass {
			case 0:
				if ft.flight[i] > 0 {
					continue
				}
			case 1:
				if ft.flight[i] >= 2 || ft.flightBy[i][ps.addr] {
					continue
				}
			case 2: // steal from demoted peers
				if ft.flightBy[i][ps.addr] || !onlySlow(i) {
					continue
				}
			case 3: // a peer with nothing to do tries a chunk somebody else holds
				if ft.flightBy[i][ps.addr] {
					continue
				}
			}
			ft.flight[i]++
			ps.flights++
			if ft.flightBy[i] == nil {
				ft.flightBy[i] = map[string]bool{}
			}
			ft.flightBy[i][ps.addr] = true
			return i, true
		}
	}
	return 0, false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// stopOthers cancels the requests of the other peers for a chunk that is done.
func (ft *fetcher) stopOthers(i int, self string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	for a, c := range ft.cancels[i] {
		if a != self {
			c()
		}
	}
}

func (ft *fetcher) release(i int, peer, failedPeer string) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if ft.flight[i]--; ft.flight[i] <= 0 {
		delete(ft.flight, i)
	}
	delete(ft.flightBy[i], peer)
	if ps := ft.peers[peer]; ps != nil && ps.flights > 0 {
		ps.flights--
	}
	delete(ft.cancels[i], peer)
	if failedPeer == "" {
		return
	}
	if ft.failedBy[i] == nil {
		ft.failedBy[i] = map[string]bool{}
	}
	ft.failedBy[i][failedPeer] = true
	// when every live peer failed this chunk, let them try again
	all := true
	for a, ps := range ft.peers {
		ps.mu.Lock()
		skip := ps.dropped
		ps.mu.Unlock()
		if !skip && !ft.failedBy[i][a] {
			all = false
		}
	}
	if all {
		delete(ft.failedBy, i)
	}
}

func (ft *fetcher) worker(ctx context.Context, ps *peerState, id int) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)*7919 + int64(len(ps.addr))))
	for ctx.Err() == nil {
		ps.mu.Lock()
		dropped, paused := ps.dropped, time.Now().Before(ps.droppedUntil)
		stale := ps.have == nil || time.Since(ps.haveAt) > haveRefresh
		ps.mu.Unlock()
		if dropped {
			return
		}
		if paused {
			sleepCtx(ctx, time.Second)
			continue
		}
		if stale && id == 0 { // one worker per peer keeps the chunk map fresh
			ft.refreshHave(ctx, ps)
			continue
		}
		i, ok := ft.pick(ps, rng)
		if !ok {
			if ft.done.Count() >= ft.m.NumChunks() {
				return
			}
			sleepCtx(ctx, 250*time.Millisecond)
			continue
		}
		ft.fetchChunk(ctx, ps, i)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (ft *fetcher) fetchChunk(ctx context.Context, ps *peerState, i int) {
	off := int64(i) * ft.m.ChunkSize
	l := ft.m.ChunkLen(i)
	began := time.Now()
	rctx, cancel := context.WithTimeout(ctx, chunkTimeout(l))
	ft.mu.Lock()
	if ft.cancels[i] == nil {
		ft.cancels[i] = map[string]context.CancelFunc{}
	}
	ft.cancels[i][ps.addr] = cancel
	ft.mu.Unlock()
	b, status, err := ft.p.get(rctx, ps.addr, "/"+ft.kind+"/image", fmt.Sprintf("bytes=%d-%d", off, off+l-1), l, &ft.rx, &ps.rx)
	cancel()
	if ctx.Err() != nil || (err != nil && ft.done.Has(i)) {
		// the download ended, or another peer was faster and this request was cancelled
		ft.release(i, ps.addr, "")
		return
	}
	if err == nil && status != http.StatusPartialContent {
		err = fmt.Errorf("status %d", status)
		if status == http.StatusNotFound {
			// it lost the chunk (or is no longer serving the image): refresh its map
			ps.mu.Lock()
			ps.have = nil
			ps.mu.Unlock()
		}
	}
	if err == nil && int64(len(b)) != l {
		err = fmt.Errorf("got %d bytes, expected %d", len(b), l)
	}
	if err != nil {
		ps.mu.Lock()
		ft.failLocked(ps, err)
		ps.mu.Unlock()
		ft.release(i, ps.addr, ps.addr)
		return
	}
	if chunkHash(b) != ft.m.Chunks[i] {
		ps.mu.Lock()
		ps.failures++
		ps.bad++
		ft.logf("p2p: chunk %d from %s does not match the manifest, trying another peer", i, ps.addr)
		if ps.bad >= maxBadChunks {
			ft.dropLocked(ps, "served bad chunks")
		}
		ps.mu.Unlock()
		ft.release(i, ps.addr, ps.addr)
		return
	}
	if ft.done.Has(i) { // another peer was faster (endgame)
		ft.release(i, ps.addr, "")
		return
	}
	if _, err := ft.f.WriteAt(b, off); err != nil {
		ft.release(i, ps.addr, "")
		select {
		case ft.fatal <- fmt.Errorf("writing %s: %w", ft.cfg.DestPath, err):
		default:
		}
		return
	}
	// two peers can both deliver the last copy of a chunk (endgame, a steal):
	// only the first counts, the bytes are identical and verified
	ft.mu.Lock()
	first := !ft.done.Has(i)
	ft.done.Set(i)
	ft.mu.Unlock()
	if !first {
		ft.release(i, ps.addr, "")
		return
	}
	if el := time.Since(began).Seconds(); el > 0 {
		// a peer that finishes between two samples still gets a speed
		ft.mu.Lock()
		if ps.busy == 0 {
			if cr := float64(l) / el * workersPerPeer; cr > ps.rate {
				ps.rate = cr
			}
		}
		ft.mu.Unlock()
	}
	ft.stopOthers(i, ps.addr)
	ft.release(i, ps.addr, "")
	ps.mu.Lock()
	ps.bytes += l
	ps.chunks++
	ps.consec = 0
	ps.mu.Unlock()
	ft.saveState(false)
}

// peerCap is how many peers a download looks at.
func peerCap(t images.P2PTarget) int {
	if t.Pin != "" {
		return maxPinnedPeers
	}
	return maxPeers
}
