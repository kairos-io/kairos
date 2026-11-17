package auroraboot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// DockerBuildArgs is what the docker-build test seam receives. Production
// swaps in an implementation that shells out to `docker build`.
type DockerBuildArgs struct {
	Tag             string
	DockerfilePath  string
	BuildContextDir string
	Logger          io.Writer // when non-nil, stdout+stderr fan into this
}

// AurorabootCLIArgs is what the auroraboot-CLI test seam receives for the
// `auroraboot sysext|confext` invocation.
type AurorabootCLIArgs struct {
	Type          string // "sysext" | "confext"
	Name          string
	SourceImage   string
	Arch          string
	OutputDir     string
	PrivateKey    string
	Certificate   string
	IncludePaths  []string
	ServiceReload bool
	Logger        io.Writer // when non-nil, stdout+stderr fan into this
}

// DockerBuildFunc abstracts `docker build`.
type DockerBuildFunc func(ctx context.Context, args DockerBuildArgs) error

// AurorabootCLIFunc abstracts `auroraboot sysext|confext`.
type AurorabootCLIFunc func(ctx context.Context, args AurorabootCLIArgs) error

// ExtensionBuilder is the in-process builder.ExtensionBuilder implementation.
type ExtensionBuilder struct {
	baseDir         string
	store           store.ExtensionStore
	artifacts       store.ArtifactStore // used to resolve Source.SourceArtifactID
	dockerBuildFn   DockerBuildFunc
	aurorabootCLIFn AurorabootCLIFunc
	logBroadcaster  builder.LogBroadcaster

	mu     sync.RWMutex
	builds map[string]*extBuildState
}

type extBuildState struct {
	status builder.ExtensionBuildStatus
	cancel context.CancelFunc
}

// NewExtensionBuilder creates an in-process ExtensionBuilder. The seams
// default to real shellouts; tests swap them with With* setters.
func NewExtensionBuilder(baseDir string, s store.ExtensionStore) *ExtensionBuilder {
	return &ExtensionBuilder{
		baseDir:         baseDir,
		store:           s,
		dockerBuildFn:   DefaultDockerBuildFunc,
		aurorabootCLIFn: DefaultAurorabootCLIFunc,
		builds:          make(map[string]*extBuildState),
	}
}

// WithDockerBuildFunc swaps the docker-build seam (test entry point).
func (b *ExtensionBuilder) WithDockerBuildFunc(fn DockerBuildFunc) *ExtensionBuilder {
	b.dockerBuildFn = fn
	return b
}

// WithAurorabootCLIFunc swaps the auroraboot CLI seam (test entry point).
func (b *ExtensionBuilder) WithAurorabootCLIFunc(fn AurorabootCLIFunc) *ExtensionBuilder {
	b.aurorabootCLIFn = fn
	return b
}

// WithArtifactStore wires the artifact store used by Source.Mode=artifact
// resolution. Not required for Mode=image or Mode=dockerfile.
func (b *ExtensionBuilder) WithArtifactStore(s store.ArtifactStore) *ExtensionBuilder {
	b.artifacts = s
	return b
}

// WithLogBroadcaster fans every log chunk out to a UI hub. Mirrors the
// existing Builder.WithLogBroadcaster.
func (b *ExtensionBuilder) WithLogBroadcaster(lb builder.LogBroadcaster) *ExtensionBuilder {
	b.logBroadcaster = lb
	return b
}

// DefaultDockerBuildFunc shells out to the host docker daemon.
// Matches the call shape used by Builder.dockerBuild.
var DefaultDockerBuildFunc DockerBuildFunc = func(ctx context.Context, args DockerBuildArgs) error {
	cmd := exec.CommandContext(ctx, "docker", "build", "--no-cache",
		"-t", args.Tag, "-f", args.DockerfilePath, args.BuildContextDir)
	if args.Logger != nil {
		cmd.Stdout = args.Logger
		cmd.Stderr = args.Logger
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker build: %w", err)
		}
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build: %w\n%s", err, string(out))
	}
	return nil
}

// AurorabootCLIArgv renders the argv for the `auroraboot sysext|confext`
// invocation. Split out from DefaultAurorabootCLIFunc so the flag selection is
// assertable without execing the binary: whether a flag is emitted at all is
// the part that broke (see the sysext-only guards below).
func AurorabootCLIArgv(a AurorabootCLIArgs) []string {
	// urfave/cli v2 only parses flags that appear BEFORE positional args;
	// flags placed after `<name> <container>` are silently dropped. Keep
	// every flag in front of the positional pair.
	cliArgs := []string{a.Type, "--arch", a.Arch, "--output", a.OutputDir}
	if a.PrivateKey != "" {
		cliArgs = append(cliArgs, "--private-key", a.PrivateKey)
	}
	if a.Certificate != "" {
		cliArgs = append(cliArgs, "--certificate", a.Certificate)
	}
	// --include-path and --service-reload are declared by SysextCmd only
	// (internal/cmd/sysext.go); ConfextCmd takes just the common flags. Passing
	// either to a confext makes urfave/cli reject the whole invocation with
	// "flag provided but not defined", failing the build.
	if a.Type == "sysext" {
		for _, p := range a.IncludePaths {
			cliArgs = append(cliArgs, "--include-path", p)
		}
		if a.ServiceReload {
			cliArgs = append(cliArgs, "--service-reload")
		}
	}
	return append(cliArgs, a.Name, a.SourceImage)
}

