package elemental

import (
	"context"
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// fetchP2P is a variable so tests can fetch without peers.
var fetchP2P = p2p.Fetch

// fetchP2PImage downloads the image other nodes serve into target, in verified
// chunks from all the peers at once. A partial download is kept, next to a
// state file, so a rerun resumes it. An image that failed verification is
// removed by the fetch itself.
func (e *Elemental) fetchP2PImage(target string, imgSrc *sdkImages.ImageSource) error {
	t, err := imgSrc.P2PTarget()
	if err != nil {
		return err
	}
	n, err := p2p.NetConfigFromOptions(e.config.P2PSource)
	if err != nil {
		return err
	}
	auth, err := p2p.AuthFromOptions(e.config.P2PSource)
	if err != nil {
		return err
	}
	log := e.config.Logger
	log.Warnf("The p2p image source is EXPERIMENTAL: fetching %s", t.String())
	res, err := fetchP2P(context.Background(), p2p.FetchConfig{
		NetConfig: n,
		Auth:      auth,
		Target:    t,
		DestPath:  target,
		Logf:      func(f string, a ...any) { log.Infof(f, a...) },
		OnProgress: func(done, total int64, peers int) {
			log.Infof("p2p: %d/%d MiB from %d peers", done>>20, total>>20, peers)
		},
	})
	if err != nil {
		log.Errorf("Could not fetch %s: %s", t.String(), err)
		return fmt.Errorf("p2p fetch: %w", err)
	}
	for _, p := range res.Peers {
		log.Infof("p2p: %s served %d bytes in %d chunks", p.Addr, p.Bytes, p.Chunks)
	}
	log.Infof("p2p: image downloaded, sha256 %s, the manifest was authenticated by: %s", res.SHA256, res.Trust)
	return nil
}
