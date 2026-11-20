package redfish

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stmcginnis/gofish/schemas"
)

// This file implements the READ-ONLY `redfish probe` diagnostic (design §4c / RFC
// 0001 P2). Probe recombines the existing discovery building blocks — the same the
// deploy flow uses — to report what a BMC actually exposes and to emit a starter
// quirk profile (tier C: UNVERIFIED) the operator can tweak. It performs NO writes:
// no InsertMedia, no boot PATCH, no Reset, no Eject. Like Inspect, it returns a
// plain ProbeReport so gofish types never leak into internal/cmd (the D1 guardrail).

// ProbeReport is the plain, gofish-free result of Deployer.Probe. internal/cmd
// renders it to human-readable text and/or a starter YAML quirk profile; it never
// sees a gofish object.
type ProbeReport struct {
	// Endpoint is the BMC endpoint that was probed.
	Endpoint string
	// HasSessionService reports whether the ServiceRoot advertises a SessionService
	// (a top-level SessionService or Links.Sessions). When false, session auth is
	// unavailable and only Basic auth works.
	HasSessionService bool
	// AuthModeUsed is the auth mode the probe's connection actually used
	// ("session" or "basic"), after resolving AuthModeAuto against the ServiceRoot.
	AuthModeUsed string

	// SystemIDs lists the Redfish Ids of every ComputerSystem member, in collection
	// order. With more than one, the operator must pass --system-id (and set
	// BMCTarget.SystemID); the media/reset/inspect sections below describe
	// SelectedSystemID.
	SystemIDs []string
	// SelectedSystemID is the ComputerSystem the media/reset/inspect sections were
	// gathered against. With exactly one system it is that system; with more than
	// one and no --system-id it is the first member (MultipleSystems is then true).
	SelectedSystemID string
	// MultipleSystems is true when the BMC exposes more than one ComputerSystem and
	// no explicit system was pinned, so the report is for the first member only.
	MultipleSystems bool

	// System is the typed hardware summary of SelectedSystemID (model, manufacturer,
	// serial, memory, CPU, detected features). Serial is printed as-is: this is the
	// operator's own BMC. firmware is reported separately below.
	System SystemInfo
	// FirmwareVersion is the system's BIOS/firmware version string when the BMC
	// advertises one (ComputerSystem.BiosVersion), otherwise empty.
	FirmwareVersion string

	// Media is the flattened, spec-default-ordered VirtualMedia candidate list (the
	// same []MediaView the deploy flow's quirk seam receives): System-hosted first,
	// then each Manager's. Empty when the BMC exposes no virtual media.
	Media []MediaView
	// DefaultCDIndex is the index into Media of the member the spec-default search
	// would pick for a CD/DVD deployment, or -1 when none is CD/DVD-capable.
	DefaultCDIndex int
	// ManagerHostedCDOnly is true when the only CD/DVD-capable media is hosted on a
	// Manager (the HPE iLO signal): the starter profile then suggests a
	// manager-first mediaSearch order.
	ManagerHostedCDOnly bool

	// PowerState is the selected system's current Redfish PowerState (e.g. "On").
	PowerState string
	// AllowableResetTypes is the system's advertised ResetType allowable values,
	// empty when the BMC does not advertise them.
	AllowableResetTypes []string
	// DefaultResetType is the ResetType the core would choose by default for the
	// current power state, validated against AllowableResetTypes.
	DefaultResetType string
}

