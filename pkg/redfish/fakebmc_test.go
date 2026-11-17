package redfish_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// fakeBMC is a spec-accurate, in-memory Redfish service used to drive the
// gofish-backed Deployer in tests. gofish talks to it over real HTTP, so every
// document it serves (ServiceRoot, collections, ComputerSystem, VirtualMedia,
// Task) must be valid Redfish/odata. The fake records the requests it received
// so assertions can verify the protocol flow (session body, no hardcoded IDs,
// InsertMedia URL-pull body, boot override PATCH, ResetType, session DELETE).
type fakeBMC struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest

	// behaviour toggles
	insertMediaStatus int  // HTTP status the InsertMedia action returns (default 202)
	mediaOnManager    bool // serve VirtualMedia on the Manager, not the System
	taskStates        []string
	taskIdx           int

	// ejectCalled records that the VirtualMedia.EjectMedia action was invoked, so
	// Finalize specs can assert the load-bearing eject actually fired.
	ejectCalled bool
	// bootPatchStatus, when non-zero, is the HTTP status the ComputerSystem boot
	// PATCH returns. Default 200. Set 4xx/5xx to drive the boot-to-disk error path
	// and confirm Finalize still succeeds (boot-to-disk is best-effort).
	bootPatchStatus int

	// noSessionService, when true, serves a ServiceRoot WITHOUT a SessionService
	// (no top-level SessionService and no Links.Sessions) and returns 404 for the
	// session-create path, mimicking sushy-tools emulators and BMCs that expose no
	// session auth. All other resources (Systems/VirtualMedia/Task) are still
	// served so the Basic-auth deploy path can be exercised.
	noSessionService bool

	// serviceRootFailAfter, when non-zero, serves that many ServiceRoot GETs and
	// answers 503 to every later one, so a ServiceRoot read that happens after
	// Connect (gofish reads it once while connecting) can be made to fail.
	serviceRootFailAfter int
	serviceRootServed    int

	// systemIDs is the set of ComputerSystem Ids the Systems collection exposes.
	// Defaults to a single "sys-xyz" member (matching the original single-system
	// fake). Set more than one to exercise the fail-safe system selector. Each Id
	// gets its own ComputerSystem, VirtualMedia and Reset action under
	// /redfish/v1/Systems/{id}.
	systemIDs []string

	// insertMediaExtendedInfo, when true, makes a failing InsertMedia return a
	// Redfish error body carrying @Message.ExtendedInfo (Message/MessageId/
	// Resolution) so the error-surfacing path can be asserted.
	insertMediaExtendedInfo bool

	// bootModeAllowableValues, when non-nil, is served as the Boot
	// "BootSourceOverrideMode@Redfish.AllowableValues" annotation so UEFI feature
	// detection can be driven. nil omits the annotation entirely (the common
	// real-world case where a BMC does not advertise supported boot modes).
	bootModeAllowableValues []string
	// withSecureBoot, when true, adds a SecureBoot link to the ComputerSystem so
	// SecureBoot feature detection can be asserted.
	withSecureBoot bool

	// biosVersion, when non-empty, is served as the ComputerSystem BiosVersion so
	// the probe's firmware reporting can be asserted. Empty omits the field.
	biosVersion string

	// captured bodies
	sessionBody     map[string]any
	insertBody      map[string]any
	bootPatchBody   map[string]any
	resetBody       map[string]any
	sessionLocation string
	// resetSystemID records the Id of the ComputerSystem whose Reset action was
	// last invoked, so a multi-system spec can assert the correct machine was hit.
	resetSystemID string
	// resetTypes records every ResetType received, in order, so the power-cycle
	// finalize specs can assert the off->on sequence around the eject.
	resetTypes []string

	// powerState is the PowerState the ComputerSystem reports. The power-cycle
	// finalize re-fetches the system and polls this to Off; a GracefulShutdown/
	// ForceOff Reset flips it to Off so pollPowerOff terminates, an On Reset flips it
	// back to On. Defaults to "On".
	powerState string
}

type recordedRequest struct {
	Method   string
	Path     string
	AuthType string // "Basic", "Bearer", "Token" (X-Auth-Token), or "" (none)
}

const fakeAuthToken = "fake-x-auth-token-123"

