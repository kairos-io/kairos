package mos_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// The DevSec SSH baseline is the reference spec we harden against.
// See: https://github.com/dev-sec/ssh-baseline
//
// Kairos-init hardens ciphers, MACs, key exchange, forwarding,
// timeouts, moduli, and host-key sizes in the base image. The three
// auth-mode controls (PasswordAuthentication, AuthenticationMethods,
// ChallengeResponseAuthentication) are deliberately left permissive
// so a fresh boot with the default password still works, and they
// get flipped on the installed system by kairos-agent when the
// cloud-config sets `install.ssh_hardening: true` — the single-flag opt-in
// this test exercises end-to-end.
//
// Flow:
//  1. Install Kairos with tests/assets/ssh-hardening-install.yaml,
//     which sets ssh_hardening: true and gives the kairos user an
//     ephemeral ed25519 key. No password on that user: kairos-agent
//     writes the auth-mode drop-in at install time, so password auth
//     will be off on first boot.
//  2. Trigger reboot in the background and DON'T wait for peg to
//     reconnect (it can't — peg is password-only, the drop-in has
//     disabled that). Poll SSH with the ephemeral key from the runner
//     until sshd on the installed system answers.
//  3. Materialise `sshd -T` output into a directory the DevSec profile
//     can read (its sshd_config resource does not follow Include).
//  4. Run cinc-auditor (FOSS drop-in for chef/inspec) against the
//     installed system with the key. Waivers cover controls the
//     profile can't verify against this build (openssh built without
//     krb5/lastlog, ChallengeResponseAuthentication → KbdInteractive
//     rename, LoginGraceTime format-only mismatch).
var _ = Describe("ssh hardening", Label("ssh-hardening"), func() {
	var (
		vm      VM
		keyPath string
	)

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)

		keyDir, err := os.MkdirTemp("", "kairos-ssh-hardening-*")
		Expect(err).ToNot(HaveOccurred())
		keyPath = filepath.Join(keyDir, "id_ed25519")
		Expect(exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "").Run()).To(Succeed())

		pub, err := os.ReadFile(keyPath + ".pub")
		Expect(err).ToNot(HaveOccurred())
		pubkey := strings.TrimSpace(string(pub))

		tmpl, err := os.ReadFile(filepath.Join("assets", "ssh-hardening-install.yaml"))
		Expect(err).ToNot(HaveOccurred())
		rendered := strings.ReplaceAll(string(tmpl), "@PUBKEY@", pubkey)
		installCfg := filepath.Join(keyDir, "install.yaml")
		Expect(os.WriteFile(installCfg, []byte(rendered), 0o644)).To(Succeed())

		By("shipping the install cloud-config", func() {
			Expect(vm.Scp(installCfg, "/tmp/install.yaml", "0644")).To(Succeed())
		})

		By("installing Kairos with install.ssh_hardening: true", func() {
			out, err := vm.Sudo(`kairos-agent --debug manual-install --device "auto" /tmp/install.yaml`)
			GinkgoWriter.Printf("manual-install output:\n%s\n", out)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Running after-install hook"), "install did not reach the after-install hook")
			_, _ = vm.Sudo("sync")
		})

		By("triggering reboot into the installed system", func() {
			// vm.Reboot() would call EventuallyConnects with password
			// auth, which the ssh_hardening drop-in has disabled. Detach
			// and let the key probe below observe the returning sshd.
			_, _ = vm.Sudo("nohup sh -c 'sleep 2; reboot' >/dev/null 2>&1 &")
		})

		By("waiting for sshd on the installed system to accept the ephemeral key", func() {
			sshPort := vm.SSHPort()
			Eventually(func() error {
				cmd := exec.Command("ssh",
					"-i", keyPath,
					// IdentitiesOnly + PreferredAuthentications=publickey
					// force ssh to try ONLY our -i key. Otherwise it tries
					// agent keys and default IdentityFiles first, and the
					// kairos-init drop-in's MaxAuthTries=2 disconnects
					// before it gets to the right one.
					"-o", "IdentitiesOnly=yes",
					"-o", "PreferredAuthentications=publickey",
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					"-o", "PasswordAuthentication=no",
					"-o", "ConnectTimeout=5",
					"-p", sshPort,
					user()+"@127.0.0.1",
					"echo ping")
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%w: %s", err, string(out))
				}
				if !strings.Contains(string(out), "ping") {
					return fmt.Errorf("unexpected output: %q", string(out))
				}
				return nil
			}, 3*time.Minute, 5*time.Second).Should(Succeed())
		})

		By("materialising the effective sshd config for the profile to read", func() {
			// DevSec ssh-baseline uses InSpec's sshd_config resource,
			// which parses /etc/ssh/sshd_config directly and does NOT
			// follow the Include directive to /etc/ssh/sshd_config.d.
			// Our hardening ships as drop-ins, so we materialise the
			// effective config (via `sshd -T`) into a file the profile
			// can point at with sshd_custom_path. Perms match what
			// sshd-04/sshd-05 assert about /etc/ssh and sshd_config in
			// a hardened system. moduli is copied so sshd-48's DH-primes
			// bash check finds it under the same custom path.
			sshPort := vm.SSHPort()
			cmd := exec.Command("ssh",
				"-i", keyPath,
				"-o", "IdentitiesOnly=yes",
				"-o", "PreferredAuthentications=publickey",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-p", sshPort,
				user()+"@127.0.0.1",
				`sudo sh -c '
					set -eu
					mkdir -p /tmp/sshd-effective
					sshd -T > /tmp/sshd-effective/sshd_config
					cp /etc/ssh/moduli /tmp/sshd-effective/moduli
					chown -R root:root /tmp/sshd-effective
					chmod 755 /tmp/sshd-effective
					chmod 644 /tmp/sshd-effective/moduli
					chmod 600 /tmp/sshd-effective/sshd_config
				'`,
			)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))
		})
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serialPath := filepath.Join(vm.StateDir, "serial.log")
			_ = os.MkdirAll("logs", 0o755)
			if serial, err := os.ReadFile(serialPath); err == nil {
				_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, 0o644)
				lines := strings.Split(string(serial), "\n")
				const tail = 300
				if len(lines) > tail {
					lines = lines[len(lines)-tail:]
				}
				GinkgoWriter.Printf("\n--- last %d lines of %s ---\n%s\n--- end serial log ---\n",
					len(lines), serialPath, strings.Join(lines, "\n"))
			} else {
				GinkgoWriter.Printf("could not read %s: %v\n", serialPath, err)
			}
		}
		Expect(vm.Destroy(nil)).ToNot(HaveOccurred())
	})

	It("passes the DevSec ssh-baseline profile", func() {
		sshPort := vm.SSHPort()
		reportDir := "logs"
		Expect(os.MkdirAll(reportDir, 0o755)).To(Succeed())
		reportPath := filepath.Join(reportDir, "ssh-baseline.json")

		cmd := exec.Command("cinc-auditor", "exec",
			"https://github.com/dev-sec/ssh-baseline",
			"--target", fmt.Sprintf("ssh://%s@127.0.0.1:%s", user(), sshPort),
			"-i", keyPath,
			"--sudo",
			// The profile also carries ssh-* controls that check the
			// client-side /etc/ssh/ssh_config. Kairos hardens the
			// server (sshd), so only run the sshd-* controls.
			"--controls", "/^sshd-/",
			// Point the profile at the materialised effective config so
			// drop-in directives are visible (the InSpec sshd_config
			// resource does not follow Include on its own).
			"--input", "sshd_custom_path=/tmp/sshd-effective",
			// Waive controls the profile can't verify meaningfully
			// against this build (openssh built without krb5/lastlog,
			// modern directive renames, format-only mismatches).
			// `run: false` in the waiver YAML skips execution so the
			// controls don't count towards the pass/fail total. Not
			// combined with --filter-waived-controls: that combination
			// triggers a method_source parse crash in cinc-auditor 7.1.7.
			"--waiver-file", filepath.Join("assets", "ssh-baseline-waivers.yaml"),
			"--reporter", "cli", "json:"+reportPath,
			"--chef-license", "accept-silent",
		)
		cmd.Env = append(os.Environ(), "TERM=dumb")
		out, err := cmd.CombinedOutput()
		GinkgoWriter.Printf("cinc-auditor output:\n%s\n", string(out))

		// cinc-auditor exit codes: 0 all passed, 100 at least one
		// failed, 101 all passed with some skipped (steady state once
		// waivers apply). Anything else is a real error (usage, target
		// unreachable, etc.).
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 101 {
			err = nil
		}
		Expect(err).ToNot(HaveOccurred(),
			"ssh-baseline profile reported failures; see %s for the JSON report", reportPath)
	})
})