// Probe performs a read-only diagnostic of the connected BMC and returns a plain
// ProbeReport. It must be called after Connect. It selects the system the same way
// Deploy does when a SystemID is pinned; otherwise, to stay diagnostic against a
// multi-system BMC (where selectSystem deliberately refuses to guess), it reports
// every system Id and gathers the media/reset/inspect sections against the first
// member, flagging MultipleSystems so the caller can tell the operator to pin one.
//
// Probe issues only GETs (discovery, Inspect, reset-type allowable values). It never
// inserts media, patches boot, resets, or ejects.
func (d *Deployer) Probe(ctx context.Context) (*ProbeReport, error) {
	if d.client == nil {
		return nil, errors.New("not connected: call Connect first")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	report := &ProbeReport{
		Endpoint:          d.endpoint,
		HasSessionService: !d.usedBasicAuth,
		DefaultCDIndex:    -1,
	}
	if d.usedBasicAuth {
		report.AuthModeUsed = string(AuthModeBasic)
	} else {
		report.AuthModeUsed = string(AuthModeSession)
	}
	// Session auth proves a SessionService, and auto only falls back to Basic when
	// the ServiceRoot has none. An explicit basic Connect never read the
	// ServiceRoot, so read it here: the auth mode used says nothing about it.
	// HasSessionService is informational, so a failed read (a 5xx, a body that
	// does not parse, a redirect our stricter client rejects) leaves it false
	// rather than failing a probe that worked before this read existed.
	if d.usedBasicAuth && d.authMode == AuthModeBasic {
		if hasSession, err := d.serviceRootHasSessionService(ctx); err == nil {
			report.HasSessionService = hasSession
		}
	}

	systems, err := d.client.GetService().Systems()
	if err != nil {
		return nil, fmt.Errorf("discovering systems: %w", d.scrub(err))
	}
	if len(systems) == 0 {
		return nil, errors.New("no ComputerSystem members found on the Redfish service")
	}
	report.SystemIDs = systemIDs(systems)

	// Pick the system to describe. Honour an explicit SystemID; otherwise, unlike
	// the deploy flow's fail-safe selectSystem (which refuses to guess), the probe
	// describes the first member but flags that the operator must pin one.
	system := systems[0]
	if d.systemID != "" {
		found := false
		for _, sys := range systems {
			if sys.ID == d.systemID {
				system, found = sys, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no ComputerSystem with Id %q found on the Redfish service; available system Ids: %s",
				d.systemID, strings.Join(report.SystemIDs, ", "))
		}
	} else if len(systems) > 1 {
		report.MultipleSystems = true
	}
	report.SelectedSystemID = system.ID

	// Inspect (read-only): model/manufacturer/serial/memory/cpu/features.
	report.System = SystemInfo{
		ID:           system.ID,
		Name:         system.Name,
		Model:        system.Model,
		Manufacturer: system.Manufacturer,
		SerialNumber: system.SerialNumber,
		PowerState:   string(system.PowerState),
		Features:     detectFeatures(system),
	}
	if v := system.MemorySummary.TotalSystemMemoryGiB; v != nil {
		report.System.MemoryGiB = int(*v)
	}
	if v := system.ProcessorSummary.Count; v != nil {
		report.System.ProcessorCount = int(*v)
	}
	report.FirmwareVersion = strings.TrimSpace(system.BiosVersion)

	// Virtual media: the flattened, spec-default-ordered candidate views — exactly
	// what the deploy flow's mediaSearch hook sees. No writes; discovery GETs only.
	candidates, views := d.mediaCandidates(system)
	report.Media = views
	report.DefaultCDIndex = firstCDIndex(candidates)
	report.ManagerHostedCDOnly = onlyManagerHostedCD(candidates, views)

	// Reset: the advertised allowable types and the core's default for this state.
	report.PowerState = string(system.PowerState)
	if allowed, err := system.GetSupportedResetTypes(); err == nil {
		report.AllowableResetTypes = resetTypeStrings(allowed)
	}
	report.DefaultResetType = string(chooseResetType(system))

	return report, nil
}

// firstCDIndex returns the index of the first CD/DVD-capable media in the
// spec-default candidate order, or -1 when none advertises CD/DVD.
func firstCDIndex(candidates []*schemas.VirtualMedia) int {
	for i, vm := range candidates {
		if mediaSupportsCD(vm) {
			return i
		}
	}
	return -1
}

// onlyManagerHostedCD reports whether the BMC exposes CD/DVD-capable virtual media
// AND every such member is Manager-hosted (Location "manager:<id>"). That is the
// HPE iLO signal: the spec-default System-first search finds nothing, and a
// manager-first mediaSearch order is required. It returns false when there is no
// CD/DVD media at all, or when at least one CD/DVD member is System-hosted (the
// spec default already works).
func onlyManagerHostedCD(candidates []*schemas.VirtualMedia, views []MediaView) bool {
	sawCD := false
	for i, vm := range candidates {
		if !mediaSupportsCD(vm) {
			continue
		}
		sawCD = true
		if views[i].Location == "system" {
			return false
		}
	}
	return sawCD
}
