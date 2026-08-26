package debugbundle

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// defaultOutputDir is tmpfs on live media and is NOT globbed by `kairos-agent
// logs`, so writing the bundle here avoids recursion.
const defaultOutputDir = "/run/kairos"

// LogDir is where the installer writes its logs and the collected system
// extras; `kairos-agent logs` globs *.log here into the bundle.
const LogDir = "/var/log/kairos"

// Log files the installer writes under LogDir (globbed by `kairos-agent logs`
// and listed in the stdlib fallback tarball).
const (
	// InstallerLog is the installer's own structured log.
	InstallerLog = LogDir + "/installer.log"
	// AgentOutputLog is the full agent transcript (raw stdout + stderr)
	// captured during an install.
	AgentOutputLog = LogDir + "/agent-output.log"
)

// GenerateBundle collects the system extras into LogDir, then builds the bundle
// tarball (via `kairos-agent logs`, or the stdlib fallback) and returns its
// path. It does NOT start the HTTP server — it is the headless path used by the
// `--collect-debug-bundle` CLI flag and reused by the TUI's build step.
func GenerateBundle(agentBin string, ctx Context, now time.Time) (string, error) {
	extras, _ := CollectExtras(ExecRunner{}, ctx, LogDir)
	out := OutputPath(now)
	files := append([]string{InstallerLog, AgentOutputLog}, extras...)
	if err := Generate(agentBin, out, files); err != nil {
		return "", err
	}
	return out, nil
}

// OutputPath returns a timestamped bundle path under /run/kairos, falling back
// to the OS temp dir when /run/kairos is not writable.
func OutputPath(now time.Time) string {
	name := fmt.Sprintf("kairos-logs-%s.tar.gz", now.Format("20060102-150405"))
	if err := os.MkdirAll(defaultOutputDir, 0o755); err == nil {
		if f, err := os.CreateTemp(defaultOutputDir, ".writable"); err == nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return filepath.Join(defaultOutputDir, name)
		}
	}
	return filepath.Join(os.TempDir(), name)
}

// Generate builds the bundle at outputPath. When agentBin is non-empty it runs
// `kairos-agent logs --output <outputPath>` (which globs /var/log/kairos and
// gathers journald). If agentBin is empty, or that command fails, it falls back
// to a stdlib tarball of files.
func Generate(agentBin, outputPath string, files []string) error {
	if agentBin != "" {
		if err := exec.Command(agentBin, "logs", "--output", outputPath).Run(); err == nil {
			return nil
		}
	}
	return buildTarball(outputPath, files)
}

func buildTarball(outputPath string, files []string) error {
	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for _, f := range files {
		// best-effort: skip unreadable files rather than failing the bundle.
		_ = addFile(tw, f)
	}
	return nil
}

func addFile(tw *tar.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    filepath.Base(path),
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}
