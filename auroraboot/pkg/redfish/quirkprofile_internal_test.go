package redfish

import (
	"strings"
	"testing"

	"github.com/stmcginnis/gofish/schemas"
)

// TestEmptyProfileEqualsGeneric is the regression guard for the YAML producer: an
// empty profile (just a name) must compile to the zero-value quirks — every hook
// nil — so its behaviour is byte-for-byte identical to genericQuirks(). This is
// the same contract TestGenericProfileHasNoHooks guards for the Go producer.
func TestEmptyProfileEqualsGeneric(t *testing.T) {
	q, err := LoadProfile([]byte("name: empty\n"))
	if err != nil {
		t.Fatalf("compiling empty profile: %v", err)
	}
	if q.mediaSearch != nil || q.mediaType != nil || q.resetType != nil ||
		q.tuneInsertParams != nil || q.pushMedia != nil {
		t.Fatal("empty profile compiled to non-nil hooks; it must equal the generic zero-value quirks")
	}
	// genericQuirks is the reference zero value (plus a name); the only difference
	// from an empty profile is the name.
	gen := genericQuirks()
	if gen.mediaSearch != nil || gen.mediaType != nil || gen.resetType != nil ||
		gen.tuneInsertParams != nil || gen.pushMedia != nil {
		t.Fatal("genericQuirks must remain all-nil-hook for this equivalence to hold")
	}
}

