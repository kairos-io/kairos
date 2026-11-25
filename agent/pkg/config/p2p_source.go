package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/spf13/viper"
)

// p2pSourceKey is the root block that configures the EXPERIMENTAL p2p image
// source, both the seeding service and the p2p:// sources of install, upgrade
// and reset. It is not the p2p block, which is the Kairos p2p network:
//
//	p2p-source:
//	  experimental: true   # required for anything to run
//	  enabled: true        # also run the seeding service
//	  allow: [10.0.0.0/24]
const p2pSourceKey = "p2p-source"

func decodeP2POptions(raw interface{}) (sdkImages.P2POptions, error) {
	var o sdkImages.P2POptions
	if raw == nil {
		return o, nil
	}
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      &o,
		ErrorUnused: true,
		MatchName:   func(key, field string) bool { return key == field },
		DecodeHook: mapstructure.DecodeHookFuncType(func(from, to reflect.Type, value interface{}) (interface{}, error) {
			if to.Kind() == reflect.String && from.Kind() != reflect.String {
				return nil, fmt.Errorf("must be a string")
			}
			return value, nil
		}),
	})
	if err != nil {
		return o, err
	}
	// Decoder errors can echo values, keep the diagnostic fixed.
	if err := dec.Decode(raw); err != nil {
		return o, fmt.Errorf("%s must be an object with enabled, experimental, bind, port, allow, upload-limit, max-peers, insecure, max-image-size, timeout and disable-announce", p2pSourceKey)
	}
	if o.Port < 0 || o.Port > 65535 || o.UploadLimit < 0 || o.MaxPeers < 0 || o.MaxImageSize < 0 || o.Timeout < 0 {
		return o, fmt.Errorf("%s: port, upload-limit, max-peers, max-image-size and timeout must be within range", p2pSourceKey)
	}
	return o, nil
}

// P2POptionsFromConfig reads the p2p-source block of a scanned config.
func P2POptionsFromConfig(cfg *sdkConfig.Config) (sdkImages.P2POptions, error) {
	if cfg == nil {
		return sdkImages.P2POptions{}, nil
	}
	ccString, err := cfg.Collector.String()
	if err != nil {
		return sdkImages.P2POptions{}, err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(ccString)); err != nil {
		return sdkImages.P2POptions{}, err
	}
	return decodeP2POptions(v.Get(p2pSourceKey))
}

// applyP2POptions makes the p2p-source options available to the code that
// fetches the image, when one of the sources is a p2p source, and refuses to go
// on unless the user opted in to the experimental feature. Without a p2p source
// the block is ignored and cleared from the config.
func applyP2POptions(cfg *sdkConfig.Config, sources ...*sdkImages.ImageSource) error {
	cfg.P2PSource = nil
	uses := false
	for _, s := range sources {
		if s != nil && s.IsP2P() {
			uses = true
		}
	}
	if !uses {
		return nil
	}
	opts, err := P2POptionsFromConfig(cfg)
	if err != nil {
		return err
	}
	if err := p2p.RequireExperimental(opts.Experimental); err != nil {
		return err
	}
	for _, c := range opts.Allow {
		if _, err := p2p.ParseAllowlist([]string{c}); err != nil {
			return fmt.Errorf("%s.allow: %w", p2pSourceKey, err)
		}
	}
	auth, err := p2p.AuthFromOptions(&opts)
	if err != nil {
		return fmt.Errorf("%s: %w", p2pSourceKey, err)
	}
	// The manifest comes from a peer: refuse now, before any partition is
	// touched, a source nothing would authenticate.
	for _, s := range sources {
		if s == nil || !s.IsP2P() {
			continue
		}
		t, err := s.P2PTarget()
		if err != nil {
			return err
		}
		if err := auth.CheckTarget(t); err != nil {
			return err
		}
	}
	cfg.P2PSource = &opts
	return nil
}

// p2pSourceSize asks the peers how big the image is.
func p2pSourceSize(cfg *sdkConfig.Config, source *sdkImages.ImageSource) (int64, error) {
	t, err := source.P2PTarget()
	if err != nil {
		return 0, err
	}
	n, err := p2p.NetConfigFromOptions(cfg.P2PSource)
	if err != nil {
		return 0, err
	}
	auth, err := p2p.AuthFromOptions(cfg.P2PSource)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m, err := p2p.Probe(ctx, n, auth, t, func(f string, a ...any) { cfg.Logger.Infof(f, a...) })
	if err != nil {
		return 0, err
	}
	return m.Size, nil
}

// checkP2PImage makes sure a p2p source names an image of the right kind for
// the slot it is deployed to: the recovery image is another kind of filesystem
// than the active and passive ones, they cannot stand in for each other.
func checkP2PImage(source *sdkImages.ImageSource, recoverySlot bool) error {
	if source == nil || !source.IsP2P() {
		return nil
	}
	t, err := source.P2PTarget()
	if err != nil {
		return err
	}
	if recoverySlot && t.Image != sdkImages.P2PImageRecovery {
		return fmt.Errorf("the recovery system can only be deployed from the recovery image of a node: use p2p://.../recovery")
	}
	if !recoverySlot && t.Image == sdkImages.P2PImageRecovery {
		return fmt.Errorf("the recovery image of a node cannot be deployed as the active or passive system: use p2p://.../active or passive")
	}
	return nil
}

func checkP2PInstallImages(active, passive, recovery *sdkImages.ImageSource) error {
	for _, c := range []struct {
		src      *sdkImages.ImageSource
		recovery bool
	}{{active, false}, {passive, false}, {recovery, true}} {
		if err := checkP2PImage(c.src, c.recovery); err != nil {
			return err
		}
	}
	return nil
}