func newFakeBMC() *fakeBMC {
	f := &fakeBMC{
		insertMediaStatus: http.StatusAccepted,
		// Drive the Task to a terminal Completed state on the second poll so the
		// poll loop is genuinely exercised.
		taskStates:      []string{"Running", "Completed"},
		sessionLocation: "/redfish/v1/SessionService/Sessions/sess-abc",
		// Default to a UEFI-capable system: it advertises UEFI in its boot-mode
		// allowable values, matching the common server BMC and the UEFI default of
		// the deploy path.
		bootModeAllowableValues: []string{"Legacy", "UEFI"},
		// A single system by default, preserving the original fake's behaviour.
		systemIDs: []string{"sys-xyz"},
		// Systems power on by default (matches the computerSystem PowerState below).
		powerState: "On",
	}
	f.server = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeBMC) Close() { f.server.Close() }

func (f *fakeBMC) URL() string { return f.server.URL }

// record stores the method+path and the kind of auth credential an incoming
// request carried, so tests can assert session vs Basic auth.
func (f *fakeBMC) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	authType := ""
	switch {
	case strings.HasPrefix(r.Header.Get("Authorization"), "Basic "):
		authType = "Basic"
	case strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "):
		authType = "Bearer"
	case r.Header.Get("X-Auth-Token") != "":
		authType = "Token"
	}
	f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, AuthType: authType})
}

// serviceRootExhausted counts a ServiceRoot GET and reports whether it is past
// serviceRootFailAfter and must fail.
func (f *fakeBMC) serviceRootExhausted() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.serviceRootFailAfter == 0 {
		return false
	}
	f.serviceRootServed++
	return f.serviceRootServed > f.serviceRootFailAfter
}

// sawAuthType reports whether any recorded request to a path with the given
// prefix carried the given auth type ("Basic"/"Bearer"/"Token").
func (f *fakeBMC) sawAuthType(authType, pathPrefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		if req.AuthType == authType && strings.HasPrefix(req.Path, pathPrefix) {
			return true
		}
	}
	return false
}

// finalizeEventOrder returns the ordered sequence of the load-bearing finalize
// actions as they were received: "power-off" (a GracefulShutdown/ForceOff Reset),
// "eject" (VirtualMedia.EjectMedia), and "power-on" (an On/ForceOn Reset). It lets a
// power-cycle spec assert the off -> eject -> on ordering. Boot PATCHes and other
// requests are ignored.
func (f *fakeBMC) finalizeEventOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	resetIdx := 0
	var events []string
	for _, req := range f.requests {
		switch {
		case strings.HasSuffix(req.Path, "/Actions/VirtualMedia.EjectMedia") && req.Method == http.MethodPost:
			events = append(events, "eject")
		case strings.HasSuffix(req.Path, "/Actions/ComputerSystem.Reset") && req.Method == http.MethodPost:
			// Map this reset to the ResetType captured at the same ordinal.
			if resetIdx < len(f.resetTypes) {
				switch f.resetTypes[resetIdx] {
				case "GracefulShutdown", "ForceOff":
					events = append(events, "power-off")
				case "On", "ForceOn":
					events = append(events, "power-on")
				}
			}
			resetIdx++
		}
	}
	return events
}

// sawRequest reports whether a request with the given method and path prefix was
// received.
func (f *fakeBMC) sawRequest(method, pathPrefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		if req.Method == method && strings.HasPrefix(req.Path, pathPrefix) {
			return true
		}
	}
	return false
}

