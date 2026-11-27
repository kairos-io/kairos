package p2p_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const chunk = 256 << 10

func randomFile(dir, name string, size int) string {
	b := make([]byte, size)
	_, err := rand.Read(b)
	Expect(err).ToNot(HaveOccurred())
	p := filepath.Join(dir, name)
	Expect(os.WriteFile(p, b, 0o600)).To(Succeed())
	return p
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	defer l.Close() //nolint:errcheck
	return l.Addr().(*net.TCPAddr).Port
}

// startRetry starts a service on a port freePort found. The port can be taken
// between the two calls (an ephemeral port of some other test connection), so
// an "address already in use" gets a fresh port, a few times.
func startRetry(cfg p2p.ServiceConfig) (*p2p.Service, error) {
	var s *p2p.Service
	var err error
	for i := 0; i < 5; i++ {
		if s, err = p2p.StartService(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "address already in use") {
			return s, err
		}
		cfg.ListenPort = freePort()
	}
	return s, err
}

func mustCIDR(c string) *net.IPNet {
	_, n, err := net.ParseCIDR(c)
	Expect(err).ToNot(HaveOccurred())
	return n
}

type logBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuf) logf(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, f+"\n", a...)
}
func (l *logBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }
func (l *logBuf) Reset()         { l.mu.Lock(); defer l.mu.Unlock(); l.b.Reset() }

var _ = Describe("experimental gate", func() {
	It("refuses to run without opt in", func() {
		GinkgoT().Setenv(p2p.EnvExperimental, "")
		Expect(p2p.RequireExperimental(false)).To(MatchError(ContainSubstring("EXPERIMENTAL")))
	})
	It("accepts the flag or the env var", func() {
		Expect(p2p.RequireExperimental(true)).To(Succeed())
		GinkgoT().Setenv(p2p.EnvExperimental, "1")
		Expect(p2p.RequireExperimental(false)).To(Succeed())
	})
})

