package images

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	// DefaultP2PPort is the port a p2p seeder listens on when none is named.
	DefaultP2PPort = 42069

	// The images a node shares with its peers. Nothing else is ever served.
	P2PImageActive   = "active"
	P2PImagePassive  = "passive"
	P2PImageRecovery = "recovery"
)

// P2PImages lists the image kinds a p2p source can name.
var P2PImages = []string{P2PImageActive, P2PImagePassive, P2PImageRecovery}

// ValidP2PImage tells if s names a shareable image.
func ValidP2PImage(s string) bool {
	for _, i := range P2PImages {
		if i == s {
			return true
		}
	}
	return false
}

// P2PTarget is a parsed, EXPERIMENTAL p2p image source:
//
//	p2p://peer1[:port][,peer2[:port]]/active|passive|recovery[?pin=<hex>]
//	p2p://active                       (no host: peers are found with mDNS)
//
// The image is fetched in chunks from all the listed peers at once. The
// sha256 pin is the one thing that makes a peer trustworthy: it is the sha256
// of the image the node will accept. Without it the download is refused unless
// P2POptions.Insecure is set.
type P2PTarget struct {
	// Peers are host:port pairs, the default port filled in. Empty means discovery.
	Peers []string
	// Image is one of the P2PImage constants.
	Image string
	// SHA256 is the lowercase hex pin of the whole image. Without it the
	// download is refused unless the insecure opt in is set.
	SHA256 string
	// Pin is the optional lowercase hex root of the manifest: the sha256 over the
	// image kind, the size, the chunk size, the image hash and the ordered chunk
	// hashes. It authenticates the whole chunk list before any chunk is
	// fetched, so a rogue manifest costs one small download and never an image.
	// It is what "kairos agent p2p seed" and "pin" print.
	Pin string
	// Size is the optional size of the image in bytes. When it is there a
	// manifest of another size is discarded before any chunk is fetched, and the
	// space check uses it. "kairos agent p2p seed" and "pin" print it.
	Size int64
}

// Discover reports whether the peers must be found with mDNS.
func (t P2PTarget) Discover() bool { return len(t.Peers) == 0 }

// value is the form kept inside the ImageSource: [peers/]image[?pin=hex&sha256=hex&size=n] (the pin forms).
func (t P2PTarget) value() string {
	v := t.Image
	if !t.Discover() {
		v = strings.Join(t.Peers, ",") + "/" + t.Image
	}
	var q []string
	if t.Pin != "" {
		q = append(q, "pin="+t.Pin)
	}
	if t.SHA256 != "" {
		q = append(q, "sha256="+t.SHA256)
	}
	if t.Size > 0 {
		q = append(q, "size="+strconv.FormatInt(t.Size, 10))
	}
	if len(q) > 0 {
		v += "?" + strings.Join(q, "&")
	}
	return v
}

// String returns the p2p:// URI of the target.
func (t P2PTarget) String() string { return p2p + "://" + t.value() }

// IsP2P reports whether the image is fetched from other nodes with the
// experimental p2p transfer.
func (i ImageSource) IsP2P() bool { return i.srcType == p2p }

// P2PTarget returns the parsed target of a p2p source.
func (i ImageSource) P2PTarget() (P2PTarget, error) {
	if !i.IsP2P() {
		return P2PTarget{}, fmt.Errorf("%s is not a p2p source", i.String())
	}
	return parseP2PString(i.source)
}

// ParseP2PURI parses a p2p:// URI.
func ParseP2PURI(uri string) (P2PTarget, error) {
	rest, ok := strings.CutPrefix(uri, p2p+"://")
	if !ok {
		return P2PTarget{}, fmt.Errorf("invalid p2p source: use p2p://[peer[,peer]/]active|passive|recovery?pin=<hex>")
	}
	return parseP2PString(rest)
}

func (i *ImageSource) parseP2PURI(uri string) error {
	t, err := ParseP2PURI(uri)
	if err != nil {
		return err
	}
	i.srcType = p2p
	i.source = t.value()
	return nil
}

func NewP2PSrc(t P2PTarget) *ImageSource {
	return &ImageSource{source: t.value(), srcType: p2p}
}

