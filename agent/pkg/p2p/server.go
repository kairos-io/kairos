package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
	"golang.org/x/time/rate"
)

// Wire protocol, per image kind K (active, passive, recovery), all GET:
//
//	/K/manifest   the Manifest as JSON
//	/K/have       {"id": <manifest id>, "chunks": N, "have": <base64 bitmap>}
//	              (always every chunk: only complete, verified images are served)
//	/K/image      the image, with or without a Range: bytes=a-b
//	/K/pin        {"image": K, "size": N, "sha256": <hex>, "pin": <root>, "uri": ...}, for the
//	              operator to copy. Clients never use it: the pin has to come
//	              from the user, not from the peer being checked.
//
// Nothing else is served.

// writeStallTimeout is how long a client may not read before it is cut.
var writeStallTimeout = 20 * time.Second

const (
	defaultMaxInflight   = 32  // concurrent image streams, all peers together
	defaultMaxConns      = 128 // open connections, all peers together
	defaultMaxConnsPerIP = 8
	maxInflightPerIP     = 4
	smallReqRate         = 20 // manifest/have requests per second per IP
	smallReqBurst        = 40
	smallBytesRate       = 1 << 20 // manifest/have/pin bytes per second per IP, whatever the upload limit
	smallBytesBurst      = 4 << 20
	smallTotalRate       = 8 << 20 // and per second for all the addresses together
	smallTotalBurst      = 16 << 20
	maxTrackedIPs        = 4096
)

// share is a complete, verified image. A node that is still downloading serves
// nothing: chunks that were only checked against a manifest a peer supplied are
// never handed on.
type share struct {
	m  *Manifest
	id string
	r  io.ReaderAt
}

// Server serves shares over HTTP to allowlisted peers.
type Server struct {
	ln      net.Listener
	srv     *http.Server
	logf    func(string, ...any)
	limiter *rate.Limiter // upload, all peers together
	sem     chan struct{} // image streams in flight

	mu       sync.RWMutex
	shares   map[string]*share
	ipMu     sync.Mutex
	streams  map[string][]*stream
	limiters map[string]*rate.Limiter
	bytesLim map[string]*rate.Limiter
	smallAll *rate.Limiter

	lastDropLog time.Time
}

// newServer starts serving on a listener it takes over.
func newServer(l net.Listener, n NetConfig, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	maxInflight := n.MaxPeers
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	maxConns := n.MaxConns
	if maxConns <= 0 {
		maxConns = defaultMaxConns
	}
	maxConns = fitFDLimit(maxConns, logf)
	perIP := n.MaxConnsPerIP
	if perIP <= 0 {
		perIP = defaultMaxConnsPerIP
	}
	s := &Server{ln: l, logf: logf, sem: make(chan struct{}, maxInflight), shares: map[string]*share{},
		streams: map[string][]*stream{}, limiters: map[string]*rate.Limiter{}, bytesLim: map[string]*rate.Limiter{}, smallAll: rate.NewLimiter(smallTotalRate, smallTotalBurst)}
	up := n.UploadBytesPerSec
	if up <= 0 {
		up = DefaultUploadBytesPerSec // never unlimited: a seeder has a sane ceiling out of the box
	}
	burst := up
	if burst < 64<<10 {
		burst = 64 << 10
	}
	s.limiter = rate.NewLimiter(rate.Limit(up), burst)
	// There is no WriteTimeout: it would cap the length of a transfer. Progress
	// is enforced per write (writeStallTimeout) instead.
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10,
		// the connection is kept with the request so a stream can be evicted by closing it
		ConnContext: func(ctx context.Context, c net.Conn) context.Context { return context.WithValue(ctx, connKey{}, c) }}
	go s.srv.Serve(newLimitListener(l, n.Allow, maxConns, perIP, s.dropped)) //nolint:errcheck
	return s
}

