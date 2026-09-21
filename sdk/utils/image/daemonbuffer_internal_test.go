package image

// This file is in package image, not image_test, because the assertion it
// makes is about the daemon options GetImage itself passes. Reaching them
// through the exported surface would only prove that the option exists, not
// that the pull uses it.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	v1random "github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	api "github.com/moby/moby/api/types/image"
	mobyclient "github.com/moby/moby/client"
)

// saveStreamBytes is the size of the layer inside the synthetic `docker save`
// stream the fake daemon serves. It has to be large enough that buffering the
// stream on the heap is unmistakable next to the ordinary allocations of a
// pull, and small enough to stay cheap in CI.
const saveStreamBytes = 32 << 20

// heapBudget is how much heap growth the pull is allowed while the image is
// still reachable. Memory buffering retains the whole save stream on
// imageOpener.bytes, so it lands above saveStreamBytes; file buffering copies
// through a fixed io.Copy buffer and lands in the low megabytes.
const heapBudget = 8 << 20

// fakeDaemon serves one prebuilt `docker save` tarball from disk and records
// how many times it was asked for it. It implements daemon.Client.
type fakeDaemon struct {
	tarPath   string
	inspect   api.InspectResponse
	history   []api.HistoryResponseItem
	saveCalls atomic.Int64
}

func (f *fakeDaemon) Ping(_ context.Context, _ mobyclient.PingOptions) (mobyclient.PingResult, error) {
	return mobyclient.PingResult{APIVersion: "1.48"}, nil
}

func (f *fakeDaemon) ImageSave(_ context.Context, _ []string, _ ...mobyclient.ImageSaveOption) (mobyclient.ImageSaveResult, error) {
	f.saveCalls.Add(1)
	return os.Open(f.tarPath)
}

func (f *fakeDaemon) ImageInspect(_ context.Context, _ string, _ ...mobyclient.ImageInspectOption) (mobyclient.ImageInspectResult, error) {
	return mobyclient.ImageInspectResult{InspectResponse: f.inspect}, nil
}

func (f *fakeDaemon) ImageHistory(_ context.Context, _ string, _ ...mobyclient.ImageHistoryOption) (mobyclient.ImageHistoryResult, error) {
	return mobyclient.ImageHistoryResult{Items: f.history}, nil
}

func (f *fakeDaemon) ImageLoad(_ context.Context, _ io.Reader, _ ...mobyclient.ImageLoadOption) (mobyclient.ImageLoadResult, error) {
	return nil, fmt.Errorf("ImageLoad is not part of the pull path")
}

func (f *fakeDaemon) ImageTag(_ context.Context, _ mobyclient.ImageTagOptions) (mobyclient.ImageTagResult, error) {
	return mobyclient.ImageTagResult{}, fmt.Errorf("ImageTag is not part of the pull path")
}

// writeSaveStream builds a docker-save-format tarball holding one image with a
// single layer of byteSize random bytes, writes it to dir and returns its path
// alongside the inspect response a daemon would report for it. Nothing about
// the image is retained: the caller measures heap, so the bytes used to build
// the archive must be collectable before it starts.
func writeSaveStream(t *testing.T, dir string, byteSize int64) (string, api.InspectResponse, []api.HistoryResponseItem) {
	t.Helper()

	img, err := v1random.Image(byteSize, 1)
	if err != nil {
		t.Fatalf("building the synthetic image: %v", err)
	}

	tarPath := filepath.Join(dir, "save.tar")
	ref, err := name.NewTag("kairos.test/oom:latest")
	if err != nil {
		t.Fatalf("parsing the tag: %v", err)
	}
	if err := tarball.WriteToFile(tarPath, ref, img); err != nil {
		t.Fatalf("writing the save stream: %v", err)
	}

	// The daemon image reads its config from `docker inspect`, not from the
	// tarball, so the inspect response has to agree with the archive on the
	// diff IDs and with GetImage on the platform, or GetImage discards the
	// daemon image and falls through to a remote pull.
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatalf("reading the synthetic config: %v", err)
	}
	digest, err := img.ConfigName()
	if err != nil {
		t.Fatalf("reading the synthetic config digest: %v", err)
	}

	layers := make([]string, 0, len(cfg.RootFS.DiffIDs))
	for _, d := range cfg.RootFS.DiffIDs {
		layers = append(layers, d.String())
	}

	inspect := api.InspectResponse{
		ID:           digest.String(),
		RepoTags:     []string{ref.Name()},
		Created:      time.Now().UTC().Format(time.RFC3339Nano),
		Architecture: runtime.GOARCH,
		Os:           runtime.GOOS,
		RootFS:       api.RootFS{Type: "layers", Layers: layers},
	}
	history := []api.HistoryResponseItem{{
		ID:        digest.String(),
		Created:   time.Now().Unix(),
		CreatedBy: "random",
		Size:      byteSize,
		Tags:      []string{ref.Name()},
	}}

	return tarPath, inspect, history
}

