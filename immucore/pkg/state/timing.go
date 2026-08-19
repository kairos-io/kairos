package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	internalUtils "github.com/kairos-io/immucore/internal/utils"
	"github.com/spectrocloud-labs/herd"
)

// TimelineTraceFile is the basename of the machine-readable trace written under constants.LogDir.
const TimelineTraceFile = "boot-timeline.json"

// StepTiming holds the wall-clock duration of a single DAG step.
type StepTiming struct {
	Name     string
	Duration time.Duration
	Err      error
}

// timeline is a process-global, mutex-guarded registry of step timings.
// herd runs DAG ops concurrently, so every access is guarded.
var timeline = struct {
	sync.Mutex
	steps map[string]StepTiming
}{steps: map[string]StepTiming{}}

// recordTiming stores (or overwrites) the timing for a named step in a race-safe way.
func recordTiming(name string, d time.Duration, err error) {
	timeline.Lock()
	defer timeline.Unlock()
	timeline.steps[name] = StepTiming{Name: name, Duration: d, Err: err}
}

// RunTimed executes fn, measuring its wall-clock duration and recording it under name.
// It returns the error fn returned, unchanged.
func RunTimed(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	recordTiming(name, time.Since(start), err)
	return err
}

// TimedCallback wraps a herd callback so its wall-clock duration is recorded under name,
// and returns it as a herd.WithCallback OpOption. This is the drop-in replacement for
// herd.WithCallback(fn) in step registrations: herd.WithCallback(fn) -> state.TimedCallback(name, fn).
func TimedCallback(name string, fn func(context.Context) error) herd.OpOption {
	return herd.WithCallback(func(ctx context.Context) error {
		return RunTimed(name, func() error { return fn(ctx) })
	})
}

// Timings returns a copy of the recorded step timings, keyed by step name.
func Timings() map[string]StepTiming {
	timeline.Lock()
	defer timeline.Unlock()
	out := make(map[string]StepTiming, len(timeline.steps))
	for k, v := range timeline.steps {
		out[k] = v
	}
	return out
}

// ResetTimeline clears all recorded timings. Mainly useful for tests.
func ResetTimeline() {
	timeline.Lock()
	defer timeline.Unlock()
	timeline.steps = map[string]StepTiming{}
}

// sortedTimings returns the recorded timings sorted slowest-first (ties broken by name).
func sortedTimings() []StepTiming {
	tm := Timings()
	out := make([]StepTiming, 0, len(tm))
	for _, v := range tm {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Duration == out[j].Duration {
			return out[i].Name < out[j].Name
		}
		return out[i].Duration > out[j].Duration
	})
	return out
}

// RenderTimeline renders the boot timeline as a human-readable string,
// listing steps slowest-first with their durations.
func RenderTimeline() string {
	steps := sortedTimings()
	if len(steps) == 0 {
		return "Boot timeline: no steps recorded\n"
	}
	var b strings.Builder
	b.WriteString("Boot timeline (slowest first):\n")
	for i, s := range steps {
		status := "ok"
		if s.Err != nil {
			status = fmt.Sprintf("error: %s", s.Err.Error())
		}
		fmt.Fprintf(&b, "%d. <%s> %s (%s)\n", i+1, s.Name, s.Duration.Round(time.Millisecond), status)
	}
	return b.String()
}

// timelineEntry is the JSON shape of a single step in the trace file.
type timelineEntry struct {
	Name       string  `json:"name"`
	DurationMs float64 `json:"duration_ms"`
	Error      string  `json:"error,omitempty"`
}

// WriteTimelineTrace writes the boot timeline as machine-readable JSON to dir/boot-timeline.json,
// slowest-first. It returns the path written.
func WriteTimelineTrace(dir string) (string, error) {
	steps := sortedTimings()
	entries := make([]timelineEntry, 0, len(steps))
	for _, s := range steps {
		e := timelineEntry{
			Name:       s.Name,
			DurationMs: float64(s.Duration) / float64(time.Millisecond),
		}
		if s.Err != nil {
			e.Error = s.Err.Error()
		}
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, TimelineTraceFile)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// LogTimeline emits the boot timeline to the logger (slowest-first, structured per step)
// and writes the machine-readable trace under dir. Non-fatal: failures are logged, not returned.
func LogTimeline(dir string) {
	for i, s := range sortedTimings() {
		ev := internalUtils.KLog.Logger.Info().
			Int("rank", i+1).
			Str("step", s.Name).
			Dur("duration", s.Duration)
		if s.Err != nil {
			ev = ev.Str("error", s.Err.Error())
		}
		ev.Msg("Boot timeline step")
	}

	path, err := WriteTimelineTrace(dir)
	if err != nil {
		internalUtils.KLog.Logger.Warn().Err(err).Str("dir", dir).Msg("Could not write boot timeline trace")
		return
	}
	internalUtils.KLog.Logger.Info().Str("path", path).Msg("Wrote boot timeline trace")
}
