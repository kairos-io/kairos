# Jetson Thor QSPI Firmware Staging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect QSPI boot firmware version mismatch on Jetson AGX Thor at install time, stage a UEFI capsule update when the image is newer, and abort the install with a legible message when it is not recoverable.

**Architecture:** A standalone shell script (`kairos-jetson-qspi-update`) holds all logic and is shipped into the image as a Go const in `pkg/bundled`, following the existing `NvidiaL4TScript` pattern. A new cloud-config, `13_nvidia_qspi.yaml`, invokes it on the `after-install-chroot` stage. All filesystem roots the script touches are overridable by environment variable so the logic is testable without hardware.

**Tech Stack:** Go 1.x, Ginkgo v2 + Gomega (matching `pkg/validation`), Bash, yip cloud-config schema.

**Design spec:** `docs/superpowers/specs/2026-07-20-jetson-thor-qspi-firmware-design.md`
**Tracking issue:** [kairos-io/kairos#4228](https://github.com/kairos-io/kairos/issues/4228)

> **Post-implementation correction.** Three shell code blocks below were amended after
> implementation exposed defects in them. All three were instances of the same trap: under
> `set -euo pipefail`, a bare `var="$(cmd)"` assignment dies *silently* when the inner
> command fails, because `set -e` fires before the script's own `fail` handler can run. The
> affected sites were the `dpkg-query`, `awk`/`head`, `encode_version`, and inline `stat`
> substitutions. The blocks now show the corrected `|| fail` form. Also, the dry-run check
> in Task 3 moved above the payload lookup. Anyone re-running this plan gets the fixed code.

**Repository:** All tasks are in `kairos-io/kairos-init`, not `kairos-io/kairos`. A prerequisite
commit bumping the Thor L4T pin from `38.4` to `39.2` already exists on branch
`bump-thor-l4t-39.2` (`pkg/stages/steps_install.go:80-82`).

## Global Constraints

Every task's requirements implicitly include this section. Values are copied verbatim from the spec.

- **Version encoding:** `(branch << 16) | (major << 8) | minor`. `38.0.0` = `2490368`, `38.4.0` = `2491392`, `39.2.0` = `2556416`.
- **Hard version floor:** `2490368` (38.0.0). Below this, abort — the board needs a USB host flash.
- **Capsule payload:** `/opt/ota_package/t26x/TEGRA_BL_3834_agx.Cap`, 29,378,612 bytes.
- **Chip id → payload dir:** `0x23`→`t23x`, `0x26`→`t26x`. Thor is `0x26`. Read from `/etc/nv_boot_control.conf`.
- **ESRT path:** `/sys/firmware/efi/esrt/entries/entry0/fw_version` (plain decimal integer).
- **EFI variable:** `OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c` in `/sys/firmware/efi/efivars/`.
- **EFI variable payload:** exactly 12 bytes, `\x07\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00` (`\x07` = NV+BS+RT attributes, `\x04` = bit 2).
- **Capsule directory on ESP:** `EFI/UpdateCapsule/`.
- **ESP filesystem label:** `COS_GRUB` (`kairos-sdk` `constants.EfiLabel`).
- **Never** copy `BOOTAA64.efi` over the ESP's `EFI/BOOT/` — that is Kairos's own bootloader.
- **Never** modify `/boot/extlinux/extlinux.conf`.
- **Never** run `dpkg-reconfigure nvidia-l4t-bootloader` — its postinst does both of the above.
- **Never** remove `/opt/nvidia/l4t-packages/.nv-l4t-disable-boot-fw-update-in-preinstall`.
- **Never** attempt a firmware downgrade. NVIDIA marks rollback unsupported.

## Environment facts established during design

These were verified against the real code and do not need re-deriving:

- `AfterInstallChrootHook` (`kairos-agent` `pkg/action/install.go:189`) chroots into
  `spec.Active.MountPoint` and bind-mounts **only** persistent and OEM.
- `Chroot.defaultMounts` (`kairos-agent` `pkg/utils/chroot.go:43`) is
  `["/dev", "/dev/pts", "/proc", "/sys"]`. So `/sys/firmware/efi/esrt` and
  `/sys/firmware/efi/efivars` **are** reachable inside the chroot, and reflect the live
  running firmware — the same physical QSPI. `/dev` is available for resolving the ESP.
- The **ESP is not mounted inside the chroot**. The script must mount it itself, by label.
- The hook runs *after* grub install, so the ESP exists and is formatted.
- `/opt/ota_package/` is present inside the chroot because it is part of the installed image.

## File Structure

| File | Responsibility |
|---|---|
| `pkg/bundled/jetson_qspi.go` (create) | Holds the script as a Go const, plus its install path const. |
| `pkg/bundled/jetson_qspi_test.go` (create) | Ginkgo suite exercising the script against fixture directories. |
| `pkg/bundled/bundled_suite_test.go` (create) | Ginkgo bootstrap for the `bundled` package. |
| `pkg/bundled/cloudconfigs/13_nvidia_qspi.yaml` (create) | Invokes the script on `after-install-chroot`. Auto-embedded by the existing `//go:embed cloudconfigs/*`. |
| `pkg/stages/steps_install.go` (modify) | One new step writing the script to `/usr/sbin`, Thor-gated. |

---

### Task 1: Version encoding

**Files:**
- Create: `pkg/bundled/jetson_qspi.go`
- Create: `pkg/bundled/bundled_suite_test.go`
- Create: `pkg/bundled/jetson_qspi_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `bundled.JetsonQSPIScript` (string const, the script body) and
  `bundled.JetsonQSPIScriptPath` (string const, `/usr/sbin/kairos-jetson-qspi-update`).
  The script exposes a hidden subcommand `__encode_version <x.y.z>` that prints the encoded
  integer to stdout — used by tests only.

- [ ] **Step 1: Create the Ginkgo suite bootstrap**

Create `pkg/bundled/bundled_suite_test.go`:

```go
package bundled_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBundled(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bundled Suite")
}
```

- [ ] **Step 2: Write the failing test**

Create `pkg/bundled/jetson_qspi_test.go`:

```go
package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeScript drops the bundled script into a temp dir and returns its path.
func writeScript() string {
	dir := GinkgoT().TempDir()
	path := filepath.Join(dir, "kairos-jetson-qspi-update")
	Expect(os.WriteFile(path, []byte(bundled.JetsonQSPIScript), 0o755)).To(Succeed())
	return path
}

var _ = Describe("Jetson QSPI script", func() {
	Describe("version encoding", func() {
		DescribeTable("encodes L4T versions as ESRT integers",
			func(version string, expected string) {
				out, err := exec.Command(writeScript(), "__encode_version", version).Output()
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(string(out))).To(Equal(expected))
			},
			Entry("floor version", "38.0.0", "2490368"),
			Entry("previous Thor pin", "38.4.0", "2491392"),
			Entry("current Thor pin", "39.2.0", "2556416"),
			Entry("two-component version", "39.2", "2556416"),
		)
	})
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: FAIL — `undefined: bundled.JetsonQSPIScript`

- [ ] **Step 4: Write minimal implementation**

Create `pkg/bundled/jetson_qspi.go`:

```go
package bundled

// JetsonQSPIScriptPath is where the QSPI update script is installed in the image.
const JetsonQSPIScriptPath = "/usr/sbin/kairos-jetson-qspi-update"

// JetsonQSPIScript checks the board's QSPI boot firmware version against the L4T version
// baked into the image, and stages a UEFI capsule update when the image is newer.
//
// It deliberately does not use `dpkg-reconfigure nvidia-l4t-bootloader`: that package's
// postinst also overwrites the ESP's EFI/BOOT/BOOTAA64.efi with NVIDIA's L4TLauncher
// (which would replace Kairos's own bootloader) and rewrites /boot/extlinux/extlinux.conf.
// We reproduce only the two operations that actually stage the capsule.
//
// See kairos-io/kairos#4228.
const JetsonQSPIScript = `#!/bin/bash
set -euo pipefail

# All roots are overridable so the logic can be tested without hardware.
ESRT_FW_VERSION_FILE="${ESRT_FW_VERSION_FILE:-/sys/firmware/efi/esrt/entries/entry0/fw_version}"
NV_BOOT_CONTROL_CONF="${NV_BOOT_CONTROL_CONF:-/etc/nv_boot_control.conf}"
OTA_PACKAGE_DIR="${OTA_PACKAGE_DIR:-/opt/ota_package}"
EFIVARS_DIR="${EFIVARS_DIR:-/sys/firmware/efi/efivars}"
ESP_MOUNT_DIR="${ESP_MOUNT_DIR:-/run/kairos-esp}"
ESP_LABEL="${ESP_LABEL:-COS_GRUB}"

# Below this the board cannot be updated in-band and needs a USB host flash.
FACTORY_QSPI_VER="2490368" # 38.0.0

log()  { echo "[kairos-qspi] $*"; }
fail() { echo "[kairos-qspi] ERROR: $*" >&2; exit 1; }

# encode_version 39.2.0 -> 2556416
encode_version() {
	local v="$1" branch major minor
	branch="$(echo "${v}" | cut -d. -f1)"
	major="$(echo "${v}" | cut -d. -f2)"
	minor="$(echo "${v}" | cut -d. -f3)"
	[ -n "${minor}" ] || minor=0
	echo $(( (branch << 16) | (major << 8) | minor ))
}

# Hidden entrypoint for tests.
if [ "${1:-}" = "__encode_version" ]; then
	encode_version "$2"
	exit 0
fi
`
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: PASS — 4 specs

- [ ] **Step 6: Commit**

```bash
cd /home/mudler/_git/kairos-init
git add pkg/bundled/jetson_qspi.go pkg/bundled/jetson_qspi_test.go pkg/bundled/bundled_suite_test.go
git commit -m "feat: add Jetson QSPI script with version encoding

Refs kairos-io/kairos#4228"
```

---

### Task 2: Decision logic

**Files:**
- Modify: `pkg/bundled/jetson_qspi.go`
- Modify: `pkg/bundled/jetson_qspi_test.go`

**Interfaces:**
- Consumes: `encode_version` from Task 1.
- Produces: the script's main decision path. Exit code `0` = proceed (staged or no-op),
  `1` = abort the install. Diagnostics on stderr, progress on stdout.

- [ ] **Step 1: Write the failing test**

Append to the `Describe("Jetson QSPI script", ...)` block in `pkg/bundled/jetson_qspi_test.go`:

```go
	Describe("decision logic", func() {
		// env builds a fixture environment: an ESRT file holding the board's current
		// firmware version, and an nv_boot_control.conf declaring a Thor chip id.
		env := func(currentQSPI, chipID string) (string, []string) {
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte(currentQSPI+"\n"), 0o644)).To(Succeed())
			conf := filepath.Join(dir, "nv_boot_control.conf")
			Expect(os.WriteFile(conf, []byte("TNSPEC 3834-0008\nCHIPID "+chipID+"\n"), 0o644)).To(Succeed())
			return dir, append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"NV_BOOT_CONTROL_CONF="+conf,
				"KAIROS_QSPI_DRY_RUN=1",
			)
		}

		run := func(envv []string, imageVersion string) (string, error) {
			cmd := exec.Command(writeScript())
			cmd.Env = append(envv, "KAIROS_QSPI_IMAGE_VERSION="+imageVersion)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}

		It("stages a capsule when the image is newer than the board", func() {
			_, envv := env("2490368", "0x26") // board 38.0.0
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("staging capsule"))
		})

		It("does nothing when versions match", func() {
			_, envv := env("2556416", "0x26")
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("already matches"))
			Expect(out).ToNot(ContainSubstring("staging capsule"))
		})

		It("aborts when the board firmware is newer than the image", func() {
			_, envv := env("2556416", "0x26") // board 39.2.0
			out, err := run(envv, "38.4.0")   // image 38.4.0
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("newer than this image"))
			Expect(out).To(ContainSubstring("2556416"))
			Expect(out).To(ContainSubstring("2491392"))
		})

		It("aborts when the board is below the 38.0.0 floor", func() {
			_, envv := env("2424832", "0x26") // 37.0.0
			out, err := run(envv, "39.2.0")
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("USB host flash"))
		})

		It("skips silently on non-Thor chip ids", func() {
			_, envv := env("2490368", "0x23") // Orin
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("not a Thor board"))
		})

		It("aborts when ESRT is unreadable", func() {
			dir := GinkgoT().TempDir()
			conf := filepath.Join(dir, "nv_boot_control.conf")
			Expect(os.WriteFile(conf, []byte("CHIPID 0x26\n"), 0o644)).To(Succeed())
			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+filepath.Join(dir, "absent"),
				"NV_BOOT_CONTROL_CONF="+conf,
				"KAIROS_QSPI_DRY_RUN=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("cannot read current firmware version"))
		})
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: FAIL — the script exits 0 with no output for every case, so the
`ContainSubstring` assertions fail.