// DefaultAurorabootCLIFunc shells out to this binary's sysext|confext
// subcommand. The command lives in internal/cmd/sysext.go.
var DefaultAurorabootCLIFunc AurorabootCLIFunc = func(ctx context.Context, a AurorabootCLIArgs) error {
	cmd := exec.CommandContext(ctx, "auroraboot", AurorabootCLIArgv(a)...)
	if a.Logger != nil {
		cmd.Stdout = a.Logger
		cmd.Stderr = a.Logger
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("auroraboot %s: %w", a.Type, err)
		}
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("auroraboot %s: %w\n%s", a.Type, err, string(out))
	}
	return nil
}

// Build starts an asynchronous extension build and returns immediately with
// a Pending status. The actual work happens in a goroutine; callers poll
// Status to observe progress.
func (b *ExtensionBuilder) Build(ctx context.Context, opts builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	outputDir := filepath.Join(b.baseDir, id)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	// Build outlives the HTTP request that triggered it.
	buildCtx, cancel := context.WithCancel(context.Background())
	bs := &extBuildState{
		status: builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildPending},
		cancel: cancel,
	}

	b.mu.Lock()
	b.builds[id] = bs
	b.mu.Unlock()

	if b.store != nil {
		rec := &store.ExtensionRecord{
			ID:               id,
			Name:             opts.Name,
			Type:             opts.Type,
			Phase:            builder.BuildPending,
			Arch:             opts.Arch,
			Version:          opts.Version,
			SourceMode:       opts.Source.Mode,
			SourceArtifactID: opts.Source.SourceArtifactID,
			SourceImage:      opts.Source.BaseImage,
			Dockerfile:       opts.Source.Dockerfile,
			ExtraSteps:       opts.Source.ExtraSteps,
			Hierarchies:      opts.Hierarchies,
			ServiceReload:    opts.ServiceReload,
			SigningKeySetID:  opts.SigningKeySetID,
			DownloadToken:    opts.DownloadToken,
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
		}
		if err := b.store.Create(ctx, rec); err != nil {
			cancel()
			return nil, fmt.Errorf("persisting extension record: %w", err)
		}
	}

	go b.run(buildCtx, bs, opts, outputDir)

	return &builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildPending}, nil
}

func (b *ExtensionBuilder) run(ctx context.Context, bs *extBuildState, opts builder.ExtensionBuildOptions, outputDir string) {
	b.setPhase(bs, builder.BuildBuilding, "")

	// Set up a log writer so docker + auroraboot output streams into the DB.
	var logger io.Writer = io.Discard
	if b.store != nil {
		w := &extDBLogWriter{store: b.store, id: bs.status.ID, broadcaster: b.logBroadcaster}
		defer w.Flush()
		logger = w
	}

	containerImage, err := b.resolveSource(ctx, opts, outputDir, logger)
	if err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}
	b.updateContainerImage(bs.status.ID, containerImage)
	b.mu.Lock()
	bs.status.ContainerImage = containerImage
	b.mu.Unlock()

	if err := b.aurorabootCLIFn(ctx, AurorabootCLIArgs{
		Type:          opts.Type,
		Name:          opts.Name,
		SourceImage:   containerImage,
		Arch:          opts.Arch,
		OutputDir:     outputDir,
		PrivateKey:    opts.Signing.PrivateKey,
		Certificate:   opts.Signing.Certificate,
		IncludePaths:  opts.Hierarchies,
		ServiceReload: opts.ServiceReload,
		Logger:        logger,
	}); err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}

	// Cancel only closes the build context, so a CLI that was already
	// finishing can return success after the cancellation. Publishing Ready
	// then hands the caller an extension that Cancel said would not be built.
	if err := ctx.Err(); err != nil {
		b.setPhase(bs, builder.BuildError, err.Error())
		return
	}

	rawFilename := opts.Name + "." + opts.Type + ".raw"
	b.updateRawFilename(bs.status.ID, rawFilename)
	b.mu.Lock()
	bs.status.RawFile = rawFilename
	b.mu.Unlock()
	b.setPhase(bs, builder.BuildReady, "")
}

