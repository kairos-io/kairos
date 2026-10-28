package state

import (
	"context"
	"errors"
	"fmt"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("mountCustomBinds with a symlinked mountpoint", func() {
	// stubBindMount replaces the real mount(2), which needs root. refuse names
	// the one mountpoint that answers as a symlink; every other entry mounts.
	// It records the order it was called in, so a spec can tell "the entry was
	// skipped" apart from "the loop stopped at it".
	stubBindMount := func(refuse string, attempted *[]string) {
		old := bindMountFn
		bindMountFn = func(mountpoint, root, stateTarget string) (op.MountOperation, error) {
			*attempted = append(*attempted, mountpoint)
			operation := op.MountBind(mountpoint, root, stateTarget)
			if mountpoint == refuse {
				target := "/etc/pki/tls/certs"
				return operation, fmt.Errorf("%w: %s -> %s", cnst.ErrMountTargetIsSymlink, mountpoint, target)
			}
			return operation, nil
		}
		DeferCleanup(func() { bindMountFn = old })
	}

	// specs is the file names of the fstab entries the step collected.
	specs := func(s *State) []string {
		out := []string{}
		for _, f := range s.fstabs {
			out = append(out, f.File)
		}
		return out
	}

	It("mounts the other entries, writes no entry for the symlink, and returns nil", func() {
		var attempted []string
		stubBindMount("/etc/ssl/certs", &attempted)

		// Rootdir "/" is the UKI shape, where OpWriteFstab hard-depends on
		// this step, so an error here costs the machine its /etc/fstab.
		s := &State{
			Rootdir:    "/",
			StateDir:   "/usr/local/.state",
			BindMounts: []string{"/etc/ssl/certs", "/etc/systemd", "/var/lib/rancher"},
		}

		Expect(s.mountCustomBinds(context.Background())).To(Succeed())

		Expect(attempted).To(ConsistOf("/etc/ssl/certs", "/etc/systemd", "/var/lib/rancher"),
			"every entry has to be attempted, the refusal is not a stop")
		Expect(specs(s)).To(ConsistOf("/etc/systemd", "/var/lib/rancher"))
		Expect(specs(s)).NotTo(ContainElement("/etc/ssl/certs"),
			"a refused bind must not reach fstab, or it is retried after switch_root")
	})

	It("still reports an error that is not the symlink refusal", func() {
		var attempted []string
		old := bindMountFn
		bindMountFn = func(mountpoint, root, stateTarget string) (op.MountOperation, error) {
			attempted = append(attempted, mountpoint)
			operation := op.MountBind(mountpoint, root, stateTarget)
			if mountpoint == "/etc/systemd" {
				return operation, errors.New("no such device")
			}
			return operation, nil
		}
		DeferCleanup(func() { bindMountFn = old })

		s := &State{
			Rootdir:    "/",
			StateDir:   "/usr/local/.state",
			BindMounts: []string{"/etc/systemd", "/var/lib/rancher"},
		}

		err := s.mountCustomBinds(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no such device"))
		Expect(attempted).To(ConsistOf("/etc/systemd", "/var/lib/rancher"))
		Expect(specs(s)).To(ConsistOf("/var/lib/rancher"))
	})

	It("refuses nothing when no mountpoint is a symlink", func() {
		var attempted []string
		stubBindMount("/no/such/path", &attempted)

		s := &State{
			Rootdir:    "/",
			StateDir:   "/usr/local/.state",
			BindMounts: []string{"/etc/systemd", "/var/lib/rancher"},
		}

		Expect(s.mountCustomBinds(context.Background())).To(Succeed())
		Expect(specs(s)).To(ConsistOf("/etc/systemd", "/var/lib/rancher"))
	})
})
