package services

import (
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"

	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// k0sUserHome is the k0s data directory (constant.DataDirDefault upstream).
// `k0s install controller` gives it to every account it creates as the home
// directory, so we do the same.
const k0sUserHome = "/var/lib/k0s"

// K0sControllerUsers are the accounts that `k0s install controller` creates from
// the default spec.install.systemUsers. Upstream names five components (etcd,
// kine, konnectivity, kube-apiserver, kube-scheduler) but kine runs as the
// apiserver account so it can read the kine socket, which leaves four names.
//
// Without these accounts k0s still starts, but etcd/kine, kube-apiserver,
// kube-controller-manager and kube-scheduler fall back to root and their key
// material ends up owned by root.
var K0sControllerUsers = []string{"etcd", "konnectivity-server", "kube-apiserver", "kube-scheduler"}

// userManager creates system accounts. Everything it touches is behind a field so
// the tests can drive it without a real /etc/passwd.
type userManager struct {
	logger   loggerpkg.KairosLogger
	lookup   func(name string) (bool, error)
	lookPath func(file string) (string, error)
	run      func(name string, args ...string) ([]byte, error)
}

func newUserManager(logger loggerpkg.KairosLogger) *userManager {
	return &userManager{
		logger:   logger,
		lookup:   userExists,
		lookPath: exec.LookPath,
		run: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).CombinedOutput()
		},
	}
}

// userExists reports whether an account is already in the password database.
func userExists(name string) (bool, error) {
	_, err := user.Lookup(name)
	if err == nil {
		return true, nil
	}
	if errors.As(err, new(user.UnknownUserError)) {
		return false, nil
	}
	return false, err
}

// EnsureK0sControllerUsers creates the k0s controller accounts that are missing
// from the image. It is the part of `k0s install controller` we cannot get any
// other way: we write the unit files ourselves, so the native installer never
// runs and never creates them.
func EnsureK0sControllerUsers(logger loggerpkg.KairosLogger) error {
	return newUserManager(logger).ensure(K0sControllerUsers, k0sUserHome)
}

// ensure creates every missing account, reporting all failures rather than the
// first one.
func (m *userManager) ensure(names []string, home string) error {
	var (
		shell string
		errs  []error
	)

	for _, name := range names {
		exists, err := m.lookup(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("looking up user %q: %w", name, err))
			continue
		}
		if exists {
			m.logger.Logger.Debug().Str("user", name).Msg("k0s system user already exists")
			continue
		}

		if shell == "" {
			// Resolved lazily: an image where every account is already present
			// needs no shell, and should not fail for the lack of one.
			if shell, err = m.nologinShell(); err != nil {
				// Without a shell nothing below can succeed, so stop here
				// instead of repeating the same failure per account.
				errs = append(errs, err)
				break
			}
		}

		m.logger.Logger.Info().Str("user", name).Msg("Creating k0s system user")
		if err := m.createUser(name, home, shell); err != nil {
			errs = append(errs, fmt.Errorf("creating user %q: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

// nologinShell returns a shell that denies logins. Anything but /bin/false is
// cosmetic: these accounts exist to own processes and files, never to log in.
func (m *userManager) nologinShell() (string, error) {
	for _, candidate := range []string{"nologin", "false"} {
		if shell, err := m.lookPath(candidate); err == nil {
			return shell, nil
		}
	}
	return "", errors.New("no nologin shell found, cannot create the k0s system users")
}

// createUser adds a single system account.
//
// Three tools have to be covered. shadow-utils' useradd is on the Debian, RedHat,
// SUSE and Arch images. Alpine has neither useradd nor Debian's adduser, only
// busybox's applet of that name, which takes short options and rejects the long
// ones the other two want.
//
// No numeric UID is pinned. /etc/passwd ships inside the immutable image while
// /var/lib/k0s is persistent (PERSISTENT_STATE_PATHS in
// kairos-init/pkg/bundled/cloudconfigs/00_rootfs.yaml), so an account's UID can
// shift when the image is rebuilt with a different package set. k0s repairs
// that itself on every controller start: Etcd.Init calls recursiveChown
// (filepath.Walk + os.Chown) over EtcdDataDir and EtcdCertDir, and Kine.Init
// chowns its socket dir, db dir and db file (k0sproject/k0s
// pkg/component/controller/etcd.go at dddd0e5a). So a UID change self-heals,
// where pinning numbers here would trade that for a build that fails wherever
// the number is already taken.
func (m *userManager) createUser(name, home, shell string) error {
	if path, err := m.lookPath("useradd"); err == nil {
		return m.exec(path, "--home", home, "--shell", shell, "--system", "--no-create-home", name)
	}

	path, err := m.lookPath("adduser")
	if err != nil {
		return errors.New("neither useradd nor adduser is available")
	}

	// Which adduser this is cannot be told from its path. busybox installs its
	// applets as symlinks on Alpine but as hardlinks elsewhere, and the
	// multi-call binary is not always named "busybox" (busybox.static,
	// busybox-extras), so resolving the link and comparing the basename gets
	// it wrong on those and sends long options to a tool that rejects them.
	// Ask the tool instead: an adduser that refuses the flags creates nothing,
	// so retrying with the busybox short options is safe.
	longErr := m.exec(path, "--disabled-password", "--gecos", "", "--home", home, "--shell", shell, "--system", "--no-create-home", name)
	if longErr == nil {
		return nil
	}

	if shortErr := m.exec(path, "-h", home, "-s", shell, "-S", "-D", "-H", name); shortErr != nil {
		return errors.Join(
			fmt.Errorf("long options: %w", longErr),
			fmt.Errorf("busybox short options: %w", shortErr),
		)
	}

	return nil
}

func (m *userManager) exec(name string, args ...string) error {
	out, err := m.run(name, args...)
	if err != nil {
		return fmt.Errorf("%s: %w: %s", filepath.Base(name), err, string(out))
	}
	return nil
}
