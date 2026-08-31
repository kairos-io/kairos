package phonehome

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// runCommand / removeFile / readFile / writeFile / globFiles are the
// package-local indirections used so tests can swap in fake runners without
// having to build a full fake filesystem. Production code goes through
// exec.Command and the os package.
var (
	runCommand = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput() //nosec G204 -- callers pass fixed systemctl subcommands only
	}
	removeFile = func(path string) error { return os.Remove(path) }
	readFile   = func(path string) ([]byte, error) { return os.ReadFile(path) }
	writeFile  = func(path string, data []byte, perm os.FileMode) error {
		return os.WriteFile(path, data, perm)
	}
	globFiles = func(pattern string) ([]string, error) { return filepath.Glob(pattern) }
)

// oemGlobs are the cloud-config file patterns Kairos ships under /oem. We
// scan both because both extensions appear in the wild.
var oemGlobs = []string{"/oem/*.yaml", "/oem/*.yml"}

// Uninstall runs the on-host phone-home teardown: disable the systemd unit,
// remove the unit file, reload systemd, drop the credentials file, and
// strip the `phonehome:` stanza from every cloud-config under /oem
// (deleting the file if phonehome was its only content).
//
// When stopService is true, the running unit is stopped first. This is the
// right thing for the local `kairos-agent phone-home uninstall` CLI — it
// runs in its own process, so `systemctl stop kairos-agent-phonehome` kills
// the *service* process, not the CLI process doing the teardown.
//
// When stopService is false, the stop step is skipped. The remote
// `unregister` command handler uses this path: the agent invoking Uninstall
// IS the service process, and `systemctl stop` would SIGTERM it before the
// "Completed" status could be written to the WebSocket. Instead, the caller
// lets this function finish, flushes the Completed status, then terminates
// the process itself via Client.Stop(). Once the unit file is gone and
// systemd has been reloaded, a natural process exit is all systemd needs.
//
// Every step is best-effort and idempotent. Running this twice against the
// same node is a no-op. The returned error is non-nil only when something
// unexpected (permission denied, I/O error that isn't "not found") prevents
// cleanup — the summary string names the offending path so operators can
// act on it.
func Uninstall(stopService bool) (string, error) {
	var (
		lines []string
		fatal error
	)
	note := func(msg string) { lines = append(lines, msg) }
	noteErr := func(msg string, err error) {
		lines = append(lines, msg)
		if fatal == nil {
			fatal = err
		}
	}

	if stopService {
		if out, err := runCommand("systemctl", "stop", ServiceName); err != nil {
			note(fmt.Sprintf("stopping %s: %s (%s)", ServiceName, strings.TrimSpace(string(out)), err))
		} else {
			note("stopped " + ServiceName)
		}
	}

	// Disable before removing the unit file — disable needs the file there
	// to resolve the Install section's WantedBy symlinks.
	if out, err := runCommand("systemctl", "disable", ServiceName); err != nil {
		note(fmt.Sprintf("disabling %s: %s (%s)", ServiceName, strings.TrimSpace(string(out)), err))
	} else {
		note("disabled " + ServiceName)
	}

	if err := removeFile(ServicePath); err != nil {
		if os.IsNotExist(err) {
			note("unit file already absent: " + ServicePath)
		} else {
			noteErr(fmt.Sprintf("removing %s: %v", ServicePath, err), err)
		}
	} else {
		note("removed " + ServicePath)
	}

	// daemon-reload so the removed unit drops from the unit cache. After
	// this point systemd no longer tracks this unit; a clean process exit
	// is enough for the service cgroup to be reaped.
	if out, err := runCommand("systemctl", "daemon-reload"); err != nil {
		note(fmt.Sprintf("daemon-reload: %s (%s)", strings.TrimSpace(string(out)), err))
	} else {
		note("daemon-reload: ok")
	}

	if err := removeFile(DefaultCredentialsPath); err != nil {
		if os.IsNotExist(err) {
			note("credentials already absent: " + DefaultCredentialsPath)
		} else {
			noteErr(fmt.Sprintf("removing %s: %v", DefaultCredentialsPath, err), err)
		}
	} else {
		note("removed " + DefaultCredentialsPath)
	}

	// OEM cleanup. Kairos installs end up merging every datasource + overlay
	// cloud-config into a single /oem/90_custom.yaml, so we can't just
	// os.Remove a fixed path — we walk every file under /oem, find the ones
	// that carry a top-level `phonehome:` key, and either delete the file
	// (if phonehome was its only content) or rewrite it with that key
	// stripped. Key order and the `#cloud-config` header are preserved.
	oemLines, oemErr := cleanPhonehomeFromOEM()
	lines = append(lines, oemLines...)
	if oemErr != nil && fatal == nil {
		fatal = oemErr
	}

	return strings.Join(lines, "\n"), fatal
}

