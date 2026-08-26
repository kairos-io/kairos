package tui

import (
	"os"
	"strings"

	"github.com/kairos-io/kairos-installer/prereqs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/mudler/go-pluggable"
)

// Interactive-install check plugins are a dedicated plugin set, separate from
// the generic agent providers (agent-provider-* under /system/providers). They
// are named tui-check-<name> and discovered in the directories below (plus the
// current working directory, which is mostly useful for tests and development).
const CheckPluginPrefix = "tui-check"

// CheckPluginPaths are the directories scanned for tui-check-* plugins.
var CheckPluginPaths = []string{"/system/tui-checks", "/usr/local/system/tui-checks"}

// newCheckManager builds a go-pluggable manager dedicated to the
// interactive-install check events and autoloads the tui-check-* plugins. It
// deliberately does NOT install the global error->os.Exit handler that
// internal/bus uses, so a misbehaving check plugin can never kill the TUI; all
// problems are logged and surfaced through the returned data instead.
func newCheckManager(log sdkLogger.KairosLogger) *pluggable.Manager {
	m := pluggable.NewManager([]pluggable.EventType{prereqs.EventChecks, prereqs.EventChecksApply})

	wd, _ := os.Getwd()
	paths := append(append([]string{}, CheckPluginPaths...), wd)

	log.Logger.Info().
		Strs("paths", paths).
		Str("prefix", CheckPluginPrefix+"-").
		Msg("Discovering interactive-install check plugins")

	m.Autoload(CheckPluginPrefix, paths...).Register()

	if len(m.Plugins) == 0 {
		log.Logger.Info().Strs("paths", paths).Msg("No interactive-install check plugins found")
	}
	for _, p := range m.Plugins {
		log.Logger.Info().Str("name", p.Name).Str("executable", p.Executable).Msg("Found check plugin")
	}
	return m
}

// logPluginResponse logs everything a check plugin returned: its state/error,
// the size of the data, and — crucially — its captured stdout/stderr (go-
// pluggable puts the plugin's output in EventResponse.Logs, so this is the only
// way the plugin's own logging reaches journald).
func logPluginResponse(log sdkLogger.KairosLogger, phase string, p *pluggable.Plugin, r *pluggable.EventResponse) {
	evt := log.Logger.Info().
		Str("phase", phase).
		Str("plugin", p.Name).
		Str("executable", p.Executable).
		Int("data_bytes", len(r.Data))
	if r.State != "" {
		evt = evt.Str("state", r.State)
	}
	if r.Error != "" {
		evt = evt.Str("error", r.Error)
	}
	evt.Msg("Check plugin responded")

	if strings.TrimSpace(r.Logs) != "" {
		for line := range strings.SplitSeq(strings.TrimRight(r.Logs, "\n"), "\n") {
			log.Logger.Info().Str("plugin", p.Name).Msgf("[%s] %s", p.Name, line)
		}
	}
}

// gatherChecks publishes the prerequisites check event on the dedicated manager
// and collects the checks returned by every plugin. It is synchronous (go-
// pluggable runs the plugins inline during Publish).
func gatherChecks(m *pluggable.Manager, log sdkLogger.KairosLogger, config string) ([]prereqs.Check, error) {
	var checks []prereqs.Check
	var firstErr error

	m.Response(prereqs.EventChecks, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		logPluginResponse(log, "checks", p, r)
		if r.Data == "" {
			return
		}
		parsed, err := prereqs.ParseChecks(r.Data)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Logger.Error().Err(err).Str("plugin", p.Name).Msg("Failed parsing checks from plugin")
			return
		}
		log.Logger.Info().Str("plugin", p.Name).Int("checks", len(parsed)).Msg("Parsed checks from plugin")
		checks = append(checks, parsed...)
	})

	log.Logger.Info().Str("event", string(prereqs.EventChecks)).Int("plugins", len(m.Plugins)).Msg("Publishing checks event to plugins")
	if _, err := m.Publish(prereqs.EventChecks, prereqs.ChecksPayload{Config: config}); err != nil {
		log.Logger.Error().Err(err).Msg("Failed publishing checks event")
		return checks, err
	}
	log.Logger.Info().Int("checks", len(checks)).Msg("Collected checks from all plugins")
	return checks, firstErr
}

// applyDecisions publishes the apply event with the user's decisions and
// collects the results returned by every plugin.
func applyDecisions(m *pluggable.Manager, log sdkLogger.KairosLogger, decisions []prereqs.Decision, config string) ([]prereqs.ApplyResult, error) {
	var results []prereqs.ApplyResult
	var firstErr error

	m.Response(prereqs.EventChecksApply, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		logPluginResponse(log, "apply", p, r)
		if r.Data == "" {
			return
		}
		parsed, err := prereqs.ParseApplyResults(r.Data)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Logger.Error().Err(err).Str("plugin", p.Name).Msg("Failed parsing apply results from plugin")
			return
		}
		results = append(results, parsed...)
	})

	log.Logger.Info().Str("event", string(prereqs.EventChecksApply)).Int("decisions", len(decisions)).Msg("Publishing apply event to plugins")
	if _, err := m.Publish(prereqs.EventChecksApply, prereqs.ApplyPayload{Decisions: decisions, Config: config}); err != nil {
		log.Logger.Error().Err(err).Msg("Failed publishing apply event")
		return results, err
	}
	log.Logger.Info().Int("results", len(results)).Msg("Collected apply results from all plugins")
	return results, firstErr
}
