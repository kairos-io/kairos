package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		value string
		want  Request
		ok    bool
	}{
		{value: "tool", want: Request{Name: "tool"}, ok: true},
		{value: "tool@v1", want: Request{Name: "tool", Version: "v1"}, ok: true},
		{value: "", ok: false},
		{value: "@v1", ok: false},
		{value: "tool@", ok: false},
		{value: "tool@v1@extra", ok: false},
		{value: " tool", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseRequest(tt.value)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseRequest() error = %v, want success %v", err, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ParseRequest() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMaterialize(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	repository := strings.TrimPrefix(registry.URL, "http://") + "/extensions/tool"
	digest := pushImage(t, repository, []layerSpec{{data: "raw extension", mediaType: rawMediaType}})
	catalog := writeCatalog(t, repository+"@"+digest)
	destination := t.TempDir()

	paths, err := Materialize(context.Background(), []string{catalog}, []Request{{Name: "tool"}}, "amd64", destination, true)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(destination, "tool.sysext.raw") {
		t.Fatalf("Materialize() paths = %v", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "raw extension" {
		t.Fatalf("materialized data = %q", data)
	}
}

func TestMaterializeLoadsHTTPCatalogAndResolvesVersion(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	repository := strings.TrimPrefix(registry.URL, "http://") + "/extensions/tool"
	digest := pushImage(t, repository, []layerSpec{{data: "version two", mediaType: rawMediaType}})
	catalogData := catalogJSON(t, []catalogLayer{{name: "tool", latest: "v1", versions: map[string]string{"v2": repository + "@" + digest}}})
	catalogServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write(catalogData)
	}))
	t.Cleanup(catalogServer.Close)
	destination := t.TempDir()

	paths, err := Materialize(context.Background(), []string{catalogServer.URL}, []Request{{Name: "tool", Version: "v2"}}, "amd64", destination, true)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "version two" {
		t.Fatalf("materialized data = %q", data)
	}
}

func TestMaterializeRejectsDuplicateNames(t *testing.T) {
	_, err := Materialize(context.Background(), []string{"unused"}, []Request{{Name: "tool"}, {Name: "tool", Version: "v2"}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Materialize() error = %v, want duplicate error", err)
	}
}

func TestMaterializeRejectsUnexpectedLayerCount(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	repository := strings.TrimPrefix(registry.URL, "http://") + "/extensions/tool"
	digest := pushImage(t, repository, []layerSpec{
		{data: "one", mediaType: rawMediaType},
		{data: "two", mediaType: rawMediaType},
	})
	catalog := writeCatalog(t, repository+"@"+digest)

	_, err := Materialize(context.Background(), []string{catalog}, []Request{{Name: "tool"}}, "amd64", t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), "expected exactly one") {
		t.Fatalf("Materialize() error = %v, want layer count error", err)
	}
}

func TestMaterializeCleansTemporaryFilesOnFailure(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	registryHost := strings.TrimPrefix(registry.URL, "http://")
	validRepository := registryHost + "/extensions/valid"
	invalidRepository := registryHost + "/extensions/invalid"
	validDigest := pushImage(t, validRepository, []layerSpec{{data: "valid", mediaType: rawMediaType}})
	invalidDigest := pushImage(t, invalidRepository, []layerSpec{{data: "wrong", mediaType: "application/octet-stream"}})
	catalog := writeCatalogLayers(t, []catalogLayer{
		{name: "valid", latest: "v1", versions: map[string]string{"v1": validRepository + "@" + validDigest}},
		{name: "invalid", latest: "v1", versions: map[string]string{"v1": invalidRepository + "@" + invalidDigest}},
	})
	destination := t.TempDir()

	_, err := Materialize(context.Background(), []string{catalog}, []Request{{Name: "valid"}, {Name: "invalid"}}, "amd64", destination, true)
	if err == nil || !strings.Contains(err.Error(), "media type") {
		t.Fatalf("Materialize() error = %v, want media type error", err)
	}
	entries, readErr := os.ReadDir(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("destination contains files after failure: %v", entries)
	}
}

