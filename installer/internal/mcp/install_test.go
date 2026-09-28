package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
)

// install repartitions a disk. Each of these is a way an agent could destroy
// one by accident, and none of them may reach kairos-agent.
func TestInstallRefusesToRun(t *testing.T) {
	tests := []struct {
		name     string
		input    installInput
		wantText string
	}{
		{
			name:     "without confirm",
			input:    installInput{Device: "/dev/sda"},
			wantText: "confirm was not true",
		},
		{
			name:     "with no device at all",
			input:    installInput{Confirm: true},
			wantText: "no device was given",
		},
		{
			name:     "on a device the machine does not have",
			input:    installInput{Device: "/dev/nvme0n1", Confirm: true},
			wantText: "not an installation candidate",
		},
		{
			name:     "with a finish action that is not one of ours",
			input:    installInput{Device: "/dev/sda", Confirm: true, FinishAction: "selfdestruct"},
			wantText: "finish_action",
		},
		{
			name:     "with cloud_config that is not YAML",
			input:    installInput{Device: "/dev/sda", Confirm: true, CloudConfig: "\tnot: [valid"},
			wantText: "not valid YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var installer *fakeInstaller
			session, _ := testServer(t, func(s *Server) {
				installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
				s.installer = installer
			})

			res := call(t, session, ToolInstall, tt.input)
			if !res.IsError {
				t.Fatalf("the install was not refused: %s", resultText(t, res))
			}
			if text := resultText(t, res); !strings.Contains(text, tt.wantText) {
				t.Errorf("refusal is %q, want it to mention %q", text, tt.wantText)
			}

			// The point of the refusal: nothing was installed.
			if installer.calls != 0 {
				t.Errorf("the agent was called %d times, want 0", installer.calls)
			}
		})
	}
}

// A device that was a candidate when the agent last scanned, and is not one
// now, must not be installed to on the strength of the stale answer.
func TestInstallRechecksTheDeviceAgainstTheCurrentDisks(t *testing.T) {
	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
		s.installer = installer
		// The disk the agent saw earlier has gone.
		s.scanDisks = func() ([]disks.Disk, error) {
			return []disks.Disk{{Path: "/dev/sdb", Size: "10.00 GiB"}}, nil
		}
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if !res.IsError {
		t.Fatal("installing to a disk that is gone was not refused")
	}
	if text := resultText(t, res); !strings.Contains(text, "/dev/sdb") {
		t.Errorf("refusal %q does not say what the candidates are", text)
	}
	if installer.calls != 0 {
		t.Errorf("the agent was called %d times, want 0", installer.calls)
	}
}

func TestInstallSucceeds(t *testing.T) {
	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		installer = &fakeInstaller{
			agentBin: "/usr/bin/kairos-agent",
			events:   steps(agentrun.StepPartition, agentrun.StepActive, agentrun.StepDone),
		}
		s.installer = installer
	})

	res := call(t, session, ToolInstall, installInput{
		Device:       "/dev/sda",
		Confirm:      true,
		Source:       "oci:quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.5.0",
		FinishAction: FinishReboot,
	})
	if res.IsError {
		t.Fatalf("the install failed: %s", resultText(t, res))
	}

	out := decodeOutput[installOutput](t, res)
	if !out.Succeeded {
		t.Error("succeeded is false on a successful install")
	}
	if got, want := strings.Join(out.StepsReached, ","), "partition,active,done"; got != want {
		t.Errorf("steps_reached = %q, want %q", got, want)
	}

	if installer.calls != 1 {
		t.Fatalf("the agent was called %d times, want 1", installer.calls)
	}
	if installer.finish != FinishReboot {
		t.Errorf("finish action passed to the agent = %q, want %q", installer.finish, FinishReboot)
	}

	// The agent is driven through a file, so what matters is what is in it.
	if !strings.HasPrefix(installer.config, "#cloud-config\n") {
		t.Errorf("the config the agent got is not a cloud-config:\n%s", installer.config)
	}
	for _, want := range []string{"device: /dev/sda", "reboot: true", "oci:quay.io/kairos/ubuntu"} {
		if !strings.Contains(installer.config, want) {
			t.Errorf("the config the agent got does not contain %q:\n%s", want, installer.config)
		}
	}
}