func decodeBody(r *http.Request) map[string]any {
	b, err := io.ReadAll(r.Body)
	if err != nil || len(b) == 0 {
		return nil
	}
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

func (f *fakeBMC) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1":
		if f.serviceRootExhausted() {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		f.writeJSON(w, f.serviceRoot())

	// A session-less BMC (sushy-style) has no SessionService: the create path 404s.
	case r.URL.Path == "/redfish/v1/SessionService/Sessions" && r.Method == http.MethodPost && f.noSessionService:
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)

	// --- session lifecycle ---
	case r.URL.Path == "/redfish/v1/SessionService/Sessions" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.sessionBody = decodeBody(r)
		f.mu.Unlock()
		w.Header().Set("X-Auth-Token", fakeAuthToken)
		w.Header().Set("Location", f.sessionLocation)
		w.WriteHeader(http.StatusCreated)
		f.writeJSON(w, map[string]any{
			"@odata.id":   f.sessionLocation,
			"@odata.type": "#Session.v1_3_0.Session",
			"Id":          "sess-abc",
			"Name":        "User Session",
			"UserName":    "admin",
		})
	case r.URL.Path == f.sessionLocation && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)

	// --- systems ---
	case r.URL.Path == "/redfish/v1/Systems":
		f.writeJSON(w, f.systemsCollection())
	case f.isSystemPath(r.URL.Path) && r.Method == http.MethodGet:
		f.writeJSON(w, f.computerSystem(f.systemIDFromPath(r.URL.Path)))
	case f.isSystemPath(r.URL.Path) && r.Method == http.MethodPatch:
		f.mu.Lock()
		f.bootPatchBody = decodeBody(r)
		status := f.bootPatchStatus
		f.mu.Unlock()
		if status >= 400 {
			http.Error(w, "boot PATCH rejected", status)
			return
		}
		w.WriteHeader(http.StatusOK)
		f.writeJSON(w, f.computerSystem(f.systemIDFromPath(r.URL.Path)))
	case strings.HasSuffix(r.URL.Path, "/Actions/ComputerSystem.Reset") && r.Method == http.MethodPost:
		f.mu.Lock()
		f.resetBody = decodeBody(r)
		f.resetSystemID = resetSystemIDFromPath(r.URL.Path)
		if rt, ok := f.resetBody["ResetType"].(string); ok {
			f.resetTypes = append(f.resetTypes, rt)
			// Reflect the requested power transition so a power-cycle finalize can
			// poll PowerState to Off and confirm the On reset afterwards.
			switch rt {
			case "GracefulShutdown", "ForceOff":
				f.powerState = "Off"
			case "On", "ForceOn":
				f.powerState = "On"
			}
		}
		f.mu.Unlock()
		w.Header().Set("Location", "/redfish/v1/TaskService/Tasks/task-1")
		w.WriteHeader(http.StatusAccepted)
		f.writeJSON(w, f.task("Running"))

	// --- managers ---
	case r.URL.Path == "/redfish/v1/Managers":
		f.writeJSON(w, f.collection("Managers Collection", "/redfish/v1/Managers/mgr-1"))
	case r.URL.Path == "/redfish/v1/Managers/mgr-1" && r.Method == http.MethodGet:
		f.writeJSON(w, f.manager())

	// --- virtual media ---
	case f.isSystemVirtualMediaCollectionPath(r.URL.Path):
		id := f.systemIDFromVirtualMediaCollectionPath(r.URL.Path)
		f.writeJSON(w, f.collection("VirtualMedia Collection", "/redfish/v1/Systems/"+id+"/VirtualMedia/Cd"))
	case f.isSystemVirtualMediaCdPath(r.URL.Path) && r.Method == http.MethodGet:
		f.writeJSON(w, f.virtualMedia(r.URL.Path))
	case r.URL.Path == "/redfish/v1/Managers/mgr-1/VirtualMedia":
		f.writeJSON(w, f.collection("VirtualMedia Collection", "/redfish/v1/Managers/mgr-1/VirtualMedia/Cd"))
	case r.URL.Path == "/redfish/v1/Managers/mgr-1/VirtualMedia/Cd" && r.Method == http.MethodGet:
		f.writeJSON(w, f.virtualMedia("/redfish/v1/Managers/mgr-1/VirtualMedia/Cd"))
	case strings.HasSuffix(r.URL.Path, "/Actions/VirtualMedia.InsertMedia") && r.Method == http.MethodPost:
		f.mu.Lock()
		f.insertBody = decodeBody(r)
		status := f.insertMediaStatus
		f.mu.Unlock()
		if status >= 400 {
			w.WriteHeader(status)
			errBody := map[string]any{
				"code":    "Base.1.0.GeneralError",
				"message": "InsertMedia failed",
			}
			if f.insertMediaExtendedInfo {
				errBody["@Message.ExtendedInfo"] = []map[string]any{
					{
						"Message":    "The image URL could not be reached by the BMC.",
						"MessageId":  "Base.1.0.ResourceAtUriUnauthorized",
						"Resolution": "Verify the image URL is reachable from the BMC network.",
					},
				}
			}
			f.writeJSON(w, map[string]any{"error": errBody})
			return
		}
		w.Header().Set("Location", "/redfish/v1/TaskService/Tasks/task-insert")
		w.WriteHeader(http.StatusAccepted)
		f.writeJSON(w, f.task("Running"))
	case strings.HasSuffix(r.URL.Path, "/Actions/VirtualMedia.EjectMedia") && r.Method == http.MethodPost:
		f.mu.Lock()
		f.ejectCalled = true
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	// --- tasks ---
	case strings.HasPrefix(r.URL.Path, "/redfish/v1/TaskService/Tasks/") && r.Method == http.MethodGet:
		f.writeJSON(w, f.task(f.nextTaskState()))

	default:
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
	}
}

