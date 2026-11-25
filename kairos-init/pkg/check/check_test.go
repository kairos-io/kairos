package check

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
)

// distro describes a made-up root filesystem: the os-release and the files in it.
type distro struct {
	id, idLike, version, pretty string
	// files are executables in /usr/bin, or absolute paths of empty files.
	files []string
}

// build writes the root filesystem and gives its directory.
func build(t *testing.T, d distro) string {
	t.Helper()
	root := t.TempDir()
	if d.id != "" || d.pretty != "" {
		osRelease := "PRETTY_NAME=\"" + d.pretty + "\"\nID=" + d.id + "\nID_LIKE=\"" + d.idLike + "\"\nVERSION_ID=\"" + d.version + "\"\n"
		write(t, filepath.Join(root, "etc/os-release"), osRelease, 0o644)
	}
	for _, f := range d.files {
		if !strings.HasPrefix(f, "/") {
			f = "/usr/bin/" + f
		}
		write(t, filepath.Join(root, f), "", 0o755)
	}
	return root
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// output runs the check and gives the report as printed.
func output(t *testing.T, root string, opts Options) (*Report, string) {
	t.Helper()
	opts.Root = root
	if opts.Arch == "" {
		opts.Arch = "x86_64"
	}
	r, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r.Print(&out, false)
	return r, out.String()
}

// Base container images have nothing but the package manager installed.
var (
	ubuntu = func(version string) distro {
		return distro{"ubuntu", "debian", version, "Ubuntu " + version, []string{"apt-get"}}
	}
	debian = func(version string) distro {
		return distro{"debian", "", version, "Debian GNU/Linux " + version, []string{"apt-get"}}
	}
	alpine = distro{"alpine", "", "3.23.8", "Alpine Linux v3.23", []string{"apk"}}
	leap   = func(version string) distro {
		return distro{"opensuse-leap", "suse opensuse", version, "openSUSE Leap " + version, []string{"zypper"}}
	}
)

func TestVerdicts(t *testing.T) {
	tests := []struct {
		name  string
		d     distro
		opts  Options
		arch  string
		ok    bool
		wants []string
		// nots are texts the output must not hold.
		nots []string
	}{
		{name: "ubuntu", d: ubuntu("24.04"), ok: true, wants: []string{"this system can become"}},
		{name: "debian", d: debian("13"), ok: true},
		{name: "fedora", d: distro{"fedora", "", "43", "Fedora Linux 43 (Container Image)", []string{"dnf"}}, ok: true},
		{name: "alpine", d: alpine, ok: true},
		{name: "alpine with Trusted Boot", d: alpine, opts: Options{TrustedBoot: true}, wants: []string{"Trusted Boot needs systemd"}},
		{name: "alpine with the busybox init", d: distro{"alpine", "", "3.23.8", "Alpine Linux v3.23", []string{"apk", "/sbin/init", "/etc/inittab"}}, ok: true},
		{name: "alpine edge", d: distro{"alpine", "", "3.24.0_alpha20261001", "Alpine Linux edge", []string{"apk"}}, ok: true},
		{name: "opensuse leap", d: leap("16.0"), ok: true},
		{name: "SLES without all packages in its public repositories", d: distro{"sles", "suse", "15.7", "SUSE Linux Enterprise Server 15 SP7", []string{"zypper"}}, ok: true, wants: []string{"warn  packages: SLES 15.7", "subscription"}},
		{name: "SLES through ID_LIKE=suse", d: distro{"sles", "suse", "16.0", "SUSE Linux Enterprise Server 16.0", []string{"zypper"}}, ok: true, wants: []string{"need a subscription"}},
		{name: "rhel", d: distro{"rhel", "fedora", "9.6", "Red Hat Enterprise Linux 9.6 (Plow)", []string{"dnf"}}, ok: true, wants: []string{"need a subscription"}},
		{name: "debian without all packages", d: debian("11"), wants: []string{"Debian 12 or newer"}},
		{name: "leap without all packages", d: leap("15.6"), wants: []string{"Leap 16 or newer"}},

		// Derivatives.
		{name: "centos, a derivative both tools know", d: distro{"centos", "rhel fedora", "10", "CentOS Stream 10 (Coughlan)", []string{"dnf"}}, ok: true, wants: []string{"as a derivative of rhel", "a UEFI install fails"}},
		{name: "debian derivative", d: distro{"kali", "debian", "2026.3", "Kali GNU/Linux Rolling", []string{"apt-get"}}, wants: []string{"unknown package manager", "builds no initramfs", "The debian family can."}},
		{name: "ubuntu derivative with its own versions", d: distro{"linuxmint", "ubuntu debian", "22", "Linux Mint 22", []string{"apt-get"}}, wants: []string{"kairos-init has none for Ubuntu 22"}},
		{name: "derivative that keeps the ID of its parent", d: distro{"ubuntu", "debian", "24.04", "Derivative Linux", []string{"apt-get"}}, wants: []string{"builds no initramfs"}},
		{name: "derivative that keeps the names of its parent", d: distro{"ubuntu", "debian", "24.04", "Ubuntu 24.04 (Derivative)", []string{"apt-get"}}, ok: true},
		{name: "another init system", d: distro{"devuan", "debian", "5", "Debian based", []string{"apt-get", "/sbin/init"}}, wants: []string{"neither systemd nor OpenRC"}},
		{name: "ID_LIKE=redhat", d: distro{"mylinux", "redhat", "1.0", "Fedora based", []string{"dnf"}}, wants: []string{"family: redhat, as a derivative of fedora"}},
		{name: "ID_LIKE=suse", d: distro{"mylinux", "suse", "1.0", "SUSE based", []string{"zypper"}}, ok: true, wants: []string{"as a derivative of opensuse-leap"}},
		{name: "ID_LIKE with a pattern", d: distro{"mylinux", "*", "1.0", "Something", nil}, wants: []string{"nor ID_LIKE=*"}},

		// Package managers that yip knows.
		{name: "yip knows dnf for centos", d: distro{"centos", "rhel", "1.0", "none", []string{"dnf"}}, wants: []string{"packages: dnf"}},
		{name: "yip knows dnf for openEuler", d: distro{"openEuler", "fedora", "1.0", "none", []string{"dnf"}}, wants: []string{"packages: dnf"}},
		{name: "yip knows zypper for suse", d: distro{"suse", "sles", "16.0", "none", []string{"zypper"}}, wants: []string{"packages: zypper"}},
		{name: "rocky 8 has an old systemd", d: distro{"rocky", "rhel centos fedora", "8.10", "Rocky Linux 8.10", []string{"dnf"}}, ok: true, wants: []string{"systemd-cryptenroll is missing"}},
		{name: "rocky 8 with Trusted Boot", d: distro{"rocky", "rhel centos fedora", "8.10", "Rocky Linux 8.10", []string{"dnf"}}, opts: Options{TrustedBoot: true}, wants: []string{"systemd-cryptenroll is missing"}},
		{name: "no package manager that yip knows", d: distro{"almalinux", "rhel centos fedora", "9.6", "AlmaLinux 9.6", []string{"microdnf"}}, wants: []string{"dnf is missing"}, nots: []string{"family can"}},
		{name: "no package manager", d: distro{"ubuntu", "debian", "24.04", "Ubuntu 24.04", nil}, wants: []string{"apt-get is missing"}},

		// Ubuntu releases.
		{name: "ubuntu 20.04", d: ubuntu("20.04"), ok: true},
		{name: "ubuntu 22.04", d: ubuntu("22.04"), ok: true},
		{name: "ubuntu 24.10", d: ubuntu("24.10"), ok: true},
		{name: "ubuntu 25.10", d: ubuntu("25.10"), ok: true},
		{name: "ubuntu 26.04", d: ubuntu("26.04"), ok: true},
		{name: "ubuntu 26.10", d: ubuntu("26.10"), ok: true},
		{name: "ubuntu 28.04", d: ubuntu("28.04"), ok: true},
		{name: "ubuntu 18.04", d: ubuntu("18.04"), wants: []string{"kairos-init has none for Ubuntu"}},
		{name: "ubuntu 23.10", d: ubuntu("23.10"), wants: []string{"kairos-init has none for Ubuntu"}},
		{name: "ubuntu 26.03", d: ubuntu("26.03"), wants: []string{"kairos-init has none for Ubuntu"}},
		{name: "ubuntu 22.04.5", d: ubuntu("22.04.5"), wants: []string{"kairos-init has none for Ubuntu"}},
		{name: "ubuntu v24.04", d: distro{"ubuntu", "debian", "v24.04", "Ubuntu", []string{"apt-get"}}, wants: []string{"kairos-init has none"}},
		{name: "ubuntu 26.04-beta", d: distro{"ubuntu", "debian", "26.04-beta", "Ubuntu", []string{"apt-get"}}, wants: []string{"kairos-init has none"}},
		{name: "ubuntu on riscv64, whose kernel list has no release", d: ubuntu("23.10"), arch: "riscv64", ok: true},
		{name: "ubuntu 24.04 with Trusted Boot", d: ubuntu("24.04"), opts: Options{TrustedBoot: true}, ok: true},
		{name: "ubuntu 25.10 with Trusted Boot", d: ubuntu("25.10"), opts: Options{TrustedBoot: true}, ok: true},
		{name: "ubuntu 26.04 with Trusted Boot", d: ubuntu("26.04"), opts: Options{TrustedBoot: true}, ok: true},
		{name: "ubuntu 22.04 with Trusted Boot", d: ubuntu("22.04"), opts: Options{TrustedBoot: true}, wants: []string{"kairos-init has none"}},
		{name: "ubuntu 20.04 with Trusted Boot", d: ubuntu("20.04"), opts: Options{TrustedBoot: true}, wants: []string{"kairos-init has none"}},

		// Systems no list covers.
		{name: "arch", d: distro{"arch", "", "20261122.0.629543", "Arch Linux", []string{"pacman"}}, wants: []string{"no package list for the Arch family"}},
		{name: "gentoo", d: distro{"gentoo", "", "2.18", "Gentoo Linux", nil}, wants: []string{"kairos-init knows neither"}},
		{name: "void", d: distro{"void", "", "", "Void Linux", nil}, wants: []string{"builds no initramfs"}},
		{name: "no VERSION_ID", d: distro{"debian", "", "", "Debian GNU/Linux forky/sid", []string{"apt-get"}}, wants: []string{"is not a version"}},
		{name: "no os-release", d: distro{}, wants: []string{"has no ID"}},

		// Platform.
		{name: "both init systems", d: distro{"alpine", "", "3.23.8", "Alpine Linux v3.23", []string{"apk", "systemctl", "openrc"}}, wants: []string{"yip cannot choose"}},
		{name: "architecture Kairos is not built for", d: ubuntu("24.04"), arch: "armv7l", wants: []string{"architecture: armv7l"}},
		{name: "aarch64", d: ubuntu("24.04"), arch: "aarch64", ok: true, wants: []string{"architecture: aarch64"}},
		{name: "riscv64", d: ubuntu("24.04"), arch: "riscv64", ok: true, wants: []string{"architecture: riscv64"}},
		{name: "no path to shim and GRUB", d: alpine, ok: true, wants: []string{"It cannot be installed on a UEFI machine."}},
		{name: "no path to shim and GRUB, and no BIOS", d: alpine, arch: "aarch64", ok: true, wants: []string{"no BIOS"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Arch = tt.arch
			r, out := output(t, build(t, tt.d), tt.opts)
			if r.OK() != tt.ok {
				t.Errorf("OK is %v, want %v:\n%s", r.OK(), tt.ok, out)
			}
			for _, want := range tt.wants {
				if !strings.Contains(out, want) {
					t.Errorf("the output does not hold %q:\n%s", want, out)
				}
			}
			for _, not := range tt.nots {
				if strings.Contains(out, not) {
					t.Errorf("the output holds %q:\n%s", not, out)
				}
			}
		})
	}
}

// Every distribution kairos-init knows is a known family here, with the same
// family: the check asks kairos-init and has no list of its own.
func TestKnownDistributions(t *testing.T) {
	for id, family := range map[string]values.Family{
		"debian": values.DebianFamily, "ubuntu": values.DebianFamily, "fedora": values.RedHatFamily,
		"rocky": values.RedHatFamily, "almalinux": values.RedHatFamily, "ol": values.RedHatFamily,
		"rhel": values.RedHatFamily, "arch": values.ArchFamily, "alpine": values.AlpineFamily,
		"opensuse-leap": values.SUSEFamily, "opensuse-tumbleweed": values.SUSEFamily, "sles": values.SUSEFamily,
		"sle-micro-rancher": values.SUSEFamily, "hadron": values.HadronFamily,
	} {
		_, out := output(t, build(t, distro{id, "", "1.0", "none", nil}), Options{})
		if want := "family: " + family.String() + " (ID=" + id + ")"; !strings.Contains(out, want) {
			t.Errorf("%s: the output does not hold %q:\n%s", id, want, out)
		}
	}
}

// The package manager is the one yip's package step picks.
func TestPackageManagers(t *testing.T) {
	for pair, want := range map[string]string{
		"debian": "apt-get", "ubuntu": "apt-get", "fedora": "dnf", "rocky": "dnf", "ol": "dnf",
		"almalinux": "dnf", "rhel": "dnf", "alpine": "apk", "opensuse-leap": "zypper", "opensuse-tumbleweed": "zypper",
	} {
		root := build(t, distro{id: pair, version: "1.0", pretty: "none"})
		if got := packageManager(newRootFS(root)); got != want {
			t.Errorf("%s: package manager %q, want %q", pair, got, want)
		}
	}
	if got := packageManager(newRootFS(build(t, distro{id: "kali", idLike: "debian", version: "1", pretty: "Kali"}))); got != "" {
		t.Errorf("kali: package manager %q, want none", got)
	}
}

// completeSystem is a system that has everything: nothing brings what is
// missing after kairos-init.
func completeSystem(t *testing.T) string {
	t.Helper()
	commands := strings.Fields(`bash gawk awk sync lvm dracut lsinitrd grub-install grub-editenv mount umount blkid lsblk
		findmnt udevadm depmod modprobe rsync tar cryptsetup e2fsck tune2fs mkfs.ext2 mkfs.ext3 mkfs.ext4
		mkfs.vfat mksquashfs dmsetup sudo less mount.nfs sshd curl NetworkManager systemctl
		systemd-cryptenroll systemd-sysext`)
	files := append(commands, "/boot/vmlinuz", "/boot/initrd",
		"/usr/lib/systemd/systemd-cryptsetup", "/usr/lib/grub/i386-pc/modinfo.sh",
		"/usr/lib/shim/shimx64.efi.signed", "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed")
	for _, m := range []string{"loopback", "squash4", "xzio", "gzio", "regexp"} {
		files = append(files, "/usr/lib/grub/x86_64-efi/"+m+".mod")
	}
	root := build(t, distro{"ubuntu", "debian", "24.04", "Ubuntu 24.04", files})
	modules := filepath.Join(root, "lib/modules/6.8.0")
	write(t, filepath.Join(modules, "modules.builtin"), "", 0o644)
	dep := ""
	for _, m := range []string{"overlay", "squashfs", "loop", "ext4", "vfat", "isofs", "dm_mod", "dm_crypt", "dm_snapshot", "tpm_crb", "tpm_tis", "dm_multipath"} {
		dep += "kernel/" + m + ".ko.zst:\n"
	}
	write(t, filepath.Join(modules, "modules.dep"), dep, 0o644)
	return root
}

func TestCompleteSystem(t *testing.T) {
	for _, opts := range []Options{{Converted: true}, {Converted: true, TrustedBoot: true}} {
		r, out := output(t, completeSystem(t), opts)
		// A Trusted Boot check of another root cannot read the systemd version.
		if !r.OK() || (!opts.TrustedBoot && r.Warns() != 0) || !strings.Contains(out, "has what Kairos needs") {
			t.Errorf("%+v: not complete:\n%s", opts, out)
		}
	}
}

// Each thing a system has is checked: without it there is a fail, or a warn.
func TestWithout(t *testing.T) {
	type removal struct {
		path string
		fail bool
		opts Options
	}
	var removals []removal
	for _, c := range strings.Fields(`bash gawk awk sync lvm dracut grub-editenv mount umount blkid lsblk findmnt udevadm depmod
		modprobe rsync tar cryptsetup e2fsck tune2fs mkfs.ext2 mkfs.ext3 mkfs.ext4 mkfs.vfat sudo less mount.nfs systemctl`) {
		removals = append(removals, removal{path: "/usr/bin/" + c, fail: true})
	}
	for _, c := range strings.Fields("lsinitrd grub-install mksquashfs dmsetup sshd curl NetworkManager systemd-cryptenroll systemd-sysext") {
		removals = append(removals, removal{path: "/usr/bin/" + c})
	}
	for _, p := range []string{"/boot/vmlinuz", "/boot/initrd", "/lib/modules"} {
		removals = append(removals, removal{path: p, fail: true})
	}
	for _, p := range []string{"/usr/lib/grub/i386-pc/modinfo.sh", "/usr/lib/grub/x86_64-efi/squash4.mod",
		"/usr/lib/shim/shimx64.efi.signed", "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed",
		"/usr/lib/systemd/systemd-cryptsetup"} {
		removals = append(removals, removal{path: p})
	}
	for _, p := range []string{"/usr/bin/systemd-cryptenroll", "/usr/lib/systemd/systemd-cryptsetup", "/usr/bin/mkfs.vfat"} {
		removals = append(removals, removal{path: p, fail: true, opts: Options{TrustedBoot: true}})
	}
	for _, r := range removals {
		t.Run(r.path, func(t *testing.T) {
			root := completeSystem(t)
			if err := os.RemoveAll(filepath.Join(root, r.path)); err != nil {
				t.Fatal(err)
			}
			r.opts.Converted = true
			rep, out := output(t, root, r.opts)
			status := "  warn  "
			if r.fail {
				status = "  fail  "
			}
			if rep.OK() == r.fail || !strings.Contains(out, status) {
				t.Errorf("without %s: OK is %v, want a line %q:\n%s", r.path, rep.OK(), status, out)
			}
		})
	}

	root := completeSystem(t)
	for _, p := range []string{"/boot/initrd", "/usr/bin/dracut", "/usr/bin/grub-editenv", "/usr/lib/shim"} {
		_ = os.RemoveAll(filepath.Join(root, p))
	}
	if r, out := output(t, root, Options{Converted: true, TrustedBoot: true}); !r.OK() {
		t.Errorf("Trusted Boot does not need an initramfs tool, GRUB or shim:\n%s", out)
	}
}

// Something that is there but cannot be used is not there.
func TestUnusable(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(t *testing.T, root string)
		want   string
	}{
		{"command that is not executable", func(t *testing.T, root string) {
			_ = os.Chmod(filepath.Join(root, "usr/bin/rsync"), 0o644)
		}, "rsync is missing"},
		{"directory with the name of a command", func(t *testing.T, root string) {
			p := filepath.Join(root, "usr/bin/rsync")
			_ = os.Remove(p)
			_ = os.Mkdir(p, 0o755)
		}, "rsync is missing"},
		{"kernel link to nowhere", func(t *testing.T, root string) {
			p := filepath.Join(root, "boot/vmlinuz")
			_ = os.Remove(p)
			_ = os.Symlink("/nowhere", p)
		}, "/boot/vmlinuz is missing"},
		{"command in /usr/local", func(t *testing.T, root string) {
			_ = os.MkdirAll(filepath.Join(root, "usr/local/bin"), 0o755)
			_ = os.Rename(filepath.Join(root, "usr/bin/rsync"), filepath.Join(root, "usr/local/bin/rsync"))
		}, "rsync is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := completeSystem(t)
			tt.break_(t, root)
			r, out := output(t, root, Options{Converted: true})
			if r.OK() || !strings.Contains(out, tt.want) {
				t.Errorf("OK is %v, want a fail with %q:\n%s", r.OK(), tt.want, out)
			}
		})
	}
}

