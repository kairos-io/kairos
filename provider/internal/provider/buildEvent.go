package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/provider/internal/services"
	"github.com/kairos-io/kairos/v4/sdk/bus"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/kairos-io/kairos/v4/sdk/verify"
	"github.com/mudler/go-pluggable"
)

const (
	K3s = "k3s"
	K0s = "k0s"

	k3sInstallScriptURL = "https://get.k3s.io"
	k0sInstallScriptURL = "https://get.k0s.sh"

	// k3sInstallScriptSumsURL is published by k3s-io/k3s itself, alongside
	// install.sh in the same repo, specifically so this script can be
	// verified (added in k3s-io/k3s#8312). get.k3s.io serves the master
	// branch's install.sh byte-for-byte, so checking the download against
	// this sibling file catches a compromised or spoofed get.k3s.io without
	// k3s needing to sign anything new.
	k3sInstallScriptSumsURL = "https://raw.githubusercontent.com/k3s-io/k3s/master/install.sh.sha256sum"
)

// downloadK3sInstaller fetches the k3s install script from scriptURL and
// verifies it against the sha256 digest published at sumsURL before writing
// it to dest. It fails closed: a fetch error, a parse error, or a digest
// mismatch all return an error and leave dest untouched.
func downloadK3sInstaller(scriptURL, sumsURL, dest string) error {
	sums, err := verify.FetchChecksums(sumsURL)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", sumsURL, err)
	}
	want, err := verify.ChecksumFromSumsFile(sums, "install.sh")
	if err != nil {
		return fmt.Errorf("parse %s: %w", sumsURL, err)
	}
	data, err := verify.VerifiedDownload(scriptURL, want)
	if err != nil {
		return fmt.Errorf("download %s: %w", scriptURL, err)
	}
	return os.WriteFile(dest, data, 0644)
}