func (f *fakeBMC) writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

// nextTaskState advances the canned Task state machine so the Deployer's poll
// loop observes progress towards a terminal state.
func (f *fakeBMC) nextTaskState() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.taskIdx >= len(f.taskStates) {
		return f.taskStates[len(f.taskStates)-1]
	}
	s := f.taskStates[f.taskIdx]
	f.taskIdx++
	return s
}

func (f *fakeBMC) serviceRoot() map[string]any {
	root := map[string]any{
		"@odata.id":      "/redfish/v1/",
		"@odata.type":    "#ServiceRoot.v1_5_0.ServiceRoot",
		"Id":             "RootService",
		"Name":           "Root Service",
		"RedfishVersion": "1.6.0",
		"Systems":        map[string]any{"@odata.id": "/redfish/v1/Systems"},
		"Managers":       map[string]any{"@odata.id": "/redfish/v1/Managers"},
		"Tasks":          map[string]any{"@odata.id": "/redfish/v1/TaskService"},
	}
	// A session-less BMC advertises neither a SessionService nor Links.Sessions;
	// auto-detect must fall back to Basic auth for it.
	if !f.noSessionService {
		root["SessionService"] = map[string]any{"@odata.id": "/redfish/v1/SessionService"}
		root["Links"] = map[string]any{
			"Sessions": map[string]any{"@odata.id": "/redfish/v1/SessionService/Sessions"},
		}
	}
	return root
}

func (f *fakeBMC) collection(name string, members ...string) map[string]any {
	memberObjs := make([]map[string]any, 0, len(members))
	for _, m := range members {
		memberObjs = append(memberObjs, map[string]any{"@odata.id": m})
	}
	return map[string]any{
		"@odata.id":           "/redfish/v1/collection",
		"@odata.type":         "#Collection.Collection",
		"Name":                name,
		"Members@odata.count": len(members),
		"Members":             memberObjs,
	}
}

// systemsCollection serves the Systems collection with one member per
// configured systemID, in order.
func (f *fakeBMC) systemsCollection() map[string]any {
	members := make([]string, 0, len(f.systemIDs))
	for _, id := range f.systemIDs {
		members = append(members, "/redfish/v1/Systems/"+id)
	}
	return f.collection("Systems Collection", members...)
}

// knownSystemID reports whether id is one of the configured systems.
func (f *fakeBMC) knownSystemID(id string) bool {
	for _, s := range f.systemIDs {
		if s == id {
			return true
		}
	}
	return false
}

// isSystemPath matches GET/PATCH on a ComputerSystem member, e.g.
// /redfish/v1/Systems/{id}.
func (f *fakeBMC) isSystemPath(path string) bool {
	id := f.systemIDFromPath(path)
	return id != "" && f.knownSystemID(id)
}

func (f *fakeBMC) systemIDFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/redfish/v1/Systems/")
	if !ok || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// resetSystemIDFromPath extracts the ComputerSystem Id from a Reset action path
// (/redfish/v1/Systems/{id}/Actions/ComputerSystem.Reset). Unlike the GET/PATCH
// member routing it does NOT gate on the configured systemIDs: the Reset handler
// is matched purely by its action suffix so it works for both the synthesized
// fake (sys-xyz / node-*) and a recorded mockup tree (e.g. iLO's "1"). Returns ""
// when the path is not a System Reset action.
func resetSystemIDFromPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/redfish/v1/Systems/")
	if !ok {
		return ""
	}
	id, ok := strings.CutSuffix(rest, "/Actions/ComputerSystem.Reset")
	if !ok || strings.Contains(id, "/") {
		return ""
	}
	return id
}

func (f *fakeBMC) isSystemVirtualMediaCollectionPath(path string) bool {
	return f.systemIDFromVirtualMediaCollectionPath(path) != ""
}

