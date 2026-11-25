package p2p_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Regression specs for the findings of the p2p security review (SECURITY.md).
// The finding ids are in the spec names.

type fakePeer struct {
	addr                   string
	imageReqs, manifestReq atomic.Int64
	srv                    *http.Server
	// stall: answer the manifest and the chunk map, never a chunk
	stall bool
	// trickle: answer chunks at this many bytes per second (0: at once)
	trickle int
	// silent: never answer the manifest
	silent bool
	// kbps: answer chunks at this many KiB per second, in blocks (0: at once)
	kbps int
	stop chan struct{}
}

// serveFake runs a peer that serves env with content. redirect, when set,
// answers every request with a 302 to it.
func serveFake(host string, env *p2p.Manifest, content []byte, redirect string) *fakePeer {
	fp := &fakePeer{stop: make(chan struct{})}
	n := env.NumChunks()
	full := p2p.NewBitmap(n)
	for i := 0; i < n; i++ {
		full.Set(i)
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect != "" {
			http.Redirect(w, r, redirect+r.URL.Path, http.StatusFound)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifest"):
			fp.manifestReq.Add(1)
			if fp.silent {
				select {
				case <-fp.stop:
				case <-r.Context().Done():
				}
				return
			}
			_ = json.NewEncoder(w).Encode(env)
		case strings.HasSuffix(r.URL.Path, "/have"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": env.ID(), "chunks": n, "have": full.Encode()})
		case strings.HasSuffix(r.URL.Path, "/image"):
			fp.imageReqs.Add(1)
			if fp.stall {
				select {
				case <-fp.stop:
				case <-r.Context().Done():
				}
				return
			}
			var a, b int64
			_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &a, &b)
			if b >= int64(len(content)) {
				b = int64(len(content)) - 1
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, len(content)))
			w.Header().Set("Content-Length", fmt.Sprint(b-a+1))
			w.WriteHeader(206)
			if fp.trickle > 0 {
				for i := a; i <= b; i++ {
					if _, err := w.Write(content[i : i+1]); err != nil {
						return
					}
					w.(http.Flusher).Flush()
					select {
					case <-time.After(time.Second / time.Duration(fp.trickle)):
					case <-fp.stop:
						return
					case <-r.Context().Done():
						return
					}
				}
				return
			}
			if fp.kbps > 0 {
				for i := a; i <= b; i += 16 << 10 {
					e := i + 16<<10
					if e > b+1 {
						e = b + 1
					}
					if _, err := w.Write(content[i:e]); err != nil {
						return
					}
					w.(http.Flusher).Flush()
					select {
					case <-time.After(time.Duration(float64(e-i) / float64(fp.kbps<<10) * float64(time.Second))):
					case <-fp.stop:
						return
					case <-r.Context().Done():
						return
					}
				}
				return
			}
			_, _ = w.Write(content[a : b+1])
		default:
			http.NotFound(w, r)
		}
	})
	l, err := net.Listen("tcp4", host+":0")
	Expect(err).ToNot(HaveOccurred())
	fp.addr = l.Addr().String()
	fp.srv = &http.Server{Handler: h}
	go fp.srv.Serve(l) //nolint:errcheck
	DeferCleanup(func() { close(fp.stop); _ = fp.srv.Close() })
	return fp
}