func cleanPhonehomeFromOEM() ([]string, error) {
	var (
		lines []string
		fatal error
	)
	seen := map[string]bool{}
	touched := 0
	for _, pattern := range oemGlobs {
		matches, _ := globFiles(pattern)
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true

			data, err := readFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				lines = append(lines, fmt.Sprintf("reading %s: %v", path, err))
				if fatal == nil {
					fatal = err
				}
				continue
			}

			out, empty, changed, err := stripPhonehomeKey(data)
			if err != nil {
				// An unrelated malformed YAML in /oem isn't our problem —
				// note it and move on, don't surface as a fatal error.
				lines = append(lines, fmt.Sprintf("skipped %s: %v", path, err))
				continue
			}
			if !changed {
				continue
			}
			touched++

			if empty {
				if err := removeFile(path); err != nil && !os.IsNotExist(err) {
					lines = append(lines, fmt.Sprintf("removing %s: %v", path, err))
					if fatal == nil {
						fatal = err
					}
					continue
				}
				lines = append(lines, "removed "+path)
				continue
			}
			if err := writeFile(path, out, 0600); err != nil {
				lines = append(lines, fmt.Sprintf("writing %s: %v", path, err))
				if fatal == nil {
					fatal = err
				}
				continue
			}
			lines = append(lines, "stripped phonehome stanza from "+path)
		}
	}
	if touched == 0 {
		lines = append(lines, "no phonehome stanza found under /oem")
	}
	return lines, fatal
}

// stripPhonehomeKey removes the top-level `phonehome:` mapping entry from a
// cloud-config document and returns the rewritten bytes. The `#cloud-config`
// header (and any other leading comment lines) are preserved verbatim; the
// remaining top-level keys keep their original order.
//
// Returns:
//   - out:     rewritten bytes (valid only when changed is true and empty is false)
//   - empty:   the file would be empty after the strip — delete it instead of rewriting
//   - changed: phonehome was found and removed
//   - err:     parse error (only returned for malformed YAML)
func stripPhonehomeKey(data []byte) (out []byte, empty, changed bool, err error) {
	header, body := splitLeadingComments(data)

	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, false, false, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, false, false, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, false, false, nil
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == "phonehome" {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			changed = true
			break
		}
	}
	if !changed {
		return nil, false, false, nil
	}
	if len(root.Content) == 0 {
		return nil, true, true, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, false, false, err
	}
	_ = enc.Close()

	final := append([]byte{}, header...)
	final = append(final, buf.Bytes()...)
	return final, false, true, nil
}

// splitLeadingComments returns the run of `#`-prefixed (and blank) lines at
// the top of the file, with their trailing newlines, plus the remainder.
// A typical Kairos cloud-config starts with `#cloud-config\n` — the encoder
// won't round-trip that reliably, so we hold it aside manually and splice
// it back on write.
func splitLeadingComments(data []byte) (header, body []byte) {
	idx := 0
	for idx < len(data) {
		end := bytes.IndexByte(data[idx:], '\n')
		var line []byte
		if end < 0 {
			line = data[idx:]
		} else {
			line = data[idx : idx+end+1]
		}
		trim := bytes.TrimLeft(line, " \t")
		// Keep #-prefixed comments and blank lines at the top.
		if len(bytes.TrimSpace(trim)) == 0 || (len(trim) > 0 && trim[0] == '#') {
			idx += len(line)
			if end < 0 {
				break
			}
			continue
		}
		break
	}
	return data[:idx], data[idx:]
}