// parseP2PString parses what follows p2p://: the peers (each with an optional
// port, comma separated), the image, and the query. url.Parse cannot be used
// on the whole thing, it refuses a port in the middle of a host list.
//
// nolint:gocyclo // a flat sequence of syntax checks on one string; each rejects one malformed form with its own message.
func parseP2PString(s string) (P2PTarget, error) {
	var t P2PTarget
	if strings.Contains(s, "#") || strings.ContainsAny(s, " \t\r\n") {
		return t, fmt.Errorf("invalid p2p source: no fragment or spaces are supported")
	}
	end := strings.IndexAny(s, "/?")
	if end < 0 {
		end = len(s)
	}
	hosts, rest := s[:end], s[end:]
	if strings.Contains(hosts, "@") {
		return t, fmt.Errorf("invalid p2p source: no user or password is supported")
	}
	u, err := url.Parse("http://x" + rest)
	if err != nil {
		return t, fmt.Errorf("invalid p2p source: %w", err)
	}
	image := strings.Trim(u.Path, "/")
	if image == "" && ValidP2PImage(hosts) {
		image, hosts = hosts, "" // p2p://active
	}
	if !ValidP2PImage(image) {
		return t, fmt.Errorf("invalid p2p source: the image must be one of %s", strings.Join(P2PImages, ", "))
	}
	t.Image = image
	seen := map[string]bool{}
	for _, p := range strings.Split(hosts, ",") {
		if p == "" {
			continue
		}
		hp, err := normalizeP2PPeer(p)
		if err != nil {
			return t, err
		}
		if !seen[hp] {
			seen[hp] = true
			t.Peers = append(t.Peers, hp)
		}
	}
	// A malformed query, an unknown option or an option given twice is an
	// error: nothing is silently dropped from a pin.
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return t, fmt.Errorf("invalid p2p source: the options are malformed")
	}
	for k, v := range q {
		if k != "pin" && k != "sha256" && k != "size" {
			return t, fmt.Errorf("invalid p2p source: unknown option %q", k)
		}
		if len(v) != 1 {
			return t, fmt.Errorf("invalid p2p source: option %q is given more than once", k)
		}
	}
	for _, o := range []struct {
		name string
		dst  *string
	}{{"pin", &t.Pin}, {"sha256", &t.SHA256}} {
		if v := q.Get(o.name); v != "" {
			if b, err := hex.DecodeString(v); err != nil || len(b) != 32 {
				return t, fmt.Errorf("invalid p2p source: %s must be 64 hex characters", o.name)
			}
			*o.dst = strings.ToLower(v)
		}
	}
	if q.Has("pin") && t.Pin == "" || q.Has("sha256") && t.SHA256 == "" || q.Has("size") && q.Get("size") == "" {
		return t, fmt.Errorf("invalid p2p source: an option has no value")
	}
	if v := q.Get("size"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n > 1<<42 {
			return t, fmt.Errorf("invalid p2p source: size must be a number of bytes")
		}
		if t.SHA256 == "" && t.Pin == "" {
			return t, fmt.Errorf("invalid p2p source: size only goes with a pin or a sha256")
		}
		t.Size = n
	}
	return t, nil
}

// normalizeP2PPeer returns host:port with the default port filled in.
func normalizeP2PPeer(p string) (string, error) {
	host, port, err := net.SplitHostPort(p)
	if err != nil {
		if strings.Contains(p, ":") || strings.ContainsAny(p, "[]") {
			return "", fmt.Errorf("invalid p2p source: peer %q must be host or host:port (IPv4 only)", p)
		}
		host, port = p, strconv.Itoa(DefaultP2PPort)
	}
	if host == "" || strings.HasPrefix(host, "-") {
		return "", fmt.Errorf("invalid p2p source: peer %q is not valid", p)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid p2p source: port %q is not valid", port)
	}
	return net.JoinHostPort(host, port), nil
}

// P2POptions is the p2p-source block: how this node shares and fetches images.
// All keys are optional. Nothing happens unless Experimental is set.
type P2POptions struct {
	// Enabled runs the seeding service (off by default).
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// Experimental acknowledges that the feature is experimental.
	Experimental bool `yaml:"experimental" mapstructure:"experimental"`
	// Bind is the local IPv4 address to listen on, default the first non-loopback one.
	Bind string `yaml:"bind" mapstructure:"bind"`
	// Port to listen on, default DefaultP2PPort for the seeder.
	Port int `yaml:"port" mapstructure:"port"`
	// Allow lists the networks peers must belong to. Default: the listed peers,
	// or the local subnet when peers are discovered.
	Allow []string `yaml:"allow" mapstructure:"allow"`
	// UploadLimit in KiB/s, 0 means the default (65536, 64 MiB/s).
	UploadLimit int `yaml:"upload-limit" mapstructure:"upload-limit"`
	// MaxPeers bounds the simultaneous requests served, 0 means the default.
	MaxPeers int `yaml:"max-peers" mapstructure:"max-peers"`
	// Insecure allows a source without a ?pin= or ?sha256= pin. A rogue peer on the
	// network can then make the node install any image.
	Insecure bool `yaml:"insecure" mapstructure:"insecure"`
	// MaxImageSize in MiB, larger images are refused. 0 means 16384.
	MaxImageSize int `yaml:"max-image-size" mapstructure:"max-image-size"`
	// Timeout in minutes bounds a whole download, 0 means 360.
	Timeout int `yaml:"timeout" mapstructure:"timeout"`
	// DisableAnnounce stops the node from announcing itself with mDNS.
	DisableAnnounce bool `yaml:"disable-announce" mapstructure:"disable-announce"`
}