// resolveSource produces the OCI container reference that `auroraboot
// sysext|confext` will read. Image mode returns the user-supplied tag
// verbatim. Artifact mode pulls the artifact's ContainerImage. Dockerfile
// mode and artifact+extraSteps mode synthesize a Dockerfile, build it, and
// return the resulting tag.
func (b *ExtensionBuilder) resolveSource(ctx context.Context, opts builder.ExtensionBuildOptions, outputDir string, logger io.Writer) (string, error) {
	switch opts.Source.Mode {
	case "image":
		if opts.Source.BaseImage == "" {
			return "", fmt.Errorf("source.baseImage required for mode=image")
		}
		return opts.Source.BaseImage, nil

	case "artifact":
		if b.artifacts == nil {
			return "", fmt.Errorf("artifact store not wired; cannot resolve mode=artifact")
		}
		art, err := b.artifacts.GetByID(ctx, opts.Source.SourceArtifactID)
		if err != nil {
			return "", fmt.Errorf("artifact %s: %w", opts.Source.SourceArtifactID, err)
		}
		if opts.Source.ExtraSteps == "" {
			return art.ContainerImage, nil
		}
		// The reference comes from a stored artifact record, not from the
		// request, so the validating wrapper never saw it.
		if err := builder.ValidateImageRef("artifact image", art.ContainerImage); err != nil {
			return "", err
		}
		dockerfile := fmt.Sprintf("FROM %s\n%s\n", art.ContainerImage, opts.Source.ExtraSteps)
		return b.dockerBuildAndTag(ctx, opts.ID, dockerfile, opts.Source.BuildContextDir, outputDir, logger)

	case "dockerfile":
		if opts.Source.Dockerfile == "" {
			return "", fmt.Errorf("source.dockerfile required for mode=dockerfile")
		}
		return b.dockerBuildAndTag(ctx, opts.ID, opts.Source.Dockerfile, opts.Source.BuildContextDir, outputDir, logger)

	default:
		return "", fmt.Errorf("unsupported source.mode %q", opts.Source.Mode)
	}
}

func (b *ExtensionBuilder) dockerBuildAndTag(ctx context.Context, id, dockerfile, contextDir, outputDir string, logger io.Writer) (string, error) {
	dockerfilePath := filepath.Join(outputDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}
	if contextDir == "" {
		contextDir = outputDir
	}
	tag := "auroraboot-extbuild:" + id
	if err := b.dockerBuildFn(ctx, DockerBuildArgs{
		Tag:             tag,
		DockerfilePath:  dockerfilePath,
		BuildContextDir: contextDir,
		Logger:          logger,
	}); err != nil {
		return "", fmt.Errorf("docker build: %w", err)
	}
	return tag, nil
}

func (b *ExtensionBuilder) updateContainerImage(id, image string) {
	if b.store == nil {
		return
	}
	rec, err := b.store.GetByID(context.Background(), id)
	if err != nil {
		return
	}
	rec.ContainerImage = image
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Create(context.Background(), rec)
}

func (b *ExtensionBuilder) updateRawFilename(id, name string) {
	if b.store == nil {
		return
	}
	rec, err := b.store.GetByID(context.Background(), id)
	if err != nil {
		return
	}
	rec.RawFilename = name
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Create(context.Background(), rec)
}

func (b *ExtensionBuilder) setPhase(bs *extBuildState, phase, msg string) {
	b.mu.Lock()
	bs.status.Phase = phase
	bs.status.Message = msg
	id := bs.status.ID
	b.mu.Unlock()

	if b.store == nil {
		return
	}
	rec, err := b.store.GetByID(context.Background(), id)
	if err != nil {
		return
	}
	rec.Phase = phase
	rec.Message = msg
	rec.UpdatedAt = time.Now().UTC()
	_ = b.store.Create(context.Background(), rec)
}

// Status returns the in-memory state of a known build. Returns an error
// for unknown IDs (callers can fall back to a store lookup if desired).
func (b *ExtensionBuilder) Status(_ context.Context, id string) (*builder.ExtensionBuildStatus, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	bs, ok := b.builds[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	cp := bs.status
	return &cp, nil
}

// List returns one status per known build in arbitrary order.
func (b *ExtensionBuilder) List(_ context.Context) ([]*builder.ExtensionBuildStatus, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*builder.ExtensionBuildStatus, 0, len(b.builds))
	for _, bs := range b.builds {
		cp := bs.status
		out = append(out, &cp)
	}
	return out, nil
}

// Cancel signals the build's context. The goroutine transitions to Error
// when the in-flight seam returns ctx.Err().
func (b *ExtensionBuilder) Cancel(_ context.Context, id string) error {
	b.mu.Lock()
	bs, ok := b.builds[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	bs.cancel()
	return nil
}

// extDBLogWriter mirrors dbLogWriter but writes to ExtensionStore.AppendLog.
type extDBLogWriter struct {
	store       store.ExtensionStore
	id          string
	buf         bytes.Buffer
	mu          sync.Mutex
	broadcaster builder.LogBroadcaster
}

func (w *extDBLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if w.buf.Len() > 4096 {
		w.flushLocked()
	}
	return n, err
}

func (w *extDBLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

func (w *extDBLogWriter) flushLocked() {
	if w.buf.Len() == 0 {
		return
	}
	text := w.buf.String()
	w.buf.Reset()
	_ = w.store.AppendLog(context.Background(), w.id, text)
	if w.broadcaster != nil {
		w.broadcaster.BroadcastLogChunk(w.id, text)
	}
}
