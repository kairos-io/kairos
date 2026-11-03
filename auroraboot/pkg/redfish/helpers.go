package redfish

import "github.com/stmcginnis/gofish/schemas"

// progressFunc is the internal, always-safe-to-call progress callback shape used
// throughout Deploy. nil DeployRequest.Progress callbacks are wrapped into a
// no-op by newProgressReporter so call sites never nil-check.
type progressFunc func(step string, percent int)

// taskPollFloor / taskPollCeil bound the percentage reported while polling the
// async Task: the deploy is 80% done when polling starts and tops out at 95
// until the terminal "completed" (100) is reported by Deploy itself.
const (
	taskPollFloor = 80
	taskPollCeil  = 95
)

// newProgressReporter returns a non-nil progress callback. When fn is nil it
// returns a no-op so the Deploy flow can call report(...) unconditionally.
func newProgressReporter(fn func(step string, percent int)) progressFunc {
	if fn == nil {
		return func(string, int) {}
	}
	return fn
}

// taskPollPercent maps a Task's PercentComplete (0..100) onto the poll window
// [taskPollFloor, taskPollCeil]. When the BMC does not report PercentComplete it
// returns the floor so progress at least advances to the polling stage.
func taskPollPercent(task *schemas.Task) int {
	if task.PercentComplete == nil {
		return taskPollFloor
	}
	p := int(*task.PercentComplete)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return taskPollFloor + (taskPollCeil-taskPollFloor)*p/100
}

// mediaSupportsCD reports whether a VirtualMedia resource advertises CD or DVD
// media support.
func mediaSupportsCD(vm *schemas.VirtualMedia) bool {
	for _, t := range vm.MediaTypes {
		if t == schemas.CDVirtualMediaType || t == schemas.DVDVirtualMediaType {
			return true
		}
	}
	return false
}

// bootSource maps our BootTarget to the gofish BootSource value, defaulting to Cd.
func bootSource(t BootTarget) schemas.BootSource {
	switch t {
	case BootTargetUSB:
		return schemas.UsbBootSource
	case BootTargetCd:
		return schemas.CdBootSource
	default:
		return schemas.CdBootSource
	}
}

// bootMode maps our BootMode to the gofish BootSourceOverrideMode. It is only
// called for an explicit (non-empty) BootMode: an empty BootMode means "do not
// force a firmware boot mode" and is handled by setOneTimeBoot, which leaves the
// field unset so it is omitted from the PATCH. An unrecognised non-empty value
// returns empty so the caller can reject it rather than silently forcing a mode.
func bootMode(m BootMode) schemas.BootSourceOverrideMode {
	switch m {
	case BootModeLegacy:
		return schemas.LegacyBootSourceOverrideMode
	case BootModeUEFI:
		return schemas.UEFIBootSourceOverrideMode
	default:
		return ""
	}
}

// chooseResetType picks a sensible ResetType from the system's current power
// state, validated against the advertised allowable values when present. Off
// systems are powered On; running systems get a ForceRestart (falling back to
// GracefulRestart, then On) to drive the install boot.
func chooseResetType(system *schemas.ComputerSystem) schemas.ResetType {
	allowed, _ := system.GetSupportedResetTypes()

	if system.PowerState == schemas.OffPowerState {
		return firstSupported(allowed, schemas.OnResetType, schemas.ForceOnResetType)
	}
	return firstSupported(allowed,
		schemas.ForceRestartResetType,
		schemas.GracefulRestartResetType,
		schemas.OnResetType,
	)
}

// firstSupported returns the first preference present in allowed; if allowed is
// empty (the BMC advertised nothing) it returns the first preference unchanged.
func firstSupported(allowed []schemas.ResetType, preferences ...schemas.ResetType) schemas.ResetType {
	if len(allowed) == 0 {
		return preferences[0]
	}
	for _, pref := range preferences {
		for _, a := range allowed {
			if a == pref {
				return pref
			}
		}
	}
	// None of our preferences are allowed; fall back to the first preference and
	// let the BMC reject it with a useful error.
	return preferences[0]
}

// isKnownTaskState reports whether s is a member of the Redfish TaskState enum.
// An unknown value means the BMC returned a garbage/anomalous state that we must
// not treat as a normal non-terminal state (which would loop until the context
// deadline); pollTask rejects it with a clear error instead.
func isKnownTaskState(s schemas.TaskState) bool {
	switch s {
	case schemas.NewTaskState,
		schemas.StartingTaskState,
		schemas.RunningTaskState,
		schemas.SuspendedTaskState,
		schemas.InterruptedTaskState,
		schemas.PendingTaskState,
		schemas.StoppingTaskState,
		schemas.CompletedTaskState,
		schemas.KilledTaskState,
		schemas.ExceptionTaskState,
		schemas.ServiceTaskState,
		schemas.CancellingTaskState,
		schemas.CancelledTaskState:
		return true
	default:
		return false
	}
}

// isTerminalTaskState reports whether a Redfish TaskState is final.
func isTerminalTaskState(s schemas.TaskState) bool {
	switch s {
	case schemas.CompletedTaskState,
		schemas.ExceptionTaskState,
		schemas.KilledTaskState,
		schemas.CancelledTaskState:
		return true
	default:
		return false
	}
}

// taskMessages flattens a Task's messages into human-readable strings.
func taskMessages(task *schemas.Task) []string {
	if len(task.Messages) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(task.Messages))
	for _, m := range task.Messages {
		if m.Message != "" {
			msgs = append(msgs, m.Message)
		}
	}
	return msgs
}

// pickTaskURI returns the first non-empty Task monitor URI among the supplied
// TaskMonitorInfos, in preference order. A nil or empty TaskMonitor means the BMC
// answered synchronously and there is nothing to poll.
func pickTaskURI(infos ...*schemas.TaskMonitorInfo) string {
	for _, info := range infos {
		if info == nil {
			continue
		}
		if info.TaskMonitor != "" {
			return info.TaskMonitor
		}
		if info.Task != nil && info.Task.ODataID != "" {
			return info.Task.ODataID
		}
	}
	return ""
}