- [ ] **Step 3: Write minimal implementation**

Append to the `JetsonQSPIScript` const in `pkg/bundled/jetson_qspi.go`, immediately before
the closing backtick:

```bash
# --- board gate ---------------------------------------------------------------
if [ ! -r "${NV_BOOT_CONTROL_CONF}" ]; then
	log "no ${NV_BOOT_CONTROL_CONF}, not a Jetson board with boot control; skipping"
	exit 0
fi

chipid="$(awk '/^CHIPID/ {print $2}' "${NV_BOOT_CONTROL_CONF}" | head -1)" ||
	fail "cannot determine the chip id from ${NV_BOOT_CONTROL_CONF}"
case "${chipid}" in
	0x26) payload_subdir="t26x" ;;
	*)    log "chip id ${chipid} is not a Thor board; skipping"; exit 0 ;;
esac

# --- current firmware version -------------------------------------------------
if [ ! -r "${ESRT_FW_VERSION_FILE}" ]; then
	fail "cannot read current firmware version from ${ESRT_FW_VERSION_FILE}"
fi
current_qspi_ver="$(tr -d '[:space:]' < "${ESRT_FW_VERSION_FILE}")"
if ! [ "${current_qspi_ver}" -eq "${current_qspi_ver}" ] 2>/dev/null; then
	fail "cannot read current firmware version from ${ESRT_FW_VERSION_FILE}"
fi

# --- expected version from the image ------------------------------------------
if [ -n "${KAIROS_QSPI_IMAGE_VERSION:-}" ]; then
	image_pkg_ver="${KAIROS_QSPI_IMAGE_VERSION}"
else
	image_pkg_ver="$(dpkg-query -W -f='${Version}' nvidia-l4t-bootloader 2>/dev/null | cut -d- -f1)" || image_pkg_ver=""
fi
[ -n "${image_pkg_ver}" ] || fail "cannot determine the L4T bootloader version in this image"
image_ver="$(encode_version "${image_pkg_ver}")" ||
	fail "cannot parse the L4T bootloader version '${image_pkg_ver}' found in this image"

log "board firmware: ${current_qspi_ver}, image L4T: ${image_ver} (${image_pkg_ver})"

# --- decision -----------------------------------------------------------------
if [ "${current_qspi_ver}" -lt "${FACTORY_QSPI_VER}" ]; then
	fail "board firmware ${current_qspi_ver} is below the supported floor ${FACTORY_QSPI_VER} (38.0.0).
       This board cannot be updated from a running system and needs a USB host flash.
       See https://canonical-ubuntu-for-jetson.readthedocs-hosted.com/latest/how-to/flash/"
fi

if [ "${current_qspi_ver}" -gt "${image_ver}" ]; then
	fail "board firmware ${current_qspi_ver} is newer than this image (${image_ver}).
       UEFI capsule update cannot downgrade firmware, so this board would not boot.
       Use a Kairos image built for L4T ${current_qspi_ver}, or reflash the board.
       See https://github.com/kairos-io/kairos/issues/4228"
fi

if [ "${current_qspi_ver}" -eq "${image_ver}" ]; then
	log "board firmware already matches the image; nothing to do"
	exit 0
fi

log "board firmware is older than the image; staging capsule update"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: PASS — 10 specs

- [ ] **Step 5: Commit**

```bash
cd /home/mudler/_git/kairos-init
git add pkg/bundled/jetson_qspi.go pkg/bundled/jetson_qspi_test.go
git commit -m "feat: add QSPI version decision logic

