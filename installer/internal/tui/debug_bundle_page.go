// internal/tui/debug_bundle_page.go
package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kairos-io/kairos-sdk/agentrun"

	"github.com/kairos-io/kairos-installer/internal/debugbundle"
)

type bundleState int

const (
	bundleCollecting bundleState = iota
	bundleReady
	bundleUSBSelect
	bundleFailed
)

type debugBundlePage struct {
	once       sync.Once
	state      bundleState
	resultCh   chan bundleResult
	path       string
	urls       []string
	server     *debugbundle.Server
	usbMessage string
	usbMounts  []debugbundle.RemovableMount
	usbCursor  int
	errMsg     string
}

type bundleResult struct {
	path   string
	server *debugbundle.Server
	err    error
}

func newDebugBundlePage() *debugBundlePage {
	return &debugBundlePage{state: bundleCollecting, resultCh: make(chan bundleResult, 1)}
}

// CheckBundleMsg polls the build result channel.
type CheckBundleMsg struct{}

func (p *debugBundlePage) Init() tea.Cmd {
	p.once.Do(func() {
		// Snapshot everything buildBundle needs on the Update goroutine, BEFORE
		// spawning the worker, so the worker never touches the mainModel global
		// (which the Update goroutine mutates on WindowSizeMsg / navigation).
		agentBin := agentrun.ResolveAgentBin()
		redacted, _ := RenderRedactedCloudConfig(&mainModel)
		cmd := agentrun.Command(agentBin, "<config>", mainModel.source, mainModel.finishAction)
		ctx := debugbundle.Context{
			AgentBin:            agentBin,
			AgentArgs:           cmd.Args[1:],
			Disk:                mainModel.disk,
			Source:              mainModel.source,
			Version:             version,
			CloudConfigRedacted: redacted,
		}
		go func() {
			p.resultCh <- buildBundle(agentBin, ctx)
		}()
	})
	return func() tea.Msg { return CheckBundleMsg{} }
}

// buildBundle collects extras, generates the tarball, and starts the HTTP
// server. It runs off the bubbletea goroutine and operates only on the snapshot
// passed in, never on the mainModel global.
func buildBundle(agentBin string, ctx debugbundle.Context) bundleResult {
	out, err := debugbundle.GenerateBundle(agentBin, ctx, time.Now())
	if err != nil {
		return bundleResult{err: err}
	}
	srv, _ := debugbundle.Serve(out)
	return bundleResult{path: out, server: srv}
}

func (p *debugBundlePage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch m := msg.(type) {
	case CheckBundleMsg:
		// Once the result has been received, state moves off bundleCollecting.
		// Short-circuit so re-opening the page doesn't schedule a 100ms tick
		// forever.
		if p.state != bundleCollecting {
			return p, nil
		}
		select {
		case res := <-p.resultCh:
			if res.err != nil {
				p.state = bundleFailed
				p.errMsg = res.err.Error()
				return p, nil
			}
			p.path = res.path
			p.server = res.server
			if res.server != nil {
				p.urls = res.server.URLs()
			}
			p.state = bundleReady
			return p, nil
		default:
			return p, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return CheckBundleMsg{} })
		}
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

func (p *debugBundlePage) handleKey(m tea.KeyMsg) (Page, tea.Cmd) {
	switch p.state {
	case bundleReady:
		if m.String() == "u" {
			// Scan for removable mounts and open the selection menu. With none
			// found, stay on the ready screen and tell the user to plug one in
			// and press u to rescan.
			mounts, err := debugbundle.RemovableMounts()
			if err != nil || len(mounts) == 0 {
				p.usbMessage = "No USB drive detected. Plug one in and press u to rescan."
				return p, nil
			}
			p.usbMounts = mounts
			p.usbCursor = 0
			p.usbMessage = ""
			p.state = bundleUSBSelect
			return p, nil
		}
	case bundleUSBSelect:
		switch m.String() {
		case "up", "k":
			if p.usbCursor > 0 {
				p.usbCursor--
			}
		case "down", "j":
			if p.usbCursor < len(p.usbMounts)-1 {
				p.usbCursor++
			}
		case "esc":
			p.state = bundleReady
			return p, nil
		case "enter":
			p.usbMessage = copyToMount(p.path, p.usbMounts[p.usbCursor])
			p.state = bundleReady
			return p, nil
		}
	}
	return p, nil
}