// An error the agent reports in its progress stream is the real cause, and has
// to survive into the tool result rather than being replaced by a bare exit
// status.
func TestInstallReportsTheAgentsOwnError(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.installer = &fakeInstaller{
			agentBin: "/usr/bin/kairos-agent",
			events: append(steps(agentrun.StepPartition),
				agentrun.ProgressEvent{Event: agentrun.EventError, Message: "no space left on device"}),
			err: errors.New("exit status 1"),
		}
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if !res.IsError {
		t.Fatal("a failed install was reported as a success")
	}

	out := decodeOutput[installOutput](t, res)
	if out.Succeeded {
		t.Error("succeeded is true on a failed install")
	}
	if out.Error != "no space left on device" {
		t.Errorf("error = %q, want the agent's own message", out.Error)
	}
	if text := resultText(t, res); !strings.Contains(text, "partition") {
		t.Errorf("the failure %q does not say how far the install got", text)
	}
}

// A process exit with no error event still has to be reported.
func TestInstallReportsABareExitFailure(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent", err: errors.New("exit status 127")}
	})

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if !res.IsError {
		t.Fatal("a failed install was reported as a success")
	}

	out := decodeOutput[installOutput](t, res)
	if out.Error != "exit status 127" {
		t.Errorf("error = %q, want the exit status", out.Error)
	}
}

// A retry after a successful install would wipe what was just written.
func TestInstallRefusesASecondRun(t *testing.T) {
	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent", events: steps(agentrun.StepDone)}
		s.installer = installer
	})

	if res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true}); res.IsError {
		t.Fatalf("the first install failed: %s", resultText(t, res))
	}

	res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true})
	if !res.IsError {
		t.Fatal("a second install was allowed")
	}
	if installer.calls != 1 {
		t.Errorf("the agent was called %d times, want 1", installer.calls)
	}
}

// A failed install can be retried: the disk is already spoiled, and refusing
// would leave the machine stuck with no way forward.
func TestInstallCanBeRetriedAfterAFailure(t *testing.T) {
	var installer *fakeInstaller
	session, _ := testServer(t, func(s *Server) {
		installer = &fakeInstaller{agentBin: "/usr/bin/kairos-agent", err: errors.New("exit status 1")}
		s.installer = installer
	})

	if res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true}); !res.IsError {
		t.Fatal("the first install should have failed")
	}
	if res := call(t, session, ToolInstall, installInput{Device: "/dev/sda", Confirm: true}); !res.IsError {
		t.Fatal("the retry should also have failed")
	}

	if installer.calls != 2 {
		t.Errorf("the agent was called %d times, want 2", installer.calls)
	}
}

func TestCollectDebugBundle(t *testing.T) {
	session, _ := testServer(t, nil)

	res := call(t, session, ToolDebugBundle, debugBundleInput{})
	if res.IsError {
		t.Fatalf("collect_debug_bundle failed: %s", resultText(t, res))
	}

	out := decodeOutput[debugBundleOutput](t, res)
	if out.Path != "/tmp/bundle.tar.gz" {
		t.Errorf("path = %q", out.Path)
	}
	if text := resultText(t, res); !strings.Contains(text, "sensitive") {
		t.Errorf("the result %q does not warn about the contents", text)
	}
}

func TestCollectDebugBundleReportsAFailure(t *testing.T) {
	session, _ := testServer(t, func(s *Server) {
		s.generateBundle = func() (string, error) { return "", errors.New("no space") }
	})

	res := call(t, session, ToolDebugBundle, debugBundleInput{})
	if !res.IsError {
		t.Fatal("a bundle failure should be a tool error")
	}
}
