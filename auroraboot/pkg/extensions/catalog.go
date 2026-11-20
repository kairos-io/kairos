// Package extensions resolves catalog entries into local system extension files.
package extensions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	sdkextensions "github.com/kairos-io/kairos/v4/sdk/extensions"
)

const rawMediaType types.MediaType = "application/vnd.kairos.sysext.raw"

// DefaultCatalog is the catalog used when a build names extensions and
// configures none of its own: the hadron-layers index Kairos publishes, which
// is also the default the agent reads on the node. A build that wants another
// index states it, and then this one is not consulted at all.
const DefaultCatalog = "https://kairos-io.github.io/hadron-layers/releases.json"

var requestPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// FileScheme marks an extension request that names a local .raw image instead
// of a catalog entry. A build that already has the image on disk, such as a
// test fixture living next to the spec that reads it, then needs no registry
// and no catalog to bake it in.
const FileScheme = "file://"

// Request identifies an extension to bake into an artifact: either a named
// catalog entry with an optional catalog version, or, when Name carries the
// FileScheme prefix, a local .raw image to copy as it is.
type Request struct {
	Name    string
	Version string
}

// FilePath returns the local image path of a file request. The second result
// reports whether this is one, so a caller can tell an unset path from a
// catalog entry.
func (r Request) FilePath() (string, bool) {
	path, found := strings.CutPrefix(r.Name, FileScheme)
	return path, found
}

// fileOutputName is the name a file request's image is written under, which is
// the base name of the source. Keeping it means the build stages the file the
// spec pointed at, under the name the spec used.
func (r Request) fileOutputName() (string, error) {
	path, isFile := r.FilePath()
	if !isFile {
		return "", fmt.Errorf("extension request %q is not a %s request", formatRequest(r), FileScheme)
	}
	base := filepath.Base(filepath.Clean(path))
	if filepath.Ext(base) != ".raw" {
		// immucore merges only .raw entries, and both the ISO and the disk
		// paths stage whatever comes back from here, so accepting another
		// suffix would put a file on the image that never gets merged.
		return "", fmt.Errorf("extension %q is not a .raw image", base)
	}
	return base, nil
}

// ParseRequest parses name, name@version, or file://<path to a .raw image>.
func ParseRequest(value string) (Request, error) {
	if strings.HasPrefix(value, FileScheme) {
		// A local image has no catalog version to select, so the whole value
		// is the path. That also keeps an @ inside a path from being read as
		// a version separator.
		if strings.TrimPrefix(value, FileScheme) == "" {
			return Request{}, fmt.Errorf("invalid extension request %q", value)
		}
		request := Request{Name: value}
		if _, err := request.fileOutputName(); err != nil {
			return Request{}, err
		}
		return request, nil
	}

	parts := strings.Split(value, "@")
	if len(parts) > 2 || len(parts) == 0 || !requestPart.MatchString(parts[0]) {
		return Request{}, fmt.Errorf("invalid extension request %q", value)
	}
	request := Request{Name: parts[0]}
	if len(parts) == 2 {
		if !requestPart.MatchString(parts[1]) {
			return Request{}, fmt.Errorf("invalid extension request %q", value)
		}
		request.Version = parts[1]
	}
	return request, nil
}

