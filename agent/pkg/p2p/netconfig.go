package p2p

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// EnvExperimental must be "1"/"true" (or the flag / p2p-source.experimental
// set) for any p2p functionality to run.
const EnvExperimental = "KAIROS_P2P_EXPERIMENTAL"

// RequireExperimental returns an error unless the user explicitly opted in.
func RequireExperimental(flag bool) error {
	if flag {
		return nil
	}
	switch strings.ToLower(os.Getenv(EnvExperimental)) {
	case "1", "true", "yes":
		return nil
	}
	return fmt.Errorf("the p2p image source is EXPERIMENTAL and disabled by default: set p2p-source.experimental: true in the cloud-config, pass --experimental, or set %s=1", EnvExperimental)
}

// NetConfig holds the knobs shared by seeders and downloaders.
type NetConfig struct {
	// ListenHost is the local IPv4 to bind to. Empty means the first
	// non-loopback address; the agent never listens on every interface.
	ListenHost string
	// ListenPort is the TCP port; 0 picks a random one (seeder default is images.DefaultP2PPort).
	ListenPort int
	// Allow lists the CIDRs peers (in and out) must belong to. Required.
	Allow Allowlist
	// UploadBytesPerSec limits upload speed; 0 means DefaultUploadBytesPerSec.
	UploadBytesPerSec int
	// MaxPeers bounds the image streams served at the same time; 0 means 32.
	MaxPeers int
	// MaxConns bounds the open connections; 0 means 128.
	MaxConns int
	// MaxConnsPerIP bounds the open connections of one address; 0 means 8.
	MaxConnsPerIP int
}

func (n NetConfig) validate() error {
	if len(n.Allow) == 0 {
		return fmt.Errorf("p2p needs at least one allowed peer network: refusing to talk to the whole internet")
	}
	if n.ListenHost != "" && net.ParseIP(n.ListenHost) == nil {
		return fmt.Errorf("listen host %q is not an IP address", n.ListenHost)
	}
	return nil
}

func hostOf(addr string) string {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return h
}

// NetConfigFromOptions turns the p2p-source block into a NetConfig. A nil
// block means all defaults.
func NetConfigFromOptions(o *images.P2POptions) (NetConfig, error) {
	var n NetConfig
	if o == nil {
		return n, nil
	}
	allow, err := ParseAllowlist(o.Allow)
	if err != nil {
		return n, err
	}
	return NetConfig{ListenHost: o.Bind, ListenPort: o.Port, Allow: allow, UploadBytesPerSec: o.UploadLimit * 1024, MaxPeers: o.MaxPeers}, nil
}

// DefaultUploadBytesPerSec is the upload limit when none is configured.
const DefaultUploadBytesPerSec = 64 << 20

// DefaultMaxImageSize bounds the image a manifest may announce.
const DefaultMaxImageSize = 16 << 30

// Auth says what a downloader requires of a source.
type Auth struct {
	// Insecure allows a source without a ?pin= or ?sha256= pin.
	Insecure bool
	// MaxImageSize in bytes, 0 means DefaultMaxImageSize.
	MaxImageSize int64
	// Timeout bounds a whole fetch, all the manifests that are tried included.
	// 0 means DefaultFetchTimeout.
	Timeout time.Duration
	// StallTimeout is how long a download from one manifest may make no
	// progress before it is dropped. 0 means DefaultStallTimeout.
	StallTimeout time.Duration
}

// Defaults of the fetch deadlines.
const (
	DefaultFetchTimeout = 6 * time.Hour
	DefaultStallTimeout = 60 * time.Second
)

// AuthFromOptions builds the Auth of the p2p-source block.
func AuthFromOptions(o *images.P2POptions) (Auth, error) {
	var a Auth
	if o == nil {
		return a, nil
	}
	a.Insecure = o.Insecure
	a.MaxImageSize = int64(o.MaxImageSize) << 20
	a.Timeout = time.Duration(o.Timeout) * time.Minute
	return a, nil
}

// CheckTarget refuses, before any peer is contacted, a source without a pin
// unless the explicit insecure opt in is set: any host on the network could
// otherwise serve an image that then runs as root on this node.
func (a Auth) CheckTarget(t images.P2PTarget) error {
	if a.Insecure || t.SHA256 != "" || t.Pin != "" {
		return nil
	}
	return fmt.Errorf("refusing p2p source %s: it has no pin, so any host on the network could make this node install anything. "+
		"Use the source kairos agent p2p seed prints (it carries ?pin=), or add ?sha256=<sha256 of the image>, or (dangerous) set p2p-source.insecure: true", t.String())
}

// PinURI returns the source that fetches an image served at host:port, with
// the pin of its manifest. The default port is left out.
func PinURI(host string, port int, kind, pin string) string {
	peer := host
	if port != images.DefaultP2PPort {
		peer = net.JoinHostPort(host, strconv.Itoa(port))
	}
	return fmt.Sprintf("p2p://%s/%s?pin=%s", peer, kind, pin)
}