var _ = Describe("allowlist", func() {
	It("allows only listed networks", func() {
		a, err := p2p.ParseAllowlist([]string{"10.0.0.0/24", "192.168.1.5"})
		Expect(err).ToNot(HaveOccurred())
		Expect(a.Allows(net.ParseIP("10.0.0.7"))).To(BeTrue())
		Expect(a.Allows(net.ParseIP("192.168.1.5"))).To(BeTrue())
		Expect(a.Allows(net.ParseIP("192.168.1.6"))).To(BeFalse())
		Expect(a.Allows(net.ParseIP("8.8.8.8"))).To(BeFalse())
	})
	It("rejects garbage and an empty list allows nothing", func() {
		_, err := p2p.ParseAllowlist([]string{"nope/33"})
		Expect(err).To(HaveOccurred())
		Expect(p2p.Allowlist{}.Allows(net.ParseIP("10.0.0.1"))).To(BeFalse())
	})
	It("defaults to the listed peers, or the local subnet when discovering", func() {
		a, err := p2p.DefaultAllowlist([]string{"10.1.2.3:42069", "10.1.2.4:42069"}, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(a.Allows(net.ParseIP("10.1.2.3"))).To(BeTrue())
		Expect(a.Allows(net.ParseIP("10.1.2.5"))).To(BeFalse())
		a, err = p2p.DefaultAllowlist(nil, "127.0.0.1")
		Expect(err).ToNot(HaveOccurred())
		Expect(a.Allows(net.ParseIP("127.0.0.9"))).To(BeTrue())
		Expect(a.Allows(net.ParseIP("10.0.0.1"))).To(BeFalse())
		_, err = p2p.DefaultAllowlist(nil, "203.0.113.77")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("manifest", func() {
	var dir string
	BeforeEach(func() { dir = GinkgoT().TempDir() })

	It("hashes every chunk and the whole image in one pass", func() {
		p := randomFile(dir, "active.img", 3*chunk+100)
		m, err := p2p.BuildManifest(p, "active", chunk)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Size).To(BeEquivalentTo(3*chunk + 100))
		Expect(m.NumChunks()).To(Equal(4))
		Expect(m.ChunkLen(0)).To(BeEquivalentTo(chunk))
		Expect(m.ChunkLen(3)).To(BeEquivalentTo(100))
		whole, _ := p2p.SHA256File(p)
		Expect(m.SHA256).To(Equal(whole))
		m2, _ := p2p.BuildManifest(p, "active", chunk)
		Expect(m2.ID()).To(Equal(m.ID()))
	})
	It("has an id that follows the content", func() {
		p := randomFile(dir, "active.img", 2*chunk)
		m1, _ := p2p.BuildManifest(p, "active", chunk)
		m2, _ := p2p.BuildManifest(p, "active", chunk)
		Expect(m2.ID()).To(Equal(m1.ID()))
		for _, mut := range []func(*p2p.Manifest){
			func(m *p2p.Manifest) { m.Chunks[1] = strings.Repeat("0", 64) },
			func(m *p2p.Manifest) { m.Image = "passive" },
			func(m *p2p.Manifest) { m.Size-- },
			func(m *p2p.Manifest) { m.SHA256 = strings.Repeat("1", 64) },
		} {
			c := *m1
			c.Chunks = append([]string{}, m1.Chunks...)
			mut(&c)
			Expect(c.ID()).ToNot(Equal(m1.ID()))
		}
	})
	It("uses 4 MiB chunks by default", func() {
		p := randomFile(dir, "active.img", 9<<20)
		m, err := p2p.BuildManifest(p, "active", 0)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.ChunkSize).To(BeEquivalentTo(4 << 20))
		Expect(m.NumChunks()).To(Equal(3))
	})
	It("refuses empty files and directories", func() {
		_, err := p2p.BuildManifest(dir, "active", 0)
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, "e"), nil, 0o600)).To(Succeed())
		_, err = p2p.BuildManifest(filepath.Join(dir, "e"), "active", 0)
		Expect(err).To(HaveOccurred())
	})
	DescribeTable("validates what a peer sends",
		func(mut func(m *p2p.Manifest), ok bool) {
			p := randomFile(GinkgoT().TempDir(), "a.img", 2*chunk)
			m, err := p2p.BuildManifest(p, "active", chunk)
			Expect(err).ToNot(HaveOccurred())
			mut(m)
			if ok {
				Expect(m.Validate()).To(Succeed())
			} else {
				Expect(m.Validate()).ToNot(Succeed())
			}
		},
		Entry("untouched", func(*p2p.Manifest) {}, true),
		Entry("bad version", func(m *p2p.Manifest) { m.Version = 2 }, false),
		Entry("unknown image", func(m *p2p.Manifest) { m.Image = "persistent" }, false),
		Entry("zero size", func(m *p2p.Manifest) { m.Size = 0 }, false),
		Entry("huge size", func(m *p2p.Manifest) { m.Size = 1 << 50 }, false),
		Entry("tiny chunk size", func(m *p2p.Manifest) { m.ChunkSize = 1 }, false),
		Entry("chunk count mismatch", func(m *p2p.Manifest) { m.Chunks = m.Chunks[:1] }, false),
		Entry("bad chunk hash", func(m *p2p.Manifest) { m.Chunks[0] = "zz" }, false),
		Entry("upper case hash", func(m *p2p.Manifest) { m.Chunks[0] = strings.ToUpper(m.Chunks[0]) }, false),
		Entry("bad image hash", func(m *p2p.Manifest) { m.SHA256 = "abc" }, false),
	)
})

var _ = Describe("pin sources", func() {
	It("leaves the default port out and keeps another one", func() {
		root := strings.Repeat("ab", 32)
		Expect(p2p.PinURI("10.0.0.2", 42069, "active", root)).To(Equal("p2p://10.0.0.2/active?pin=" + root))
		Expect(p2p.PinURI("10.0.0.2", 4000, "passive", root)).To(Equal("p2p://10.0.0.2:4000/passive?pin=" + root))
		t, err := images.ParseP2PURI(p2p.PinURI("10.0.0.2", 4000, "passive", root))
		Expect(err).ToNot(HaveOccurred())
		Expect(t.Peers).To(Equal([]string{"10.0.0.2:4000"}))
		Expect(t.Pin).To(Equal(root))
	})
	It("derives the root from everything a manifest says", func() {
		m := &p2p.Manifest{Version: 1, Image: "active", Size: 8 << 20, ChunkSize: 4 << 20, SHA256: strings.Repeat("11", 32),
			Chunks: []string{strings.Repeat("22", 32), strings.Repeat("33", 32)}}
		root := m.Root()
		Expect(root).To(HaveLen(64))
		for name, mut := range map[string]func(*p2p.Manifest){
			"image":       func(m *p2p.Manifest) { m.Image = "passive" },
			"size":        func(m *p2p.Manifest) { m.Size++ },
			"chunk size":  func(m *p2p.Manifest) { m.ChunkSize *= 2 },
			"sha256":      func(m *p2p.Manifest) { m.SHA256 = strings.Repeat("12", 32) },
			"chunk hash":  func(m *p2p.Manifest) { m.Chunks[1] = strings.Repeat("34", 32) },
			"chunk order": func(m *p2p.Manifest) { m.Chunks[0], m.Chunks[1] = m.Chunks[1], m.Chunks[0] },
		} {
			c := *m
			c.Chunks = append([]string(nil), m.Chunks...)
			mut(&c)
			Expect(c.Root()).ToNot(Equal(root), name)
		}
	})
})