Aborts when board firmware is newer than the image, which NVIDIA's own
one-directional check does not do.

Refs kairos-io/kairos#4228"
```

---

### Task 3: Capsule staging

**Files:**
- Modify: `pkg/bundled/jetson_qspi.go`
- Modify: `pkg/bundled/jetson_qspi_test.go`

**Interfaces:**
- Consumes: `payload_subdir` and the decision path from Task 2.
- Produces: the staging side effects — capsule copied to `<esp>/EFI/UpdateCapsule/` and the
  12-byte `OsIndications` EFI variable written.

- [ ] **Step 1: Write the failing test**

Append to the `Describe("Jetson QSPI script", ...)` block:

```go
	Describe("capsule staging", func() {
		It("copies the capsule to the ESP and sets OsIndications", func() {
			dir := GinkgoT().TempDir()

			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			conf := filepath.Join(dir, "nv_boot_control.conf")
			Expect(os.WriteFile(conf, []byte("CHIPID 0x26\n"), 0o644)).To(Succeed())

			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			capsule := filepath.Join(ota, "TEGRA_BL_3834_agx.Cap")
			Expect(os.WriteFile(capsule, []byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())

			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())
			efivars := filepath.Join(dir, "efivars")
			Expect(os.MkdirAll(efivars, 0o755)).To(Succeed())

			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"NV_BOOT_CONTROL_CONF="+conf,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+efivars,
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))

			staged, err := os.ReadFile(filepath.Join(esp, "EFI", "UpdateCapsule", "TEGRA_BL_3834_agx.Cap"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(staged)).To(Equal("CAPSULE-PAYLOAD"))

			v, err := os.ReadFile(filepath.Join(efivars,
				"OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c"))
			Expect(err).ToNot(HaveOccurred())
			Expect(v).To(Equal([]byte{0x07, 0, 0, 0, 0x04, 0, 0, 0, 0, 0, 0, 0}))
		})

		It("never writes a bootloader into EFI/BOOT", func() {
			// Regression guard: NVIDIA's postinst copies L4TLauncher over
			// EFI/BOOT/BOOTAA64.efi, which on Kairos is our own bootloader.
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("BOOTAA64"))
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("extlinux"))
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("dpkg-reconfigure"))
		})

		It("aborts when the ESP lacks room for the capsule", func() {
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			conf := filepath.Join(dir, "nv_boot_control.conf")
			Expect(os.WriteFile(conf, []byte("CHIPID 0x26\n"), 0o644)).To(Succeed())
			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(ota, "TEGRA_BL_3834_agx.Cap"),
				[]byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())
			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())

			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"NV_BOOT_CONTROL_CONF="+conf,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+filepath.Join(dir, "efivars"),
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
				"KAIROS_QSPI_FORCE_FREE_KB=1", // pretend the ESP is full
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("not enough free space"))
		})
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: FAIL — the capsule is never copied, so `os.ReadFile` on the staged path errors
with "no such file or directory".