// heapInUse reports the bytes of live heap after a collection, so what it
// returns is retained rather than merely allocated.
func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// tempTarballs lists the temporary archives the daemon package's file-backed
// opener leaves in dir while its image is alive.
func tempTarballs(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "go-containerregistry-*.tar"))
	if err != nil {
		t.Fatalf("globbing %s: %v", dir, err)
	}
	return matches
}

// TestGetImageDoesNotBufferTheDaemonSaveOnTheHeap is kairos-io/kairos#3037: a
// local image pulled through GetImage used to be read into memory whole, so an
// image larger than available RAM aborted the build with
// "fatal error: runtime: out of memory" before any layer was extracted.
func TestGetImageDoesNotBufferTheDaemonSaveOnTheHeap(t *testing.T) {
	tmp := t.TempDir()
	// The daemon package spools through os.CreateTemp(""), so pointing TMPDIR
	// at a directory this test owns is what makes the spool file observable.
	t.Setenv("TMPDIR", tmp)

	streamDir := t.TempDir()
	tarPath, inspect, history := writeSaveStream(t, streamDir, saveStreamBytes)

	streamSize := int64(0)
	if fi, err := os.Stat(tarPath); err != nil {
		t.Fatalf("stat %s: %v", tarPath, err)
	} else {
		streamSize = fi.Size()
	}
	if streamSize < saveStreamBytes {
		t.Fatalf("save stream is %d bytes, expected at least %d; the fixture is not exercising the buffer", streamSize, saveStreamBytes)
	}

	fake := &fakeDaemon{tarPath: tarPath, inspect: inspect, history: history}

	// Add the fake client to the options GetImage already uses, rather than
	// replacing them, so the buffering choice under test stays in effect.
	restore := daemonImageOptions
	daemonImageOptions = append(append([]daemon.Option{}, restore...), daemon.WithClient(fake))
	t.Cleanup(func() { daemonImageOptions = restore })

	baseline := heapInUse()

	img, err := GetImage("kairos.test/oom:latest", "", nil, nil)
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}

	// Drain every layer instead of running ExtractOCIImage: the extractor
	// chowns each entry to its recorded UID/GID, which fails as a non-root
	// user and would prevent this test from running in CI. Reading each layer
	// end-to-end still forces the daemon opener to spool the save stream, so
	// the buffering choice is exercised the same way.
	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	if len(layers) == 0 {
		t.Fatal("the synthetic image reports zero layers; the fixture is not exercising the buffer")
	}
	for i, l := range layers {
		rc, err := l.Uncompressed()
		if err != nil {
			t.Fatalf("layer %d Uncompressed: %v", i, err)
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			rc.Close()
			t.Fatalf("draining layer %d: %v", i, err)
		}
		if err := rc.Close(); err != nil {
			t.Fatalf("closing layer %d: %v", i, err)
		}
	}

	// Measured while img, and so the opener holding the buffer, is still
	// reachable. Reading the stat before the assertions keeps that true.
	peak := heapInUse()
	spools := tempTarballs(t, tmp)
	saves := fake.saveCalls.Load()
	runtime.KeepAlive(img)

	if peak > baseline+heapBudget {
		t.Errorf("heap grew by %d bytes over a %d byte save stream, budget is %d: the stream is being buffered in memory",
			peak-baseline, streamSize, heapBudget)
	}

	if len(spools) != 1 {
		t.Errorf("found %d spool files in %s, want exactly 1: the save stream is not being buffered to disk", len(spools), tmp)
	} else if fi, err := os.Stat(spools[0]); err != nil {
		t.Errorf("stat %s: %v", spools[0], err)
	} else if fi.Size() != streamSize {
		t.Errorf("spool file is %d bytes, save stream is %d: the whole stream did not reach disk", fi.Size(), streamSize)
	}

	// Unbuffered would also keep the heap flat, by re-running `docker save`
	// for the manifest and again for every layer. On a multi-gigabyte image
	// that trade is not worth making, so the count is part of the contract.
	if saves != 1 {
		t.Errorf("the daemon was asked to save the image %d times, want 1", saves)
	}

}

// v1.Hash is imported for the diff-ID conversion in writeSaveStream; keep the
// reference explicit so the import survives a refactor of that helper.
var _ = v1.Hash{}
