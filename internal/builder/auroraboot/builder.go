package auroraboot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/kairos-io/AuroraBoot/deployer"
	"github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/uki"
	sdklogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/spectrocloud-labs/herd"
)

// DeployerFunc abstracts the AuroraBoot deployer execution so we can mock it
// in tests. logSink, when non-nil, receives a copy of every deployer step's
// zerolog output alongside the process stdout, so the caller's per-build log
// pane sees the same progress the terminal does.
type DeployerFunc func(ctx context.Context, config schema.Config, artifact schema.ReleaseArtifact, outputDir string, logSink io.Writer) error

// UKIBuildFunc abstracts the AuroraBoot pkg/uki.Build call so tests can
// capture the options we pass without running a real build.
type UKIBuildFunc func(opts uki.Options) error

// KairosifyFunc abstracts the kairos-init derivation step so tests can run the
// build goroutine end to end without a container runtime. ensureKairosified
// short-circuits when the Builder has no store, but a test that asserts on the
// persisted artifact record must pass one, so that short circuit is not
// available to it and the step would shell out to `docker build` for real.
type KairosifyFunc func(ctx context.Context, image string, opts builder.BuildOptions, outputDir string, logSink io.Writer) (string, error)

// DefaultDeployerFunc runs AuroraBoot for real. When logSink is non-nil the
// per-Deployer logger is wired to tee stdout + logSink so the UI's live-log
// pane receives the same progress lines the operator sees in their terminal.
// Errors returned from step callbacks (which the deployer aggregates in
// CollectErrors and which typically come from library calls that do NOT
// emit through zerolog - the Lchown/dump-source path in particular) are
// echoed to logSink before we return so the log pane surfaces them, not
// only the artifact record's message field.
func DefaultDeployerFunc(ctx context.Context, config schema.Config, artifact schema.ReleaseArtifact, outputDir string, logSink io.Writer) error {
	d := deployer.NewDeployer(config, artifact, herd.EnableInit)
	if logSink != nil {
		d.Log = teeKairosLogger(d.Log, logSink)
	}
	if err := deployer.RegisterAll(d); err != nil {
		return fmt.Errorf("registering deployer steps: %w", err)
	}
	if err := d.Run(ctx); err != nil {
		if logSink != nil {
			fmt.Fprintf(logSink, "\n=== deployer run failed ===\n%v\n", err)
		}
		return fmt.Errorf("running deployer: %w", err)
	}
	if err := d.CollectErrors(); err != nil {
		if logSink != nil {
			fmt.Fprintf(logSink, "\n=== deployer step errors ===\n%v\n", err)
		}
		return fmt.Errorf("deployer errors: %w", err)
	}
	return nil
}

// DefaultUKIBuildFunc calls pkg/uki.Build for real.
func DefaultUKIBuildFunc(opts uki.Options) error { return uki.Build(opts) }

// Builder implements builder.ArtifactBuilder using AuroraBoot.
type Builder struct {
	builds         map[string]*buildState
	mu             sync.RWMutex
	baseDir        string
	deployFunc     DeployerFunc
	ukiBuildFn     UKIBuildFunc
	platformsFn    PlatformsFunc
	kairosifyFn    KairosifyFunc
	store          store.ArtifactStore
	logBroadcaster builder.LogBroadcaster
	netbootManager *netbootmgr.Manager
	// kairosInitImage is the kairos-init image kairosify uses when a build
	// does not name one.
	kairosInitImage string
}

type buildState struct {
	status builder.BuildStatus
	cancel context.CancelFunc
}

// dbLogWriter buffers log output and periodically flushes to the artifact
// store. When a broadcaster is attached, every flush also fans the chunk
// out to any subscribed UI clients so they can render logs in real time.
// redactValues are substrings scrubbed from every flushed chunk before
// either sink sees it, so a build step that echoes the injected
// cloud-config does not persist the fleet registration token or the
// default node password.
type dbLogWriter struct {
	store        store.ArtifactStore
	id           string
	buf          bytes.Buffer
	mu           sync.Mutex
	ctx          context.Context
	broadcaster  builder.LogBroadcaster
	redactValues []string
}

func (w *dbLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if w.buf.Len() > 4096 {
		w.flush()
	}
	return n, err
}

func (w *dbLogWriter) flush() {
	if w.buf.Len() == 0 {
		return
	}
	text := w.buf.String()
	w.buf.Reset()
	text = builder.RedactLine(text, w.redactValues)
	_ = w.store.AppendLog(w.ctx, w.id, text)
	if w.broadcaster != nil {
		w.broadcaster.BroadcastLogChunk(w.id, text)
	}
}

// Flush writes any buffered log data to the store.
func (w *dbLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush()
}

