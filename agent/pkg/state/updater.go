package state

import (
	"time"

	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
)

// BackupSuffix is appended to a corrupt state file before writing a fresh one.
const BackupSuffix = ".bak"

// Clock returns the current time. Kept as a function value so tests can inject
// a deterministic clock.
type Clock func() time.Time

// loadOrRecover loads the state from path. If the file exists but is
// unparseable it is renamed to <path>.bak (overwriting any prior backup) and a
// fresh Default() is returned. IO errors that are not parse failures are
// returned as-is.
func loadOrRecover(fs sdkFs.KairosFS, path string) (*KairosState, error) {
	s, err := Load(fs, path)
	if err == nil {
		return s, nil
	}
	if _, statErr := fs.Stat(path); statErr == nil {
		_ = fs.Rename(path, path+BackupSuffix)
		return Default(), nil
	}
	return nil, err
}

func stamp(now Clock) string {
	return now().UTC().Format(time.RFC3339)
}

// RecordInstall marks the machine's initial installation. Only the
// first-install fields are set; the upgrade fields stay at their defaults.
func RecordInstall(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.FirstInstall = stamp(now)
	s.FirstInstallSource = source
	return s.Save(fs, path)
}

// RecordActiveUpgrade records a new active image upgrade.
func RecordActiveUpgrade(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastActiveUpgrade = stamp(now)
	s.LastActiveSource = source
	return s.Save(fs, path)
}

// RecordRecoveryUpgrade records a new recovery image install.
func RecordRecoveryUpgrade(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastRecoveryUpgrade = stamp(now)
	s.LastRecoverySource = source
	return s.Save(fs, path)
}

// RecordReset marks a factory reset. FirstInstall is left untouched.
func RecordReset(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastReset = stamp(now)
	s.LastResetSource = source
	return s.Save(fs, path)
}