- [ ] **Step 3: Write minimal implementation**

Append to the `JetsonQSPIScript` const in `pkg/bundled/jetson_qspi.go`, immediately before
the closing backtick:

```bash
# A dry run must exit before anything that requires the real image layout
# (e.g. OTA_PACKAGE_DIR), since the decision-logic tests exercise this seam
# without staging fixtures in place.
if [ "${KAIROS_QSPI_DRY_RUN:-0}" = "1" ]; then
	log "dry run; not staging"
	exit 0
fi

# --- locate the payload -------------------------------------------------------
capsule_src="${OTA_PACKAGE_DIR}/${payload_subdir}/TEGRA_BL_3834_agx.Cap"
[ -r "${capsule_src}" ] || fail "capsule payload not found at ${capsule_src}"
capsule_size="$(stat -c %s "${capsule_src}")" || fail "cannot stat ${capsule_src}"
capsule_kb=$(( (capsule_size + 1023) / 1024 ))

# --- mount the ESP ------------------------------------------------------------
# The after-install-chroot hook does not bind-mount the ESP into the chroot, so we
# mount it ourselves by label. /dev is bind-mounted, so the device is resolvable.
mkdir -p "${ESP_MOUNT_DIR}"
if [ "${KAIROS_QSPI_SKIP_MOUNT:-0}" != "1" ]; then
	esp_dev="$(blkid -L "${ESP_LABEL}")" || fail "cannot find an ESP labelled ${ESP_LABEL}"
	mount "${esp_dev}" "${ESP_MOUNT_DIR}" || fail "cannot mount ${esp_dev} at ${ESP_MOUNT_DIR}"
	trap 'umount "${ESP_MOUNT_DIR}" || true' EXIT
fi

# --- capacity check -----------------------------------------------------------
if [ -n "${KAIROS_QSPI_FORCE_FREE_KB:-}" ]; then
	free_kb="${KAIROS_QSPI_FORCE_FREE_KB}"
else
	free_kb="$(df -Pk "${ESP_MOUNT_DIR}" | awk 'NR==2 {print $4}')"
fi
if [ "${free_kb}" -lt "${capsule_kb}" ]; then
	fail "not enough free space on the ESP: need ${capsule_kb} KiB, have ${free_kb} KiB.
       The Kairos ESP may need to be larger for this board."
fi

# --- stage --------------------------------------------------------------------
capsule_dir="${ESP_MOUNT_DIR}/EFI/UpdateCapsule"
mkdir -p "${capsule_dir}"
cp "${capsule_src}" "${capsule_dir}/"
log "staged $(basename "${capsule_src}") on the ESP"

# Set bit 2 of OsIndications (EFI_OS_INDICATIONS_FILE_CAPSULE_DELIVERY_SUPPORTED).
# UEFI applies the capsule on the next boot, before Linux starts.
osind="${EFIVARS_DIR}/OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c"
mkdir -p "${EFIVARS_DIR}"
printf '\x07\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00' > "${osind}" ||
	fail "cannot write ${osind}"

log "firmware will be updated on the next boot"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: PASS — 13 specs

- [ ] **Step 5: Verify the script passes shellcheck**

Extract the script body out of the Go const and lint it:

```bash
cd /home/mudler/_git/kairos-init
awk '/^const JetsonQSPIScript = `/{f=1;next} /^`$/{f=0} f' pkg/bundled/jetson_qspi.go > /tmp/qspi.sh
shellcheck -S warning /tmp/qspi.sh
```
Expected: no warnings. If `shellcheck` is not installed, skip this step and say so rather
than reporting it as passed.

- [ ] **Step 6: Commit**

```bash
cd /home/mudler/_git/kairos-init
git add pkg/bundled/jetson_qspi.go pkg/bundled/jetson_qspi_test.go
git commit -m "feat: stage QSPI capsule on the ESP

