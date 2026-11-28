package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	httpimpl "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/provider/internal/services"
	"github.com/kairos-io/kairos/v4/sdk/bus"
	sdkhttp "github.com/kairos-io/kairos/v4/sdk/types/http"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/kairos-io/kairos/v4/sdk/verify"
	"github.com/mudler/go-pluggable"
)

const (
	K3s = "k3s"
	K0s = "k0s"

	k3sInstallScriptURL = "https://get.k3s.io"

	// k3sInstallScriptSumsURL is published by k3s-io/k3s itself, alongside
	// install.sh in the same repo, specifically so this script can be
	// verified (added in k3s-io/k3s#8312). get.k3s.io serves the master
	// branch's install.sh byte-for-byte, so checking the download against
	// this sibling file catches a compromised or spoofed get.k3s.io without
	// k3s needing to sign anything new.
	k3sInstallScriptSumsURL = "https://raw.githubusercontent.com/k3s-io/k3s/master/install.sh.sha256sum"

	// k0sStableVersionURL is what get.k0s.sh itself resolves K0S_VERSION
	// from when the caller doesn't set one. Resolving it here too — instead
	// of leaving it to the script — means the exact version string is known
	// up front, so the matching release's sha256sums.txt can be fetched
	// before anything is installed.
	k0sStableVersionURL = "https://docs.k0sproject.io/stable.txt"

	// k0sBinaryDest is where the verified k0s binary is written directly;
	// the OpenRC/systemd unit content in services.K0sServices hardcodes this
	// same path.
	k0sBinaryDest = "/usr/bin/k0s"
)

// downloadK3sInstaller fetches the k3s install script from scriptURL and
// verifies it against the sha256 digest published at sumsURL before writing
// it to dest. It fails closed: a fetch error, a parse error, or a digest
// mismatch all return an error and leave dest untouched.
func downloadK3sInstaller(client sdkhttp.Client, l loggerpkg.KairosLogger, scriptURL, sumsURL, dest string) error {
	sums, err := verify.FetchChecksums(client, l, sumsURL)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", sumsURL, err)
	}
	want, err := verify.ChecksumFromSumsFile(sums, "install.sh")
	if err != nil {
		return fmt.Errorf("parse %s: %w", sumsURL, err)
	}
	return verify.VerifiedDownload(client, l, scriptURL, dest, want)
}

// resolveK0sVersion returns version unchanged if set, otherwise resolves
// k0s's current stable release the same way get.k0s.sh resolves K0S_VERSION
// internally when it isn't set — a plain fetch of stableURL. Resolving it
// here (rather than leaving it to the script k0s ships) is what lets the
// caller know which release's sha256sums.txt to fetch and verify against
// before anything is installed. stableURL is a parameter (production callers
// pass k0sStableVersionURL) so tests can point it at a local server instead
// of the real https://docs.k0sproject.io/stable.txt.
func resolveK0sVersion(client sdkhttp.Client, l loggerpkg.KairosLogger, stableURL, version string) (string, error) {
	if version != "" {
		return version, nil
	}
	body, err := verify.FetchChecksums(client, l, stableURL)
	if err != nil {
		return "", fmt.Errorf("resolve stable k0s version from %s: %w", stableURL, err)
	}
	resolved := strings.TrimSpace(string(body))
	if resolved == "" {
		return "", fmt.Errorf("%s returned an empty version", stableURL)
	}
	return resolved, nil
}

// k0sReleaseTag percent-encodes the "+" in a k0s version (e.g.
// "v1.36.4+k0s.0") for use as a release-tag URL path segment. GitHub's own
// release links for k0sproject/k0s escape it the same way (e.g.
// ".../download/v1.36.4%2Bk0s.0/sha256sums.txt").
func k0sReleaseTag(version string) string {
	return strings.ReplaceAll(version, "+", "%2B")
}