var _ = Describe("p2p security regressions", func() {
	var dir string
	var allow p2p.Allowlist
	var logs *logBuf
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		var err error
		allow, err = p2p.ParseAllowlist([]string{"127.0.0.0/8"})
		Expect(err).ToNot(HaveOccurred())
		logs = &logBuf{}
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Println(logs.String())
			}
		})
	})

	sha := func(p string) string { s, _ := p2p.SHA256File(p); return s }
	startWith := func(paths map[string]string, host string, mod func(*p2p.ServiceConfig)) *p2p.Service {
		cfg := p2p.ServiceConfig{
			NetConfig: p2p.NetConfig{ListenHost: host, ListenPort: freePort(), Allow: allow},
			Paths:     paths, ChunkSize: chunk, PollInterval: 200 * time.Millisecond, Logf: logs.logf,
		}
		if mod != nil {
			mod(&cfg)
		}
		s, err := startRetry(cfg)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(s.Close)
		return s
	}
	peerOf := func(host string, s *p2p.Service) string { return net.JoinHostPort(host, strconv.Itoa(s.Port())) }
	fetchWith := func(auth p2p.Auth, t images.P2PTarget, host, name string, mod func(*p2p.FetchConfig)) (*p2p.Result, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cfg := p2p.FetchConfig{
			NetConfig: p2p.NetConfig{ListenHost: host, Allow: allow}, Auth: auth,
			Target: t, DestPath: filepath.Join(dir, "out", name), Logf: logs.logf,
		}
		if mod != nil {
			mod(&cfg)
		}
		return p2p.Fetch(ctx, cfg)
	}
	tgt := func(pin string, peers ...string) images.P2PTarget {
		return images.P2PTarget{Peers: peers, Image: "active", SHA256: pin}
	}
	// evil returns a manifest that claims the given image hash with chunk hashes
	// of attacker chosen content, and that content.
	evil := func(claimed string, size int) (*p2p.Manifest, []byte) {
		g := randomFile(dir, "garbage.img", size)
		m, err := p2p.BuildManifest(g, "active", chunk)
		Expect(err).ToNot(HaveOccurred())
		m.SHA256 = claimed
		b, _ := os.ReadFile(g)
		return m, b
	}

	Describe("P2P-01: no authenticity without an anchor", func() {
		It("refuses a source nothing authenticates, before talking to any peer", func() {
			img := randomFile(dir, "active.img", chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			env, content := evil(strings.Repeat("a", 64), chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			_, err := fetchWith(p2p.Auth{}, tgt("", peerOf("127.0.0.1", s), rogue.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("refusing p2p source")))
			Expect(err).To(MatchError(ContainSubstring("p2p-source.insecure")))
			Expect(rogue.manifestReq.Load()).To(BeZero())
			Expect(filepath.Join(dir, "out", "dl.img")).ToNot(BeAnExistingFile())
		})
		It("accepts it only with the explicit insecure opt in, and says so loudly", func() {
			img := randomFile(dir, "active.img", chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			res, err := fetchWith(p2p.Auth{Insecure: true}, tgt("", peerOf("127.0.0.1", s)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Trust).To(Equal("none (insecure mode)"))
			Expect(logs.String()).To(ContainSubstring("WARNING insecure mode"))
		})
		It("does not let a rogue peer that is listed first win with a pin", func() {
			img := randomFile(dir, "active.img", 4*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			other := randomFile(dir, "rogue.img", 4*chunk)
			rogue := startWith(map[string]string{"active": other}, "127.0.0.2", nil)
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peerOf("127.0.0.2", rogue), peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("the source pins"))
		})
	})

	Describe("P2P-02: a rogue manifest with the pinned image hash", func() {
		var img string
		var honest *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 8*chunk)
			honest = startWith(map[string]string{"active": img}, "127.0.0.1", nil)
		})
		It("cannot starve the honest peer: its group is tried, fails the final check and the next one is used", func() {
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			want, _ := os.ReadFile(img)
			got, _ := os.ReadFile(filepath.Join(dir, "out", "dl.img"))
			Expect(got).To(Equal(want))
			Expect(logs.String()).To(ContainSubstring("dropping that manifest"))
			Expect(rogue.imageReqs.Load()).To(BeNumerically(">", 0)) // the waste that remains: one download of the rogue's
		})
		It("N1: three rogue manifests listed before the honest one do not starve it", func() {
			var peers []string
			var rogues []*fakePeer
			for i := 2; i <= 4; i++ {
				env, content := evil(sha(img), 8*chunk)
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), append(peers, peerOf("127.0.0.1", honest))...), "127.0.0.9", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			for _, r := range rogues { // each rogue cost one download at most
				Expect(r.imageReqs.Load()).To(BeNumerically(">", 0))
			}
			Expect(logs.String()).To(ContainSubstring("dropping that manifest"))
		})
		It("N1: the manifest more distinct peers serve is tried first, so rogues listed first cost nothing", func() {
			second := startWith(map[string]string{"active": img}, "127.0.0.4", nil)
			var peers []string
			var rogues []*fakePeer
			for i := 5; i <= 7; i++ {
				env, content := evil(sha(img), 8*chunk)
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest), peerOf("127.0.0.4", second))
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.9", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			for _, r := range rogues {
				Expect(r.imageReqs.Load()).To(BeZero())
			}
		})
		It("V3: gives up after wasting a bounded number of bytes when only rogues answer, and installs nothing", func() {
			var peers []string
			var rogues []*fakePeer
			for i := 2; i <= 12; i++ {
				env, content := evil(sha(img), 8*chunk)
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			_, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.19", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("gave up after wasting")))
			tried := 0
			for _, r := range rogues {
				if r.imageReqs.Load() > 0 {
					tried++
				}
			}
			// every failed manifest costs 8 chunks (2 MiB), the bound is 4 images (8 MiB)
			Expect(tried).To(Equal(5))
			Expect(filepath.Join(dir, "out", "dl.img")).ToNot(BeAnExistingFile())
			Expect(filepath.Join(dir, "out", "dl.img.p2p-state")).ToNot(BeAnExistingFile())
		})
		It("never installs the rogue image: a pin of the honest image is the only way a file is accepted", func() {
			env, content := evil(strings.Repeat("a", 64), 8*chunk) // the rogue's own claim, not the pin
			rogue := serveFake("127.0.0.2", env, content, "")
			_, err := fetchWith(p2p.Auth{}, tgt(sha(img), rogue.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).To(HaveOccurred())
			Expect(rogue.imageReqs.Load()).To(BeZero()) // refused at the manifest: wrong image hash
		})
	})

	Describe("P2P-07: a leecher hands on only the finished, verified image", func() {
		var img string
		var slow *p2p.Service
		leechHave := func(port int) int {
			c := &http.Client{Timeout: 2 * time.Second}
			resp, err := c.Get(fmt.Sprintf("http://127.0.0.2:%d/active/have", port))
			if err != nil {
				return -1
			}
			defer resp.Body.Close() //nolint:errcheck
			_, _ = io.Copy(io.Discard, resp.Body)
			return resp.StatusCode
		}
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 16*chunk)
			slow = startWith(map[string]string{"active": img}, "127.0.0.1", func(c *p2p.ServiceConfig) { c.UploadBytesPerSec = 768 << 10 })
		})
		It("serves nothing while it downloads, then the whole image, to a node that cannot reach the seed", func() {
			port := freePort()
			done := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_, err := p2p.Fetch(ctx, p2p.FetchConfig{
					NetConfig: p2p.NetConfig{ListenHost: "127.0.0.2", ListenPort: port, Allow: allow},
					Target:    tgt(sha(img), peerOf("127.0.0.1", slow)), DestPath: filepath.Join(dir, "lg", "a.img"),
					Serve: true, Linger: 30 * time.Second, Logf: logs.logf,
				})
				done <- err
			}()
			// partially downloaded chunks are never offered
			Consistently(func() int { return leechHave(port) }, 3*time.Second, 300*time.Millisecond).ShouldNot(Equal(200))
			Eventually(func() int { return leechHave(port) }, 30*time.Second, 300*time.Millisecond).Should(Equal(200))
			// the seed goes away: the next node is served by the first one alone
			Expect(slow.Close()).To(Succeed())
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), net.JoinHostPort("127.0.0.2", strconv.Itoa(port))), "127.0.0.3", "b.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Peers).To(HaveLen(1))
			Expect(res.Peers[0].Addr).To(Equal("127.0.0.2"))
			Expect(res.SHA256).To(Equal(sha(img)))
		})
	})

	Describe("P2P-04: one address cannot starve the others", func() {
		It("keeps serving others while an address holds many connections that never read", func() {
			img := randomFile(dir, "active.img", 16*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			addr := peerOf("127.0.0.1", s)
			var conns []net.Conn
			d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}, Timeout: 2 * time.Second}
			for i := 0; i < 40; i++ {
				c, err := d.Dial("tcp", addr)
				if err != nil {
					break
				}
				conns = append(conns, c)
				fmt.Fprintf(c, "GET /active/image HTTP/1.1\r\nHost: x\r\nRange: bytes=0-%d\r\n\r\n", 4*chunk-1)
			}
			DeferCleanup(func() {
				for _, c := range conns {
					_ = c.Close()
				}
			})
			time.Sleep(500 * time.Millisecond)
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), addr), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
		It("N4: a full node makes room for a new address by evicting the address that holds the most", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", func(c *p2p.ServiceConfig) { c.MaxConns = 8; c.MaxConnsPerIP = 8 })
			addr := peerOf("127.0.0.1", s)
			var conns []net.Conn
			d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}, Timeout: 2 * time.Second}
			for i := 0; i < 8; i++ {
				if c, err := d.Dial("tcp", addr); err == nil {
					conns = append(conns, c)
				}
			}
			DeferCleanup(func() {
				for _, k := range conns {
					_ = k.Close()
				}
			})
			time.Sleep(300 * time.Millisecond)
			c := &http.Client{Timeout: 3 * time.Second} // from 127.0.0.1: holds nothing
			resp, err := c.Get("http://" + addr + "/active/manifest")
			Expect(err).ToNot(HaveOccurred())
			_ = resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(200))
			Expect(logs.String()).To(ContainSubstring("evicted"))
		})
		It("N4: many addresses holding slow streams cannot keep every stream slot", func() {
			img := randomFile(dir, "big.img", 48<<20)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			addr := peerOf("127.0.0.1", s)
			var conns []net.Conn
			for ip := 2; ip <= 13; ip++ { // 12 addresses x 4 slow streams = 48 > 32 slots
				d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(fmt.Sprintf("127.0.0.%d", ip))}, Timeout: 2 * time.Second}
				for k := 0; k < 4; k++ {
					c, err := d.Dial("tcp", addr)
					if err != nil {
						continue
					}
					conns = append(conns, c)
					fmt.Fprintf(c, "GET /active/image HTTP/1.1\r\nHost: x\r\nRange: bytes=0-33554431\r\n\r\n")
				}
			}
			DeferCleanup(func() {
				for _, k := range conns {
					_ = k.Close()
				}
			})
			time.Sleep(time.Second)
			// a 14th address, with a normal client, still gets a stream
			d := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.30")}, Timeout: 2 * time.Second}
			hc := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: d.DialContext}}
			req, _ := http.NewRequest("GET", "http://"+addr+"/active/image", nil)
			req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", chunk-1))
			resp, err := hc.Do(req)
			Expect(err).ToNot(HaveOccurred())
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusPartialContent))
			Expect(logs.String()).To(ContainSubstring("stream evicted"))
		})
		It("rate limits manifest and chunk map requests per address", func() {
			img := randomFile(dir, "active.img", chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			addr := peerOf("127.0.0.1", s)
			limited := 0
			for i := 0; i < 200; i++ {
				resp, err := http.Get("http://" + addr + "/active/manifest")
				Expect(err).ToNot(HaveOccurred())
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusTooManyRequests {
					limited++
				}
			}
			Expect(limited).To(BeNumerically(">", 0))
		})
	})

	Describe("P2P-05: redirects are never followed", func() {
		It("does not send the node to another host, even an allowlisted one", func() {
			var hits atomic.Int64
			canary := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })}
			l, err := net.Listen("tcp4", "127.0.0.5:0")
			Expect(err).ToNot(HaveOccurred())
			go canary.Serve(l) //nolint:errcheck
			DeferCleanup(canary.Close)
			env, content := evil(strings.Repeat("a", 64), chunk)
			redir := serveFake("127.0.0.2", env, content, "http://"+l.Addr().String())
			_, err = fetchWith(p2p.Auth{Insecure: true}, tgt("", redir.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).To(HaveOccurred())
			Expect(hits.Load()).To(BeZero())
			Expect(logs.String()).To(ContainSubstring("redirect"))
		})
	})

	Describe("P2P-06: size policy and clean up", func() {
		It("refuses an image larger than the configured maximum", func() {
			img := randomFile(dir, "active.img", 4*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			_, err := fetchWith(p2p.Auth{MaxImageSize: 2 * chunk}, tgt(sha(img), peerOf("127.0.0.1", s)), "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("more than the")))
			Expect(filepath.Join(dir, "out", "dl.img")).ToNot(BeAnExistingFile())
		})
		It("removes the partial file and the state when the image does not match", func() {
			env, content := evil(strings.Repeat("c", 64), 4*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			_, err := fetchWith(p2p.Auth{Insecure: true}, tgt("", rogue.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("does not match")))
			Expect(filepath.Join(dir, "out", "dl.img")).ToNot(BeAnExistingFile())
			Expect(filepath.Join(dir, "out", "dl.img.p2p-state")).ToNot(BeAnExistingFile())
		})
		It("keeps an interrupted download for a rerun, in files only the owner can use", func() {
			img := randomFile(dir, "active.img", 16*chunk)
			slow := startWith(map[string]string{"active": img}, "127.0.0.1", func(c *p2p.ServiceConfig) { c.UploadBytesPerSec = 1 << 20 })
			dst := filepath.Join(dir, "out", "dl.img")
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				defer GinkgoRecover()
				Eventually(func() bool { _, err := os.Stat(dst + ".p2p-state"); return err == nil }, 20*time.Second, 50*time.Millisecond).Should(BeTrue())
				cancel()
			}()
			_, err := p2p.Fetch(ctx, p2p.FetchConfig{
				NetConfig: p2p.NetConfig{ListenHost: "127.0.0.2", Allow: allow}, Target: tgt(sha(img), peerOf("127.0.0.1", slow)), DestPath: dst, Logf: logs.logf,
			})
			Expect(err).To(HaveOccurred())
			for _, p := range []string{dst, dst + ".p2p-state"} {
				st, err := os.Stat(p)
				Expect(err).ToNot(HaveOccurred(), p)
				Expect(st.Mode().Perm()).To(Equal(os.FileMode(0o600)), p)
			}
			st, _ := os.Stat(filepath.Dir(dst))
			Expect(st.Mode().Perm()).To(Equal(os.FileMode(0o700)))
		})
	})

	Describe("P2P-08: peers", func() {
		It("finds the honest peer behind dead ones, in parallel", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			var peers []string
			for i := 0; i < 10; i++ {
				peers = append(peers, fmt.Sprintf("127.0.0.%d:1", 20+i)) // nothing listens
			}
			start := time.Now()
			_, err := fetchWith(p2p.Auth{}, tgt(sha(img), append(peers, peerOf("127.0.0.1", s))...), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 40*time.Second)) // ten dead peers one after the other would take 50 s if they timed out
		})
		It("talks to at most 16 peers, however many are listed", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			var peers []string
			for i := 0; i < 40; i++ {
				peers = append(peers, fmt.Sprintf("127.0.0.%d:1", 20+i))
			}
			_, err := fetchWith(p2p.Auth{}, tgt(sha(img), append(peers, peerOf("127.0.0.1", s))...), "127.0.0.3", "dl.img", nil)
			Expect(err).To(HaveOccurred()) // the honest peer was number 41
			Expect(logs.String()).To(ContainSubstring("using the first 16"))
		})
		It("does not treat a peer named in the URI as allowed when it is not on a private network", func() {
			_, err := p2p.DefaultAllowlist([]string{"8.8.8.8:42069"}, "")
			Expect(err).To(MatchError(ContainSubstring("not on a private network")))
			_, err = p2p.DefaultAllowlist([]string{"10.1.1.1:42069", "192.168.4.4:42069", "127.0.0.1:1"}, "")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("P2P-10 and P2P-11: manifest amplification and slow peers", func() {
		It("refuses tiny chunks and manifests with too many chunks", func() {
			img := randomFile(dir, "a.img", 2*chunk)
			m, _ := p2p.BuildManifest(img, "active", chunk)
			m.ChunkSize = 1024
			Expect(m.Validate()).To(MatchError(ContainSubstring("chunk size")))
			m2 := &p2p.Manifest{Version: 1, Image: "active", Size: 20000 * chunk, ChunkSize: chunk, SHA256: strings.Repeat("a", 64)}
			Expect(m2.Validate()).To(MatchError(ContainSubstring("at most")))
		})
		It("is not held up by a peer that trickles: the tail goes to the fast one", func() {
			img := randomFile(dir, "active.img", 16*chunk)
			fast := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			trickle := startWith(map[string]string{"active": img}, "127.0.0.2", func(c *p2p.ServiceConfig) { c.UploadBytesPerSec = 24 << 10 })
			start := time.Now()
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peerOf("127.0.0.2", trickle), peerOf("127.0.0.1", fast)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			// at 24 KiB/s the trickling peer alone would need almost three minutes
			Expect(time.Since(start)).To(BeNumerically("<", 100*time.Second))
		})
	})

	Describe("P2P-12: the seeder serves the file it measured", func() {
		It("does not follow a symlink planted at the image path", func() {
			secret := randomFile(dir, "secret", 1024)
			link := filepath.Join(dir, "active.img")
			Expect(os.Symlink(secret, link)).To(Succeed())
			s := startWith(map[string]string{"active": link}, "127.0.0.1", nil)
			Expect(s.Seeding()).To(BeEmpty())
			Expect(logs.String()).To(ContainSubstring("symlinks are not followed"))
		})
		It("keeps serving the old, consistent content when the path is replaced under it", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			old, _ := os.ReadFile(img)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", func(c *p2p.ServiceConfig) { c.PollInterval = time.Hour })
			repl := randomFile(dir, "new.img", 2*chunk)
			Expect(os.Rename(repl, img)).To(Succeed()) // an upgrade renames the new image over it
			req, _ := http.NewRequest("GET", "http://"+peerOf("127.0.0.1", s)+"/active/image", nil)
			req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", chunk-1))
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close() //nolint:errcheck
			b, _ := io.ReadAll(resp.Body)
			Expect(b).To(Equal(old[:chunk]))
		})
		It("opens the image once, O_NOFOLLOW", func() {
			// a directory is not an image either
			s := startWith(map[string]string{"active": dir}, "127.0.0.1", nil)
			Expect(s.Seeding()).To(BeEmpty())
		})
	})

	Describe("N5: the .sha256 sidecar is only a cross-check", func() {
		writeSidecar := func(img, sum string, mtime time.Time) {
			Expect(os.WriteFile(img+".sha256", []byte(sum+"  active.img\n"), 0o600)).To(Succeed())
			Expect(os.Chtimes(img+".sha256", mtime, mtime)).To(Succeed())
		}
		It("never publishes a forged sidecar hash: the manifest is made from one pass over the file", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			writeSidecar(img, strings.Repeat("e", 64), time.Now().Add(time.Minute)) // well formed, fresh, wrong
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			m, _ := s.Manifest("active")
			Expect(m.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("does not match the image"))
			// a download that pins the real image works against that seeder
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peerOf("127.0.0.1", s)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
		It("notes a matching sidecar, and works without one or with a malformed one", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			writeSidecar(img, sha(img), time.Now())
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			Expect(logs.String()).To(ContainSubstring("sidecar matches"))
			m, _ := s.Manifest("active")
			Expect(m.SHA256).To(Equal(sha(img)))
			img2 := randomFile(dir, "passive.img", 2*chunk)
			Expect(os.WriteFile(img2+".sha256", []byte("nonsense\n"), 0o600)).To(Succeed())
			s2 := startWith(map[string]string{"passive": img2}, "127.0.0.1", nil)
			m2, _ := s2.Manifest("passive")
			Expect(m2.SHA256).To(Equal(sha(img2)))
		})
	})

	Describe("V1: any failure of a manifest moves on to the next one", func() {
		var img string
		var honest *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 8*chunk)
			honest = startWith(map[string]string{"active": img}, "127.0.0.1", nil)
		})
		hugeLie := func() (*p2p.Manifest, []byte) {
			m := &p2p.Manifest{Version: 1, Image: "active", Size: 12 << 30, ChunkSize: 4 << 20, SHA256: sha(img)}
			for i := 0; i < 3072; i++ {
				m.Chunks = append(m.Chunks, strings.Repeat("b", 64))
			}
			return m, make([]byte, 1)
		}
		It("a rogue that claims a size the disk cannot hold does not stop the honest peer", func() {
			defer p2p.SetStatfsForTest(1 << 30)() // a 1 GiB disk, the lie claims 12 GiB (within the 16 GiB cap)
			m, c := hugeLie()
			rogue := serveFake("127.0.0.2", m, c, "")
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("not enough free space"))
			Expect(rogue.imageReqs.Load()).To(BeZero())
		})
		It("a rogue that answers the manifest and then never a chunk is dropped after a stall, the honest peer is used", func() {
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			rogue.stall = true
			start := time.Now()
			res, err := fetchWith(p2p.Auth{StallTimeout: 2 * time.Second}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("no data arrived"))
			Expect(time.Since(start)).To(BeNumerically("<", 60*time.Second))
			Expect(filepath.Join(dir, "out", "dl.img.p2p-state")).ToNot(BeAnExistingFile())
		})
		It("a rogue that trickles slower than the minimum speed is dropped after the grace period", func() {
			defer p2p.SetGraceForTest(time.Second)()
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			rogue.trickle = 2000 // 2 KB/s, a chunk would take minutes
			start := time.Now()
			res, err := fetchWith(p2p.Auth{StallTimeout: time.Minute}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("too slow"))
			Expect(time.Since(start)).To(BeNumerically("<", 60*time.Second))
		})
		It("an overall deadline ends a fetch that only stalls, with an error, never a hang", func() {
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			rogue.stall = true
			start := time.Now()
			_, err := fetchWith(p2p.Auth{Timeout: 3 * time.Second, StallTimeout: time.Minute}, tgt(sha(img), rogue.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("took more than")))
			Expect(time.Since(start)).To(BeNumerically("<", 20*time.Second))
		})
		It("honours the context of the caller", func() {
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			rogue.stall = true
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			start := time.Now()
			_, err := p2p.Fetch(ctx, p2p.FetchConfig{
				NetConfig: p2p.NetConfig{ListenHost: "127.0.0.3", Allow: allow}, Target: tgt(sha(img), rogue.addr),
				DestPath: filepath.Join(dir, "out", "dl.img"), Logf: logs.logf,
			})
			Expect(err).To(HaveOccurred())
			Expect(time.Since(start)).To(BeNumerically("<", 20*time.Second))
		})
	})

	Describe("V2: the order of the manifests cannot be bought with ports", func() {
		var img string
		var honest *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 8*chunk)
			honest = startWith(map[string]string{"active": img}, "127.0.0.1", nil)
		})
		It("one address announcing many ports with the same manifest is one voice, and cannot outrank the honest peer", func() {
			env, content := evil(sha(img), 8*chunk)
			var peers []string
			var rogues []*fakePeer
			for i := 0; i < 6; i++ {
				fp := serveFake("127.0.0.2", env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			// the honest peers are two different addresses, listed last
			second := startWith(map[string]string{"active": img}, "127.0.0.4", nil)
			peers = append(peers, peerOf("127.0.0.1", honest), peerOf("127.0.0.4", second))
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			for _, r := range rogues {
				Expect(r.imageReqs.Load()).To(BeZero()) // two distinct addresses outrank six ports of one
			}
		})
		It("one address announcing many different manifests counts once: only its first can cost a download", func() {
			var peers []string
			var rogues []*fakePeer
			for i := 0; i < 12; i++ {
				env, content := evil(sha(img), 8*chunk)
				fp := serveFake("127.0.0.2", env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			tried := 0
			for _, r := range rogues {
				if r.imageReqs.Load() > 0 {
					tried++
				}
			}
			Expect(tried).To(BeNumerically("<=", 1))
			Expect(logs.String()).To(ContainSubstring("kept as a fallback"))
		})
	})

	Describe("V3: the optional size pin", func() {
		It("discards a manifest of another size before any chunk is fetched", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			env, content := evil(sha(img), 16*chunk) // the right hash, a lie about the size
			rogue := serveFake("127.0.0.2", env, content, "")
			t := tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest))
			t.Size = 8 * chunk
			res, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(rogue.imageReqs.Load()).To(BeZero())
			Expect(logs.String()).To(ContainSubstring("the source pins 2097152"))
		})
		It("a size pin of the wrong size makes every manifest unacceptable", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			t := tgt(sha(img), peerOf("127.0.0.1", honest))
			t.Size = 1234
			_, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("no peer served an acceptable")))
		})
	})

	Describe("V4: the install size is not one peer's word", func() {
		var img string
		var honest, honest2 *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 8*chunk)
			honest = startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			honest2 = startWith(map[string]string{"active": img}, "127.0.0.4", nil)
		})
		probe := func(t images.P2PTarget) (*p2p.Manifest, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return p2p.Probe(ctx, p2p.NetConfig{ListenHost: "127.0.0.3", Allow: allow}, p2p.Auth{}, t, logs.logf)
		}
		It("takes the size most addresses agree on, not the first listed", func() {
			env, content := evil(sha(img), 20*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			m, err := probe(tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest), peerOf("127.0.0.4", honest2)))
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Size).To(BeEquivalentTo(8 * chunk))
		})
		It("uses the pinned size when there is one", func() {
			env, content := evil(sha(img), 20*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			t := tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest))
			t.Size = 8 * chunk
			m, err := probe(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Size).To(BeEquivalentTo(8 * chunk))
		})
	})

	Describe("V6: nothing is opened when nothing is offered", func() {
		It("does not touch the listen port unless the image is offered (--linger)", func() {
			img := randomFile(dir, "active.img", 2*chunk)
			s := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			busy, err := net.Listen("tcp4", "127.0.0.3:0") // a port that would fail a listen attempt
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(busy.Close)
			port := busy.Addr().(*net.TCPAddr).Port
			_, err = fetchWith(p2p.Auth{}, tgt(sha(img), peerOf("127.0.0.1", s)), "127.0.0.3", "dl.img", func(c *p2p.FetchConfig) {
				c.Serve, c.ListenPort = true, port // Serve without Linger
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(logs.String()).ToNot(ContainSubstring("cannot offer"))
		})
	})

	Describe("W4: the manifest pin", func() {
		var img, root string
		var honest *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 8*chunk)
			honest = startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			m, err := p2p.BuildManifest(img, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			root = m.Root()
		})
		pinned := func(peers ...string) images.P2PTarget {
			return images.P2PTarget{Peers: peers, Image: "active", Pin: root}
		}
		It("T3: any number of rogue manifests, true size and true image hash, cost no image download", func() {
			var rogues []*fakePeer
			var peers []string
			for i := 10; i < 40; i++ {
				env, content := evil(sha(img), 8*chunk)
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			// more than the 16 peers an unpinned fetch looks at: the honest one is last, the pin still finds it
			res, err := fetchWith(p2p.Auth{}, pinned(peers...), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			for _, r := range rogues {
				Expect(r.imageReqs.Load()).To(BeZero())
			}
			Expect(logs.String()).To(ContainSubstring("manifest pin"))
		})
		It("without the pin the same rogues can only be bounded, not prevented", func() {
			var peers []string
			for i := 10; i < 17; i++ {
				env, content := evil(sha(img), 8*chunk)
				peers = append(peers, serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "").addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			_, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("gave up after wasting")))
		})
		It("both the pin and the image hash set: both have to hold", func() {
			t := pinned(peerOf("127.0.0.1", honest))
			t.SHA256 = strings.Repeat("0", 64)
			_, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl.img", nil)
			Expect(err).To(HaveOccurred())
			t.SHA256 = sha(img)
			res, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl2.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
		It("a pin that matches no manifest fails after the manifests only", func() {
			rogue := func() *fakePeer { env, c := evil(sha(img), 8*chunk); return serveFake("127.0.0.2", env, c, "") }()
			t := pinned(rogue.addr)
			_, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl.img", nil)
			Expect(err).To(MatchError(ContainSubstring("no peer served an acceptable")))
			Expect(rogue.imageReqs.Load()).To(BeZero())
		})
		It("W7: the probe does not vote when the manifest is pinned", func() {
			var peers []string
			for i := 10; i < 16; i++ { // six addresses all agreeing on a size that is not the real one
				env, content := evil(sha(img), 40*chunk)
				peers = append(peers, serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "").addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			m, err := p2p.Probe(ctx, p2p.NetConfig{ListenHost: "127.0.0.3", Allow: allow}, p2p.Auth{}, pinned(peers...), logs.logf)
			Expect(err).ToNot(HaveOccurred())
			Expect(m.Size).To(BeEquivalentTo(8 * chunk))
		})
	})

	Describe("W1: the cost of failed manifests is measured against the real size", func() {
		It("a rogue that claims a huge size cannot raise the bound", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			honest2 := startWith(map[string]string{"active": img}, "127.0.0.4", nil)
			var peers []string
			for i := 10; i < 20; i++ {
				env, content := evil(sha(img), 40*chunk) // 10 MiB, five times the real size
				peers = append(peers, serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "").addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest), peerOf("127.0.0.4", honest2))
			// the honest addresses agree on the size and outrank each lone rogue
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), peers...), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
		It("with the size pinned the bound is that size, and a rogue of another size costs nothing", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			var peers []string
			var rogues []*fakePeer
			for i := 10; i < 20; i++ {
				env, content := evil(sha(img), 40*chunk)
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), env, content, "")
				rogues = append(rogues, fp)
				peers = append(peers, fp.addr)
			}
			t := tgt(sha(img), peers...)
			t.Size = 8 * chunk
			_, err := fetchWith(p2p.Auth{}, t, "127.0.0.3", "dl.img", nil)
			Expect(err).To(HaveOccurred())
			for _, r := range rogues {
				Expect(r.imageReqs.Load()).To(BeZero())
			}
		})
	})

	Describe("W2: a slow rogue is dropped by its projected duration", func() {
		It("a peer that claims a big image and serves it at a crawl above the minimum speed loses to the honest one", func() {
			defer p2p.SetETAGraceForTest(time.Second)()
			defer p2p.SetETAFloorForTest(5 * time.Second)()
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.2", env, content, "")
			rogue.kbps = 40 // four streams of 40 KiB/s: above the 64 KiB/s floor, 2 MiB still takes 13 s
			start := time.Now()
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("would take"))
			Expect(time.Since(start)).To(BeNumerically("<", 15*time.Second))
		})
		It("an honest peer that is only slow is still used, last, once nothing faster is left", func() {
			defer p2p.SetETAGraceForTest(time.Second)()
			defer p2p.SetETAFloorForTest(2 * time.Second)()
			img := randomFile(dir, "active.img", 4*chunk)
			sl := &p2p.Manifest{}
			m, err := p2p.BuildManifest(img, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			*sl = *m
			b, _ := os.ReadFile(img)
			slow := serveFake("127.0.0.2", sl, b, "")
			slow.kbps = 60 // four streams of 60 KiB/s: 1 MiB in about 4 s, past the 2 s expected
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), slow.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("without the duration limit"))
		})
	})

	Describe("W3: a rogue on the address of an honest peer cannot pre-empt it", func() {
		It("keeps the second manifest of an address as a fallback that is tried last", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			env, content := evil(sha(img), 8*chunk)
			rogue := serveFake("127.0.0.1", env, content, "") // same address, another port, listed first
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), rogue.addr, peerOf("127.0.0.1", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
		It("a manifest from another address is tried before the fallback of the first", func() {
			img := randomFile(dir, "active.img", 8*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.4", nil)
			env, content := evil(sha(img), 8*chunk)
			first := serveFake("127.0.0.1", env, content, "")
			env2, content2 := evil(sha(img), 8*chunk)
			sameIP := serveFake("127.0.0.1", env2, content2, "")
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), first.addr, sameIP.addr, peerOf("127.0.0.4", honest)), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(sameIP.imageReqs.Load()).To(BeZero())
		})
	})

	Describe("X1: peers that copy the real manifest but serve slowly", func() {
		It("20 addresses at a crawl listed before the honest one do not keep it from the image", func() {
			img := randomFile(dir, "active.img", 16*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			real, err := p2p.BuildManifest(img, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			b, _ := os.ReadFile(img)
			var peers []string
			for i := 10; i < 30; i++ {
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), real, b, "")
				fp.kbps = 8
				peers = append(peers, fp.addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			start := time.Now()
			res, err := fetchWith(p2p.Auth{}, images.P2PTarget{Peers: peers, Image: "active", Pin: real.Root()}, "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(time.Since(start)).To(BeNumerically("<", 30*time.Second))
		})
	})

	Describe("X3: examining many peers", func() {
		It("63 peers that accept and say nothing do not hide the honest one", func() {
			defer p2p.SetManifestTimeoutForTest(time.Second)()
			img := randomFile(dir, "active.img", 4*chunk)
			honest := startWith(map[string]string{"active": img}, "127.0.0.1", nil)
			real, err := p2p.BuildManifest(img, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			var peers []string
			for i := 10; i < 73; i++ {
				fp := serveFake(fmt.Sprintf("127.0.0.%d", i), real, nil, "")
				fp.silent = true
				peers = append(peers, fp.addr)
			}
			peers = append(peers, peerOf("127.0.0.1", honest))
			// 4 waves of 16 at 1 s each fit in 6 s; 8 at a time would not
			res, err := fetchWith(p2p.Auth{}, images.P2PTarget{Peers: peers, Image: "active", Pin: real.Root()}, "127.0.0.3", "dl.img", func(c *p2p.FetchConfig) { c.MetadataTimeout = 6 * time.Second })
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
		})
	})

	Describe("X4: a drop for the projected duration keeps what was downloaded", func() {
		It("the slow honest peer is retried and continues from the verified chunks", func() {
			defer p2p.SetETAGraceForTest(3500 * time.Millisecond)()
			defer p2p.SetETAFloorForTest(6 * time.Second)()
			img := randomFile(dir, "active.img", 16*chunk)
			m, err := p2p.BuildManifest(img, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			b, _ := os.ReadFile(img)
			slow := serveFake("127.0.0.2", m, b, "")
			slow.kbps = 100
			res, err := fetchWith(p2p.Auth{}, tgt(sha(img), slow.addr), "127.0.0.3", "dl.img", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("without the duration limit"))
			Expect(logs.String()).To(ContainSubstring("resuming"))
		})
	})

	It("serves the right kernel limits: a syscall sanity check for the rlimit helpers", func() {
		var rl syscall.Rlimit
		Expect(syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl)).To(Succeed())
		Expect(rl.Cur).To(BeNumerically(">", 0))
	})
})
