package services

import (
	"errors"
	"fmt"
	"os/exec"

	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type recordedCommand struct {
	name string
	args []string
}

// managerFixture builds a userManager whose whole environment is scripted:
// which accounts exist, which binaries are on PATH, which of them are busybox
// symlinks, and what every invocation returns.
type managerFixture struct {
	existing  map[string]bool
	lookupErr map[string]error
	onPath    map[string]string
	busybox   map[string]bool
	runErr    error
	calls     []recordedCommand
}

func (f *managerFixture) manager() *userManager {
	return &userManager{
		logger: loggerpkg.NewKairosLogger("test", "fatal", true),
		lookup: func(name string) (bool, error) {
			if err, ok := f.lookupErr[name]; ok {
				return false, err
			}
			return f.existing[name], nil
		},
		lookPath: func(file string) (string, error) {
			if path, ok := f.onPath[file]; ok {
				return path, nil
			}
			return "", exec.ErrNotFound
		},
		evalLink: func(path string) (string, error) {
			if f.busybox[path] {
				return "/bin/busybox", nil
			}
			return path, nil
		},
		run: func(name string, args ...string) ([]byte, error) {
			f.calls = append(f.calls, recordedCommand{name: name, args: args})
			if f.runErr != nil {
				return []byte("boom"), f.runErr
			}
			return nil, nil
		},
	}
}

// withUseradd is the shadow-utils environment: Debian, RedHat, SUSE and Arch.
func withUseradd() *managerFixture {
	return &managerFixture{
		existing: map[string]bool{},
		onPath:   map[string]string{"useradd": "/usr/sbin/useradd", "nologin": "/usr/sbin/nologin"},
		busybox:  map[string]bool{},
	}
}

func (f *managerFixture) createdUsers() []string {
	var names []string
	for _, call := range f.calls {
		names = append(names, call.args[len(call.args)-1])
	}
	return names
}

var _ = Describe("k0s system users", func() {
	Describe("the account list", func() {
		It("matches the k0s defaults, with kine folded into the apiserver account", func() {
			// Upstream getControllerUserNames sorts and compacts
			// {Etcd, Kine, Konnectivity, KubeAPIServer, KubeScheduler}, and
			// KineUser is defined as kube-apiserver so the apiserver can read
			// the kine socket. Four names survive.
			Expect(K0sControllerUsers).To(Equal([]string{
				"etcd", "konnectivity-server", "kube-apiserver", "kube-scheduler",
			}))
		})
	})

	Describe("choosing which accounts to create", func() {
		It("creates every account on an image that has none of them", func() {
			f := withUseradd()
			Expect(f.manager().ensure(K0sControllerUsers, k0sUserHome)).To(Succeed())
			Expect(f.createdUsers()).To(Equal(K0sControllerUsers))
		})

		It("leaves accounts that already exist alone", func() {
			f := withUseradd()
			f.existing["etcd"] = true
			f.existing["kube-scheduler"] = true

			Expect(f.manager().ensure(K0sControllerUsers, k0sUserHome)).To(Succeed())
			Expect(f.createdUsers()).To(Equal([]string{"konnectivity-server", "kube-apiserver"}))
		})

		It("runs nothing at all when the image already has the accounts", func() {
			f := withUseradd()
			for _, name := range K0sControllerUsers {
				f.existing[name] = true
			}
			// No shell either: an image that needs no account must not fail
			// for the lack of a nologin shell.
			delete(f.onPath, "nologin")

			Expect(f.manager().ensure(K0sControllerUsers, k0sUserHome)).To(Succeed())
			Expect(f.calls).To(BeEmpty())
		})

		It("keeps going after a failure and reports every account that failed", func() {
			f := withUseradd()
			f.runErr = errors.New("exit status 1")

			err := f.manager().ensure(K0sControllerUsers, k0sUserHome)
			Expect(err).To(HaveOccurred())
			for _, name := range K0sControllerUsers {
				Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("creating user %q", name)))
			}
			Expect(err.Error()).To(ContainSubstring("boom"), "the tool output belongs in the error")
		})

		It("reports a password database that cannot be read", func() {
			f := withUseradd()
			f.lookupErr = map[string]error{"etcd": errors.New("permission denied")}

			err := f.manager().ensure(K0sControllerUsers, k0sUserHome)
			Expect(err).To(MatchError(ContainSubstring(`looking up user "etcd"`)))
			Expect(f.createdUsers()).To(Equal([]string{"konnectivity-server", "kube-apiserver", "kube-scheduler"}))
		})
	})

	Describe("choosing the tool", func() {
		It("passes shadow-utils long options to useradd", func() {
			f := withUseradd()
			Expect(f.manager().ensure([]string{"etcd"}, k0sUserHome)).To(Succeed())
			Expect(f.calls).To(HaveLen(1))
			Expect(f.calls[0].name).To(Equal("/usr/sbin/useradd"))
			Expect(f.calls[0].args).To(Equal([]string{
				"--home", "/var/lib/k0s", "--shell", "/usr/sbin/nologin",
				"--system", "--no-create-home", "etcd",
			}))
		})

		It("falls back to the Debian adduser when useradd is absent", func() {
			f := withUseradd()
			delete(f.onPath, "useradd")
			f.onPath["adduser"] = "/usr/sbin/adduser"

			Expect(f.manager().ensure([]string{"etcd"}, k0sUserHome)).To(Succeed())
			Expect(f.calls[0].name).To(Equal("/usr/sbin/adduser"))
			Expect(f.calls[0].args).To(Equal([]string{
				"--disabled-password", "--gecos", "", "--home", "/var/lib/k0s",
				"--shell", "/usr/sbin/nologin", "--system", "--no-create-home", "etcd",
			}))
		})

		It("uses short options when adduser is a busybox applet", func() {
			// Alpine ships neither shadow-utils nor Debian's adduser. Its
			// busybox applet rejects the long options both of those take, so
			// sending them would fail the image build on every Alpine flavor.
			f := withUseradd()
			delete(f.onPath, "useradd")
			f.onPath["adduser"] = "/usr/sbin/adduser"
			f.busybox["/usr/sbin/adduser"] = true

			Expect(f.manager().ensure([]string{"etcd"}, k0sUserHome)).To(Succeed())
			Expect(f.calls[0].args).To(Equal([]string{
				"-h", "/var/lib/k0s", "-s", "/usr/sbin/nologin", "-S", "-D", "-H", "etcd",
			}))
		})

		It("fails when the image has no tool to create users with", func() {
			f := withUseradd()
			delete(f.onPath, "useradd")

			err := f.manager().ensure([]string{"etcd"}, k0sUserHome)
			Expect(err).To(MatchError(ContainSubstring("neither useradd nor adduser is available")))
			Expect(f.calls).To(BeEmpty())
		})
	})

	Describe("choosing the shell", func() {
		It("prefers nologin over false", func() {
			f := withUseradd()
			f.onPath["false"] = "/bin/false"

			shell, err := f.manager().nologinShell()
			Expect(err).ToNot(HaveOccurred())
			Expect(shell).To(Equal("/usr/sbin/nologin"))
		})

		It("takes false when nologin is missing", func() {
			f := withUseradd()
			delete(f.onPath, "nologin")
			f.onPath["false"] = "/bin/false"

			shell, err := f.manager().nologinShell()
			Expect(err).ToNot(HaveOccurred())
			Expect(shell).To(Equal("/bin/false"))
		})

		It("gives up once rather than per account when no shell exists", func() {
			f := withUseradd()
			delete(f.onPath, "nologin")

			err := f.manager().ensure(K0sControllerUsers, k0sUserHome)
			Expect(err).To(MatchError(ContainSubstring("no nologin shell found")))
			Expect(err.Error()).ToNot(ContainSubstring("\n"), "the failure is reported once, not per account")
			Expect(f.calls).To(BeEmpty())
		})
	})
})