// Links are followed inside the root, and never out of it.
func TestLinks(t *testing.T) {
	t.Run("absolute links inside the root", func(t *testing.T) {
		root := completeSystem(t)
		must(t, os.Rename(filepath.Join(root, "usr/bin"), filepath.Join(root, "usr/programs")))
		must(t, os.Symlink("/usr/programs", filepath.Join(root, "usr/bin")))
		must(t, os.Rename(filepath.Join(root, "etc/os-release"), filepath.Join(root, "usr/lib/os-release")))
		must(t, os.Symlink("/usr/lib/os-release", filepath.Join(root, "etc/os-release")))
		if r, out := output(t, root, Options{Converted: true}); !r.OK() {
			t.Errorf("a link into the root has to work:\n%s", out)
		}
	})
	t.Run("link to a file of the host", func(t *testing.T) {
		root := build(t, distro{})
		must(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
		must(t, os.Symlink("/etc/hostname", filepath.Join(root, "etc/os-release")))
		if r, out := output(t, root, Options{}); r.OK() || !strings.Contains(out, "has no ID") {
			t.Errorf("a link out of the root has to count as nothing:\n%s", out)
		}
	})
	t.Run("os-release in /usr/lib only", func(t *testing.T) {
		root := build(t, ubuntu("24.04"))
		must(t, os.MkdirAll(filepath.Join(root, "usr/lib"), 0o755))
		must(t, os.Rename(filepath.Join(root, "etc/os-release"), filepath.Join(root, "usr/lib/os-release")))
		if r, out := output(t, root, Options{}); r.OK() || !strings.Contains(out, "has no ID") {
			t.Errorf("the Kairos tools read /etc/os-release only:\n%s", out)
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Hadron installs no package, and has no mount.nfs.
func TestHadron(t *testing.T) {
	r, out := output(t, build(t, distro{"hadron", "", "v0.6.3", "Hadron Linux", nil}), Options{})
	if r.OK() || !strings.Contains(out, "rsync is missing") {
		t.Errorf("Hadron without its tools:\n%s", out)
	}
	root := completeSystem(t)
	write(t, filepath.Join(root, "etc/os-release"), "PRETTY_NAME=\"Hadron\"\nID=hadron\nVERSION_ID=\"main\"\n", 0o644)
	must(t, os.Remove(filepath.Join(root, "usr/bin/mount.nfs")))
	if r, out := output(t, root, Options{Converted: true}); !r.OK() {
		t.Errorf("Hadron does not need mount.nfs:\n%s", out)
	}
}

func TestQuiet(t *testing.T) {
	r, err := Run(Options{Root: completeSystem(t), Arch: "x86_64", Converted: true})
	must(t, err)
	var out bytes.Buffer
	r.Print(&out, true)
	if !strings.Contains(out.String(), "Result:") || strings.Contains(out.String(), "  pass  ") {
		t.Errorf("quiet output:\n%s", out.String())
	}
}

// Kernel modules: a module, or built in, with either spelling.
func TestModules(t *testing.T) {
	root := build(t, ubuntu("24.04"))
	modules := filepath.Join(root, "lib/modules/6.8.0")
	dep := ""
	for _, m := range []string{"overlay", "squashfs", "loop", "ext4", "isofs", "dm-mod"} {
		dep += "kernel/" + m + ".ko.zst:\n"
	}
	write(t, filepath.Join(modules, "modules.dep"), dep, 0o644)
	write(t, filepath.Join(modules, "modules.builtin"), "kernel/fs/fat/vfat.ko\n", 0o644)

	r, out := output(t, root, Options{})
	if !r.OK() || !strings.Contains(out, "module dm_mod") {
		t.Errorf("kernel with the modules:\n%s", out)
	}
	r, out = output(t, root, Options{Converted: true})
	if r.OK() || !strings.Contains(out, "  fail  rsync") || !strings.Contains(out, "module dm_snapshot is missing") {
		t.Errorf("a system where nothing brings the rest:\n%s", out)
	}
	write(t, filepath.Join(modules, "modules.dep"), strings.ReplaceAll(dep, "kernel/squashfs.ko.zst:\n", ""), 0o644)
	r, out = output(t, root, Options{Converted: true})
	if r.OK() || !strings.Contains(out, "fail  module squashfs is missing") {
		t.Errorf("kernel without a required module:\n%s", out)
	}
	r, out = output(t, root, Options{})
	if !r.OK() || !strings.Contains(out, "module squashfs (install media and recovery image): not installed yet") {
		t.Errorf("base image with a small kernel:\n%s", out)
	}
	// 6.10 is newer than 6.8.
	write(t, filepath.Join(root, "lib/modules/6.10.0/modules.builtin"), "", 0o644)
	if _, out = output(t, root, Options{}); !strings.Contains(out, "kernel: 6.10.0") {
		t.Errorf("the newest kernel:\n%s", out)
	}
}

// The check changes nothing.
func TestChangesNothing(t *testing.T) {
	root := completeSystem(t)
	snapshot := func() string {
		var out []string
		must(t, filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			out = append(out, p+" "+info.Mode().String()+" "+info.ModTime().String())
			return nil
		}))
		return strings.Join(out, "\n")
	}
	before := snapshot()
	_, _ = output(t, root, Options{Converted: true})
	if snapshot() != before {
		t.Error("the check changed the root filesystem")
	}
}

func TestNotADirectory(t *testing.T) {
	if _, err := Run(Options{Root: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("no error for a root that does not exist")
	}
}

// The commands of the immucore dracut module are read from the module itself.
func TestDracutModuleCommands(t *testing.T) {
	// When the module changes, this list and the tools of the check change with it.
	want := "sync udevadm blkid lsblk e2fsck mount umount rsync cryptsetup gawk awk mkfs.ext2 mkfs.ext3 mkfs.ext4 mkfs.vfat"
	if got := strings.Join(dracutModuleCommands(), " "); got != want {
		t.Errorf("the dracut module commands are %q, want %q", got, want)
	}
}

func TestOptions(t *testing.T) {
	kernelRoot := func(t *testing.T) string {
		root := build(t, ubuntu("18.04"))
		write(t, filepath.Join(root, "lib/modules/5.4.0/modules.builtin"), "", 0o644)
		return root
	}
	tests := []struct {
		name  string
		root  func(t *testing.T) string
		opts  Options
		arch  string
		ok    bool
		wants []string
	}{
		{"a release outside the kernel list is refused", func(t *testing.T) string { return build(t, ubuntu("18.04")) }, Options{}, "", false, []string{"kairos-init has none for Ubuntu"}},
		{"a board model is not judged by the generic kernel list", func(t *testing.T) string { return build(t, ubuntu("18.04")) }, Options{Model: "rpi4"}, "", true, nil},
		{"nvidia-dgx-spark on arm64", func(t *testing.T) string { return build(t, ubuntu("24.04")) }, Options{Model: "nvidia-dgx-spark"}, "aarch64", true, nil},
		{"a kernel that is already installed", kernelRoot, Options{}, "", true, []string{"kernel: 5.4.0"}},
		{"installKernel is skipped and the system has no kernel", func(t *testing.T) string { return build(t, ubuntu("24.04")) }, Options{SkipSteps: []string{"installKernel"}}, "", false, []string{"fail  /lib/modules is missing"}},
		{"installKernel is skipped and the system has a kernel with its modules", func(t *testing.T) string {
			root := kernelRoot(t)
			dep := ""
			for _, m := range []string{"overlay", "squashfs", "loop", "ext4", "vfat", "isofs", "dm_mod", "dm_crypt"} {
				dep += "kernel/" + m + ".ko:\n"
			}
			write(t, filepath.Join(root, "lib/modules/5.4.0/modules.dep"), dep, 0o644)
			return root
		}, Options{SkipSteps: []string{"installKernel"}}, "", true, nil},
		{"installPackages is skipped and installKernel brings the kernel", func(t *testing.T) string { return build(t, ubuntu("24.04")) }, Options{SkipSteps: []string{"installPackages"}}, "", false, []string{"pass  /lib/modules (kernel): not installed yet", "fail  rsync is missing"}},
		{"Trusted Boot on a release without its kernel, with a kernel installed", func(t *testing.T) string {
			root := build(t, ubuntu("22.04"))
			write(t, filepath.Join(root, "lib/modules/5.15.0/modules.builtin"), "", 0o644)
			return root
		}, Options{TrustedBoot: true}, "", false, []string{"kairos-init has none for Ubuntu 22.04"}},
		{"debian 11 with installPackages skipped", func(t *testing.T) string { return build(t, debian("11")) }, Options{SkipSteps: []string{"installPackages"}}, "", false, []string{"fail  rsync is missing"}},
		{"initrd is skipped, so PRETTY_NAME does not matter", func(t *testing.T) string {
			return build(t, distro{"ubuntu", "debian", "24.04", "", []string{"apt-get"}})
		}, Options{SkipSteps: []string{"initrd"}}, "", true, []string{"the initramfs is not built"}},
		{"the init stage is skipped (--stage install)", func(t *testing.T) string {
			return build(t, distro{"ubuntu", "debian", "24.04", "", []string{"apt-get"}})
		}, Options{SkipSteps: []string{"init"}}, "", true, nil},
		{"a stage extension may bring a tool", func(t *testing.T) string { return build(t, ubuntu("24.04")) }, Options{SkipSteps: []string{"installPackages"}, Extensions: true}, "", true, []string{"a stage extension may bring it"}},
		{"only SLES in the name", func(t *testing.T) string {
			return build(t, distro{"mylinux", "suse", "16.0", "SLES based", []string{"zypper"}})
		}, Options{}, "", false, []string{"but not the stages that put immucore into it"}},
		{"the install stage is skipped", func(t *testing.T) string { return build(t, ubuntu("18.04")) }, Options{SkipSteps: []string{"install"}}, "", false, []string{"fail  rsync is missing"}},
		{"installPackages is skipped, so nothing brings the tools", func(t *testing.T) string { return build(t, ubuntu("24.04")) }, Options{SkipSteps: []string{"installPackages"}}, "", false, []string{"fail  rsync is missing"}},
		{"installPackages is skipped on a family without a list", func(t *testing.T) string {
			return build(t, distro{"arch", "", "1", "Arch Linux", []string{"pacman"}})
		}, Options{SkipSteps: []string{"installPackages"}}, "", false, []string{"fail  rsync is missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Arch = tt.arch
			r, out := output(t, tt.root(t), tt.opts)
			if r.OK() != tt.ok {
				t.Errorf("OK is %v, want %v:\n%s", r.OK(), tt.ok, out)
			}
			for _, want := range tt.wants {
				if !strings.Contains(out, want) {
					t.Errorf("the output does not hold %q:\n%s", want, out)
				}
			}
		})
	}
}

// The stages match on PRETTY_NAME alone: NAME does not count.
func TestNameIsNotPrettyName(t *testing.T) {
	root := build(t, ubuntu("24.04"))
	write(t, filepath.Join(root, "etc/os-release"), "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"24.04\"\n", 0o644)
	r, out := output(t, root, Options{})
	if r.OK() || !strings.Contains(out, "no kairos-init stage matches PRETTY_NAME=none") {
		t.Errorf("an os-release with NAME only:\n%s", out)
	}
}

// A link inside the root that points at the host is not followed.
func TestModuleFilesStayInTheRoot(t *testing.T) {
	root := completeSystem(t)
	dep := filepath.Join(root, "lib/modules/6.8.0/modules.dep")
	must(t, os.Remove(dep))
	must(t, os.Symlink("/dev/zero", dep))
	r, out := output(t, root, Options{Converted: true})
	if r.OK() || !strings.Contains(out, "module overlay is missing") {
		t.Errorf("a modules.dep that is a link to a device:\n%s", out)
	}
}

func TestKernelDirectoryWithoutKernel(t *testing.T) {
	root := build(t, ubuntu("24.04"))
	must(t, os.MkdirAll(filepath.Join(root, "lib/modules"), 0o755))
	_, out := output(t, root, Options{Converted: true})
	if !strings.Contains(out, "a kernel under /lib/modules is missing") {
		t.Errorf("an empty /lib/modules:\n%s", out)
	}
}

func TestSystemdVersion(t *testing.T) {
	tests := []struct {
		name    string
		version int
		err     error
		tb      bool
		status  Status
	}{
		{"new enough", 256, nil, true, Pass},
		{"the limit", 252, nil, false, Pass},
		{"old, optional", 249, nil, false, Warn},
		{"old with Trusted Boot", 249, nil, true, Fail},
		{"unreadable", 0, os.ErrNotExist, false, Warn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := systemdVersion
			t.Cleanup(func() { systemdVersion = old })
			systemdVersion = func(string) (int, error) { return tt.version, tt.err }
			c := &checker{opts: Options{TrustedBoot: tt.tb}, report: &Report{}, systemd: "/usr/bin/systemctl"}
			c.systemdVersion()
			if got := c.report.Findings[0].Status; got != tt.status {
				t.Errorf("status %s, want %s", got, tt.status)
			}
		})
	}
}

// A board model's kernel is built for the board: its modules are not required.
func TestBoardModelModules(t *testing.T) {
	root := build(t, ubuntu("24.04"))
	write(t, filepath.Join(root, "lib/modules/6.8.0/modules.builtin"), "", 0o644)
	opts := Options{Model: "rpi4", SkipSteps: []string{"install"}, Arch: "aarch64"}
	_, out := output(t, root, opts)
	if strings.Contains(out, "fail  module") {
		t.Errorf("a missing module of a board kernel is not a fail:\n%s", out)
	}
	opts.Model = ""
	if _, out = output(t, root, opts); !strings.Contains(out, "fail  module overlay is missing") {
		t.Errorf("the generic kernel needs its modules:\n%s", out)
	}
}

// A pipe in place of os-release does not block the check.
func TestOSReleaseIsAPipe(t *testing.T) {
	root := build(t, distro{})
	must(t, os.MkdirAll(filepath.Join(root, "etc"), 0o755))
	must(t, syscall.Mkfifo(filepath.Join(root, "etc/os-release"), 0o644))
	r, out := output(t, root, Options{})
	if r.OK() || !strings.Contains(out, "has no ID") {
		t.Errorf("a pipe as os-release:\n%s", out)
	}
}