Mounts the ESP by label, checks capacity, copies the capsule and sets
OsIndications bit 2. Deliberately avoids dpkg-reconfigure so NVIDIA's
postinst cannot overwrite Kairos's bootloader.

Refs kairos-io/kairos#4228"
```

---

### Task 4: Ship the script and wire the cloud-config

**Files:**
- Modify: `pkg/stages/steps_install.go` (the Thor/NVIDIA step list, near line 205)
- Create: `pkg/bundled/cloudconfigs/13_nvidia_qspi.yaml`

**Interfaces:**
- Consumes: `bundled.JetsonQSPIScript`, `bundled.JetsonQSPIScriptPath` from Tasks 1–3.
- Produces: the script at `/usr/sbin/kairos-jetson-qspi-update` in Thor images, invoked on
  `after-install-chroot`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/bundled/jetson_qspi_test.go`:

```go
var _ = Describe("Jetson QSPI cloud-config", func() {
	It("is embedded and runs on after-install-chroot", func() {
		data, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/13_nvidia_qspi.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("after-install-chroot"))
		Expect(string(data)).To(ContainSubstring(bundled.JetsonQSPIScriptPath))
	})
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: FAIL — `file does not exist` reading `cloudconfigs/13_nvidia_qspi.yaml`

- [ ] **Step 3: Create the cloud-config**

Create `pkg/bundled/cloudconfigs/13_nvidia_qspi.yaml`. It is picked up automatically by the
existing `//go:embed cloudconfigs/*` in `pkg/bundled/bundled.go:32`.

