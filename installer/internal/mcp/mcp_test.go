package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/prereqs"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// fakeInstaller records what it was asked to install and replays a scripted
// progress stream instead of repartitioning anything.
type fakeInstaller struct {
	agentBin string
	events   []agentrun.ProgressEvent
	err      error

	calls   int
	cfgPath string
	config  string
	source  string
	finish  string
}

func (f *fakeInstaller) ResolveAgentBin() string { return f.agentBin }

func (f *fakeInstaller) Run(agentBin, cfgPath, source, finishAction string, onEvent func(agentrun.ProgressEvent), _ func(string)) error {
	f.calls++
	f.cfgPath = cfgPath
	f.source = source
	f.finish = finishAction

	// Read the config back off disk: what the agent would actually get.
	if b, err := os.ReadFile(cfgPath); err == nil {
		f.config = string(b)
	}

	for _, ev := range f.events {
		onEvent(ev)
	}

	return f.err
}

func steps(names ...string) []agentrun.ProgressEvent {
	out := make([]agentrun.ProgressEvent, 0, len(names))
	for _, n := range names {
		out = append(out, agentrun.ProgressEvent{Event: agentrun.EventStep, Step: n})
	}
	return out
}

// newFakeServer builds a Server with every outside dependency replaced, so no
// test can reach a real disk.
func newFakeServer(t *testing.T, configure func(*Server)) *Server {
	t.Helper()

	s := &Server{
		log: sdkLogger.NewKairosLogger("test", "fatal", true),
		scanDisks: func() ([]disks.Disk, error) {
			return []disks.Disk{
				{Path: "/dev/sda", SizeBytes: 20 << 30, Size: "20.00 GiB", Model: "TESTDISK"},
			}, nil
		},
		gatherChecks:   func(string) ([]prereqs.Check, error) { return nil, nil },
		applyDecisions: func([]prereqs.Decision, string) ([]prereqs.ApplyResult, error) { return nil, nil },
		installer:      &fakeInstaller{agentBin: "/usr/bin/kairos-agent"},
		generateBundle: func() (string, error) { return "/tmp/bundle.tar.gz", nil },
	}

	dir := t.TempDir()
	s.writeCloudConfig = func(content string) (string, func(), error) {
		path := filepath.Join(dir, "install.yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			return "", nil, err
		}
		return path, func() {}, nil
	}

	if configure != nil {
		configure(s)
	}

	return s
}

// testServer wires a fake Server to a client over the in-memory transport, to
// drive it through the real protocol without an HTTP round trip.
func testServer(t *testing.T, configure func(*Server)) (*mcp.ClientSession, *Server) {
	t.Helper()

	s := newFakeServer(t, configure)

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := s.MCPServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session, s
}

func call(t *testing.T, session *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}

	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}

	return strings.Join(parts, "\n")
}

func decodeOutput[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()

	var out T
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-encoding the structured output: %v", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decoding the structured output: %v", err)
	}

	return out
}

// Every tool the server advertises has to be callable, or a client discovers a
// tool it cannot use.
func TestEveryAdvertisedToolIsCallable(t *testing.T) {
	session, _ := testServer(t, nil)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	want := map[string]bool{
		ToolListDisks: true, ToolListPrerequisites: true, ToolApplyPrerequisites: true,
		ToolInstallOptions: true, ToolInstall: true, ToolDebugBundle: true,
	}

	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s has no description, so a client cannot tell what it does", tool.Name)
		}
	}

	for name := range want {
		if !got[name] {
			t.Errorf("%s is not advertised", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s is advertised but not expected", name)
		}
	}
}

// The install tool has to be marked destructive, which is the only signal a
// client has that it must not be called speculatively.
func TestInstallIsAnnotatedDestructive(t *testing.T) {
	session, _ := testServer(t, nil)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	for _, tool := range res.Tools {
		if tool.Name != ToolInstall {
			continue
		}
		if tool.Annotations == nil {
			t.Fatal("install carries no annotations")
		}
		if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Error("install is not annotated as destructive")
		}
		if tool.Annotations.ReadOnlyHint {
			t.Error("install is annotated read-only")
		}
		return
	}

	t.Fatal("install is not advertised")
}

func TestListDisks(t *testing.T) {
	session, _ := testServer(t, nil)

	res := call(t, session, ToolListDisks, listDisksInput{})
	if res.IsError {
		t.Fatalf("list_disks failed: %s", resultText(t, res))
	}

	out := decodeOutput[listDisksOutput](t, res)
	if len(out.Disks) != 1 || out.Disks[0].Path != "/dev/sda" {
		t.Fatalf("disks = %+v, want one /dev/sda", out.Disks)
	}
	if text := resultText(t, res); !strings.Contains(text, "/dev/sda") || !strings.Contains(text, "20.00 GiB") {
		t.Errorf("text does not describe the disk: %q", text)
	}
}