// ParseRequests parses a list of name or name@version values, reporting the
// first one that is not a valid request.
func ParseRequests(values []string) ([]Request, error) {
	requests := make([]Request, 0, len(values))
	for _, value := range values {
		request, err := ParseRequest(value)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

// Materialize resolves requests and writes their raw images to destination.
//
// The catalogs are searched in order, so the first one publishing a name wins
// and an operator can put their own index ahead of the default one. A
// FileScheme request names an image that is already on disk and is copied
// instead, so a build made up of those alone reads no catalog at all.
func Materialize(ctx context.Context, catalogSources []string, requests []Request, architecture string, destination string, insecure bool) ([]string, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	fromCatalog := false
	for _, request := range requests {
		parsed, err := ParseRequest(request.Name)
		_, isFile := request.FilePath()
		if err != nil && isFile {
			// A rejected path says what is wrong with it, which the generic
			// message below would throw away.
			return nil, err
		}
		if err != nil || parsed.Version != "" || (request.Version != "" && !requestPart.MatchString(request.Version)) {
			return nil, fmt.Errorf("invalid extension request %q", formatRequest(request))
		}

		outputName := request.Name + ".sysext.raw"
		if isFile {
			if request.Version != "" {
				return nil, fmt.Errorf("extension request %q must not set a version: a local image has none to select", formatRequest(request))
			}
			if outputName, err = request.fileOutputName(); err != nil {
				return nil, err
			}
		} else {
			fromCatalog = true
		}

		// Two requests that land on one file name would overwrite each other
		// in destination, whether they collide by catalog name or by the base
		// name of two different paths.
		if _, exists := seen[outputName]; exists {
			return nil, fmt.Errorf("duplicate extension name %q", outputName)
		}
		seen[outputName] = struct{}{}
		names = append(names, outputName)
	}

	// Architecture selects a catalog entry, so it is only required when there
	// is a catalog entry to select. A local image is taken as it is.
	if fromCatalog && architecture == "" {
		return nil, fmt.Errorf("extension architecture must not be empty")
	}

	var catalogs sdkextensions.Catalogs
	if fromCatalog {
		loaded, err := loadCatalogs(ctx, catalogSources)
		if err != nil {
			return nil, err
		}
		catalogs = loaded
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return nil, fmt.Errorf("create extension destination: %w", err)
	}

	temporary := make([]string, 0, len(requests))
	outputs := make([]string, 0, len(requests))
	cleanup := func() {
		for _, path := range temporary {
			_ = os.Remove(path)
		}
	}
	defer cleanup()

	for index, request := range requests {
		output := filepath.Join(destination, names[index])

		if path, isFile := request.FilePath(); isFile {
			temp, copyErr := copyLocalImage(destination, names[index], path)
			if temp != "" {
				temporary = append(temporary, temp)
			}
			if copyErr != nil {
				return nil, copyErr
			}
			outputs = append(outputs, output)
			continue
		}

		// Shadowed repositories are the later catalogs publishing the same
		// name, which a build does not act on: the first one already won.
		resolved, _, resolveErr := catalogs.Resolve(request.Name, request.Version, architecture)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve extension %q: %w", request.Name, resolveErr)
		}
		options := []name.Option{}
		if insecure {
			options = append(options, name.Insecure)
		}
		reference, parseErr := name.NewDigest(resolved.OCI, options...)
		if parseErr != nil {
			return nil, fmt.Errorf("parse OCI reference for extension %q: %w", request.Name, parseErr)
		}
		image, pullErr := remote.Image(reference, remote.WithContext(ctx))
		if pullErr != nil {
			return nil, fmt.Errorf("pull extension %q: %w", request.Name, pullErr)
		}
		layers, layersErr := image.Layers()
		if layersErr != nil {
			return nil, fmt.Errorf("read layers for extension %q: %w", request.Name, layersErr)
		}
		if len(layers) != 1 {
			return nil, fmt.Errorf("extension %q has %d layers, expected exactly one", request.Name, len(layers))
		}
		mediaType, mediaErr := layers[0].MediaType()
		if mediaErr != nil {
			return nil, fmt.Errorf("read layer media type for extension %q: %w", request.Name, mediaErr)
		}
		if mediaType != rawMediaType {
			return nil, fmt.Errorf("extension %q layer media type is %q, expected %q", request.Name, mediaType, rawMediaType)
		}
		stream, streamErr := layers[0].Compressed()
		if streamErr != nil {
			return nil, fmt.Errorf("read extension %q layer: %w", request.Name, streamErr)
		}
		temp, tempErr := os.CreateTemp(destination, "."+request.Name+"-*.sysext.raw")
		if tempErr != nil {
			stream.Close()
			return nil, fmt.Errorf("create temporary file for extension %q: %w", request.Name, tempErr)
		}
		temporary = append(temporary, temp.Name())
		_, copyErr := io.Copy(temp, stream)
		streamErr = stream.Close()
		closeErr := temp.Close()
		if copyErr != nil || streamErr != nil || closeErr != nil {
			return nil, fmt.Errorf("write extension %q: %w", request.Name, firstError(copyErr, streamErr, closeErr))
		}
		outputs = append(outputs, output)
	}

	for index, path := range temporary {
		if err := os.Rename(path, outputs[index]); err != nil {
			for _, output := range outputs[:index] {
				_ = os.Remove(output)
			}
			return nil, fmt.Errorf("install extension %q: %w", requests[index].Name, err)
		}
	}
	return outputs, nil
}

// Catalogs returns the catalogs to search: the configured ones, or the
// default one when a build configured none. Callers that report the catalog
// back to an operator use this, so what they print is what was read.
func Catalogs(sources []string) []string {
	if len(sources) == 0 {
		return []string{DefaultCatalog}
	}
	return sources
}

// loadCatalogs reads every configured catalog, in the order given. A build
// states the catalogs it wants, so one that cannot be read is an error rather
// than a catalog to skip.
func loadCatalogs(ctx context.Context, sources []string) (sdkextensions.Catalogs, error) {
	sources = Catalogs(sources)
	catalogs := make(sdkextensions.Catalogs, 0, len(sources))
	for _, source := range sources {
		catalog, err := loadCatalog(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", source, err)
		}
		catalogs = append(catalogs, catalog)
	}
	return catalogs, nil
}

// openCatalogSource is the seam tests use to see which source a build asked
// for without reaching the network.
var openCatalogSource = openCatalog

func loadCatalog(ctx context.Context, source string) (sdkextensions.Catalog, error) {
	reader, err := openCatalogSource(ctx, source)
	if err != nil {
		return sdkextensions.Catalog{}, err
	}
	defer reader.Close()
	return sdkextensions.Parse(reader)
}

func openCatalog(ctx context.Context, source string) (io.ReadCloser, error) {
	parsed, err := url.Parse(source)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if requestErr != nil {
			return nil, fmt.Errorf("create catalog request: %w", requestErr)
		}
		response, responseErr := http.DefaultClient.Do(request)
		if responseErr != nil {
			return nil, fmt.Errorf("load extension catalog: %w", responseErr)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, fmt.Errorf("load extension catalog: HTTP status %s", response.Status)
		}
		return response.Body, nil
	}
	file, openErr := os.Open(source)
	if openErr != nil {
		return nil, fmt.Errorf("load extension catalog: %w", openErr)
	}
	return file, nil
}

