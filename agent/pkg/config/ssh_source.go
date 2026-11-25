package config

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/kairos-io/kairos/v4/agent/pkg/sshimage"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/spf13/viper"
)

// sshSourceKey is the block that tells how to authenticate to the node an
// ssh:// source points to. It can sit at the root of the config, for every
// action, and inside install, upgrade or reset. A key set in the block of the
// action wins over the same key at the root, keys it leaves out fall back to it:
//
//	ssh-source:
//	  identity-file: /oem/ssh/fleet
//	  known-hosts-file: /oem/ssh/known_hosts
//	upgrade:
//	  system:
//	    source: ssh://10.0.0.5
//	  ssh-source:
//	    user: upgrader
//
// It only ever names files. Private keys are not accepted inline, the cloud
// config is copied around and persisted.
const sshSourceKey = "ssh-source"

type sshSourceFields struct {
	User                *string  `mapstructure:"user"`
	IdentityFile        *string  `mapstructure:"identity-file"`
	KnownHostsFile      *string  `mapstructure:"known-hosts-file"`
	HostKeyFingerprints []string `mapstructure:"host-key-fingerprints"`
}

// over returns the fields with the ones set in specific taking the place of the ones in f.
func (f sshSourceFields) over(specific sshSourceFields) sshSourceFields {
	if specific.User != nil {
		f.User = specific.User
	}
	if specific.IdentityFile != nil {
		f.IdentityFile = specific.IdentityFile
	}
	if specific.KnownHostsFile != nil {
		f.KnownHostsFile = specific.KnownHostsFile
	}
	if len(specific.HostKeyFingerprints) > 0 {
		f.HostKeyFingerprints = specific.HostKeyFingerprints
	}
	return f
}

func decodeSSHSource(raw interface{}, where string) (sshSourceFields, error) {
	var fields sshSourceFields
	if raw == nil {
		return fields, nil
	}
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      &fields,
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
		return fields, err
	}
	// Decoder errors can echo values, keep the diagnostic fixed.
	if err := dec.Decode(raw); err != nil {
		return fields, fmt.Errorf("%s must be an object with user, identity-file, known-hosts-file and host-key-fingerprints", where)
	}
	return fields, nil
}

// applySSHOptions makes the options of the ssh-source blocks available to the
// code that fetches the image, when one of the sources is an ssh source. The
// block of the action (subkey: install, upgrade or reset) overrides the one at
// the root key by key. Without an ssh source the blocks are ignored, and
// cleared from the config.
func applySSHOptions(cfg *sdkConfig.Config, subkey string, sources ...*sdkImages.ImageSource) error {
	cfg.SSHSource = nil
	uses := false
	for _, s := range sources {
		if s != nil && s.IsSSH() {
			uses = true
		}
	}
	if !uses {
		return nil
	}

	ccString, err := cfg.Collector.String()
	if err != nil {
		return err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(ccString)); err != nil {
		return err
	}
	root, err := decodeSSHSource(v.Get(sshSourceKey), sshSourceKey)
	if err != nil {
		return err
	}
	var specific sshSourceFields
	if sub := v.Sub(subkey); sub != nil {
		if specific, err = decodeSSHSource(sub.Get(sshSourceKey), subkey+"."+sshSourceKey); err != nil {
			return err
		}
	}
	fields := root.over(specific)
	for _, fp := range fields.HostKeyFingerprints {
		if !strings.HasPrefix(fp, "SHA256:") {
			return fmt.Errorf("%s.%s.host-key-fingerprints entries must look like SHA256:<base64>, as printed by ssh-keygen -lf", subkey, sshSourceKey)
		}
	}
	opts := &sdkImages.SSHSourceOptions{HostKeyFingerprints: fields.HostKeyFingerprints}
	if fields.User != nil {
		opts.User = *fields.User
	}
	if fields.IdentityFile != nil {
		opts.IdentityFile = *fields.IdentityFile
	}
	if fields.KnownHostsFile != nil {
		opts.KnownHostsFile = *fields.KnownHostsFile
	}
	cfg.SSHSource = opts
	return nil
}

// sshSourceSize asks the serving node how big its image is.
func sshSourceSize(cfg *sdkConfig.Config, source *sdkImages.ImageSource) (int64, error) {
	t, err := source.SSHTarget()
	if err != nil {
		return 0, err
	}
	if cfg.SSHSource == nil {
		cfg.SSHSource = &sdkImages.SSHSourceOptions{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	h, err := sshimage.Stat(ctx, sshimage.Options{SSHSourceOptions: *cfg.SSHSource, Logger: cfg.Logger}, t)
	if err != nil {
		return 0, err
	}
	return h.Size, nil
}