// TestProfileValidationMatrix covers the strict load-time validation: unknown
// keys, the Image rejection, and bad enums are all errors with actionable messages.
func TestProfileValidationMatrix(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantErr  string // substring the error must contain
		wantPass bool
	}{
		{
			name:    "missing name",
			yaml:    "mediaType: CD\n",
			wantErr: "name is required",
		},
		{
			name:    "unknown top-level key",
			yaml:    "name: x\nbogusKey: true\n",
			wantErr: "field bogusKey not found",
		},
		{
			name:    "set Image is rejected",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    Image: http://evil/iso\n",
			wantErr: "Image",
		},
		{
			name:    "clear Image (lowercase) is rejected",
			yaml:    "name: x\ntuneInsertParams:\n  clear: [image]\n",
			wantErr: "Image",
		},
		{
			name:    "non-allowlisted insert field",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    WriteOnce: true\n",
			wantErr: "allowlist",
		},
		{
			name:    "set MediaType outside the enum",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    MediaType: Bogus\n",
			wantErr: "MediaType Bogus is not one of CD, DVD",
		},
		{
			name:    "set TransferProtocolType outside the enum",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    TransferProtocolType: GOPHER\n",
			wantErr: "TransferProtocolType GOPHER is not one of",
		},
		{
			name:    "set Inserted to a string",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    Inserted: \"no\"\n",
			wantErr: "Inserted no is not a boolean",
		},
		{
			name:    "set WriteProtected to a number",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    WriteProtected: 0\n",
			wantErr: "WriteProtected 0 is not a boolean",
		},
		{
			name:    "set MediaType to a number",
			yaml:    "name: x\ntuneInsertParams:\n  set:\n    MediaType: 42\n",
			wantErr: "MediaType 42 is not one of",
		},
		{
			name:    "bad mediaType enum",
			yaml:    "name: x\nmediaType: Tape\n",
			wantErr: "not one of CD, DVD",
		},
		{
			name:    "bad resetType then enum",
			yaml:    "name: x\nresetType:\n  - { when: { powerState: \"*\" }, then: Explode }\n",
			wantErr: "not a known Redfish ResetType",
		},
		{
			name:    "bad powerState enum",
			yaml:    "name: x\nresetType:\n  - { when: { powerState: Maybe }, then: On }\n",
			wantErr: "not one of On, Off, *",
		},
		{
			name:    "bad mediaSearch order entry",
			yaml:    "name: x\nmediaSearch:\n  order: [galaxy]\n",
			wantErr: "not one of system, manager",
		},
		{
			name:     "valid full profile",
			yaml:     "name: ilo\nmatch: { vendor: HPE }\nmediaSearch: { order: [manager, system] }\nmediaType: CD\nresetType:\n  - { when: { powerState: \"Off\" }, then: On }\n  - { when: { powerState: \"*\" }, then: ForceRestart }\ntuneInsertParams:\n  clear: [TransferProtocolType]\n  set: { WriteProtected: true }\nvalidatedFirmware: \"iLO5 2.44\"\n",
			wantPass: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProfile([]byte(tc.yaml))
			if tc.wantPass {
				if err != nil {
					t.Fatalf("expected profile to load, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestCompileMediaSearchOrder checks the index-ordering closure: selectors emit
// matching candidate indexes in order, "manager" matches any manager:<id>, and
// unmatched candidates are dropped.
func TestCompileMediaSearchOrder(t *testing.T) {
	q, err := LoadProfile([]byte("name: ilo\nmediaSearch: { order: [manager, system] }\n"))
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	views := []MediaView{
		{Index: 0, Location: "system"},
		{Index: 1, Location: "manager:mgr-1"},
		{Index: 2, Location: "manager:mgr-2"},
	}
	got := q.mediaSearch(views)
	want := []int{1, 2, 0}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// A specific manager id selects only that manager.
	q2, err := LoadProfile([]byte("name: x\nmediaSearch: { order: [\"manager:mgr-2\"] }\n"))
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	got2 := q2.mediaSearch(views)
	if len(got2) != 1 || got2[0] != 2 {
		t.Fatalf("manager:mgr-2 order = %v, want [2]", got2)
	}
}

// TestCompileResetTypeRules checks the first-match-wins reset closure and that a
// non-matching power state falls back to the core default.
func TestCompileResetTypeRules(t *testing.T) {
	q, err := LoadProfile([]byte("name: x\nresetType:\n  - { when: { powerState: \"Off\" }, then: On }\n  - { when: { powerState: \"*\" }, then: ForceRestart }\n"))
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}

	// Off -> On (case-insensitive match against the system's PowerState).
	if got := q.resetType(ResetView{PowerState: "off", Default: "GracefulRestart"}); got != schemas.OnResetType {
		t.Fatalf("Off rule = %q, want On", got)
	}
	// On -> ForceRestart via the "*" rule.
	if got := q.resetType(ResetView{PowerState: "On", Default: "GracefulRestart"}); got != schemas.ForceRestartResetType {
		t.Fatalf("On rule = %q, want ForceRestart", got)
	}

	// With no matching rule and no wildcard, fall back to the core default.
	q2, err := LoadProfile([]byte("name: x\nresetType:\n  - { when: { powerState: \"Off\" }, then: On }\n"))
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if got := q2.resetType(ResetView{PowerState: "On", Default: "GracefulRestart"}); got != schemas.ResetType("GracefulRestart") {
		t.Fatalf("non-matching rule = %q, want the default GracefulRestart", got)
	}
}

// TestTuneInsertParamsClearAndSetReachTheRequest compiles a tuneInsertParams
// profile and applies its patch to spec-default InsertMedia parameters: every
// allowlisted field named in clear must be dropped from the request, and every
// set value must land as given.
func TestTuneInsertParamsClearAndSetReachTheRequest(t *testing.T) {
	newParams := func() *schemas.VirtualMediaInsertMediaParameters {
		proto := schemas.TransferProtocolType("HTTP")
		inserted, wp := true, true
		return &schemas.VirtualMediaInsertMediaParameters{
			Image:                "http://example/img.iso",
			MediaType:            schemas.CDVirtualMediaType,
			TransferProtocolType: &proto,
			Inserted:             &inserted,
			WriteProtected:       &wp,
		}
	}
	apply := func(t *testing.T, yaml string) *schemas.VirtualMediaInsertMediaParameters {
		t.Helper()
		q, err := LoadProfile([]byte(yaml))
		if err != nil {
			t.Fatalf("loading profile: %v", err)
		}
		p := newParams()
		applyInsertPatch(p, q.tuneInsertParams(insertParamsView(p)))
		return p
	}

	t.Run("clear drops every allowlisted field", func(t *testing.T) {
		p := apply(t, "name: x\ntuneInsertParams:\n  clear: [MediaType, TransferProtocolType, Inserted, WriteProtected]\n")
		if p.MediaType != "" || p.TransferProtocolType != nil || p.Inserted != nil || p.WriteProtected != nil {
			t.Fatalf("clear left fields behind: %+v", p)
		}
		if p.Image != "http://example/img.iso" {
			t.Fatalf("clear touched the image URL: %q", p.Image)
		}
	})

	t.Run("set lands every value", func(t *testing.T) {
		p := apply(t, "name: x\ntuneInsertParams:\n  set: { MediaType: USBStick, TransferProtocolType: HTTPS, Inserted: false, WriteProtected: false }\n")
		if p.MediaType != schemas.USBStickVirtualMediaType {
			t.Fatalf("MediaType = %q, want USBStick", p.MediaType)
		}
		if p.TransferProtocolType == nil || *p.TransferProtocolType != "HTTPS" {
			t.Fatalf("TransferProtocolType = %v, want HTTPS", p.TransferProtocolType)
		}
		if p.Inserted == nil || *p.Inserted || p.WriteProtected == nil || *p.WriteProtected {
			t.Fatalf("booleans not set to false: Inserted=%v WriteProtected=%v", p.Inserted, p.WriteProtected)
		}
	})

	t.Run("clear wins over set", func(t *testing.T) {
		p := apply(t, "name: x\ntuneInsertParams:\n  clear: [WriteProtected]\n  set: { WriteProtected: false }\n")
		if p.WriteProtected != nil {
			t.Fatalf("WriteProtected = %v, want it dropped", *p.WriteProtected)
		}
	})
}
