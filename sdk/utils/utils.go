package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/denisbrodbeck/machineid"
	"github.com/joho/godotenv"
	"github.com/pterm/pterm"
	"github.com/qeesung/image2ascii/convert"
)

const (
	systemd = "systemd"
	openrc  = "openrc"
	unknown = "unknown"
)

type KeyNotFoundErr struct {
	Err error
}

func (err KeyNotFoundErr) Error() string {
	return err.Err.Error()
}

func SH(c string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Env = os.Environ()
	o, err := cmd.CombinedOutput()
	return string(o), err
}

func SHInDir(c, dir string, envs ...string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Env = append(os.Environ(), envs...)
	cmd.Dir = dir
	o, err := cmd.CombinedOutput()
	return string(o), err
}

func Exists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
}

// UUID TODO: move this into a machine submodule
func UUID() string {
	if os.Getenv("UUID") != "" {
		return os.Getenv("UUID")
	}
	id, _ := machineid.ID()
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%s", id, hostname)
}

// OSRelease finds the value of the specified key in the /etc/kairos-release file
// As a fallback on the /etc/os-release
// or, if a second argument is passed, on the file specified by the second argument.
// (optionally file argument is there for testing reasons).
func OSRelease(key string, file ...string) (string, error) {
	var osReleaseFile string
	osReleaseFallback := "/etc/os-release"

	if len(file) > 1 {
		return "", errors.New("too many arguments passed")
	}
	if len(file) > 0 {
		osReleaseFile = file[0]
	} else {
		osReleaseFile = "/etc/kairos-release"
	}
	release, err := godotenv.Read(osReleaseFile)
	if err != nil {
		return "", err
	}
	kairosKey := "KAIROS_" + key
	v, exists := release[kairosKey]
	if !exists {
		// We try with the old naming without the prefix in case the key wasn't found
		v, exists = release[key]
		if !exists {
			// We try with fallback file
			release, err = godotenv.Read(osReleaseFallback)
			if err != nil {
				return "", err
			}
			kairosKey = "KAIROS_" + key
			v, exists = release[kairosKey]
			if !exists {
				// We try with the old naming without the prefix in case the key wasn't found
				v, exists = release[key]
				if !exists {
					return "", KeyNotFoundErr{Err: fmt.Errorf("%s key not found", kairosKey)}
				}
			}
		}
	}

	return v, nil
}

func FindCommand(def string, options []string) string {
	for _, p := range options {
		path, err := exec.LookPath(p)
		if err == nil {
			return path
		}
	}

	// Otherwise return default
	return def
}

