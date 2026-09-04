// Package extensions downloads system and configuration extensions into a
// directory. It sits below the action package so that both the agent's
// commands and the install-time hooks can install an extension: the action
// package imports the hooks, so the hooks cannot import it back.
package extensions

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/reference"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/extensions"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
)

// Install downloads the extension at uri into target, creating target when it
// does not exist yet.
func Install(cfg *sdkConfig.Config, uri, target string) error {
	download, err := parseURI(cfg, uri)
	if err != nil {
		return fmt.Errorf("failed to parse URI %s: %w", uri, err)
	}
	if _, err := cfg.Fs.Stat(target); os.IsNotExist(err) {
		if err := vfs.MkdirAll(cfg.Fs, target, 0755); err != nil {
			return fmt.Errorf("failed to create target dir %s: %w", target, err)
		}
	}
	return download.Download(target)
}

// ParseURI parses a URI and returns a SourceDownload
// implementation based on the scheme of the URI
func parseURI(cfg *sdkConfig.Config, uri string) (SourceDownload, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	value := u.Opaque
	if value == "" {
		value = filepath.Join(u.Host, u.Path)
	}
	switch scheme {
	case "oci", "docker", "container":
		n, err := reference.ParseNormalizedNamed(value)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference %s", value)
		} else if reference.IsNameOnly(n) {
			value += ":latest"
		}
		return &dockerSource{value, cfg}, nil
	case "file":
		return &fileSource{value, cfg}, nil
	case "http", "https":
		// Pass the full uri including the protocol
		return &httpSource{uri, cfg}, nil
	default:
		return nil, fmt.Errorf("invalid URI reference %s", uri)
	}
}

// SourceDownload is an interface for downloading system extensions
// from different sources. It allows for different implementations
// for different sources of system extensions, such as files, directories,
// or docker images. The interface defines a single method, Download,
// which takes a destination path as an argument and returns an error
type SourceDownload interface {
	Download(string) error
}

// fileSource is a struct that implements the SourceDownload interface
// for downloading system extensions from a file. It has two fields,
// uri, which is the URI of the file to be downloaded and cfg which points to the Config
// The Download method takes a destination path as an argument and returns an error if the
// download fails.
type fileSource struct {
	uri string
	cfg *sdkConfig.Config
}

// Download streams the file to the destination with bounded memory usage
// Uses io.Copy instead of buffering the entire file to avoid OOM on pods with limited memory
func (f *fileSource) Download(dst string) error {
	// Open source file for reading
	srcFile, err := f.cfg.Fs.Open(f.uri)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", f.uri, err)
	}
	defer srcFile.Close()

	// Get file info for permissions
	stat, err := f.cfg.Fs.Stat(f.uri)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", f.uri, err)
	}

	// Create destination file with same permissions
	dstFile := filepath.Join(dst, filepath.Base(f.uri))
	dstFileHandle, err := f.cfg.Fs.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, stat.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dstFile, err)
	}
	defer dstFileHandle.Close()

	// Stream copy (bounded memory usage)
	f.cfg.Logger.Logger.Debug().Str("uri", f.uri).Str("target", dstFile).Msg("Copying system extension")
	if _, err := io.Copy(dstFileHandle, srcFile); err != nil {
		return fmt.Errorf("failed to copy file %s to %s: %w", f.uri, dstFile, err)
	}

	return nil
}

type httpSource struct {
	uri string
	cfg *sdkConfig.Config
}

func (h httpSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	h.cfg.Logger.Logger.Debug().Str("uri", h.uri).Str("target", filepath.Join(s, filepath.Base(h.uri))).Msg("Downloading system extension")
	return h.cfg.Client.GetURL(sdkLogger.NewNullLogger(), h.uri, filepath.Join(s, filepath.Base(h.uri)))
}

type dockerSource struct {
	uri string
	cfg *sdkConfig.Config
}

func (d dockerSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	err := d.cfg.ImageExtractor.ExtractImage(d.uri, s, "")
	if err != nil {
		return err
	}
	return nil
}