// copyLocalImage copies a FileScheme request's image into destination under a
// temporary name, which the caller renames into place once every request has
// been materialized. It returns that temporary name even on failure, so a
// partial copy is cleaned up with the rest.
//
// The file is copied rather than linked or referenced: destination is staged
// into an artifact and then thrown away, and a build must not be able to
// modify the image the spec pointed at.
func copyLocalImage(destination, name, path string) (string, error) {
	source, openErr := os.Open(path)
	if openErr != nil {
		return "", fmt.Errorf("read extension %q: %w", name, openErr)
	}
	defer source.Close()

	info, statErr := source.Stat()
	if statErr != nil {
		return "", fmt.Errorf("read extension %q: %w", name, statErr)
	}
	if info.IsDir() {
		return "", fmt.Errorf("extension %q is a directory, expected a .raw image", path)
	}

	temp, tempErr := os.CreateTemp(destination, "."+name+"-*")
	if tempErr != nil {
		return "", fmt.Errorf("create temporary file for extension %q: %w", name, tempErr)
	}
	_, copyErr := io.Copy(temp, source)
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		return temp.Name(), fmt.Errorf("write extension %q: %w", name, firstError(copyErr, closeErr))
	}
	return temp.Name(), nil
}

func formatRequest(request Request) string {
	if request.Version == "" {
		return request.Name
	}
	return request.Name + "@" + request.Version
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}