// k0sReleaseURLs builds the sha256sums.txt URL and the binary-name prefix
// (everything up to, but not including, the "k0s-<version>-<arch>" filename)
// for version's k0sproject/k0s release. Pulled out as a pure function so the
// URL shape (including the "+" percent-encoding) is unit testable without a
// network call.
func k0sReleaseURLs(version string) (sumsURL, binaryURLPrefix string) {
	tag := k0sReleaseTag(version)
	sumsURL = fmt.Sprintf("https://github.com/k0sproject/k0s/releases/download/%s/sha256sums.txt", tag)
	binaryURLPrefix = fmt.Sprintf("https://github.com/k0sproject/k0s/releases/download/%s/", tag)
	return sumsURL, binaryURLPrefix
}

// k0sArch maps runtime.GOARCH onto the arch suffix k0sproject/k0s publishes
// release binaries under, mirroring _detect_arch in get.k0s.sh.
func k0sArch(goarch string) (string, error) {
	switch goarch {
	case "amd64", "arm64", "arm":
		return goarch, nil
	default:
		return "", fmt.Errorf("unsupported architecture for k0s: %s", goarch)
	}
}

// downloadVerifiedK0sBinaryAt fetches sumsURL (k0sproject/k0s's own
// sha256sums.txt for one release), finds the entry for
// "k0s-<version>-<arch>", and downloads+verifies that binary from
// binaryURLPrefix+"k0s-<version>-<arch>" to dest. Split out from
// downloadVerifiedK0sBinary so tests can point both URLs at a local server.
func downloadVerifiedK0sBinaryAt(client sdkhttp.Client, l loggerpkg.KairosLogger, sumsURL, binaryURLPrefix, version, arch, dest string) error {
	sums, err := verify.FetchChecksums(client, l, sumsURL)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", sumsURL, err)
	}
	filename := fmt.Sprintf("k0s-%s-%s", version, arch)
	want, err := verify.ChecksumFromSumsFile(sums, filename)
	if err != nil {
		return fmt.Errorf("find checksum for %s in %s: %w", filename, sumsURL, err)
	}
	url := binaryURLPrefix + filename
	if err := verify.VerifiedDownload(client, l, url, dest, want); err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	return nil
}