// New creates a new AuroraBoot-backed builder. If deployFunc is nil, DefaultDeployerFunc is used.
// If artifactStore is nil, the builder falls back to in-memory only (useful for tests).
func New(baseDir string, deployFunc DeployerFunc, artifactStore store.ArtifactStore) *Builder {
	if deployFunc == nil {
		deployFunc = DefaultDeployerFunc
	}
	return &Builder{
		builds:      make(map[string]*buildState),
		baseDir:     baseDir,
		deployFunc:  deployFunc,
		ukiBuildFn:  DefaultUKIBuildFunc,
		platformsFn: dockerBuildxPlatforms,
		store:       artifactStore,

		kairosInitImage: defaultKairosInitImage + ":" + defaultKairosInitVersion,
	}
}

// WithKairosInitImage sets the kairos-init image used for builds that do not
// name one. It becomes a FROM line in the kairosify Dockerfile, so it is
// validated here, once, rather than on every build. An empty ref keeps the
// pinned default.
func (b *Builder) WithKairosInitImage(ref string) (*Builder, error) {
	if ref == "" {
		return b, nil
	}
	if err := builder.ValidateImageRef("kairos-init image", ref); err != nil {
		return nil, err
	}
	b.kairosInitImage = ref
	return b, nil
}

// WithUKIBuildFunc swaps the pkg/uki.Build implementation used by buildUKI.
// Tests use this to capture the uki.Options we pass through.
func (b *Builder) WithUKIBuildFunc(fn UKIBuildFunc) *Builder {
	b.ukiBuildFn = fn
	return b
}

// WithPlatformsFunc swaps the buildx platform probe used by the cross-arch
// preflight. Tests use this so no unit test shells out to the developer's own
// docker and depends on the host's binfmt state.
func (b *Builder) WithPlatformsFunc(fn PlatformsFunc) *Builder {
	b.platformsFn = fn
	return b
}

// WithKairosifyFunc swaps the kairos-init derivation step. Tests that need a
// store use this to keep the build goroutine off `docker build`, which would
// otherwise pull the base and kairos-init images and run for minutes on a
// machine that has a container runtime.
func (b *Builder) WithKairosifyFunc(fn KairosifyFunc) *Builder {
	b.kairosifyFn = fn
	return b
}

// WithLogBroadcaster attaches a broadcaster that receives every log chunk
// flushed by a build. Usually a *ws.UIHub — but kept as an interface so
// the builder package doesn't have to import ws.
func (b *Builder) WithLogBroadcaster(lb builder.LogBroadcaster) *Builder {
	b.logBroadcaster = lb
	return b
}

// WithNetbootManager attaches the server's shared netboot Manager, threaded
// into the deploy context so a build with the netboot output enabled starts
// its server through the same state the dashboard's Start/Stop/Status
// endpoints use, instead of one the UI has no way to see or stop. The CLI
// never sets this, and StepStartNetboot falls back to its previous
// behavior when it's absent.
func (b *Builder) WithNetbootManager(m *netbootmgr.Manager) *Builder {
	b.netbootManager = m
	return b
}