func (f *fakeBMC) systemIDFromVirtualMediaCollectionPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/redfish/v1/Systems/")
	if !ok {
		return ""
	}
	id, ok := strings.CutSuffix(rest, "/VirtualMedia")
	if !ok || strings.Contains(id, "/") || !f.knownSystemID(id) {
		return ""
	}
	return id
}

func (f *fakeBMC) isSystemVirtualMediaCdPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/redfish/v1/Systems/")
	if !ok {
		return false
	}
	id, ok := strings.CutSuffix(rest, "/VirtualMedia/Cd")
	if !ok || strings.Contains(id, "/") {
		return false
	}
	return f.knownSystemID(id)
}

func (f *fakeBMC) computerSystem(id string) map[string]any {
	base := "/redfish/v1/Systems/" + id
	f.mu.Lock()
	power := f.powerState
	f.mu.Unlock()
	if power == "" {
		power = "On"
	}
	cs := map[string]any{
		"@odata.id":    base,
		"@odata.type":  "#ComputerSystem.v1_5_0.ComputerSystem",
		"Id":           id,
		"Name":         "Test System",
		"Manufacturer": "ACME",
		"Model":        "ProLiant-Test",
		"SerialNumber": "SN-0001",
		"PowerState":   power,
		// Nested summaries: the historical bug read these as flat fields and got
		// 0/0. They must be populated from the nested objects.
		"MemorySummary": map[string]any{
			"TotalSystemMemoryGiB": 64,
		},
		"ProcessorSummary": map[string]any{
			"Count": 8,
		},
		"Boot": f.boot(),
		"Actions": map[string]any{
			"#ComputerSystem.Reset": map[string]any{
				"target": base + "/Actions/ComputerSystem.Reset",
				"ResetType@Redfish.AllowableValues": []string{
					"On", "ForceOff", "ForceRestart", "GracefulRestart", "GracefulShutdown",
				},
			},
		},
	}
	if !f.mediaOnManager {
		cs["VirtualMedia"] = map[string]any{"@odata.id": base + "/VirtualMedia"}
	}
	if f.withSecureBoot {
		cs["SecureBoot"] = map[string]any{"@odata.id": base + "/SecureBoot"}
	}
	if f.biosVersion != "" {
		cs["BiosVersion"] = f.biosVersion
	}
	return cs
}

// boot builds the ComputerSystem Boot object, optionally including the
// BootSourceOverrideMode allowable-values annotation that drives UEFI detection.
func (f *fakeBMC) boot() map[string]any {
	boot := map[string]any{
		"BootSourceOverrideEnabled": "Disabled",
		"BootSourceOverrideTarget":  "None",
	}
	if f.bootModeAllowableValues != nil {
		boot["BootSourceOverrideMode@Redfish.AllowableValues"] = f.bootModeAllowableValues
	}
	return boot
}

func (f *fakeBMC) manager() map[string]any {
	m := map[string]any{
		"@odata.id":   "/redfish/v1/Managers/mgr-1",
		"@odata.type": "#Manager.v1_3_0.Manager",
		"Id":          "mgr-1",
		"Name":        "Test BMC",
	}
	if f.mediaOnManager {
		m["VirtualMedia"] = map[string]any{"@odata.id": "/redfish/v1/Managers/mgr-1/VirtualMedia"}
	}
	return m
}

func (f *fakeBMC) virtualMedia(id string) map[string]any {
	return map[string]any{
		"@odata.id":      id,
		"@odata.type":    "#VirtualMedia.v1_3_0.VirtualMedia",
		"Id":             "Cd",
		"Name":           "Virtual CD",
		"MediaTypes":     []string{"CD", "DVD"},
		"Image":          nil,
		"Inserted":       false,
		"WriteProtected": true,
		"Actions": map[string]any{
			"#VirtualMedia.InsertMedia": map[string]any{
				"target": id + "/Actions/VirtualMedia.InsertMedia",
			},
			"#VirtualMedia.EjectMedia": map[string]any{
				"target": id + "/Actions/VirtualMedia.EjectMedia",
			},
		},
	}
}

func (f *fakeBMC) task(state string) map[string]any {
	return map[string]any{
		"@odata.id":   "/redfish/v1/TaskService/Tasks/task-1",
		"@odata.type": "#Task.v1_4_0.Task",
		"Id":          "task-1",
		"Name":        "Deploy Task",
		"TaskState":   state,
		"TaskStatus":  "OK",
		"Messages": []map[string]any{
			{"Message": "Operation " + state},
		},
	}
}