```yaml
name: "Nvidia Jetson QSPI firmware check"
stages:
  after-install-chroot:
    - name: "Check and stage QSPI firmware update"
      if: |
        [ -x "/usr/sbin/kairos-jetson-qspi-update" ] && [ -r "/etc/nv_boot_control.conf" ]
      commands:
        - /usr/sbin/kairos-jetson-qspi-update
```

- [ ] **Step 4: Add the install step that ships the script**

In `pkg/stages/steps_install.go`, add this entry to the `stage := []schema.Stage{...}` slice
in `GetInstallStage`, directly after the `"Configure NVIDIA L4T USB device mode for NVIDIA devices"`
entry (around line 240):

```go
		{
			Name: "Install Jetson QSPI firmware update script",
			If:   isNvidiaThorBoard,
			Files: []schema.File{
				{
					Path:        bundled.JetsonQSPIScriptPath,
					Content:     bundled.JetsonQSPIScript,
					Permissions: 0o755,
					Owner:       0,
					Group:       0,
				},
			},
		},
```

No import change is needed: `pkg/stages/steps_install.go` already imports
`github.com/kairos-io/kairos-init/pkg/bundled` (line 18) and `github.com/mudler/yip/pkg/schema`
(line 24). `schema.Stage.Files` is `[]schema.File`, whose fields are `Path string`,
`Permissions uint32`, `Owner int`, `Group int`, `Content string`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/mudler/_git/kairos-init && go test ./pkg/bundled/ -run TestBundled`
Expected: PASS — 14 specs

Run: `cd /home/mudler/_git/kairos-init && gofmt -l pkg/stages/steps_install.go pkg/bundled/`
Expected: no output

Note: `go build ./...` fails in this repo with
`pattern binaries/edgevpn: no matching files found` — a pre-existing `//go:embed` issue
requiring artifacts the build system fetches separately. It is unrelated to these changes.