// Build starts an asynchronous artifact build and returns immediately with a Pending status.
func (b *Builder) Build(ctx context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	// Reject unsafe admin-supplied values before any work starts. Covers
	// kairos-init flag interpolation in the Dockerfile RUN line.
	if err := validateKairosInitOptions(opts); err != nil {
		return nil, fmt.Errorf("%w: %v", builder.ErrInvalidBuildOptions, err)
	}
	// An extension name the catalog cannot be asked for is a bad request, not
	// a build that fails half an hour in with the ISO already half written.
	if _, err := extensions.ParseRequests(opts.Extensions); err != nil {
		return nil, fmt.Errorf("%w: %v", builder.ErrInvalidBuildOptions, err)
	}

	id := opts.ID
	if id == "" {
		id = uuid.New().String()
		opts.ID = id
	}

	outputDir := filepath.Join(b.baseDir, id)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	// Use Background — the build outlives the HTTP request that triggered it.
	// The request ctx would cancel when the handler returns the 201 response.
	buildCtx, cancel := context.WithCancel(context.Background())

	bs := &buildState{
		status: builder.BuildStatus{
			ID:    id,
			Phase: builder.BuildPending,
		},
		cancel: cancel,
	}

	b.mu.Lock()
	b.builds[id] = bs
	b.mu.Unlock()

	// Persist artifact record in DB if store is available.
	if b.store != nil {
		kubernetesEnabled := opts.Provisioning.KubernetesEnabled
		rec := &store.ArtifactRecord{
			ID:                      id,
			Name:                    opts.Name,
			Phase:                   store.ArtifactPending,
			BaseImage:               opts.BaseImage,
			KairosVersion:           opts.KairosVersion,
			Model:                   opts.Model,
			ISO:                     opts.ISO,
			CloudImage:              opts.CloudImage,
			Netboot:                 opts.Netboot,
			FIPS:                    opts.FIPS,
			TrustedBoot:             opts.TrustedBoot,
			Arch:                    opts.Source.Arch,
			Variant:                 opts.Source.Variant,
			AllowInsecureRegistries: opts.Source.AllowInsecureRegistries,
			RawDisk:                 opts.Outputs.RawDisk,
			Tar:                     opts.Outputs.Tar,
			GCE:                     opts.Outputs.GCE,
			VHD:                     opts.Outputs.VHD,
			MAAS:                    opts.Outputs.MAAS,
			KairosInitImage:         opts.KairosInitImage,
			AutoInstall:             opts.Provisioning.AutoInstall,
			RegisterAuroraBoot:      opts.Provisioning.RegisterAuroraBoot,
			Dockerfile:              opts.Dockerfile,
			HadronBase:              opts.HadronBase,
			HadronFirmware:          opts.HadronFirmware,
			HadronLayers:            opts.HadronLayers,
			HadronExtra:             opts.HadronExtra,
			CloudConfig:             opts.CloudConfig,
			// The handler's own store.Create fallback (pkg/handlers/artifacts.go)
			// is skipped when the row already exists, so any field left out here
			// is silently dropped for local builds. The clone flow in the
			// ArtifactBuilder reads these directly, so an omission surfaces as
			// an empty Kubernetes card on the cloned build.
			KubernetesDistro:  opts.Source.KubernetesDistro,
			KubernetesVersion: opts.Source.KubernetesVersion,
			KubernetesEnabled: &kubernetesEnabled,
			TargetGroupID:     opts.Provisioning.TargetGroupID,
			OverlayID:         opts.OverlayID,
			// The artifact page's Build summary and the Clone flow read the
			// catalog extensions back from the row (kairos-io/kairos#5274).
			Extensions:         opts.Extensions,
			ExtensionsCatalogs: opts.ExtensionsCatalogs,
			ExtensionHierarchies: store.ExtensionHierarchies{
				Sysext:  opts.ExtensionHierarchies.Sysext,
				Confext: opts.ExtensionHierarchies.Confext,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := b.store.Create(ctx, rec); err != nil {
			cancel()
			return nil, fmt.Errorf("persisting artifact record: %w", err)
		}
	}

	go b.run(buildCtx, bs, opts, outputDir)

	return &builder.BuildStatus{
		ID:    id,
		Phase: builder.BuildPending,
	}, nil
}

func (b *Builder) run(ctx context.Context, bs *buildState, opts builder.BuildOptions, outputDir string) {
	b.setPhase(bs, builder.BuildBuilding, "")

	// Update DB phase.
	if b.store != nil {
		_ = b.updateDBPhase(ctx, bs.status.ID, store.ArtifactBuilding, "")
	}

	// Set up log writer for DB persistence plus an optional live broadcast
	// to subscribed UI clients.
	var logWriter *dbLogWriter
	if b.store != nil {
		logWriter = &dbLogWriter{
			store:        b.store,
			id:           bs.status.ID,
			ctx:          context.Background(), // use background so flushes work even after build ctx cancel
			broadcaster:  b.logBroadcaster,
			redactValues: opts.LogRedactValues,
		}
	}

	// Step 1: If Dockerfile is provided, build a container image from it.
	// The Dockerfile takes precedence — BaseImage is only used when no Dockerfile is set.
	containerImage := opts.BaseImage
	if opts.Dockerfile != "" {
		if logWriter != nil {
			fmt.Fprintf(logWriter, "=== Building from Dockerfile ===\n")
			if opts.BaseImage != "" {
				fmt.Fprintf(logWriter, "Note: Dockerfile overrides base image (%s)\n\n", opts.BaseImage)
			}
			logWriter.Flush()
		}
		img, err := b.dockerBuild(ctx, opts, outputDir, logWriter)
		if err != nil {
			msg := fmt.Sprintf("docker build failed: %v", err)
			b.setPhase(bs, builder.BuildError, msg)
			if b.store != nil {
				if logWriter != nil {
					logWriter.Flush()
				}
				_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
			}
			return
		}
		containerImage = img
	}

	if opts.Dockerfile == "" && logWriter != nil {
		fmt.Fprintf(logWriter, "=== Using base image: %s ===\n\n", containerImage)
		logWriter.Flush()
	}

	// Step 1.5: Direct base images must always be derived so requested provider
	// and variant settings are applied. Dockerfile images only need derivation
	// when the Dockerfile did not produce a complete Kairos image.
	var err error
	if opts.Dockerfile == "" && b.store != nil {
		containerImage, err = b.kairosify(ctx, containerImage, opts, outputDir, logWriter)
	} else {
		containerImage, err = b.ensureKairosified(ctx, containerImage, opts, outputDir, logWriter)
	}
	if err != nil {
		msg := fmt.Sprintf("kairosify failed: %v", err)
		b.setPhase(bs, builder.BuildError, msg)
		if b.store != nil {
			if logWriter != nil {
				logWriter.Flush()
			}
			_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
		}
		return
	}

	// Step 2: Assemble AuroraBoot config and run deployer.
	if logWriter != nil {
		fmt.Fprintf(logWriter, "=== Starting AuroraBoot build ===\n")
		fmt.Fprintf(logWriter, "Image: %s\n", containerImage)
		if opts.ISO {
			fmt.Fprintf(logWriter, "Output: ISO\n")
		}
		if opts.CloudImage {
			fmt.Fprintf(logWriter, "Output: Cloud Image (raw disk)\n")
		}
		if opts.Netboot {
			fmt.Fprintf(logWriter, "Output: Netboot\n")
		}
		fmt.Fprintf(logWriter, "Output dir: %s\n", outputDir)
		// The cloud-config carries the registration token, node passwords
		// and whatever the operator added, and this log is stored and
		// streamed to every UI client. Only its size is logged: most of
		// the document is a predictable template, so even a digest of it
		// would let a log reader test guesses for the secrets inside.
		if opts.CloudConfig != "" {
			fmt.Fprintf(logWriter, "Cloud config: %d bytes\n", len(opts.CloudConfig))
		}
		fmt.Fprintf(logWriter, "\n")
		logWriter.Flush()
	}
	config, artifact, err := b.assembleConfig(opts, containerImage, outputDir)
	if err != nil {
		msg := fmt.Sprintf("auroraboot failed: %v", err)
		b.setPhase(bs, builder.BuildError, msg)
		if b.store != nil {
			if logWriter != nil {
				logWriter.Flush()
			}
			_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
		}
		return
	}
	var sink io.Writer
	if logWriter != nil {
		sink = logWriter
	}
	if b.netbootManager != nil {
		ctx = netbootmgr.WithManager(ctx, b.netbootManager)
	}
	if err := b.deployFunc(ctx, config, artifact, outputDir, sink); err != nil {
		msg := fmt.Sprintf("auroraboot failed: %v", err)
		b.setPhase(bs, builder.BuildError, msg)
		if b.store != nil {
			if logWriter != nil {
				logWriter.Flush()
			}
			_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
		}
		return
	}

	if logWriter != nil {
		fmt.Fprintf(logWriter, "\n=== AuroraBoot build completed ===\n")
		logWriter.Flush()
	}

	// Step 2.5: Save container image as TAR if requested.
	if opts.Outputs.Tar && containerImage != "" {
		if logWriter != nil {
			fmt.Fprintf(logWriter, "\n=== Saving container image as TAR ===\n")
			logWriter.Flush()
		}
		tarPath := filepath.Join(outputDir, "container.tar")
		cmd := exec.CommandContext(ctx, "docker", "save", "-o", tarPath, containerImage)
		if logWriter != nil {
			cmd.Stdout = logWriter
			cmd.Stderr = logWriter
		}
		if err := cmd.Run(); err != nil {
			// A failed or killed save leaves a partial tar behind that
			// would be listed as an artifact.
			_ = os.Remove(tarPath)
			if ctx.Err() != nil {
				// Cancel already recorded the build as cancelled.
				return
			}
			msg := fmt.Sprintf("saving the container image as TAR failed: %v", err)
			if logWriter != nil {
				fmt.Fprintf(logWriter, "%s\n", msg)
				logWriter.Flush()
			}
			b.setPhase(bs, builder.BuildError, msg)
			if b.store != nil {
				_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
			}
			return
		}
	}

	// Step 2.6: Clean up ISO files when the caller didn't request ISO output.
	// Netboot extraction needs a generated ISO as its source, so GenISO may
	// still have run as scaffolding — but the user never asked for the ISO
	// itself and we shouldn't keep multi-GB files around or list them as
	// build outputs. The raw-disk paths don't produce ISOs at all, so the
	// only files present here would be that scaffolding ISO. This runs
	// before the UKI step, whose output is an ISO the user did ask for. A
	// Trusted Boot build asks for the ISO and gets the UKI one, so the
	// scaffolding ISO goes here too rather than sitting next to it.
	if !opts.Outputs.ISO || opts.Outputs.UKIISO() {
		for _, pattern := range []string{"*.iso", "*.iso.sha256"} {
			matches, _ := filepath.Glob(filepath.Join(outputDir, pattern))
			for _, p := range matches {
				if err := os.Remove(p); err != nil && logWriter != nil {
					fmt.Fprintf(logWriter, "Warning: could not remove unrequested %s: %v\n", filepath.Base(p), err)
				}
			}
		}
	}

	// Step 2.7: Build UKI (Unified Kernel Image) if requested.
	if opts.Outputs.UKIISO() {
		if logWriter != nil {
			fmt.Fprintf(logWriter, "\n=== Building UKI (Unified Kernel Image) ===\n")
			logWriter.Flush()
		}
		if err := b.buildUKI(ctx, opts, containerImage, outputDir, logWriter); err != nil {
			msg := fmt.Sprintf("UKI build failed: %v", err)
			if logWriter != nil {
				fmt.Fprintf(logWriter, "%s\n", msg)
				logWriter.Flush()
			}
			b.setPhase(bs, builder.BuildError, msg)
			if b.store != nil {
				_ = b.updateDBPhase(context.Background(), bs.status.ID, store.ArtifactError, msg)
			}
			return
		}
	}

	// A cancelled build stays in the Error state Cancel gave it; do not
	// overwrite it with Ready.
	if ctx.Err() != nil {
		return
	}

	// Step 3: Collect artifact paths.
	artifacts := collectArtifacts(outputDir)
	if logWriter != nil {
		fmt.Fprintf(logWriter, "Artifacts:\n")
		for _, a := range artifacts {
			fmt.Fprintf(logWriter, "  %s\n", filepath.Base(a))
		}
		logWriter.Flush()
	}

	b.mu.Lock()
	bs.status.Phase = builder.BuildReady
	bs.status.Message = ""
	bs.status.Artifacts = artifacts
	b.mu.Unlock()

	// Update DB with final state.
	if b.store != nil {
		if logWriter != nil {
			logWriter.Flush()
		}
		rec, err := b.store.GetByID(context.Background(), bs.status.ID)
		if err == nil {
			rec.Phase = store.ArtifactReady
			rec.Message = ""
			rec.ArtifactFiles = artifacts
			rec.ContainerImage = containerImage
			rec.UpdatedAt = time.Now()
			_ = b.store.Update(context.Background(), rec)
		}
	}
}

func (b *Builder) updateDBPhase(ctx context.Context, id, phase, message string) error {
	rec, err := b.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	rec.Phase = phase
	rec.Message = message
	rec.UpdatedAt = time.Now()
	return b.store.Update(ctx, rec)
}

func (b *Builder) setPhase(bs *buildState, phase, message string) {
	b.mu.Lock()
	bs.status.Phase = phase
	bs.status.Message = message
	b.mu.Unlock()
}

// kairosRelease reads /etc/kairos-release from image. ok is false when the
// image has none, which means it is not a Kairos image yet.
func (b *Builder) kairosRelease(ctx context.Context, image string) (release map[string]string, ok bool) {
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "cat", image, "/etc/kairos-release").Output()
	if err != nil {
		return nil, false
	}
	release, err = godotenv.Unmarshal(string(out))
	if err != nil {
		return map[string]string{}, true
	}
	return release, true
}

// kairosInitValueRe is the allowlist for admin-supplied values that end up
// inside the generated Dockerfile's RUN line (model, kairos version, k8s
// version). Those values are concatenated into a shell command that runs in a
// privileged build container, so they must not contain any shell
// metacharacters. The set below covers every real Kairos/k8s/model value:
//   - versions like "v3.2.1", "v1.30.0+k3s1", "latest", "24.04"
//   - models like "generic", "rpi4", "nvidia-jetson-agx-orin"
//   - the ":" and "/" that can appear in image-ish or k3s version strings
//
// It deliberately rejects space, ;, &, |, $, backtick, quotes, newline,
// >, <, (, ) and every other shell metacharacter.
var kairosInitValueRe = regexp.MustCompile(`^[A-Za-z0-9._+:/-]+$`)

// validateKairosInitValue rejects a value that would be unsafe to interpolate
// into the kairos-init Dockerfile RUN line. Empty values are the caller's
// responsibility (they are optional and simply omitted from the flags).
func validateKairosInitValue(field, value string) error {
	if !kairosInitValueRe.MatchString(value) {
		return fmt.Errorf("invalid %s %q: must match %s (no shell metacharacters)", field, value, kairosInitValueRe.String())
	}
	return nil
}

// validateKairosInitOptions checks every admin-supplied field that reaches the
// generated Dockerfile RUN body. Optional fields are only validated when set.
// It returns early (before any build starts) so a malicious value never
// reaches the build container.
func validateKairosInitOptions(opts builder.BuildOptions) error {
	if opts.Model != "" {
		if err := validateKairosInitValue("model", opts.Model); err != nil {
			return err
		}
	}
	if opts.KairosVersion != "" {
		if err := validateKairosInitValue("kairos version", opts.KairosVersion); err != nil {
			return err
		}
	}
	if opts.KubernetesVersion != "" {
		if err := validateKairosInitValue("kubernetes version", opts.KubernetesVersion); err != nil {
			return err
		}
	}
	if opts.KubernetesDistro != "" {
		if err := validateKairosInitValue("kubernetes distro", opts.KubernetesDistro); err != nil {
			return err
		}
	}
	return nil
}

// ensureKairosified defensively checks whether an image is already a complete
// Kairos image before deriving one with kairos-init.
func (b *Builder) ensureKairosified(ctx context.Context, image string, opts builder.BuildOptions, outputDir string, logWriter *dbLogWriter) (string, error) {
	if b.store == nil {
		// Test mode — skip kairosification (no Docker available)
		return image, nil
	}
	// The probe below is itself a `docker run`, so an injected derivation step
	// has to take precedence over it, not only over the build it guards.
	if b.kairosifyFn == nil {
		if release, ok := b.kairosRelease(ctx, image); ok {
			// kairos-init is skipped for this image, so its -t never runs. The
			// UKI step would pack a GRUB image into a signed UKI that does not
			// boot as Trusted Boot.
			if (opts.TrustedBoot || opts.Outputs.TrustedBoot) && release["KAIROS_TRUSTED_BOOT"] != "true" {
				return "", fmt.Errorf("the Dockerfile built a Kairos image without Trusted Boot (KAIROS_TRUSTED_BOOT is not true in /etc/kairos-release); build it with kairos-init -t true or turn Trusted Boot off")
			}
			return image, nil
		}
	}
	return b.kairosify(ctx, image, opts, outputDir, logWriter)
}

// kairosify builds a per-artifact Kairos image using kairos-init.
func (b *Builder) kairosify(ctx context.Context, image string, opts builder.BuildOptions, outputDir string, logWriter *dbLogWriter) (string, error) {
	// This step emits "RUN /kairos-init", which executes a target-architecture
	// binary, so refuse the build now if the daemon host cannot emulate that
	// architecture. Reported as kairos-io/kairos#4088.
	if err := b.checkBuildPlatform(ctx, opts.Source.Arch); err != nil {
		return "", err
	}
	if err := validateKairosInitOptions(opts); err != nil {
		return "", fmt.Errorf("validating kairos-init options: %w", err)
	}
	// Checked after validation so an injected step can never be handed options
	// the real one would have refused.
	if b.kairosifyFn != nil {
		var sink io.Writer
		if logWriter != nil {
			sink = logWriter
		}
		return b.kairosifyFn(ctx, image, opts, outputDir, sink)
	}
	kairosInitImage := opts.KairosInitImage
	if kairosInitImage == "" {
		kairosInitImage = b.kairosInitImage
	}

	// Build kairos-init flags
	var flags []string
	if opts.Model != "" {
		flags = append(flags, "-m", opts.Model)
	}
	if opts.KubernetesDistro != "" {
		flags = append(flags, "-p", opts.KubernetesDistro)
	}
	if opts.KubernetesDistro == "k3s" && opts.KubernetesVersion != "" {
		flags = append(flags, "--provider-k3s-version", opts.KubernetesVersion)
	} else if opts.KubernetesDistro == "k0s" && opts.KubernetesVersion != "" {
		flags = append(flags, "--provider-k0s-version", opts.KubernetesVersion)
	}
	if opts.FIPS {
		flags = append(flags, "--fips")
	}
	if opts.TrustedBoot {
		flags = append(flags, "-t", "true")
	}
	version := opts.KairosVersion
	if version == "" {
		version = "latest"
	}
	flags = append(flags, "--version", version)

	flagStr := strings.Join(flags, " ")

	dockerfile := fmt.Sprintf(`FROM %s AS kairos-init
FROM %s
COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install %s && \
    /kairos-init -l debug -s init %s && \
    rm -f /kairos-init
`, kairosInitImage, image, flagStr, flagStr)

	// Write Dockerfile
	dockerfilePath := filepath.Join(outputDir, "Dockerfile.kairosify")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing kairosify Dockerfile: %w", err)
	}

	tag := fmt.Sprintf("auroraboot-kairos:%s", opts.ID)
	args := []string{"build"}
	args = append(args, dockerBuildPlatformArgs(opts)...)
	args = append(args, "-t", tag, "-f", dockerfilePath, outputDir)
	cmd := exec.CommandContext(ctx, "docker", args...)
	if logWriter != nil {
		cmd.Stdout = logWriter
		cmd.Stderr = logWriter
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("kairosify docker build: %w", err)
	}
	return tag, nil
}