func K3sBin() string {
	for _, p := range []string{"/usr/bin/k3s", "/usr/local/bin/k3s"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func K0sBin() string {
	for _, p := range []string{"/usr/bin/k0s", "/usr/local/bin/k0s"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func WriteEnv(envFile string, config map[string]string) error {
	content, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	env, _ := godotenv.Unmarshal(string(content))

	for key, val := range config {
		env[key] = val
	}

	return godotenv.Write(env, envFile)
}

func Flavor() string {
	v, err := OSRelease("FLAVOR")
	if err != nil {
		return ""
	}

	return v
}

// GetInit Return the init system used by the OS
func GetInit() string {
	for _, file := range []string{"/run/systemd/system", "/sbin/systemctl", "/usr/bin/systemctl", "/usr/sbin/systemctl"} {
		if _, err := os.Stat(file); err == nil {
			return systemd
		}
	}

	for _, file := range []string{"/sbin/openrc", "/usr/sbin/openrc", "/bin/openrc", "/usr/bin/openrc"} {
		if _, err := os.Stat(file); err == nil {
			return openrc
		}
	}

	return unknown
}

func Name() string {
	v, err := OSRelease("NAME")
	if err != nil {
		return ""
	}

	return strings.ReplaceAll(v, "kairos-", "")
}

func IsOpenRCBased() bool {
	return GetInit() == openrc
}

func ShellSTDIN(s, c string) (string, error) {
	cmd := exec.Command("/bin/sh", "-c", c)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = bytes.NewBuffer([]byte(s))
	o, err := cmd.CombinedOutput()
	return string(o), err
}

func SetEnv(env []string) {
	for _, e := range env {
		pair := strings.SplitN(e, "=", 2)
		if len(pair) >= 2 {
			os.Setenv(pair[0], pair[1])
		}
	}
}

func OnSignal(fn func(), sig ...os.Signal) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, sig...)
	go func() {
		<-sigs
		fn()
	}()
}

func Shell() *exec.Cmd {
	cmd := exec.Command("/bin/sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd
}

func Prompt(t string) (string, error) {
	if t != "" {
		pterm.Info.Println(t)
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(answer), nil
}

func PrintBanner(d []byte) {
	img, _, _ := image.Decode(bytes.NewReader(d))

	convertOptions := convert.DefaultOptions
	convertOptions.FixedWidth = 100
	convertOptions.FixedHeight = 40

	// Create the image converter
	converter := convert.NewImageConverter()
	fmt.Print(converter.Image2ASCIIString(img, &convertOptions))
}

func Reboot() {
	pterm.Info.Println("Rebooting node")
	SH("reboot") //nolint:errcheck
}

func PowerOFF() {
	pterm.Info.Println("Shutdown node")
	SH(poweroffCommand(IsOpenRCBased())) //nolint:errcheck
}

// poweroffCommand returns the command used to power the node off. On systemd
// based systems `shutdown` defaults to a one minute delay, so `shutdown now` is
// used to power off immediately. openRC based systems use `poweroff`, which
// already takes effect immediately.
func poweroffCommand(openRC bool) string {
	if openRC {
		return "poweroff"
	}
	return "shutdown now"
}

func ListToOutput(rels []string, output string) []string {
	switch strings.ToLower(output) {
	case "yaml":
		d, _ := yaml.Marshal(rels)
		return []string{string(d)}
	case "json":
		d, _ := json.Marshal(rels)
		return []string{string(d)}
	default:
		return rels
	}
}

func GetInterfaceIP(in string) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("failed getting system interfaces")
		return ""
	}
	for _, i := range ifaces {
		if i.Name == in {
			addrs, _ := i.Addrs()
			// handle err
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil {
					return ip.String()

				}
			}
		}
	}
	return ""
}

// GetCurrentPlatform returns the current platform in docker style `linux/amd64` for use with image utils
func GetCurrentPlatform() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

// GetEfiGrubFiles Return possible paths for the grub.efi
// Used in auroraboot and agent.
func GetEfiGrubFiles(arch string) []string {
	var modNames []string
	switch arch {
	case "arm64":
		modNames = append(modNames, "/usr/share/efi/aarch64/grub.efi")                    // suse
		modNames = append(modNames, "/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed") // ubuntu + debian
		modNames = append(modNames, "/boot/efi/EFI/fedora/grubaa64.efi")                  // fedora
		modNames = append(modNames, "/boot/efi/EFI/rocky/grubaa64.efi")                   // rocky
		modNames = append(modNames, "/boot/efi/EFI/redhat/grubaa64.efi")                  // redhat
		modNames = append(modNames, "/boot/efi/EFI/almalinux/grubaa64.efi")               // almalinux
		modNames = append(modNames, "/usr/lib/grub/arm64-efi/grubaa64.efi")               // hadron

	case "riscv64":
		// openSUSE / SLE layout (grub2-riscv64-efi):
		modNames = append(modNames, "/usr/share/efi/riscv64/grub.efi")
		// Verified Debian 13 modules path:
		// https://packages.debian.org/trixie/riscv64/grub-efi-riscv64-bin/filelist
		modNames = append(modNames, "/usr/lib/grub/riscv64-efi/grubriscv64.efi")
		// Ubuntu Noble ships grub-efi-riscv64 / grub-efi-riscv64-bin for riscv64, and
		// Debian-family layouts commonly use the monolithic output here. Ubuntu file list
		// was not directly available during implementation, so treat this as a Debian-style
		// derivative path to validate against images:
		// https://packages.ubuntu.com/noble/admin/grub-efi
		modNames = append(modNames, "/usr/lib/grub/riscv64-efi/monolithic/grubriscv64.efi")
		// Verified Debian-family ESP path for Debian 13 systems:
		// https://wiki.debian.org/InstallingDebianOn/StarFive/VisionFiveV2
		modNames = append(modNames, "/boot/efi/EFI/debian/grubriscv64.efi")
		// Ubuntu Noble likely follows the same ESP filename convention under /EFI/ubuntu/.
		// Keep this separate from the Debian path because external consumers test Ubuntu
		// images directly:
		// https://packages.ubuntu.com/noble/admin/grub-efi
		// https://bugs.launchpad.net/bugs/2104572
		modNames = append(modNames, "/boot/efi/EFI/ubuntu/grubriscv64.efi")
		// Verified Fedora 42 ESP path:
		// https://riscv-koji.fedoraproject.org/koji/rpminfo?rpmID=25558
		modNames = append(modNames, "/boot/efi/EFI/fedora/grubriscv64.efi")
		// Verified removable-media fallback name used by Debian-family riscv64 installs:
		// https://wiki.debian.org/UEFI
		// https://wiki.debian.org/InstallingDebianOn/StarFive/VisionFiveV2
		modNames = append(modNames, "/boot/efi/EFI/BOOT/BOOTRISCV64.EFI")

	default:
		modNames = append(modNames, "/usr/share/efi/x86_64/grub.efi")                     // suse
		modNames = append(modNames, "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed") // ubuntu + debian
		modNames = append(modNames, "/boot/efi/EFI/fedora/grubx64.efi")                   // fedora
		modNames = append(modNames, "/boot/efi/EFI/rocky/grubx64.efi")                    // rocky
		modNames = append(modNames, "/boot/efi/EFI/redhat/grubx64.efi")                   // redhat
		modNames = append(modNames, "/boot/efi/EFI/almalinux/grubx64.efi")                // almalinux
		modNames = append(modNames, "/usr/lib/grub/x86_64-efi/grubx64.efi")               // hadron
	}
	return modNames
}

// GetEfiLiveGrubFiles returns possible grub.efi paths ordered for live media.
// Debian and Ubuntu's gcd*.efi binaries include ISO9660 support and use the
// /boot/grub prefix, so live ISO builders should prefer them while retaining
// every disk-oriented path as a fallback.
func GetEfiLiveGrubFiles(arch string) []string {
	switch arch {
	case "arm64", "aarch64":
		return append(
			[]string{"/usr/lib/grub/arm64-efi-signed/gcdaa64.efi.signed"},
			GetEfiGrubFiles("arm64")...,
		)
	case "riscv64":
		return GetEfiGrubFiles(arch)
	default:
		return append(
			[]string{"/usr/lib/grub/x86_64-efi-signed/gcdx64.efi.signed"},
			GetEfiGrubFiles(arch)...,
		)
	}
}

// GetEfiShimFiles Return possible paths for the shim.efi
// Used in auroraboot and agent.
func GetEfiShimFiles(arch string) []string {
	var modNames []string
	switch arch {
	case "arm64":
		modNames = append(modNames, "/usr/share/efi/aarch64/shim.efi")          // suse + Hadron
		modNames = append(modNames, "/usr/lib/shim/shimaa64.efi.dualsigned")    // ubuntu
		modNames = append(modNames, "/usr/lib/shim/shimaa64.efi.signed.latest") // ubuntu
		modNames = append(modNames, "/usr/lib/shim/shimaa64.efi.signed")        // debian, maybe ubuntu but its a link so it can be broken
		modNames = append(modNames, "/boot/efi/EFI/fedora/shim.efi")            // fedora
		modNames = append(modNames, "/boot/efi/EFI/rocky/shim.efi")             // rocky
		modNames = append(modNames, "/boot/efi/EFI/redhat/shim.efi")            // redhat
		modNames = append(modNames, "/boot/efi/EFI/redhat/shimaa64.efi")        // redhat
		modNames = append(modNames, "/boot/efi/EFI/almalinux/shim.efi")         // almalinux
	case "riscv64":
		// No stable shim file conventions are handled here for riscv64 yet.
	default:
		modNames = append(modNames, "/usr/share/efi/x86_64/shim.efi")          // suse + Hadron
		modNames = append(modNames, "/usr/lib/shim/shimx64.efi.dualsigned")    // ubuntu
		modNames = append(modNames, "/usr/lib/shim/shimx64.efi.signed.latest") // ubuntu
		modNames = append(modNames, "/usr/lib/shim/shimx64.efi.signed")        // debian, maybe ubuntu but its a link so it can be broken
		modNames = append(modNames, "/boot/efi/EFI/fedora/shim.efi")           // fedora
		modNames = append(modNames, "/boot/efi/EFI/rocky/shim.efi")            // rocky
		modNames = append(modNames, "/boot/efi/EFI/redhat/shim.efi")           // redhat
		modNames = append(modNames, "/boot/efi/EFI/redhat/shimx64.efi")        // redhat
		modNames = append(modNames, "/boot/efi/EFI/almalinux/shim.efi")        // almalinux
	}

	return modNames
}

// SystemdBootConfReader reads a systemd-boot conf file and returns a map with the key/value pairs
func SystemdBootConfReader(filePath string) (map[string]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
		if len(parts) == 1 {
			result[parts[0]] = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}