func (s *Server) dropped(addr net.Addr, why string) {
	s.mu.Lock()
	quiet := time.Since(s.lastDropLog) < time.Second
	if !quiet {
		s.lastDropLog = time.Now()
	}
	s.mu.Unlock()
	if !quiet {
		s.logf("p2p: dropped connection from %s (%s)", addr, why)
	}
}

// setShare publishes a complete image.
func (s *Server) setShare(kind string, m *Manifest, r io.ReaderAt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *m
	s.shares[kind] = &share{m: &c, id: c.ID(), r: r}
}

func (s *Server) removeShare(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.shares, kind)
}

func (s *Server) kinds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for _, k := range images.P2PImages {
		if _, ok := s.shares[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// Port is the TCP port being served.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// Close stops the server.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		return s.srv.Close()
	}
	return nil
}

// limitListener drops connections from outside the allowlist, and connections
// beyond the per address cap, before anything is read from them. When the global
// cap is reached an address that holds fewer connections than the heaviest one
// takes the place of that one's oldest connection: a few addresses (or one
// address that owns many) cannot keep every slot for themselves.
type limitListener struct {
	net.Listener
	allow    Allowlist
	maxConns int
	perIP    int
	dropped  func(net.Addr, string)

	mu    sync.Mutex
	total int
	byIP  map[string][]*countedConn
}

func newLimitListener(l net.Listener, allow Allowlist, maxConns, perIP int, dropped func(net.Addr, string)) *limitListener {
	return &limitListener{Listener: l, allow: allow, maxConns: maxConns, perIP: perIP, dropped: dropped, byIP: map[string][]*countedConn{}}
}

type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}

// evictFor returns the oldest connection of the heaviest address if key holds
// fewer connections than that one, else nil. Called with l.mu held.
func (l *limitListener) evictFor(key string) *countedConn {
	heavy, n := "", 0
	for k, v := range l.byIP {
		if len(v) > n {
			heavy, n = k, len(v)
		}
	}
	if heavy == "" || heavy == key || n <= len(l.byIP[key])+1 {
		return nil
	}
	return l.byIP[heavy][0]
}

// Accept returns the next admitted connection.
func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := ipOf(c.RemoteAddr())
		if ip == nil || !l.allow.Allows(ip) {
			l.dropped(c.RemoteAddr(), "not in allowlist")
			_ = c.Close()
			continue
		}
		key := ip.String()
		l.mu.Lock()
		if len(l.byIP[key]) >= l.perIP {
			l.mu.Unlock()
			l.dropped(c.RemoteAddr(), "too many connections")
			_ = c.Close()
			continue
		}
		if l.total >= l.maxConns {
			victim := l.evictFor(key)
			l.mu.Unlock()
			if victim == nil {
				l.dropped(c.RemoteAddr(), "too many connections")
				_ = c.Close()
				continue
			}
			l.dropped(victim.RemoteAddr(), "evicted, the node is full and this address holds the most connections")
			_ = victim.Close()
			l.mu.Lock()
			if l.total >= l.maxConns {
				l.mu.Unlock()
				_ = c.Close()
				continue
			}
		}
		cc := &countedConn{Conn: c}
		cc.release = func() {
			l.mu.Lock()
			l.total--
			list := l.byIP[key]
			for i, x := range list {
				if x == cc {
					list = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(list) == 0 {
				delete(l.byIP, key)
			} else {
				l.byIP[key] = list
			}
			l.mu.Unlock()
		}
		l.total++
		l.byIP[key] = append(l.byIP[key], cc)
		l.mu.Unlock()
		return cc, nil
	}
}

func ipOf(a net.Addr) net.IP {
	if t, ok := a.(*net.TCPAddr); ok {
		return t.IP
	}
	return net.ParseIP(hostOf(a.String()))
}

