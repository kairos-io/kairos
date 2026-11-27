package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// ServiceConfig configures the seeding service. It always serves up to three
// images: active, passive and recovery, whichever exist. Nothing else.
type ServiceConfig struct {
	NetConfig
	// StateDir holds cOS/active.img and cOS/passive.img.
	StateDir string
	// RecoveryDir holds cOS/recovery.img or cOS/recovery.squashfs.
	RecoveryDir string
	// Paths overrides the resolved path of an image kind (tests).
	Paths map[string]string
	// CacheDir stores computed manifests keyed by size+mtime+inode so restarts
	// do not rehash multi-GB images. Empty disables it.
	CacheDir string
	// ChunkSize overrides DefaultChunkSize (tests).
	ChunkSize int64
	// PollInterval is how often image files are re-checked (default 10s).
	PollInterval time.Duration
	// Announce publishes the node with mDNS.
	Announce bool
	// Logf receives log lines.
	Logf func(string, ...any)
}

type fileKey struct {
	Size    int64  `json:"size"`
	MtimeNs int64  `json:"mtimeNs"`
	Ino     uint64 `json:"ino"`
}

// entry is an image being served. The file is opened once, O_NOFOLLOW, and
// served from that descriptor: what is served always matches the manifest, even
// when the path is replaced (an upgrade renames a new image over it).
type entry struct {
	path string
	key  fileKey
	f    *os.File
	m    *Manifest
}

// Service seeds the node images over HTTP.
type Service struct {
	cfg ServiceConfig
	srv *Server

	mu      sync.Mutex
	entries map[string]*entry
	pending map[string]fileKey
	warned  map[string]bool
	stop    chan struct{}
	stopAnn func()
	wg      sync.WaitGroup
}