var _ = Describe("bitmap", func() {
	It("round trips and rejects a wrong length", func() {
		b := p2p.NewBitmap(10)
		b.Set(0)
		b.Set(9)
		Expect(b.Count()).To(Equal(2))
		d, err := p2p.DecodeBitmap(b.Encode(), 10)
		Expect(err).ToNot(HaveOccurred())
		Expect(d.Has(0) && d.Has(9) && !d.Has(5)).To(BeTrue())
		_, err = p2p.DecodeBitmap(b.Encode(), 100)
		Expect(err).To(HaveOccurred())
		b.Clear(0)
		Expect(b.Has(0)).To(BeFalse())
		Expect(b.Has(-1) || b.Has(10)).To(BeFalse())
	})
})

var _ = Describe("seeding service and fetching over loopback", func() {
	var dir string
	var allow p2p.Allowlist
	var logs *logBuf
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		var err error
		allow, err = p2p.ParseAllowlist([]string{"127.0.0.0/8"})
		Expect(err).ToNot(HaveOccurred())
		logs = &logBuf{}
	})

	startOpts := func(paths map[string]string, host string, a p2p.Allowlist, cache string, upload int) *p2p.Service {
		s, err := startRetry(p2p.ServiceConfig{
			NetConfig: p2p.NetConfig{ListenHost: host, ListenPort: freePort(), Allow: a, UploadBytesPerSec: upload},
			Paths:     paths, CacheDir: cache, ChunkSize: chunk, PollInterval: 200 * time.Millisecond, Logf: logs.logf,
		})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(s.Close)
		return s
	}
	start := func(paths map[string]string, host string) *p2p.Service {
		return startOpts(paths, host, allow, "", 0)
	}
	peer := func(s *p2p.Service) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port())) }
	target := func(image, pin string, peers ...string) images.P2PTarget {
		return images.P2PTarget{Peers: peers, Image: image, SHA256: pin}
	}
	fetch := func(t images.P2PTarget, host, name string) (*p2p.Result, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return p2p.Fetch(ctx, p2p.FetchConfig{
			Auth:      p2p.Auth{Insecure: true},
			NetConfig: p2p.NetConfig{ListenHost: host, Allow: allow},
			Target:    t, DestPath: filepath.Join(dir, "out", name), Logf: logs.logf,
		})
	}
	sha := func(p string) string { s, _ := p2p.SHA256File(p); return s }

	It("serves the images that exist and nothing else", func() {
		randomFile(dir, "active.img", 1<<20)
		s := start(map[string]string{"active": filepath.Join(dir, "active.img"), "passive": filepath.Join(dir, "nope.img")}, "127.0.0.1")
		Expect(s.Seeding()).To(Equal([]string{"active"}))
	})

	It("downloads a verified image from a seeder and reports who served what", func() {
		img := randomFile(dir, "active.img", 3*chunk+1234)
		s := start(map[string]string{"active": img}, "127.0.0.1")
		res, err := fetch(target("active", sha(img), peer(s)), "127.0.0.2", "dl.img")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.SHA256).To(Equal(sha(img)))
		Expect(res.Trust).To(Equal("image sha256 pin"))
		Expect(res.Peers).To(HaveLen(1))
		Expect(res.Peers[0].Addr).To(Equal("127.0.0.1"))
		Expect(res.Peers[0].Bytes).To(BeEquivalentTo(3*chunk + 1234))
		Expect(res.Peers[0].Chunks).To(Equal(4))
		got, _ := os.ReadFile(filepath.Join(dir, "out", "dl.img"))
		want, _ := os.ReadFile(img)
		Expect(got).To(Equal(want))
		Expect(filepath.Join(dir, "out", "dl.img.p2p-state")).ToNot(BeAnExistingFile())
	})

	It("with the insecure opt in and no pin, works and warns loudly", func() {
		img := randomFile(dir, "active.img", chunk)
		s := start(map[string]string{"active": img}, "127.0.0.1")
		res, err := fetch(target("active", "", peer(s)), "127.0.0.2", "dl.img")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Trust).To(Equal("none (insecure mode)"))
		Expect(logs.String()).To(ContainSubstring("WARNING insecure mode"))
	})

	It("fails when the pin contradicts what the peers serve", func() {
		img := randomFile(dir, "active.img", chunk)
		s := start(map[string]string{"active": img}, "127.0.0.1")
		_, err := fetch(target("active", strings.Repeat("0", 63)+"a", peer(s)), "127.0.0.2", "dl.img")
		Expect(err).To(MatchError(ContainSubstring("pins")))
		Expect(filepath.Join(dir, "out", "dl.img")).ToNot(BeAnExistingFile())
	})

	It("takes the manifest from the next peer when the first one serves another image", func() {
		good := randomFile(dir, "good.img", 2*chunk)
		bad := randomFile(dir, "bad.img", 2*chunk)
		sGood := start(map[string]string{"active": good}, "127.0.0.1")
		sBad := start(map[string]string{"active": bad}, "127.0.0.2")
		res, err := fetch(target("active", sha(good), net.JoinHostPort("127.0.0.2", strconv.Itoa(sBad.Port())), peer(sGood)), "127.0.0.3", "dl.img")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.SHA256).To(Equal(sha(good)))
	})

	Describe("the HTTP endpoints", func() {
		var s *p2p.Service
		var img string
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 3*chunk)
			s = start(map[string]string{"active": img, "passive": filepath.Join(dir, "missing")}, "127.0.0.1")
		})
		do := func(method, path string, hdr map[string]string) (*http.Response, []byte) {
			req, _ := http.NewRequest(method, "http://"+peer(s)+path, nil)
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			Expect(err).ToNot(HaveOccurred())
			defer resp.Body.Close() //nolint:errcheck
			b, _ := io.ReadAll(resp.Body)
			return resp, b
		}
		DescribeTable("only serves what is shared, read only",
			func(method, path string, status int) {
				resp, _ := do(method, path, nil)
				Expect(resp.StatusCode).To(Equal(status))
			},
			Entry("manifest", "GET", "/active/manifest", 200),
			Entry("have", "GET", "/active/have", 200),
			Entry("pin", "GET", "/active/pin", 200),
			Entry("whole complete image", "GET", "/active/image", 200),
			Entry("passive that does not exist", "GET", "/passive/manifest", 404),
			Entry("recovery not configured", "GET", "/recovery/image", 404),
			Entry("unknown image", "GET", "/persistent/image", 404),
			Entry("arbitrary path", "GET", "/etc/passwd", 404),
			Entry("traversal", "GET", "/../active/image", 404),
			Entry("unknown action", "GET", "/active/secrets", 404),
			Entry("POST", "POST", "/active/image", 405),
		)
		It("serves exact byte ranges, and refuses bad ones", func() {
			want, _ := os.ReadFile(img)
			resp, b := do("GET", "/active/image", map[string]string{"Range": "bytes=10-299"})
			Expect(resp.StatusCode).To(Equal(206))
			Expect(resp.Header.Get("Content-Range")).To(Equal(fmt.Sprintf("bytes 10-299/%d", 3*chunk)))
			Expect(b).To(Equal(want[10:300]))
			for _, r := range []string{"bytes=5-", "bytes=-5", "bytes=9-3", fmt.Sprintf("bytes=0-%d", 3*chunk), "bytes=0-1,5-6", "items=0-1"} {
				resp, _ := do("GET", "/active/image", map[string]string{"Range": r})
				Expect(resp.StatusCode).To(Equal(416), r)
			}
		})
		It("tells the operator the source with the pin, which clients never use", func() {
			_, b := do("GET", "/active/pin", nil)
			var pin struct {
				Image, SHA256, URI, Pin string
				Size                    int64
			}
			Expect(json.Unmarshal(b, &pin)).To(Succeed())
			Expect(pin.SHA256).To(Equal(sha(img)))
			Expect(pin.Image).To(Equal("active"))
			Expect(pin.Pin).To(HaveLen(64))
			Expect(pin.URI).To(Equal(fmt.Sprintf("p2p://127.0.0.1:%d/active?pin=%s", s.Port(), pin.Pin)))
			Expect(s.Pins("10.0.0.2")).To(Equal([]string{fmt.Sprintf("p2p://10.0.0.2:%d/active?pin=%s", s.Port(), pin.Pin)}))
			// what the URIs say parses back to the same pin
			t, err := images.ParseP2PURI(pin.URI)
			Expect(err).ToNot(HaveOccurred())
			Expect(t.Pin).To(Equal(pin.Pin))
		})
		It("serves the manifest the seeder computed", func() {
			_, b := do("GET", "/active/manifest", nil)
			var m p2p.Manifest
			Expect(json.Unmarshal(b, &m)).To(Succeed())
			Expect(m.SHA256).To(Equal(sha(img)))
			Expect(m.Validate()).To(Succeed())
			_, b = do("GET", "/active/have", nil)
			Expect(string(b)).To(ContainSubstring(`"chunks":3`))
		})
	})

	It("drops connections from addresses outside the allowlist", func() {
		img := randomFile(dir, "active.img", chunk)
		only2, _ := p2p.ParseAllowlist([]string{"127.0.0.2"})
		s := startOpts(map[string]string{"active": img}, "127.0.0.1", only2, "", 0)
		c := &http.Client{Timeout: 3 * time.Second}
		_, err := c.Get("http://" + peer(s) + "/active/manifest") // from 127.0.0.1
		Expect(err).To(HaveOccurred())
		Expect(logs.String()).To(ContainSubstring("not in allowlist"))
	})

	It("refuses peers outside the allowlist on the client side", func() {
		img := randomFile(dir, "active.img", chunk)
		s := start(map[string]string{"active": img}, "127.0.0.1")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := p2p.Fetch(ctx, p2p.FetchConfig{
			Auth:      p2p.Auth{Insecure: true},
			NetConfig: p2p.NetConfig{ListenHost: "127.0.0.2", Allow: p2p.Allowlist{mustCIDR("203.0.113.0/24")}},
			Target:    target("active", "", peer(s)), DestPath: filepath.Join(dir, "x.img"),
		})
		Expect(err).To(MatchError(ContainSubstring("allowlist")))
	})

	It("refuses to start without an allowlist or a bind address", func() {
		_, err := p2p.StartService(context.Background(), p2p.ServiceConfig{NetConfig: p2p.NetConfig{ListenHost: "127.0.0.1"}})
		Expect(err).To(MatchError(ContainSubstring("allowed peer network")))
		_, err = p2p.StartService(context.Background(), p2p.ServiceConfig{NetConfig: p2p.NetConfig{Allow: allow}})
		Expect(err).To(MatchError(ContainSubstring("bind address")))
	})

	It("serves the new content once an image changed, and stops when it disappears", func() {
		img := randomFile(dir, "active.img", chunk)
		s := start(map[string]string{"active": img}, "127.0.0.1")
		m1, _ := s.Manifest("active")
		randomFile(dir, "active.img", 2*chunk) // an "upgrade" rewrites the image
		Eventually(func() string {
			m, _ := s.Manifest("active")
			if m == nil {
				return ""
			}
			return m.SHA256
		}, 10*time.Second, 100*time.Millisecond).
			ShouldNot(Or(Equal(m1.SHA256), BeEmpty()))
		res, err := fetch(target("active", sha(img), peer(s)), "127.0.0.2", "dl.img")
		Expect(err).ToNot(HaveOccurred())
		Expect(res.Size).To(BeEquivalentTo(2 * chunk))
		Expect(os.Remove(img)).To(Succeed())
		Eventually(s.Seeding, 10*time.Second, 100*time.Millisecond).Should(BeEmpty())
		Expect(logs.String()).To(ContainSubstring("changed or gone"))
	})

	It("does not rehash an unchanged image after a restart (manifest cache)", func() {
		img := randomFile(dir, "active.img", 2*chunk)
		cache := filepath.Join(dir, "cache")
		s1 := startOpts(map[string]string{"active": img}, "127.0.0.1", allow, cache, 0)
		m1, _ := s1.Manifest("active")
		Expect(s1.Close()).To(Succeed())
		Expect(logs.String()).To(ContainSubstring("hashing"))
		Expect(logs.String()).ToNot(ContainSubstring("cache hit"))
		s2 := startOpts(map[string]string{"active": img}, "127.0.0.1", allow, cache, 0)
		Expect(logs.String()).To(ContainSubstring("cache hit"))
		m2, _ := s2.Manifest("active")
		Expect(m2.ID()).To(Equal(m1.ID()))
		// a touched image invalidates the cache
		Expect(os.Chtimes(img, time.Now(), time.Now().Add(time.Hour))).To(Succeed())
		Expect(s2.Close()).To(Succeed())
		logs.Reset()
		startOpts(map[string]string{"active": img}, "127.0.0.1", allow, cache, 0)
		Expect(logs.String()).ToNot(ContainSubstring("cache hit"))
		Expect(logs.String()).To(ContainSubstring("hashing"))
	})

	Describe("with misbehaving peers", func() {
		var img string
		var seed *p2p.Service
		BeforeEach(func() {
			img = randomFile(dir, "active.img", 16*chunk)
			seed = start(map[string]string{"active": img}, "127.0.0.1")
		})
		// rogue proxies the real seeder but lets mutate decide what /image returns.
		rogue := func(host string, mutate func(n int64, w http.ResponseWriter, r *http.Request) bool) string {
			var n atomic.Int64
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/image") {
					if mutate(n.Add(1), w, r) {
						return
					}
				}
				req, _ := http.NewRequest(r.Method, "http://"+peer(seed)+r.URL.Path, nil)
				req.Header = r.Header.Clone()
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					w.WriteHeader(502)
					return
				}
				defer resp.Body.Close() //nolint:errcheck
				for k, v := range resp.Header {
					w.Header()[k] = v
				}
				w.WriteHeader(resp.StatusCode)
				_, _ = io.Copy(w, resp.Body)
			})
			l, err := net.Listen("tcp4", host+":0")
			Expect(err).ToNot(HaveOccurred())
			srv := &http.Server{Handler: h}
			go srv.Serve(l) //nolint:errcheck
			DeferCleanup(srv.Close)
			return l.Addr().String()
		}

		It("retries a corrupt chunk from another peer and drops the peer that served it", func() {
			bad := rogue("127.0.0.2", func(_ int64, w http.ResponseWriter, r *http.Request) bool {
				from, to := 0, 0
				_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &from, &to)
				b := make([]byte, to-from+1)
				_, _ = rand.Read(b)
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, to, 16*chunk))
				w.WriteHeader(206)
				_, _ = w.Write(b)
				return true
			})
			res, err := fetch(target("active", sha(img), bad, peer(seed)), "127.0.0.3", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			Expect(logs.String()).To(ContainSubstring("does not match the manifest"))
			Expect(logs.String()).To(ContainSubstring("dropping peer"))
			var badStat *p2p.PeerStat
			for i := range res.Peers {
				if res.Peers[i].Addr == "127.0.0.2" {
					badStat = &res.Peers[i]
				}
			}
			Expect(badStat).ToNot(BeNil())
			Expect(badStat.Dropped).To(BeTrue())
			Expect(badStat.Bytes).To(BeZero())
		})

		It("survives a peer that dies in the middle of the transfer", func() {
			dying := rogue("127.0.0.2", func(n int64, w http.ResponseWriter, r *http.Request) bool {
				if n <= 3 {
					return false // serves a few chunks properly
				}
				hj, _ := w.(http.Hijacker)
				c, _, _ := hj.Hijack()
				_ = c.Close() // then every connection is cut
				return true
			})
			res, err := fetch(target("active", sha(img), dying, peer(seed)), "127.0.0.3", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.SHA256).To(Equal(sha(img)))
			var total int64
			for _, p := range res.Peers {
				total += p.Bytes
			}
			Expect(total).To(BeEquivalentTo(16 * chunk))
		})

		It("serves from several peers at once", func() {
			s1 := startOpts(map[string]string{"active": img}, "127.0.0.1", allow, "", 1<<20)
			s2 := startOpts(map[string]string{"active": img}, "127.0.0.2", allow, "", 1<<20)
			res, err := fetch(target("active", sha(img), peer(s1), net.JoinHostPort("127.0.0.2", strconv.Itoa(s2.Port()))), "127.0.0.3", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Peers).To(HaveLen(2))
			for _, p := range res.Peers {
				Expect(p.Bytes).To(BeNumerically(">", 0), p.Addr)
			}
			GinkgoWriter.Printf("peer stats: %+v\n", res.Peers)
		})

		It("resumes a stopped download chunk by chunk", func() {
			slow := startOpts(map[string]string{"active": img}, "127.0.0.1", allow, "", 1<<20)
			dst := filepath.Join(dir, "out", "dl.img")
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				// stop the first run once a part is on disk
				Eventually(func() bool { _, err := os.Stat(dst + ".p2p-state"); return err == nil }, 20*time.Second, 50*time.Millisecond).Should(BeTrue())
				cancel()
			}()
			_, err := p2p.Fetch(ctx, p2p.FetchConfig{
				Auth:      p2p.Auth{Insecure: true},
				NetConfig: p2p.NetConfig{ListenHost: "127.0.0.2", Allow: allow},
				Target:    target("active", sha(img), peer(slow)), DestPath: dst, Logf: logs.logf,
			})
			Expect(err).To(HaveOccurred())
			Expect(dst).To(BeAnExistingFile())

			res, err := fetch(target("active", sha(img), peer(slow)), "127.0.0.2", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Resumed).To(BeNumerically(">", 0))
			Expect(res.SHA256).To(Equal(sha(img)))
			var got int64
			for _, p := range res.Peers {
				got += p.Bytes
			}
			Expect(got).To(BeNumerically("<", 16*chunk), "a resumed run must not download everything again")
			Expect(logs.String()).To(ContainSubstring("resuming"))
		})

		It("re-fetches a resumed chunk whose bytes were damaged, and ignores a state of another manifest", func() {
			dst := filepath.Join(dir, "out", "dl.img")
			Expect(os.MkdirAll(filepath.Dir(dst), 0o755)).To(Succeed())
			// a complete copy marked as done in the state, with one chunk damaged
			data, _ := os.ReadFile(img)
			data[5*chunk+7] ^= 0xff
			Expect(os.WriteFile(dst, data, 0o600)).To(Succeed())
			m, _ := seed.Manifest("active")
			bm := p2p.NewBitmap(m.NumChunks())
			for i := 0; i < m.NumChunks(); i++ {
				bm.Set(i)
			}
			st, _ := json.Marshal(map[string]any{"id": m.ID(), "size": m.Size, "chunkSize": m.ChunkSize, "done": bm.Encode()})
			Expect(os.WriteFile(dst+".p2p-state", st, 0o600)).To(Succeed())
			res, err := fetch(target("active", sha(img), peer(seed)), "127.0.0.2", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Resumed).To(Equal(m.NumChunks() - 1))
			Expect(res.SHA256).To(Equal(sha(img)))

			// a state that belongs to another manifest is not trusted
			Expect(os.WriteFile(dst+".p2p-state", []byte(`{"id":"other","size":1,"chunkSize":1,"done":""}`), 0o600)).To(Succeed())
			res, err = fetch(target("active", sha(img), peer(seed)), "127.0.0.2", "dl.img")
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Resumed).To(BeZero())
		})
	})
})

var _ = Describe("mDNS discovery", func() {
	It("announces and queries without error (skipped where multicast is not available)", func() {
		ip, err := p2p.AutoBind()
		if err != nil {
			Skip("no non-loopback interface: " + err.Error())
		}
		stop, err := p2p.Announce(ip, 42069, "seed")
		if err != nil {
			Skip("cannot announce: " + err.Error())
		}
		defer stop()
		_, err = p2p.Discover(context.Background(), ip, 2*time.Second, nil, 16)
		Expect(err).ToNot(HaveOccurred())
	})
})