// FetchCatalogs downloads and parses the extension indexes at urls, in order.
//
// A catalog that cannot be fetched or parsed is logged and skipped rather than
// failing the call: with several catalogs configured, one unreachable index
// must not make every extension unresolvable. The error is returned only when
// no catalog at all could be read, so the caller can tell "nothing resolved
// because the name is unknown" from "nothing resolved because nothing was
// reachable".
func FetchCatalogs(cfg *sdkConfig.Config, urls []string) (extensions.Catalogs, error) {
	var (
		catalogs extensions.Catalogs
		failures []error
	)
	for _, url := range urls {
		catalog, err := fetchCatalog(cfg, url)
		if err != nil {
			cfg.Logger.Logger.Warn().Str("catalog", url).Err(err).Msg("Skipping unreadable extension catalog")
			failures = append(failures, fmt.Errorf("catalog %s: %w", url, err))
			continue
		}
		catalogs = append(catalogs, catalog)
	}
	if len(catalogs) == 0 && len(failures) > 0 {
		return nil, fmt.Errorf("no extension catalog could be read: %w", errors.Join(failures...))
	}
	return catalogs, nil
}

// fetchCatalog downloads one index through the configured HTTP client, which
// is what gives it the node's proxy and TLS settings.
func fetchCatalog(cfg *sdkConfig.Config, url string) (extensions.Catalog, error) {
	temporaryDir, err := fsutils.TempDir(cfg.Fs, "", "kairos-extension-catalog-")
	if err != nil {
		return extensions.Catalog{}, err
	}
	defer func() { _ = cfg.Fs.RemoveAll(temporaryDir) }()

	path := filepath.Join(temporaryDir, "releases.json")
	if err := cfg.Client.GetURL(cfg.Logger, url, path); err != nil {
		return extensions.Catalog{}, err
	}
	reader, err := cfg.Fs.Open(path)
	if err != nil {
		return extensions.Catalog{}, err
	}
	defer reader.Close()
	return extensions.Parse(reader)
}

// ExtensionURI turns one requested extension into a URI the install path can
// download, resolving a bare name against catalogs. An absolute path becomes a
// `file:` URI; a URI is passed through untouched.
func ResolveURI(cfg *sdkConfig.Config, catalogs extensions.Catalogs, requested extensiontypes.Extension) (string, error) {
	if requested.IsReference() {
		if requested.Version != "" {
			return "", fmt.Errorf("extension %q refers to one image, it cannot also take version %q", requested.Name, requested.Version)
		}
		if strings.HasPrefix(requested.Name, "/") {
			return "file:" + requested.Name, nil
		}
		return requested.Name, nil
	}

	resolved, shadowed, err := catalogs.Resolve(requested.Name, requested.Version, cfg.Platform.GolangArch)
	if err != nil {
		return "", err
	}
	for _, repository := range shadowed {
		cfg.Logger.Logger.Info().
			Str("extension", requested.Name).
			Str("using", resolved.Repository).
			Str("shadowed", repository).
			Msg("Extension is published by more than one catalog, taking the first")
	}
	cfg.Logger.Logger.Info().
		Str("extension", requested.Name).
		Str("version", resolved.Version).
		Str("catalog", resolved.Repository).
		Str("artifact", resolved.OCI).
		Msg("Resolved extension")
	return "oci:" + resolved.OCI, nil
}

// InstallDeclaredExtensions installs every extension in requested into target.
//
// The catalogs are fetched once, and only if some entry actually needs them,
// so a config that names nothing but URIs installs without network access to
// any index.
func InstallDeclared(cfg *sdkConfig.Config, requested extensiontypes.Extensions, target string) error {
	if len(requested) == 0 {
		return nil
	}

	var catalogs extensions.Catalogs
	if requestedNeedsCatalog(requested) {
		var err error
		catalogs, err = FetchCatalogs(cfg, cfg.Extensions.CatalogURLs())
		if err != nil {
			return err
		}
	}

	for _, extension := range requested {
		uri, err := ResolveURI(cfg, catalogs, extension)
		if err != nil {
			return fmt.Errorf("resolve extension %s: %w", extension, err)
		}
		if err := Install(cfg, uri, target); err != nil {
			return fmt.Errorf("install extension %s: %w", extension, err)
		}
		cfg.Logger.Logger.Info().Str("extension", extension.String()).Str("target", target).Msg("Installed extension")
	}
	return nil
}

// requestedNeedsCatalog reports whether any entry has to be looked up by name.
func requestedNeedsCatalog(requested extensiontypes.Extensions) bool {
	for _, extension := range requested {
		if !extension.IsReference() {
			return true
		}
	}
	return false
}