// assembleConfig builds the AuroraBoot schema.Config and schema.ReleaseArtifact from BuildOptions.
func (b *Builder) assembleConfig(opts builder.BuildOptions, containerImage, outputDir string) (schema.Config, schema.ReleaseArtifact, error) {
	config := schema.Config{
		State:             outputDir,
		DisableHTTPServer: true,
		DisableNetboot:    !opts.Netboot,
		DisableISOboot:    !opts.ISO || opts.Outputs.UKIISO(),
	}

	if opts.Source.Arch != "" {
		config.Arch = opts.Source.Arch
	}

	allowInsecure := opts.Source.AllowInsecureRegistries
	config.AllowInsecureRegistries = &allowInsecure

	if opts.CloudImage || opts.Outputs.RawDisk || opts.Outputs.GCE || opts.Outputs.VHD || opts.Outputs.MAAS {
		config.Disk.EFI = true
	}
	config.Disk.GCE = opts.Outputs.GCE
	config.Disk.VHD = opts.Outputs.VHD
	config.Disk.MAAS = opts.Outputs.MAAS

	if opts.CloudConfig != "" {
		config.CloudConfig = opts.CloudConfig
	}

	if opts.OverlayRootfs != "" {
		config.ISO.OverlayRootfs = opts.OverlayRootfs
	}

	requests, err := extensions.ParseRequests(opts.Extensions)
	if err != nil {
		return schema.Config{}, schema.ReleaseArtifact{}, err
	}
	config.ISO.Extensions = requests
	config.ISO.ExtensionsCatalogs = opts.ExtensionsCatalogs

	artifact := schema.ReleaseArtifact{}
	if containerImage != "" {
		artifact.ContainerImage = containerImage
	}

	return config, artifact, nil
}