- [ ] **Step 6: Commit**

```bash
cd /home/mudler/_git/kairos-init
git add pkg/bundled/cloudconfigs/13_nvidia_qspi.yaml pkg/bundled/jetson_qspi_test.go pkg/stages/steps_install.go
git commit -m "feat: wire QSPI check into Thor images

Ships the script to /usr/sbin and invokes it on after-install-chroot.

Refs kairos-io/kairos#4228"
```

---

### Task 5: Documentation

**Files:**
- Modify: `README.md` (in `kairos-init`)

**Interfaces:**
- Consumes: behaviour from Tasks 1–4.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Add a Jetson Thor section to the README**

Find the section covering NVIDIA/Jetson models. Add:

```markdown
### Jetson AGX Thor QSPI firmware

Thor boards will not boot if the QSPI boot firmware version does not correspond to the L4T
version in the image. Kairos images pin L4T `39.2`.

At install time, `after-install-chroot` runs `/usr/sbin/kairos-jetson-qspi-update`, which
compares the board's firmware version (from the UEFI ESRT) against the image's
`nvidia-l4t-bootloader` version and:

- stages a UEFI capsule update when the image is newer — applied by UEFI on the next boot;
- does nothing when they match;
- **aborts the install** when the board firmware is newer than the image, because UEFI
  capsule update cannot downgrade firmware. Use a Kairos image matching the board, or
  reflash the board;
- **aborts the install** when the board is below L4T 38.0.0, which needs a USB host flash.

Override the L4T version with the `L4T_VERSION` environment variable.

See [kairos-io/kairos#4228](https://github.com/kairos-io/kairos/issues/4228).
```

- [ ] **Step 2: Commit**

```bash
cd /home/mudler/_git/kairos-init
git add README.md
git commit -m "docs: document Jetson Thor QSPI firmware handling

Refs kairos-io/kairos#4228"
```

---

## Hardware validation (cannot be done in CI)

The unit tests exercise the decision logic against fixtures. These must be checked on a real
Thor before the change is considered proven, and correspond to the open questions in the spec:

- [ ] **`/etc/nv_boot_control.conf` exists** on a Kairos-installed Thor. It is created at
      flash time and shipped by no package. If absent, the board gate silently skips and the
      whole feature is inert — this is the single most likely failure mode.
- [ ] **ESP capacity.** Kairos defaults to a 64 MiB ESP; the capsule is ~29 MB. Task 3's
      capacity check turns this into a clear error rather than a corrupt copy, but if it
      trips in practice the ESP size must be raised for Thor.
- [ ] **efivars is writable** from inside the `after-install-chroot` chroot.
- [ ] **`blkid -L COS_GRUB` resolves** inside the chroot, and mounting an already-mounted
      ESP at a second mountpoint succeeds.
- [ ] **End-to-end upgrade:** a Thor at 38.0.0 installed with a 39.2 image stages the capsule
      and boots into 39.2 after exactly one reboot. Confirm with
      `cat /sys/firmware/efi/esrt/entries/entry0/fw_version` → `2556416`.
- [ ] **End-to-end abort:** a Thor at 39.2 installed with a 38.4 image aborts with a legible
      message instead of black-screening.
- [ ] **Regression:** AGX Orin and Orin NX images are unaffected — verify the step is absent
      from non-Thor builds.