// downloadVerifiedK0sBinary resolves version (falling back to k0s's current
// stable release when unset), fetches that release's own sha256sums.txt —
// published by k0sproject/k0s alongside every release binary — and
// downloads+verifies the k0s binary for the running architecture straight
// from k0sproject/k0s's GitHub release, writing it to dest.
//
// This replaces running get.k0s.sh entirely. That script (served from the
// k0sproject/get GitHub Pages repo, a plain index.html with no published
// checksum or signature of its own) does nothing but resolve K0S_VERSION and
// fetch this exact same binary; doing that fetch ourselves, verified, means
// kairos never has to execute an unverifiable script to get k0s installed.
func downloadVerifiedK0sBinary(client sdkhttp.Client, l loggerpkg.KairosLogger, version, dest string) (resolvedVersion string, err error) {
	resolvedVersion, err = resolveK0sVersion(client, l, k0sStableVersionURL, version)
	if err != nil {
		return "", err
	}
	arch, err := k0sArch(runtime.GOARCH)
	if err != nil {
		return "", err
	}
	sumsURL, binaryURLPrefix := k0sReleaseURLs(resolvedVersion)
	if err := downloadVerifiedK0sBinaryAt(client, l, sumsURL, binaryURLPrefix, resolvedVersion, arch, dest); err != nil {
		return "", err
	}
	return resolvedVersion, nil
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
	client := httpimpl.NewClient()

	// Download the installer script for the provider
	switch p.Provider {
	case K3s:
		l.Logger.Info().Msgf("Downloading installer script for %s from %s", p.Provider, k3sInstallScriptURL)
		if err := downloadK3sInstaller(client, l, k3sInstallScriptURL, k3sInstallScriptSumsURL, installerFile); err != nil {
			l.Logger.Error().Err(err).Msg("Failed to download and verify k3s installer script")
			returnData.Error = fmt.Sprintf("Failed to download and verify k3s installer script: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}
		// Make the installer script executable
		if err := os.Chmod(installerFile, 0755); err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to make installer script executable: %s", installerFile)
			returnData.Error = fmt.Sprintf("Failed to make installer script executable: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}
	case K0s:
		// get.k0s.sh (k0sproject/get, a bare GitHub Pages index.html) has no
		// published checksum or signature of its own, so it is never
		// downloaded or executed here at all. Its only job — resolve
		// K0S_VERSION and fetch the matching k0s-<version>-<arch> binary —
		// is done directly instead, verified against the sha256sums.txt
		// k0sproject/k0s publishes alongside every release.
		l.Logger.Info().Msg("Resolving k0s version and downloading its binary directly from k0sproject/k0s (sha256 verified)")
		resolvedVersion, err := downloadVerifiedK0sBinary(client, l, p.Version, k0sBinaryDest)
		if err != nil {
			l.Logger.Error().Err(err).Msg("Failed to download and verify k0s binary")
			returnData.Error = fmt.Sprintf("Failed to download and verify k0s binary: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}
		if err := os.Chmod(k0sBinaryDest, 0755); err != nil {
			l.Logger.Error().Err(err).Msgf("Failed to make %s executable", k0sBinaryDest)
			returnData.Error = fmt.Sprintf("Failed to make %s executable: %s", k0sBinaryDest, err)
			returnData.State = bus.EventResponseError
			return returnData
		}
		p.Version = resolvedVersion
	default:
		// This is not for us, its for another provider or no provider was specified
		l.Logger.Info().Msg("No valid provider specified or unsupported provider. Skipping buildtime logic.")
		returnData.State = bus.EventResponseNotApplicable
		return returnData
	}

	// Install the binaries
	var out []byte
	var err error
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
			l.Logger.Error().Err(err).Msgf("Failed to run k3s agent installer script: %s", string(out2))
			returnData.Error = fmt.Sprintf("Failed to run k3s agent installer script: %s", string(out2))
			returnData.State = bus.EventResponseError
			return returnData
		}
		out = append(out, out2...)
	case K0s:
		// The verified binary is already in place at k0sBinaryDest (done
		// above, before this switch); the installer script would have
		// produced the systemd/OpenRC unit files, but doesn't run here at
		// all, and wouldn't have worked in a Dockerfile build environment's
		// service-manager detection anyway — so those are always created
		// manually.
		l.Logger.Info().Msg("Creating k0s service file manually")
		if err := services.K0sServices(l); err != nil {
			l.Logger.Error().Err(err).Msg("Failed to create k0s service file")
			returnData.Error = fmt.Sprintf("Failed to create k0s service file: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}

		// Writing the units ourselves also skips the accounts `k0s install
		// controller` creates. Without them the controller components run as
		// root, so create them here.
		l.Logger.Info().Msg("Creating k0s system users")
		err = services.EnsureK0sControllerUsers(l)
		if err != nil {
			l.Logger.Error().Err(err).Msg("Failed to create k0s system users")
			returnData.Error = fmt.Sprintf("Failed to create k0s system users: %s", err)
			returnData.State = bus.EventResponseError
			return returnData
		}

		out = []byte(fmt.Sprintf("k0s %s installed to %s (sha256 verified against k0sproject/k0s's sha256sums.txt)\n", p.Version, k0sBinaryDest))
	}
	returnData.Data = string(out)
	returnData.State = bus.EventResponseSuccess
	l.Logger.Debug().Msg("Returning response for buildtime event")
	l.Logger.Debug().Interface("response", returnData).Msg("Response details")
	return returnData
}

// InfoEvent handles the info event for the provider. Called by kairos-init during the build process.
// It returns the installed version of the provider the caller asks about.
//
// The payload names that provider. An image can carry both k3s and k0s, because
// kairos-init takes --provider more than once, and a broadcast gives one
// response per plugin, so a plugin that handles both can report only one of
// them per event. Answering with whichever binary happens to be checked last
// hides the other one.
func InfoEvent(e *pluggable.Event) pluggable.EventResponse {
	l := loggerpkg.NewKairosLogger("provider-kairos-info", "info", true)
	l.Logger.Info().Msg("Info event received")
	l.Logger.Debug().Interface("event", e).Msg("Event details")

	// A caller that asks about no provider in particular sends the JSON null
	// that go-pluggable marshals a nil payload into. Decoding that leaves the
	// payload zeroed, which reads the same as naming no provider.
	requested := bus.ProviderPayload{}
	if e != nil && e.Data != "" {
		if err := json.Unmarshal([]byte(e.Data), &requested); err != nil {
			l.Logger.Error().Err(err).Msg("Failed to unmarshal event data")
			return pluggable.EventResponse{
				State: bus.EventResponseError,
				Data:  "",
				Error: err.Error(),
			}
		}
	}

	// Nothing to report: either the provider asked about is not installed, or
	// no supported provider is installed at all.
	infoData, found := installedProvider(requested.Provider, supportedProviders(), l)
	if !found {
		l.Logger.Info().Str("requested", requested.Provider).Msg("No provider found, returning not applicable state")
		return pluggable.EventResponse{
			State: bus.EventResponseNotApplicable,
			Data:  "",
			Error: "",
		}
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
	data := pluggable.EventResponse{
		State: bus.EventResponseSuccess,
		Data:  string(jsondata),
		Error: "",
	}

	l.Logger.Debug().Msg("Returning response for info event")
	l.Logger.Debug().Interface("response", data).Msg("Response details")

	return data
}

// providerBinary is one provider this plugin can report on: where its binary
// lives, if it is installed at all, and how to read its version.
type providerBinary struct {
	name    string
	bin     func() string
	version func(loggerpkg.KairosLogger) string
}

// supportedProviders lists the providers this plugin serves, in the order the
// fallback below walks them.
func supportedProviders() []providerBinary {
	return []providerBinary{
		{name: K0s, bin: utils.K0sBin, version: k0sVersion},
		{name: K3s, bin: utils.K3sBin, version: k3sVersion},
	}
}

// installedProvider reports the installed version of the provider named in
// requested, or nothing when that provider is not installed. An empty version
// is still an answer here: the caller asked about this provider and no other
// one can stand in for it.
//
// With no provider named it falls back to the providers that are installed, so
// that an image which was not built with --provider still reports the provider
// binary it inherited. One of them has to be chosen, and the choice is made
// here rather than left to the order the binaries happen to be probed in: a
// provider whose version could be read wins, because kairos-init drops an
// answer that carries no version and the image would then ship with no
// software version at all while another provider on it had one. Input order
// decides the rest.
//
// The second return value says whether anything was found.
func installedProvider(requested string, supported []providerBinary, l loggerpkg.KairosLogger) (bus.ProviderInstalledVersionPayload, bool) {
	installed := make([]bus.ProviderInstalledVersionPayload, 0, len(supported))
	for _, p := range supported {
		if requested != "" && requested != p.name {
			continue
		}
		if p.bin() == "" {
			continue
		}
		installed = append(installed, bus.ProviderInstalledVersionPayload{Provider: p.name, Version: p.version(l)})
	}

	if len(installed) == 0 {
		return bus.ProviderInstalledVersionPayload{}, false
	}
	if len(installed) > 1 {
		names := make([]string, 0, len(installed))
		for _, c := range installed {
			names = append(names, c.Provider)
		}
		l.Logger.Warn().Strs("installed", names).Msg("This image carries more than one provider and the event named none, so only one of them is reported")
	}

	for _, c := range installed {
		if c.Version != "" {
			return c, true
		}
	}

	return installed[0], true
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
