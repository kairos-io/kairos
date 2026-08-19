package image

import (
	"archive/tar"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	registrytypes "github.com/moby/moby/api/types/registry"
)

// referrence: https://github.com/mudler/luet/blob/master/pkg/helpers/docker/docker.go#L117
type staticAuth struct {
	auth *registrytypes.AuthConfig
}

func (s staticAuth) Authorization() (*authn.AuthConfig, error) {
	if s.auth == nil {
		return nil, nil
	}
	return &authn.AuthConfig{
		Username:      s.auth.Username,
		Password:      s.auth.Password,
		Auth:          s.auth.Auth,
		IdentityToken: s.auth.IdentityToken,
		RegistryToken: s.auth.RegistryToken,
	}, nil
}

var defaultRetryBackoff = remote.Backoff{
	Duration: 1.0 * time.Second,
	Factor:   3.0,
	Jitter:   0.1,
	Steps:    3,
}

var defaultRetryPredicate = func(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || strings.Contains(err.Error(), "connection refused") {
		logs.Warn.Printf("retrying %v", err)
		return true
	}
	return false
}

func ExtractOCIImage(img v1.Image, targetDestination string, excludes ...string) error {
	reader := mutate.Extract(img)

	var options archive.ApplyOpt
	if len(excludes) > 0 {
		// Create a Filter option to exclude files during extraction
		options = archive.WithFilter(func(hdr *tar.Header) (bool, error) {
			for _, exclude := range excludes {
				matched, matchErr := filepath.Match(exclude, hdr.Name)
				if matchErr != nil {
					return false, matchErr
				}
				if matched {
					return false, nil
				}
			}
			return true, nil
		})
	} else {
		// Return all files
		options = archive.WithFilter(func(_ *tar.Header) (bool, error) {
			return true, nil
		})
	}

	_, err := archive.Apply(context.Background(), targetDestination, reader, options)

	return err
}

// GetOption configures the behaviour of GetImage and GetOCIImageSize.
type GetOption func(*getOptions)

type getOptions struct {
	insecure bool
}

// WithInsecureRegistry allows pulling from registries that serve over plain
// HTTP or that present an untrusted/self-signed TLS certificate. When set, the
// reference is parsed as insecure (the client also attempts HTTP) and, unless a
// custom transport is provided, TLS certificate verification is skipped.
func WithInsecureRegistry() GetOption {
	return func(o *getOptions) {
		o.insecure = true
	}
}

// insecureTransport clones base into an *http.Transport whose TLS verification
// is disabled, preserving any other settings already present on base. base is
// usually http.DefaultTransport, which is a RoundTripper that consumers (and
// tests) can replace, so the type assertion is guarded to avoid panicking when
// it is not an *http.Transport.
func insecureTransport(base http.RoundTripper) *http.Transport {
	t, ok := base.(*http.Transport)
	if !ok {
		t = &http.Transport{}
	}
	t = t.Clone()
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{} //nolint:gosec
	}
	t.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
	return t
}

// GetImage if returns the proper image to pull with transport and auth
// tries local daemon first and then fallbacks into remote
// if auth is nil, it will try to use the default keychain https://github.com/google/go-containerregistry/tree/main/pkg/authn#tldr-for-consumers-of-this-package
func GetImage(targetImage, targetPlatform string, auth *registrytypes.AuthConfig, t http.RoundTripper, opts ...GetOption) (v1.Image, error) {
	var platform *v1.Platform
	var image v1.Image
	var err error

	o := &getOptions{}
	for _, fn := range opts {
		fn(o)
	}

	if targetPlatform != "" {
		platform, err = v1.ParsePlatform(targetPlatform)
		if err != nil {
			return image, err
		}
	} else {
		platform, err = v1.ParsePlatform(fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
		if err != nil {
			return image, err
		}
	}

	var nameOpts []name.Option
	if o.insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}
	ref, err := name.ParseReference(targetImage, nameOpts...)
	if err != nil {
		return image, err
	}

	if t == nil {
		if o.insecure {
			// Skip TLS verification so registries with self-signed/untrusted
			// certificates can still be reached over HTTPS.
			t = insecureTransport(http.DefaultTransport)
		} else {
			t = http.DefaultTransport
		}
	}

	tr := transport.NewRetry(t,
		transport.WithRetryBackoff(defaultRetryBackoff),
		transport.WithRetryPredicate(defaultRetryPredicate),
	)

	// Try to get the image from the local Docker daemon
	image, daemonErr := daemon.Image(ref)
	if daemonErr == nil {
		imgConfig, cfgErr := image.ConfigFile()
		if cfgErr != nil {
			logs.Warn.Printf("local daemon image %q: read config: %v; falling back to remote", ref.String(), cfgErr)
		} else if imgConfig.Architecture == platform.Architecture && imgConfig.OS == platform.OS {
			return image, nil
		}
	} else {
		logs.Warn.Printf("local daemon image %q: %v; falling back to remote", ref.String(), daemonErr)
	}

	remoteOpts := []remote.Option{
		remote.WithTransport(tr),
		remote.WithPlatform(*platform),
	}
	if auth != nil {
		remoteOpts = append(remoteOpts, remote.WithAuth(staticAuth{auth}))
	} else {
		remoteOpts = append(remoteOpts, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	}

	image, err = remote.Image(ref, remoteOpts...)

	return image, err
}

func GetOCIImageSize(targetImage, targetPlatform string, auth *registrytypes.AuthConfig, t http.RoundTripper, opts ...GetOption) (int64, error) {
	var size int64
	var img v1.Image
	var err error

	img, err = GetImage(targetImage, targetPlatform, auth, t, opts...)
	if err != nil {
		return size, err
	}
	layers, _ := img.Layers()
	for _, layer := range layers {
		s, _ := layer.Size()
		size += s
	}

	return size, nil
}
