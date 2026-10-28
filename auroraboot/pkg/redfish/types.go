package redfish

import "time"

// AuthMode selects how the Deployer authenticates to the Redfish service.
type AuthMode string

const (
	// AuthModeAuto (the default, and the value chosen for an empty AuthMode)
	// pre-checks the ServiceRoot: it uses session auth when the endpoint
	// advertises a SessionService, and falls back to HTTP Basic auth when it does
	// not (e.g. sushy-tools emulators and some BMCs expose no SessionService).
	AuthModeAuto AuthMode = "auto"
	// AuthModeSession forces Redfish session auth (a deletable session with an
	// X-Auth-Token). It requires the endpoint to expose a SessionService.
	AuthModeSession AuthMode = "session"
	// AuthModeBasic forces HTTP Basic auth: credentials are sent on every request
	// and no session is created (so there is nothing to tear down).
	AuthModeBasic AuthMode = "basic"
)

// VendorType selects a vendor quirks profile. The flow itself is spec-compliant
// for every vendor; the type is a seam for the per-vendor quirk hooks added in a
// later phase (#4112). Today every value resolves to the spec-default behaviour.
type VendorType string

const (
	// VendorGeneric targets spec-compliant BMCs (DMTF, sushy-tools, PiKVM). Default.
	VendorGeneric VendorType = "generic"
	// VendorSuperMicro selects the Supermicro quirks profile.
	VendorSuperMicro VendorType = "supermicro"
	// VendorHPE selects the HPE iLO quirks profile.
	VendorHPE VendorType = "ilo"
	// VendorDMTF selects the DMTF/PiKVM quirks profile.
	VendorDMTF VendorType = "dmtf"
)

// BootMode is the firmware boot mode for the one-time boot override.
type BootMode string

const (
	// BootModeUEFI boots the machine in UEFI mode (the AuroraBoot default).
	BootModeUEFI BootMode = "UEFI"
	// BootModeLegacy boots the machine in legacy/BIOS mode.
	BootModeLegacy BootMode = "Legacy"
)

// BootTarget is the one-time boot device the system is overridden to.
type BootTarget string

const (
	// BootTargetCd boots from the inserted virtual CD/DVD media. The default for
	// a virtual-media ISO deployment.
	BootTargetCd BootTarget = "Cd"
	// BootTargetUSB boots from virtual USB media.
	BootTargetUSB BootTarget = "Usb"
)

// SystemInfo is the typed hardware summary returned by Inspect. It is populated
// from gofish's ComputerSystem (memory/cpu are read from the nested
// MemorySummary/ProcessorSummary, fixing the old flat-field 0/0 bug, #13).
type SystemInfo struct {
	ID             string
	Name           string
	Model          string
	Manufacturer   string
	SerialNumber   string
	PowerState     string
	MemoryGiB      int
	ProcessorCount int
	// Features holds the capabilities AuroraBoot positively detected for this
	// system (keyed by feature name, e.g. "UEFI", "SecureBoot"; value always
	// true). A feature absent from this map was NOT detected and the hardware gate
	// treats it as unsupported. See features.go for how each is derived.
	Features map[string]bool
}

// DeployRequest describes a virtual-media deployment. InsertMedia is URL-pull
// (decision D4): the BMC fetches the ISO from ImageURL, so the caller must serve
// the image somewhere the BMC can reach. A local path is intentionally NOT
// accepted here.
type DeployRequest struct {
	// ImageURL is the HTTP(S)/NFS/CIFS URL the BMC pulls the ISO from. Required.
	ImageURL string
	// BootTarget is the one-time boot device. Defaults to Cd.
	BootTarget BootTarget
	// BootMode is the firmware boot mode for the one-time boot override. When
	// empty (the default) the BootSourceOverrideMode is NOT sent in the boot
	// PATCH, leaving the system in its current firmware mode — this avoids forcing
	// a firmware-mode change that some BMCs/emulators reject. Set it only to force
	// a specific mode (BootModeUEFI/BootModeLegacy).
	BootMode BootMode
	// ResetType overrides the automatically chosen power action. When empty the
	// Deployer picks On when the system is off and ForceRestart otherwise,
	// validated against the system's allowable values.
	ResetType string
	// TransferProtocolHTTPS sets the InsertMedia TransferProtocolType to HTTPS.
	// The v1 default is HTTP-on-trusted-L2; integrity is delegated to the Kairos
	// image signature, so HTTP is the unset default.
	TransferProtocolHTTPS bool
	// EjectAfter is DEPRECATED and no longer honoured by Deploy. Ejecting the media
	// right after the deploy Task completes is the WRONG time: on BMCs that ignore a
	// one-time boot override the post-install reboot then re-runs the installer (the
	// "install loop"). Eject is now a separate, signal-driven step — call Finalize
	// when the OS is actually up. The field is retained only so old callers still
	// compile; setting it has no effect.
	EjectAfter bool
	// Progress, when non-nil, is invoked at each deploy stage with a short step
	// label and a monotonically increasing percentage (0..100). It lets callers
	// surface live progress (e.g. onto a Deployment row). It must be cheap and is
	// always called synchronously from Deploy's goroutine. nil disables reporting.
	Progress func(step string, percent int)
}

// FinalizeRequest describes a post-install finalize: eject the virtual media and
// (best-effort) steer the next boot to disk. It carries no image URL — finalize
// never inserts media. The connection parameters live on the Config the Deployer
// was built with (Endpoint/SystemID/etc.); FinalizeRequest only tunes the flow.
type FinalizeRequest struct {
	// Progress, when non-nil, is invoked at each finalize stage with a short step
	// label and a monotonically increasing percentage (0..100). nil disables it.
	Progress func(step string, percent int)
	// PowerCycle, when true, switches Finalize to the power-cycle path: power the
	// system OFF, eject the media while it is off, steer the next boot to disk, then
	// power it back ON. This is the robust mode for BMCs/emulators (observed with
	// sushy-tools on libvirt) that report a media eject on a *running* machine but do
	// not actually apply it — the live machine keeps booting the ISO until a power
	// transition. On real hardware live eject usually works, so the default is false
	// (the in-place eject, unchanged). The eject is still load-bearing in either mode.
	PowerCycle bool
}

// DeployResult is the outcome of a deployment.
type DeployResult struct {
	// SystemID is the discovered ComputerSystem member ID the ISO was deployed to.
	SystemID string
	// MediaID is the discovered VirtualMedia member ID used for InsertMedia.
	MediaID string
	// TaskCompleted is true when an async Task was polled to a terminal,
	// successful state. It is false for synchronous (non-202) BMC responses.
	TaskCompleted bool
	// TaskState is the terminal Redfish TaskState when a Task was polled
	// (e.g. "Completed"), otherwise empty.
	TaskState string
	// Messages carries any human-readable messages surfaced by the BMC Task.
	Messages []string
	// StartedAt / FinishedAt bracket the deploy flow.
	StartedAt  time.Time
	FinishedAt time.Time
}