// BuildEvent handles the buildtime event for the provider. Called by kairos-init during the build process.
func BuildEvent(e *pluggable.Event) pluggable.EventResponse {
	returnData := pluggable.EventResponse{
		State: "",
		Data:  "",
		Error: "",
	}
	l := loggerpkg.NewKairosLogger("provider-kairos-build", "info", true)
	l.Logger.Info().Msg("Buildtime event received")
	l.Logger.Debug().Interface("event", e).Msg("Event details")
	// unmarshal the event data if needed
	p := &bus.ProviderPayload{}
	if e.Data != "" {
		err := json.Unmarshal([]byte(e.Data), p)
		if err != nil {
			l.Logger.Error().Err(err).Msg("Failed to unmarshal event data")
			returnData.Error = err.Error()
			returnData.State = bus.EventResponseError
			return returnData
		}
	}
	// Now move the logger to the requested log level
	l.SetLevel(p.LogLevel)
	l.Logger.Debug().Interface("payload", p).Msg("Payload details")
	installerFile := filepath.Join(os.TempDir(), "installer.sh")

	// Download the installer script for the provider
	switch p.Provider {
	case K3s:
		l.Logger.Info().Msgf("Downloading installer script for %s from %s", p.Provider, k3sInstallScriptURL)
		if err := downloadK3sInstaller(k3sInstallScriptURL, k3sInstallScriptSumsURL, installerFile); err != nil {
			l.Logger.Error().Err(err).Msg("Failed to download and verify k3s installer script")
			returnData.Error = fmt.Sprintf("Failed to download and verify k3s installer script: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}
	case K0s:
		l.Logger.Info().Msgf("Downloading installer script for %s from %s", p.Provider, k0sInstallScriptURL)
		// TODO(supply-chain): get.k0s.sh is served from the k0sproject/get
		// GitHub Pages repo (a plain index.html), which publishes no
		// companion checksum or signature for it — unlike get.k3s.io's
		// install.sh.sha256sum. There is currently no real verification
		// path to hook into here; don't invent a homegrown one for it.
		out, err := exec.Command("curl", "-sfL", k0sInstallScriptURL, "-o", installerFile).CombinedOutput()
		if err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to download installer script: %s", string(out))
			returnData.Error = fmt.Sprintf("Failed to download installer script: %s", string(out))
			returnData.State = bus.EventResponseError
			return returnData
		}
	default:
		// This is not for us, its for another provider or no provider was specified
		l.Logger.Info().Msg("No valid provider specified or unsupported provider. Skipping buildtime logic.")
		returnData.State = bus.EventResponseNotApplicable
		return returnData
	}
	// Make the installer script executable
	err := os.Chmod(installerFile, 0755)
	if err != nil {
		l.Logger.Error().Err(err).Msgf("Failed to make installer script executable: %s", installerFile)
		returnData.Error = fmt.Sprintf("Failed to make installer script executable: %s", err)
		returnData.State = bus.EventResponseError
		return returnData
	}

	// Install the binaries
	var out []byte
	switch p.Provider {
	case K3s:
		// Prepare environment variables
		env := os.Environ()
		env = append(env, "INSTALL_K3S_BIN_DIR=/usr/bin", "INSTALL_K3S_SKIP_ENABLE=true", "INSTALL_K3S_SKIP_SELINUX_RPM=true")
		if p.Version != "" {
			env = append(env, fmt.Sprintf("INSTALL_K3S_VERSION=%s", p.Version))
		}

		l.Logger.Info().Msg("Running k3s installer script")
		cmd := exec.Command("sh", installerFile)
		cmd.Env = env
		out, err = cmd.CombinedOutput()
		if err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to run k3s installer script: %s", string(out))
			returnData.Error = fmt.Sprintf("Failed to run k3s installer script: %s", string(out))
			returnData.State = bus.EventResponseError
			return returnData
		}

		// Now agent
		agentCmd := exec.Command("sh", installerFile, "agent")
		agentCmd.Env = env
		out2, err := agentCmd.CombinedOutput()
		if err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to run k3s agent installer script: %s", string(out))
			returnData.Error = fmt.Sprintf("Failed to run k3s agent installer script: %s", string(out))
			returnData.State = bus.EventResponseError
			return returnData
		}
		out = append(out, out2...)
	case K0s:
		env := os.Environ()
		if p.Version != "" {
			env = append(env, fmt.Sprintf("K0S_VERSION=%s", p.Version))
		}
		l.Logger.Info().Msg("Running k0s installer script")
		cmd := exec.Command("sh", installerFile)
		cmd.Env = env
		out, err = cmd.CombinedOutput()
		if err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to run k0s installer script: %s", string(out))
			returnData.Error = fmt.Sprintf("Failed to run k0s installer script: %s", string(out))
			returnData.State = bus.EventResponseError
			return returnData
		}
		// move the binary to a decent location t avoid overwriting it with PERSISTENT
		err = os.Rename("/usr/local/bin/k0s", "/usr/bin/k0s")
		if err != nil {
			l.Logger.Error().Err(err).Msg("Failed to move k0s binary to /usr/bin")
			returnData.Error = fmt.Sprintf("Failed to move k0s binary to /usr/bin: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}
		// Because we change the binary location, the installer script wont produce the proper services
		// also we are running in a dockerfile so the service manager identification does not work as expected
		l.Logger.Info().Msg("Creating k0s service file manually")
		err = services.K0sServices(l)
		if err != nil {
			l.Logger.Error().Err(err).Msg("Failed to create k0s service file")
			returnData.Error = fmt.Sprintf("Failed to create k0s service file: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}

	}
	returnData.Data = string(out)
	returnData.State = bus.EventResponseSuccess
	l.Logger.Debug().Msg("Returning response for buildtime event")
	l.Logger.Debug().Interface("response", returnData).Msg("Response details")
	return returnData
}

// InfoEvent handles the info event for the provider. Called by kairos-init during the build process.
// It returns the installed version of the provider if available.
func InfoEvent(e *pluggable.Event) pluggable.EventResponse {
	l := loggerpkg.NewKairosLogger("provider-kairos-info", "info", true)
	l.Logger.Info().Msg("Info event received")
	l.Logger.Debug().Interface("event", e).Msg("Event details")

	infoData := bus.ProviderInstalledVersionPayload{}

	if k3s := utils.K3sBin(); k3s != "" {
		infoData.Provider = K3s
		infoData.Version = k3sVersion(l)

	}
	if k0s := utils.K0sBin(); k0s != "" {
		infoData.Provider = K0s
		infoData.Version = k0sVersion(l)
	}

	// This is the returned data for the info event
	jsondata, err := json.Marshal(infoData)
	if err != nil {
		l.Logger.Error().Err(err).Msg("Failed to marshal info data")
		return pluggable.EventResponse{
			State: bus.EventResponseError,
			Data:  "",
			Error: err.Error(),
		}
	}
	// If no provider was found, we return an empty response with a not applicable state
	if infoData.Provider == "" {
		l.Logger.Info().Msg("No provider found, returning not applicable state")
		return pluggable.EventResponse{
			State: bus.EventResponseNotApplicable,
			Data:  "",
			Error: "",
		}
	}
	data := pluggable.EventResponse{
		State: bus.EventResponseSuccess,
		Data:  string(jsondata),
		Error: "",
	}

	l.Logger.Debug().Msg("Returning response for info event")
	l.Logger.Debug().Interface("response", data).Msg("Response details")

	return data
}

// k3sVersion retrieves the version of k3s installed on the system.
func k3sVersion(logger loggerpkg.KairosLogger) string {
	out, err := exec.Command(utils.K3sBin(), "--version").CombinedOutput()
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the k3s version: %s", err)
		return ""
	}
	// 2 lines in this format:
	// k3s version v1.21.4+k3s1 (3781f4b7)
	// go version go1.16.5
	// We need the first line
	re := regexp.MustCompile(`k3s version (v\d+\.\d+\.\d+\+k3s\d+)`)
	if re.MatchString(string(out)) {
		match := re.FindStringSubmatch(string(out))
		return match[1]
	}
	logger.Logger.Error().Msgf("Failed to parse the k3s version: %s", string(out))
	return ""
}

// k0sVersion retrieves the version of k0s installed on the system.
func k0sVersion(logger loggerpkg.KairosLogger) string {
	out, err := exec.Command(utils.K0sBin(), "version").CombinedOutput()
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the k0s version: %s", err)
		return ""
	}

	return strings.TrimSpace(string(out))
}