// StartService starts the listener and the watcher.
func StartService(ctx context.Context, cfg ServiceConfig) (*Service, error) {
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = images.DefaultP2PPort
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second
	}
	if cfg.ListenHost == "" {
		return nil, fmt.Errorf("a bind address is required: refusing to listen on every interface")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp4", net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.ListenPort)))
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, entries: map[string]*entry{}, pending: map[string]fileKey{}, warned: map[string]bool{}, stop: make(chan struct{})}
	s.srv = newServer(l, cfg.NetConfig, cfg.Logf)

	s.poll(true)
	if cfg.Announce {
		var err error
		if s.stopAnn, err = Announce(cfg.ListenHost, s.Port(), "seed"); err != nil {
			cfg.Logf("p2p: mDNS announce failed (static peer lists still work): %v", err)
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(cfg.PollInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.poll(false)
			case <-s.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return s, nil
}

// Port is the TCP port the service listens on.
func (s *Service) Port() int { return s.srv.Port() }

// Pins returns the sources, with their pins,, that fetch the images being served
// from this node, as seen from address host.
func (s *Service) Pins(host string) []string {
	var out []string
	for _, k := range s.Seeding() {
		if m, ok := s.Manifest(k); ok {
			out = append(out, PinURI(host, s.Port(), k, m.Root()))
		}
	}
	return out
}

// Seeding returns the kinds currently being served.
func (s *Service) Seeding() []string { return s.srv.kinds() }

// Manifest returns the manifest of a served kind.
func (s *Service) Manifest(kind string) (*Manifest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[kind]; ok {
		m := *e.m
		return &m, true
	}
	return nil, false
}

// Close stops everything.
func (s *Service) Close() error {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	s.wg.Wait()
	if s.stopAnn != nil {
		s.stopAnn()
	}
	return s.srv.Close()
}

func (s *Service) imagePath(kind string) string {
	if p, ok := s.cfg.Paths[kind]; ok {
		return p
	}
	switch kind {
	case images.P2PImageActive, images.P2PImagePassive:
		if s.cfg.StateDir == "" {
			return ""
		}
		return filepath.Join(s.cfg.StateDir, "cOS", kind+".img")
	case images.P2PImageRecovery:
		if s.cfg.RecoveryDir == "" {
			return ""
		}
		for _, n := range []string{"recovery.img", "recovery.squashfs"} {
			p := filepath.Join(s.cfg.RecoveryDir, "cOS", n)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// keyOf describes the file at path without following a symlink. A symlink, a
// directory or an empty file is not an image.
func keyOf(path string) (fileKey, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return fileKey{}, false
	}
	return keyFromInfo(path, st)
}

func keyFromInfo(path string, st os.FileInfo) (fileKey, bool) {
	k := fileKey{Size: st.Size(), MtimeNs: st.ModTime().UnixNano()}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		k.Ino = sys.Ino
	}
	return k, k.Size > 0
}

// openImage opens an image read only, refusing symlinks.
func openImage(path string) (*os.File, fileKey, error) {
	lst, err := os.Lstat(path)
	if err != nil {
		return nil, fileKey{}, err
	}
	if !lst.Mode().IsRegular() {
		return nil, fileKey{}, fmt.Errorf("%s is not a regular file (symlinks are not followed)", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fileKey{}, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !os.SameFile(lst, st) {
		_ = f.Close()
		return nil, fileKey{}, fmt.Errorf("%s changed while it was opened", path)
	}
	k, ok := keyFromInfo(path, st)
	if !ok {
		_ = f.Close()
		return nil, fileKey{}, fmt.Errorf("%s is empty", path)
	}
	return f, k, nil
}

// poll re-checks every image. A changed image is dropped at once and served
// again only after it has been stable for one full poll interval, so an
// upgrade rewriting active.img never serves half-written data.
func (s *Service) poll(initial bool) {
	for _, kind := range images.P2PImages {
		path := s.imagePath(kind)
		var key fileKey
		ok := false
		if path != "" {
			key, ok = keyOf(path)
		}
		s.mu.Lock()
		e := s.entries[kind]
		s.mu.Unlock()
		if e != nil && ok && e.path == path && e.key == key {
			continue
		}
		if e != nil {
			s.cfg.Logf("p2p: %s image changed or gone, no longer serving it", kind)
			s.srv.removeShare(kind)
			s.mu.Lock()
			delete(s.entries, kind)
			s.mu.Unlock()
			// requests that already started keep reading the old, consistent file
			time.AfterFunc(time.Minute, func() { _ = e.f.Close() })
		}
		if !ok {
			delete(s.pending, kind)
			if st, err := os.Lstat(path); path != "" && err == nil && !st.Mode().IsRegular() && !s.warned[kind] {
				s.warned[kind] = true
				s.cfg.Logf("p2p: %s: %s is not a regular file (symlinks are not followed), not serving it", kind, path)
			}
			continue
		}
		delete(s.warned, kind)
		if !initial && s.pending[kind] != key {
			s.pending[kind] = key // still changing, wait for it to settle
			continue
		}
		if err := s.seed(kind, path); err != nil {
			s.cfg.Logf("p2p: cannot serve %s: %v", kind, err)
			s.pending[kind] = fileKey{}
			continue
		}
		delete(s.pending, kind)
	}
}

type cacheFile struct {
	Key      fileKey   `json:"key"`
	Manifest *Manifest `json:"manifest"`
}

func (s *Service) loadCache(kind string, key fileKey) *Manifest {
	if s.cfg.CacheDir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.cfg.CacheDir, kind+".json"))
	if err != nil {
		return nil
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Key != key || c.Manifest == nil || c.Manifest.Validate() != nil || c.Manifest.Image != kind {
		return nil
	}
	return c.Manifest
}

func (s *Service) saveCache(kind string, key fileKey, m *Manifest) {
	if s.cfg.CacheDir == "" {
		return
	}
	if err := os.MkdirAll(s.cfg.CacheDir, 0o700); err != nil {
		return
	}
	b, _ := json.Marshal(cacheFile{Key: key, Manifest: m})
	_ = os.WriteFile(filepath.Join(s.cfg.CacheDir, kind+".json"), b, 0o600)
}

// sidecarSHA reads <image>.sha256 ("<hex>  name"), empty if absent or malformed.
func sidecarSHA(path string) string {
	b, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || !validHex256(strings.ToLower(f[0])) {
		return ""
	}
	return strings.ToLower(f[0])
}

func (s *Service) seed(kind, path string) error {
	f, key, err := openImage(path)
	if err != nil {
		return err
	}
	m := s.loadCache(kind, key)
	if m != nil {
		s.cfg.Logf("p2p: %s: manifest cache hit, not rehashing", kind)
	} else {
		s.cfg.Logf("p2p: %s: hashing %s (%d bytes)", kind, path, key.Size)
		if m, err = BuildManifestFrom(f, kind, s.cfg.ChunkSize); err != nil {
			_ = f.Close()
			return err
		}
		if k2, ok := keyOf(path); !ok || k2 != key {
			_ = f.Close()
			return fmt.Errorf("image changed while hashing")
		}
		s.saveCache(kind, key, m)
	}
	// An <image>.sha256 sidecar (written at install/upgrade time when that is
	// enabled) is only a cross-check. The manifest is made self-consistent from
	// one pass over the file; a sidecar that disagrees is a stale or forged file
	// and is never published.
	if sc := sidecarSHA(path); sc != "" {
		if sc == m.SHA256 {
			s.cfg.Logf("p2p: %s: the .sha256 sidecar matches the image", kind)
		} else {
			s.cfg.Logf("p2p: %s: WARNING the .sha256 sidecar (%s) does not match the image (%s), the computed hash is used", kind, sc, m.SHA256)
		}
	}
	s.srv.setShare(kind, m, f)
	s.mu.Lock()
	s.entries[kind] = &entry{path: path, key: key, f: f, m: m}
	s.mu.Unlock()
	s.cfg.Logf("p2p: serving %s sha256 %s (%d chunks of %d bytes)", kind, m.SHA256, m.NumChunks(), m.ChunkSize)
	return nil
}
