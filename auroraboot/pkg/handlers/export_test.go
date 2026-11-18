package handlers

import (
	"context"
	"io"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

// RedfishFinalizer is the test-visible alias of the unexported redfishFinalizer
// interface, so external (handlers_test) tests can supply a fake BMC client to the
// eject/finalize path without standing up a real gofish connection.
type RedfishFinalizer = redfishFinalizer

// WithTestFinalizerFactory injects a fake redfishFinalizer factory keyed on the
// redfish.Config the deploy/finalize path would otherwise pass to NewDeployer. It
// returns the handler for chaining. Test-only.
func (h *DeployHandler) WithTestFinalizerFactory(f func(cfg redfish.Config) RedfishFinalizer) *DeployHandler {
	return h.WithFinalizerFactory(func(cfg redfish.Config) redfishFinalizer { return f(cfg) })
}

// MarkEjectPendingForTest exposes the unexported markEjectPending so a test can arm
// a deployment's eject lifecycle without driving the full async deploy goroutine.
func (h *DeployHandler) MarkEjectPendingForTest(id string) { h.markEjectPending(id) }

// ResolveOperatorImageURL exposes the unexported image-URL precedence helper so
// the per-deploy > per-BMC > global-default selection can be unit-tested
// directly, without driving the async deploy goroutine.
var ResolveOperatorImageURL = resolveOperatorImageURL

// NetbootController is the test-visible alias of the unexported
// netbootController interface, so external tests can stand in for the
// netboot manager without launching a PXE server.
type NetbootController = netbootController

// WithTestNetboot replaces the handler's netboot manager. Test-only.
func (h *DeployHandler) WithTestNetboot(nb NetbootController) *DeployHandler {
	h.netboot = nb
	return h
}

// WithTestImageExporter replaces the docker pipeline that ExportImage runs to
// build an artifact's image tar. Test-only.
func (h *ArtifactHandler) WithTestImageExporter(f func(ctx context.Context, id, containerImage string, w io.Writer) error) *ArtifactHandler {
	h.exportImage = f
	return h
}

// StderrTail exposes the unexported stderrTail helper used to attach docker
// stderr to export errors.
var StderrTail = stderrTail

// ImageExportTimeout exposes the upper bound on one image export build.
const ImageExportTimeout = imageExportTimeout

// RemoveExportLeftovers exposes the cleanup of a crashed export's docker
// objects.
var RemoveExportLeftovers = removeExportLeftovers

// SetDockerCleanupTimeout shortens the bound on docker cleanup commands and
// returns a function that restores it. Test-only.
func SetDockerCleanupTimeout(d time.Duration) func() {
	prev := dockerCleanupTimeout
	dockerCleanupTimeout = d
	return func() { dockerCleanupTimeout = prev }
}