func (s *Server) smallAllowed(ip string) bool {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if len(s.limiters) > maxTrackedIPs {
		s.limiters = map[string]*rate.Limiter{}
	}
	l := s.limiters[ip]
	if l == nil {
		l = rate.NewLimiter(smallReqRate, smallReqBurst)
		s.limiters[ip] = l
	}
	return l.Allow()
}

type connKey struct{}

// stream is an image response in flight, with the connection that carries it.
type stream struct {
	conn net.Conn
	// evicted: the connection was closed to make room, and the slot (the
	// semaphore token) went to the stream that took its place, so releasing it
	// must not free the token a second time.
	evicted atomic.Bool
}

// acquireStream takes a slot for an image response. When the node is full, an
// address that holds fewer streams than the heaviest one takes the place of
// that one's oldest stream (its connection is closed): a few addresses cannot
// keep all the slots with clients that read slowly.
func (s *Server) acquireStream(ip string, conn net.Conn) (*stream, bool) {
	st := &stream{conn: conn}
	s.ipMu.Lock()
	if len(s.streams[ip]) >= maxInflightPerIP {
		s.ipMu.Unlock()
		return nil, false
	}
	s.streams[ip] = append(s.streams[ip], st)
	s.ipMu.Unlock()
	select {
	case s.sem <- struct{}{}:
		return st, true
	default:
	}
	// full: make room by evicting from the heaviest address, if it is heavier than this one
	s.ipMu.Lock()
	heavy, n := "", 0
	for k, v := range s.streams {
		if len(v) > n {
			heavy, n = k, len(v)
		}
	}
	var victim *stream
	if heavy != "" && heavy != ip && n > len(s.streams[ip])+1 {
		for _, c := range s.streams[heavy] {
			if !c.evicted.Load() {
				victim = c
				break
			}
		}
	}
	if victim != nil {
		// the slot changes hands here, not when the victim's handler notices
		// its connection is gone: nothing waits, nothing can be taken in between
		victim.evicted.Store(true)
		s.removeStreamLocked(heavy, victim)
	}
	s.ipMu.Unlock()
	if victim == nil {
		s.removeStream(ip, st)
		return nil, false
	}
	if victim.conn != nil {
		s.dropped(victim.conn.RemoteAddr(), "stream evicted, the node is full and this address holds the most streams")
		_ = victim.conn.Close()
	}
	return st, true
}