type layerSpec struct {
	data      string
	mediaType types.MediaType
}

func pushImage(t *testing.T, repository string, specs []layerSpec) string {
	t.Helper()
	image := empty.Image
	for _, spec := range specs {
		var err error
		image, err = mutate.AppendLayers(image, static.NewLayer([]byte(spec.data), spec.mediaType))
		if err != nil {
			t.Fatal(err)
		}
	}
	tag, err := name.NewTag(repository+":test", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(tag, image); err != nil {
		t.Fatal(err)
	}
	digest, err := image.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest.String()
}

func writeCatalog(t *testing.T, oci string) string {
	t.Helper()
	return writeCatalogLayers(t, []catalogLayer{{name: "tool", latest: "v1", versions: map[string]string{"v1": oci}}})
}

type catalogLayer struct {
	name     string
	latest   string
	versions map[string]string
}

func writeCatalogLayers(t *testing.T, layers []catalogLayer) string {
	t.Helper()
	data := catalogJSON(t, layers)
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func catalogJSON(t *testing.T, layers []catalogLayer) []byte {
	t.Helper()
	catalogLayers := make([]any, 0, len(layers))
	for _, layer := range layers {
		tags := make([]any, 0, len(layer.versions))
		for version, oci := range layer.versions {
			tags = append(tags, map[string]any{
				"tag": version, "sysext": map[string]any{"amd64": map[string]string{"oci": oci}},
			})
		}
		catalogLayers = append(catalogLayers, map[string]any{"name": layer.name, "latest": layer.latest, "tags": tags})
	}
	catalog := map[string]any{
		"repo": "test", "layers": catalogLayers,
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMaterializeSearchesCatalogsInOrder(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	registryHost := strings.TrimPrefix(registry.URL, "http://")
	overrideRepository := registryHost + "/extensions/override"
	defaultRepository := registryHost + "/extensions/default"
	overrideDigest := pushImage(t, overrideRepository, []layerSpec{{data: "from the first catalog", mediaType: rawMediaType}})
	defaultDigest := pushImage(t, defaultRepository, []layerSpec{{data: "from the second catalog", mediaType: rawMediaType}})

	// Both catalogs publish "tool", and only the second publishes "extra".
	override := writeCatalog(t, overrideRepository+"@"+overrideDigest)
	fallback := writeCatalogLayers(t, []catalogLayer{
		{name: "tool", latest: "v1", versions: map[string]string{"v1": defaultRepository + "@" + defaultDigest}},
		{name: "extra", latest: "v1", versions: map[string]string{"v1": defaultRepository + "@" + defaultDigest}},
	})
	destination := t.TempDir()

	paths, err := Materialize(context.Background(), []string{override, fallback}, []Request{{Name: "tool"}, {Name: "extra"}}, "amd64", destination, true)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("Materialize() paths = %v", paths)
	}
	data, err := os.ReadFile(filepath.Join(destination, "tool.sysext.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "from the first catalog" {
		t.Fatalf("tool came from %q, want the first catalog", data)
	}
	if _, err := os.Stat(filepath.Join(destination, "extra.sysext.raw")); err != nil {
		t.Fatalf("extra should have resolved against the second catalog: %v", err)
	}
}

func TestMaterializeFailsWhenOneCatalogCannotBeRead(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	repository := strings.TrimPrefix(registry.URL, "http://") + "/extensions/tool"
	digest := pushImage(t, repository, []layerSpec{{data: "raw extension", mediaType: rawMediaType}})
	catalog := writeCatalog(t, repository+"@"+digest)
	missing := filepath.Join(t.TempDir(), "absent.json")

	_, err := Materialize(context.Background(), []string{catalog, missing}, []Request{{Name: "tool"}}, "amd64", t.TempDir(), true)
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("Materialize() error = %v, want the unreadable catalog named", err)
	}
}

func TestCatalogsDefaultsToTheHadronCatalog(t *testing.T) {
	if got := Catalogs(nil); len(got) != 1 || got[0] != DefaultCatalog {
		t.Fatalf("Catalogs(nil) = %v, want [%s]", got, DefaultCatalog)
	}
	// A build that states its catalogs never reads the default one: an
	// operator pointing at their own index must not silently also resolve
	// names against ours.
	configured := []string{"first.json", "second.json"}
	if got := Catalogs(configured); !reflect.DeepEqual(got, configured) {
		t.Fatalf("Catalogs(%v) = %v, want it unchanged", configured, got)
	}
}

func TestMaterializeReadsTheDefaultCatalogWhenNoneIsConfigured(t *testing.T) {
	// No catalog configured used to be refused outright. It now reads
	// DefaultCatalog, so what the build asks for is asserted at the opener
	// rather than by reaching the network.
	original := openCatalogSource
	t.Cleanup(func() { openCatalogSource = original })
	var requested []string
	openCatalogSource = func(_ context.Context, source string) (io.ReadCloser, error) {
		requested = append(requested, source)
		return nil, fmt.Errorf("stub opener")
	}

	_, err := Materialize(context.Background(), nil, []Request{{Name: "tool"}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), DefaultCatalog) {
		t.Fatalf("Materialize() error = %v, want the default catalog named", err)
	}
	if !reflect.DeepEqual(requested, []string{DefaultCatalog}) {
		t.Fatalf("opened catalogs = %v, want [%s]", requested, DefaultCatalog)
	}
}

// writeRawImage drops a .raw extension image on disk and returns its path, so
// a spec can point a file:// request at a fixture the way the e2e suite points
// at tests/assets/sysext.
func writeRawImage(t *testing.T, dir, name, data string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseRequestAcceptsALocalImage(t *testing.T) {
	tests := []struct {
		value string
		want  Request
		ok    bool
	}{
		{value: "file:///assets/work.sysext.raw", want: Request{Name: "file:///assets/work.sysext.raw"}, ok: true},
		{value: "file://relative/work.raw", want: Request{Name: "file://relative/work.raw"}, ok: true},
		// An @ in a path is part of the path: a local image has no catalog
		// version, so nothing after it may be read as one.
		{value: "file:///assets/v1@2/work.raw", want: Request{Name: "file:///assets/v1@2/work.raw"}, ok: true},
		{value: "file://", ok: false},
		{value: "file:///assets/work.sysext", ok: false},
		{value: "file:///assets/", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseRequest(tt.value)
			if (err == nil) != tt.ok {
				t.Fatalf("ParseRequest(%q) error = %v, want success %v", tt.value, err, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ParseRequest(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestMaterializeCopiesALocalImageWithoutReadingACatalog(t *testing.T) {
	// The point of a file:// request is that a build with the image already on
	// disk needs no index and no registry, so the opener must not be reached.
	original := openCatalogSource
	t.Cleanup(func() { openCatalogSource = original })
	openCatalogSource = func(_ context.Context, source string) (io.ReadCloser, error) {
		t.Errorf("read catalog %s for a file:// request", source)
		return nil, fmt.Errorf("stub opener")
	}

	source := writeRawImage(t, t.TempDir(), "work.sysext.raw", "local extension")
	destination := t.TempDir()

	// No architecture either: it only selects a catalog entry, and there is
	// none to select.
	paths, err := Materialize(context.Background(), nil, []Request{{Name: FileScheme + source}}, "", destination, false)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(destination, "work.sysext.raw") {
		t.Fatalf("Materialize() paths = %v, want the base name of the source", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "local extension" {
		t.Fatalf("materialized data = %q", data)
	}
	// The source is staged, not moved or truncated: a second build of the same
	// spec must find it.
	if data, err = os.ReadFile(source); err != nil || string(data) != "local extension" {
		t.Fatalf("source after Materialize = %q, %v, want it untouched", data, err)
	}
}

func TestMaterializeMixesLocalImagesWithCatalogEntries(t *testing.T) {
	registry := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(registry.Close)
	repository := strings.TrimPrefix(registry.URL, "http://") + "/extensions/tool"
	digest := pushImage(t, repository, []layerSpec{{data: "raw extension", mediaType: rawMediaType}})
	catalog := writeCatalog(t, repository+"@"+digest)
	source := writeRawImage(t, t.TempDir(), "work.sysext.raw", "local extension")
	destination := t.TempDir()

	paths, err := Materialize(context.Background(), []string{catalog}, []Request{
		{Name: "tool"},
		{Name: FileScheme + source},
	}, "amd64", destination, true)
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	want := []string{
		filepath.Join(destination, "tool.sysext.raw"),
		filepath.Join(destination, "work.sysext.raw"),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Materialize() paths = %v, want %v", paths, want)
	}
	for index, expected := range []string{"raw extension", "local extension"} {
		data, readErr := os.ReadFile(paths[index])
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != expected {
			t.Fatalf("%s = %q, want %q", paths[index], data, expected)
		}
	}
}

func TestMaterializeRejectsTwoLocalImagesWithOneBaseName(t *testing.T) {
	// Different directories, one destination: the second copy would overwrite
	// the first and the build would bake one image twice.
	first := writeRawImage(t, t.TempDir(), "work.sysext.raw", "first")
	second := writeRawImage(t, t.TempDir(), "work.sysext.raw", "second")

	_, err := Materialize(context.Background(), nil, []Request{
		{Name: FileScheme + first},
		{Name: FileScheme + second},
	}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Materialize() error = %v, want duplicate error", err)
	}
}

func TestMaterializeReportsAMissingLocalImage(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.sysext.raw")

	_, err := Materialize(context.Background(), nil, []Request{{Name: FileScheme + missing}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "absent.sysext.raw") {
		t.Fatalf("Materialize() error = %v, want the missing image named", err)
	}
}

func TestMaterializeRejectsALocalImageThatIsADirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "work.raw")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Materialize(context.Background(), nil, []Request{{Name: FileScheme + directory}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("Materialize() error = %v, want a directory error", err)
	}
}

func TestMaterializeRejectsAVersionOnALocalImage(t *testing.T) {
	source := writeRawImage(t, t.TempDir(), "work.sysext.raw", "local extension")

	_, err := Materialize(context.Background(), nil, []Request{{Name: FileScheme + source, Version: "v1"}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "must not set a version") {
		t.Fatalf("Materialize() error = %v, want a version error", err)
	}
}

func TestMaterializeRejectsALocalImageThatIsNotRaw(t *testing.T) {
	source := writeRawImage(t, t.TempDir(), "work.sysext", "local extension")

	_, err := Materialize(context.Background(), nil, []Request{{Name: FileScheme + source}}, "amd64", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "not a .raw image") {
		t.Fatalf("Materialize() error = %v, want a .raw error", err)
	}
}

func TestMaterializeLeavesNothingBehindWhenALaterLocalImageIsMissing(t *testing.T) {
	// The copies are renamed into place only once every request resolved, so a
	// spec naming one good image and one bad one stages neither.
	dir := t.TempDir()
	good := writeRawImage(t, dir, "good.sysext.raw", "good")
	missing := filepath.Join(dir, "absent.sysext.raw")
	destination := t.TempDir()

	_, err := Materialize(context.Background(), nil, []Request{
		{Name: FileScheme + good},
		{Name: FileScheme + missing},
	}, "amd64", destination, false)
	if err == nil {
		t.Fatal("Materialize() error = nil, want the missing image reported")
	}
	entries, readErr := os.ReadDir(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("destination holds %v, want it empty", names)
	}
}