func TestListDisksWithNoCandidates(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.scanDisks = func() ([]disks.Disk, error) { return nil, nil }
	})

	res := call(t, session, ToolListDisks, listDisksInput{})
	if res.IsError {
		t.Fatalf("no candidates is not an error: %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "No disk") {
		t.Errorf("text does not say there is nothing to install to: %q", text)
	}
}

func TestListDisksReportsAScanFailure(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.scanDisks = func() ([]disks.Disk, error) { return nil, errors.New("ghw exploded") }
	})

	res := call(t, session, ToolListDisks, listDisksInput{})
	if !res.IsError {
		t.Fatal("a scan failure should be a tool error")
	}
	if text := resultText(t, res); !strings.Contains(text, "ghw exploded") {
		t.Errorf("text does not carry the cause: %q", text)
	}
}

func TestListPrerequisites(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.gatherChecks = func(string) ([]prereqs.Check, error) {
			return []prereqs.Check{
				{ID: "diskwipe", Title: "Wipe the LVM", Severity: prereqs.SeverityWarning, Blocking: true},
			}, nil
		}
	})

	res := call(t, session, ToolListPrerequisites, listPrerequisitesInput{})
	if res.IsError {
		t.Fatalf("list_prerequisites failed: %s", resultText(t, res))
	}

	out := decodeOutput[listPrerequisitesOutput](t, res)
	if len(out.Checks) != 1 || out.Checks[0].ID != "diskwipe" {
		t.Fatalf("checks = %+v, want the diskwipe check", out.Checks)
	}
	if text := resultText(t, res); !strings.Contains(text, "blocking") {
		t.Errorf("text does not say the check is blocking: %q", text)
	}
}

// Gather returns the checks it did collect alongside a parse error from one
// plugin. Throwing the good ones away would hide the machine's real state.
func TestListPrerequisitesKeepsChecksDespiteAPluginError(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.gatherChecks = func(string) ([]prereqs.Check, error) {
			return []prereqs.Check{{ID: "arch", Title: "Architecture"}}, errors.New("plugin returned junk")
		}
	})

	res := call(t, session, ToolListPrerequisites, listPrerequisitesInput{})
	out := decodeOutput[listPrerequisitesOutput](t, res)
	if len(out.Checks) != 1 {
		t.Fatalf("checks = %+v, want the one check that parsed", out.Checks)
	}
	if text := resultText(t, res); !strings.Contains(text, "plugin returned junk") {
		t.Errorf("text does not mention the failing plugin: %q", text)
	}
}

func TestApplyPrerequisitesRefusesNoDecisions(t *testing.T) {
	session, _ := testServer(t, nil)

	res := call(t, session, ToolApplyPrerequisites, applyPrerequisitesInput{})
	if !res.IsError {
		t.Fatal("applying nothing should be a tool error")
	}
}

func TestApplyPrerequisites(t *testing.T) {
	var got []prereqs.Decision
	session, _ := testServer(t, func(s *Server) {
		s.applyDecisions = func(d []prereqs.Decision, _ string) ([]prereqs.ApplyResult, error) {
			got = d
			return []prereqs.ApplyResult{{ID: "diskwipe", Success: true, Message: "wiped"}}, nil
		}
	})

	res := call(t, session, ToolApplyPrerequisites, applyPrerequisitesInput{
		Decisions: []prereqs.Decision{{ID: "diskwipe", Prompt: prereqs.PromptConfirm, Confirmed: true}},
	})
	if res.IsError {
		t.Fatalf("apply_prerequisites failed: %s", resultText(t, res))
	}

	if len(got) != 1 || got[0].ID != "diskwipe" || !got[0].Confirmed {
		t.Fatalf("the plugin got %+v, want the confirmed diskwipe decision", got)
	}
	if text := resultText(t, res); !strings.Contains(text, "wiped") {
		t.Errorf("text does not carry the plugin's message: %q", text)
	}
}

func TestInstallOptions(t *testing.T) {
	session, _ := testServer(t, nil)

	res := call(t, session, ToolInstallOptions, installOptionsInput{})
	if res.IsError {
		t.Fatalf("get_install_options failed: %s", resultText(t, res))
	}

	out := decodeOutput[installOptionsOutput](t, res)
	if out.AgentBinary != "/usr/bin/kairos-agent" {
		t.Errorf("agent_binary = %q", out.AgentBinary)
	}
	if !out.RequiresConfirm {
		t.Error("requires_confirm is false, but install always requires it")
	}
	if len(out.Steps) != len(agentrun.Steps) {
		t.Errorf("steps = %v, want the agentrun vocabulary", out.Steps)
	}
	if len(out.Disks) != 1 {
		t.Errorf("disks = %+v, want the one candidate", out.Disks)
	}
}

func TestInstallOptionsWithoutAnAgent(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.installer = &fakeInstaller{agentBin: ""}
	})

	res := call(t, session, ToolInstallOptions, installOptionsInput{})
	if !res.IsError {
		t.Fatal("no agent binary should be a tool error")
	}
	if text := resultText(t, res); !strings.Contains(text, "kairos-agent") {
		t.Errorf("text does not say what is missing: %q", text)
	}
}