// buildUKI invokes AuroraBoot's pkg/uki library to produce a UKI ISO.
func (b *Builder) buildUKI(ctx context.Context, opts builder.BuildOptions, containerImage, outputDir string, logWriter *dbLogWriter) error {
	signing := opts.Signing
	if signing.UKISecureBootKey == "" || signing.UKISecureBootCert == "" || signing.UKITPMPCRKey == "" {
		return fmt.Errorf("UKI requires SecureBoot keys: sb-key, sb-cert, and tpm-pcr-private-key must all be provided")
	}

	log := sdklogger.NewKairosLogger("auroraboot-uki", "info", false)
	if logWriter != nil {
		log.Logger = log.Logger.Output(logWriter)
	}

	extensionRequests, err := extensions.ParseRequests(opts.Extensions)
	if err != nil {
		return err
	}

	ukiOpts := uki.Options{
		// containerImage may be a plain Docker reference (quay.io/...), a
		// locally-tagged image (auroraboot-*), or a fully schemed source URI
		// (oci:, docker:, etc.). NewSrcFromURI resolves all of these downstream,
		// so pass it through unchanged rather than forcing a scheme here.
		Source:                  containerImage,
		OutputDir:               outputDir,
		OutputType:              string(constants.IsoOutput),
		Name:                    "kairos",
		SBKey:                   signing.UKISecureBootKey,
		SBCert:                  signing.UKISecureBootCert,
		TPMPCRPrivateKey:        signing.UKITPMPCRKey,
		PublicKeysDir:           signing.UKIPublicKeysDir,
		SecureBootEnroll:        signing.UKISecureBootEnroll,
		OverlayRootfs:           opts.OverlayRootfs,
		AllowInsecureRegistries: opts.Source.AllowInsecureRegistries,
		Extensions:              extensionRequests,
		ExtensionsCatalogs:      opts.ExtensionsCatalogs,
		CloudConfig:             opts.CloudConfig,
		Logger:                  &log,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- b.ukiBuildFn(ukiOpts) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dockerBuild runs `docker build` when a Dockerfile is provided.
func (b *Builder) dockerBuild(ctx context.Context, opts builder.BuildOptions, outputDir string, logWriter *dbLogWriter) (string, error) {
	// A custom Dockerfile's RUN steps execute target-architecture binaries, so
	// refuse the build now rather than letting it die on an exec format error
	// that names neither the architecture nor the missing emulator.
	if err := b.checkBuildPlatform(ctx, opts.Source.Arch); err != nil {
		return "", err
	}

	// The UI's Hadron composer emits middle content only (no FROM line) so
	// the operator backend can hand it to the kairos-operator as OCISpec
	// wrapped by BuildOptions.BaseImage. On the local backend the Dockerfile
	// must have its own FROM before docker will accept it, so prepend one
	// from the structured HadronBase field. Custom (non-Hadron) Dockerfile
	// builds carry their own FROM and are written verbatim.
	dockerfile := opts.Dockerfile
	if opts.HadronBase != "" {
		dockerfile = "FROM " + opts.HadronBase + "\n" + dockerfile
	}
	dockerfilePath := filepath.Join(outputDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	tag := fmt.Sprintf("auroraboot-build:%s", opts.ID)

	buildContext := opts.BuildContextDir
	if buildContext == "" {
		buildContext = outputDir
	}

	args := []string{"build", "--no-cache"}
	args = append(args, dockerBuildPlatformArgs(opts)...)
	args = append(args, "-t", tag, "-f", dockerfilePath, buildContext)
	cmd := exec.CommandContext(ctx, "docker", args...)
	if logWriter != nil {
		cmd.Stdout = logWriter
		cmd.Stderr = logWriter
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return tag, nil
}

func dockerBuildPlatformArgs(opts builder.BuildOptions) []string {
	if opts.Source.Arch == "" {
		return nil
	}
	return []string{"--platform", "linux/" + opts.Source.Arch}
}

// Status returns the current build status for a given ID.
func (b *Builder) Status(ctx context.Context, id string) (*builder.BuildStatus, error) {
	// Try DB first if store is available.
	if b.store != nil {
		rec, err := b.store.GetByID(ctx, id)
		if err == nil {
			return &builder.BuildStatus{
				ID:        rec.ID,
				Phase:     rec.Phase,
				Message:   rec.Message,
				Artifacts: rec.ArtifactFiles,
			}, nil
		}
	}

	// Fall back to in-memory.
	b.mu.RLock()
	defer b.mu.RUnlock()

	bs, ok := b.builds[id]
	if !ok {
		return nil, fmt.Errorf("build %q not found", id)
	}

	status := bs.status // copy
	return &status, nil
}

// List returns status for all builds.
func (b *Builder) List(ctx context.Context) ([]*builder.BuildStatus, error) {
	// Try DB first if store is available.
	if b.store != nil {
		records, err := b.store.List(ctx)
		if err == nil {
			result := make([]*builder.BuildStatus, 0, len(records))
			for _, rec := range records {
				result = append(result, &builder.BuildStatus{
					ID:        rec.ID,
					Phase:     rec.Phase,
					Message:   rec.Message,
					Artifacts: rec.ArtifactFiles,
				})
			}
			return result, nil
		}
	}

	// Fall back to in-memory.
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]*builder.BuildStatus, 0, len(b.builds))
	for _, bs := range b.builds {
		status := bs.status // copy
		result = append(result, &status)
	}
	return result, nil
}

// Cancel cancels a running build. Cancel is idempotent: an unknown id (e.g.
// a store row that survived a restart while the in-memory builds map did not)
// is not an error. The builder has nothing to cancel and reports success so
// the caller does not have to distinguish "already gone" from "cannot cancel".
func (b *Builder) Cancel(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	bs, ok := b.builds[id]
	if !ok {
		return nil
	}

	bs.cancel()

	// A finished build stays in b.builds. Its phase and its row are final,
	// so a cancel that races completion must not turn a Ready artifact into
	// an Error one that "Clear failed" would then delete.
	if bs.status.Phase != builder.BuildPending && bs.status.Phase != builder.BuildBuilding {
		return nil
	}

	bs.status.Phase = builder.BuildError
	bs.status.Message = "cancelled"

	// Update DB if store is available.
	if b.store != nil {
		_ = b.updateDBPhase(context.Background(), id, store.ArtifactError, "cancelled")
	}

	return nil
}

// collectArtifacts returns only the final build output files (ISOs, disk images, checksums),
// skipping the unpacked rootfs and intermediate build files.
func collectArtifacts(dir string) []string {
	// Known artifact file extensions
	artifactExts := map[string]bool{
		".iso":    true,
		".raw":    true,
		".raw.gz": true, // MAAS ddgz output (gzipped raw)
		".img":    true,
		".tar":    true,
		".tar.gz": true,
		".vhd":    true,
		".sha256": true,
	}

	var artifacts []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip intermediate directories (unpacked rootfs, build temps)
		if info.IsDir() && (info.Name() == "temp-rootfs" || info.Name() == "build" || info.Name() == "rootfs") {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		for ext := range artifactExts {
			if strings.HasSuffix(name, ext) {
				artifacts = append(artifacts, path)
				return nil
			}
		}
		return nil
	})
	return artifacts
}