func (s *Server) removeStreamLocked(ip string, st *stream) {
	list := s.streams[ip]
	for i, x := range list {
		if x == st {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(s.streams, ip)
	} else {
		s.streams[ip] = list
	}
}

func (s *Server) removeStream(ip string, st *stream) {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	s.removeStreamLocked(ip, st)
}

func (s *Server) releaseStream(ip string, st *stream) {
	s.ipMu.Lock()
	if st.evicted.Load() { // an evicted stream's slot already belongs to its successor
		s.ipMu.Unlock()
		return
	}
	s.removeStreamLocked(ip, st)
	s.ipMu.Unlock()
	<-s.sem // after leaving the list: whoever is in the list holds a token
}

// throttle spends n bytes of the upload limit, in pieces the limiter burst allows.
func (s *Server) throttle(ctx context.Context, n int) error {
	if s.limiter == nil {
		return nil
	}
	for n > 0 {
		k := n
		if k > 32<<10 {
			k = 32 << 10
		}
		if err := s.limiter.WaitN(ctx, k); err != nil {
			return err
		}
		n -= k
	}
	return nil
}

// smallBytes returns the limiter of the small responses of one address.
func (s *Server) smallBytes(ip string) *rate.Limiter {
	s.ipMu.Lock()
	defer s.ipMu.Unlock()
	if len(s.bytesLim) > maxTrackedIPs {
		s.bytesLim = map[string]*rate.Limiter{}
	}
	l := s.bytesLim[ip]
	if l == nil {
		l = rate.NewLimiter(smallBytesRate, smallBytesBurst)
		s.bytesLim[ip] = l
	}
	return l
}

// writeJSON sends v, counted against the upload limit like the image is, and
// against a per address allowance and a global budget that are there whatever
// the upload limit is: a manifest can be a megabyte, and asking for it is cheap.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	b, _ := json.Marshal(v)
	l := s.smallBytes(hostOf(r.RemoteAddr))
	for n := len(b); n > 0; {
		k := n
		if k > 32<<10 {
			k = 32 << 10
		}
		if err := l.WaitN(r.Context(), k); err != nil {
			return
		}
		if err := s.smallAll.WaitN(r.Context(), k); err != nil {
			return
		}
		n -= k
	}
	if err := s.throttle(r.Context(), len(b)); err != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(b, '\n'))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || !images.ValidP2PImage(parts[0]) {
		http.NotFound(w, r)
		return
	}
	s.mu.RLock()
	sh := s.shares[parts[0]]
	s.mu.RUnlock()
	if sh == nil {
		http.NotFound(w, r)
		return
	}
	ip := hostOf(r.RemoteAddr)
	switch parts[1] {
	case "manifest":
		if !s.smallAllowed(ip) {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		s.writeJSON(w, r, sh.m)
	case "have":
		if !s.smallAllowed(ip) {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		s.writeJSON(w, r, haveResponse{ID: sh.id, Chunks: sh.m.NumChunks(), Have: fullBitmap(sh.m.NumChunks()).Encode()})
	case "pin":
		if !s.smallAllowed(ip) {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		host, _, _ := net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
		s.writeJSON(w, r, map[string]any{"image": parts[0], "size": sh.m.Size, "sha256": sh.m.SHA256, "pin": sh.m.Root(),
			"uri": PinURI(host, s.Port(), parts[0], sh.m.Root())})
	case "image":
		conn, _ := r.Context().Value(connKey{}).(net.Conn)
		st, ok := s.acquireStream(ip, conn)
		if !ok {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		defer s.releaseStream(ip, st)
		s.serveImage(w, r, sh)
	default:
		http.NotFound(w, r)
	}
}

type haveResponse struct {
	ID     string `json:"id"`
	Chunks int    `json:"chunks"`
	Have   string `json:"have"`
}

// parseRange accepts a single "bytes=a-b" range inside [0,size).
func parseRange(h string, size int64) (from, to int64, err error) {
	if !strings.HasPrefix(h, "bytes=") || strings.Contains(h, ",") {
		return 0, 0, fmt.Errorf("only a single bytes range is supported")
	}
	a, b, ok := strings.Cut(strings.TrimPrefix(h, "bytes="), "-")
	if !ok || a == "" || b == "" {
		return 0, 0, fmt.Errorf("range must be bytes=a-b")
	}
	if from, err = strconv.ParseInt(a, 10, 64); err != nil {
		return 0, 0, err
	}
	if to, err = strconv.ParseInt(b, 10, 64); err != nil {
		return 0, 0, err
	}
	if from < 0 || to < from || to >= size {
		return 0, 0, fmt.Errorf("range out of bounds")
	}
	return from, to, nil
}

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, sh *share) {
	from, to := int64(0), sh.m.Size-1
	status := http.StatusOK
	if rh := r.Header.Get("Range"); rh != "" {
		var err error
		if from, to, err = parseRange(rh, sh.m.Size); err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		status = http.StatusPartialContent
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(to-from+1, 10))
	if status == http.StatusPartialContent {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, sh.m.Size))
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(writeStallTimeout))
	w.WriteHeader(status)
	src := io.NewSectionReader(sh.r, from, to-from+1)
	buf := make([]byte, 64<<10)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if err := s.throttle(r.Context(), n); err != nil {
				return
			}
			// the deadline follows progress: a peer that stops reading loses
			// its slot after writeStallTimeout, not after a flat 5 minutes
			_ = rc.SetWriteDeadline(time.Now().Add(writeStallTimeout))
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}