// copyToMount copies the bundle to the chosen removable mount, returning a
// user-facing status line.
func copyToMount(path string, mount debugbundle.RemovableMount) string {
	dest, err := debugbundle.CopyTo(path, mount.MountPoint)
	if err != nil {
		return "Copy failed: " + err.Error()
	}
	return "Copied to " + dest + " on " + mount.Device
}

// formatUSBMenu renders the removable-mount selection list, marking the row at
// cursor with a ">" indicator.
func formatUSBMenu(mounts []debugbundle.RemovableMount, cursor int) string {
	var b strings.Builder
	b.WriteString("Select a USB drive to copy the bundle to:\n\n")
	for i, mnt := range mounts {
		indicator := " "
		if i == cursor {
			indicator = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		fmt.Fprintf(&b, "%s %s (%s)\n", indicator, mnt.Device, mnt.MountPoint)
	}
	return b.String()
}

// formatRetrievalText renders the HTTP URLs and local path block.
func formatRetrievalText(path string, urls []string) string {
	var b strings.Builder
	b.WriteString("Retrieve over the network:\n")
	if len(urls) == 0 {
		b.WriteString("  (no usable network address detected)\n")
	} else {
		for _, u := range urls {
			b.WriteString("  curl -O " + u + "\n")
		}
	}
	fmt.Fprintf(&b, "\nLocal path: %s\n", path)
	return b.String()
}

// failureBanner explains why the debug page opened when it was triggered by a
// failed install. It is empty when the page was opened manually (ctrl+d), so the
// manual case stays a plain "collect logs on request" screen.
func failureBanner() string {
	if mainModel.installError == "" {
		return ""
	}
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	return red.Render("[!] Installation failed: "+mainModel.installError) + "\n" +
		"A debug bundle with the logs from this failure has been collected — share it when reporting the issue.\n\n"
}

// sensitiveDataWarning reminds the user to vet the bundle before sharing: it
// contains the logs, the rendered cloud-config (password redacted, but other
// fields are not), and system info that may include secrets we can't know about.
func sensitiveDataWarning() string {
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00")).Bold(true)
	return amber.Render("[!] Review the bundle before sharing it.") + "\n" +
		"It contains your logs, the install config (password redacted) and system info,\n" +
		"which may include other sensitive data. Inspect it before sending to third parties.\n"
}

func (p *debugBundlePage) View() string {
	switch p.state {
	case bundleCollecting:
		header := failureBanner()
		if header == "" {
			return "Collecting diagnostics and building debug bundle...\n"
		}
		return header + "Collecting diagnostics and building the debug bundle...\n"
	case bundleFailed:
		return failureBanner() + lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).
			Render("Failed to build debug bundle: "+p.errMsg) + "\n"
	case bundleUSBSelect:
		return formatUSBMenu(p.usbMounts, p.usbCursor)
	default: // bundleReady
		ready := "Debug bundle ready."
		if mainModel.installError == "" {
			ready = "Debug bundle ready (collected on request)."
		}
		s := failureBanner() + ready + "\n\n" + sensitiveDataWarning() + "\n" + formatRetrievalText(p.path, p.urls)
		if p.usbMessage != "" {
			s += "\n" + p.usbMessage + "\n"
		}
		return s
	}
}

func (p *debugBundlePage) Title() string { return "Debug Bundle" }

func (p *debugBundlePage) Help() string {
	if p.state == bundleReady {
		return "u: copy to USB • q/ctrl+c: quit"
	}
	if p.state == bundleUSBSelect {
		return genericNavigationHelp + " • esc: cancel"
	}
	if p.state == bundleFailed {
		return "q/ctrl+c: quit"
	}
	return "Please wait..."
}

func (p *debugBundlePage) ID() string { return DebugBundlePageID }
