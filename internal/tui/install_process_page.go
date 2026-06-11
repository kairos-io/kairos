package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kairos-io/kairos-sdk/agentrun"
)

// stepDisplay maps an agent progress step to the UI step label.
var stepDisplay = map[string]string{
	"partition":      InstallPartitionStep,
	"before-install": InstallBeforeInstallStep,
	"active":         InstallActiveStep,
	"bootloader":     InstallBootloaderStep,
	"recovery":       InstallRecoveryStep,
	"passive":        InstallPassiveStep,
	"after-install":  InstallAfterInstallStep,
	"done":           InstallCompleteStep,
}

type installProcessPage struct {
	progress int
	step     string
	steps    []string
	output   chan string
	done     chan bool
	once     sync.Once
	errorMsg string
}

func newInstallProcessPage() *installProcessPage {
	return &installProcessPage{
		progress: 0,
		step:     InstallDefaultStep,
		steps: []string{
			InstallDefaultStep, InstallPartitionStep, InstallBeforeInstallStep,
			InstallActiveStep, InstallBootloaderStep, InstallRecoveryStep,
			InstallPassiveStep, InstallAfterInstallStep, InstallCompleteStep,
		},
		output: make(chan string, 16),
		done:   make(chan bool),
	}
}

func (p *installProcessPage) Init() tea.Cmd {
	p.once.Do(func() {
		// Resolve the agent first, before rendering or writing the temp
		// cloud-config: the config carries the user's password hash, so we
		// must not create it on disk when we are not going to run the agent.
		agentBin := agentrun.ResolveAgentBin()
		if agentBin == "" {
			p.errorMsg = "kairos-agent not found (set KAIROS_AGENT_BIN or add it to PATH)"
			return
		}

		ccString, err := RenderCloudConfig(&mainModel)
		if err != nil {
			p.errorMsg = "Failed to generate install configuration: " + err.Error()
			return
		}
		f, err := os.CreateTemp("", "kairos-install-*.yaml")
		if err != nil {
			p.errorMsg = "Failed to write install configuration: " + err.Error()
			return
		}
		_, _ = f.WriteString(ccString)
		_ = f.Close()
		cfgPath := f.Name()

		go func() {
			defer close(p.done)
			defer os.Remove(cfgPath)
			// sawError is local to this goroutine and only touched by the
			// onEvent callback, which agentrun.Run invokes synchronously on
			// this same goroutine. Tracking it here (instead of reading
			// p.errorMsg, which the bubbletea Update loop writes) keeps the
			// final error check race-free.
			sawError := false
			err := agentrun.Run(agentBin, cfgPath, mainModel.source, mainModel.finishAction,
				func(ev agentrun.ProgressEvent) {
					switch ev.Event {
					case "step":
						if disp, ok := stepDisplay[ev.Step]; ok {
							p.output <- StepPrefix + disp
						}
					case "error":
						sawError = true
						p.output <- ErrorPrefix + ev.Message
					}
				},
				func(line string) { mainModel.log.Print(line) },
			)
			if err != nil && !sawError {
				p.output <- ErrorPrefix + err.Error()
			}
		}()
	})
	return func() tea.Msg { return CheckInstallerMsg{} }
}

// CheckInstallerMsg triggers a poll of the installer output channel.
type CheckInstallerMsg struct{}

func (p *installProcessPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg.(type) {
	case CheckInstallerMsg:
		// If Init hit an early error (e.g. agent not found), it never started
		// the goroutine and never closed p.done. Stop here so we don't busy-poll
		// the channels forever. This read and the writes in Init both happen on
		// the bubbletea main goroutine, so it is race-free.
		if p.errorMsg != "" {
			return p, nil
		}
		select {
		case output, ok := <-p.output:
			if !ok {
				return p, nil
			}
			if strings.HasPrefix(output, StepPrefix) {
				stepName := strings.TrimPrefix(output, StepPrefix)
				for i, s := range p.steps {
					if s == stepName {
						p.progress = i
						p.step = stepName
						break
					}
				}
			} else if strings.HasPrefix(output, ErrorPrefix) {
				p.errorMsg = strings.TrimPrefix(output, ErrorPrefix)
				p.step = "Error: " + p.errorMsg
				return p, nil
			}
			return p, func() tea.Msg { return CheckInstallerMsg{} }
		case <-p.done:
			if p.errorMsg == "" {
				p.progress = len(p.steps) - 1
				p.step = p.steps[len(p.steps)-1]
			}
			return p, nil
		default:
			return p, tea.Tick(100*time.Millisecond, func(_ time.Time) tea.Msg {
				return CheckInstallerMsg{}
			})
		}
	}
	return p, nil
}

func (p *installProcessPage) View() string {
	if p.errorMsg != "" {
		s := "Installation encountered an error.\n\n"
		s += lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).
			Render("[!] Installation error: "+p.errorMsg) + "\n\n"
		return s
	}
	s := "Installation in Progress\n\n"
	totalSteps := len(p.steps)
	progressPercent := (p.progress * 100) / (totalSteps - 1)
	barWidth := 40
	filled := barWidth * progressPercent / 100
	progressBar := lipgloss.NewStyle().Foreground(kairosHighlight2).Background(kairosBg).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(kairosBorder).Background(kairosBg).Render(strings.Repeat("░", barWidth-filled))
	s += "Progress:" + progressBar + lipgloss.NewStyle().Background(kairosBg).Render(" ")
	s += lipgloss.NewStyle().Foreground(kairosText).Background(kairosBg).Bold(true).Render(fmt.Sprintf("%d%%", progressPercent))
	s += "\n\n"
	s += fmt.Sprintf("Current step: %s\n\n", p.step)
	s += "Completed steps:\n"
	tick := lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark)
	for i := 0; i < p.progress; i++ {
		s += fmt.Sprintf("%s %s\n", tick, p.steps[i])
	}
	if p.progress < len(p.steps)-1 {
		warning := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render("[!]  Do not power off the system during installation!")
		s += "\n" + warning
	} else {
		text := "Installation completed successfully!\n"
		if mainModel.finishAction == "nothing" {
			text += "You can now reboot or shut down your system."
		}
		s += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Render(text)
	}
	return s
}

func (p *installProcessPage) Title() string { return "Installing" }

func (p *installProcessPage) Help() string {
	if p.progress >= len(p.steps)-1 || p.errorMsg != "" {
		if mainModel.finishAction == "nothing" {
			return "Press any key to exit"
		}
		return "System will " + mainModel.finishAction + " shortly"
	}
	return "Installation in progress - Use ctrl+c to abort"
}

func (p *installProcessPage) ID() string { return "install_process" }

// Abort is a no-op, matching the agent's prior behavior (its install ran
// in-process, so there was no child process to kill). Cancelling the running
// agent mid-install can be added later via context cancellation in agentrun.
func (p *installProcessPage) Abort() {}
