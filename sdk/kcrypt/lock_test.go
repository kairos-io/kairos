package kcrypt

import (
	"strings"
	"testing"
)

// procMountsBootedNode is what /proc/mounts looks like on a node where
// immucore has mounted COS_PERSISTENT at /usr/local and then bind-mounted out
// of /usr/local/.state. A bind mount carries the device name of the
// filesystem it was taken from, so the partition has one entry per bind, and
// the kernel lists them after the mount they came from.
const procMountsBootedNode = `/dev/sda2 /run/initramfs/cos-state ext4 ro,relatime 0 0
/dev/sda4 /oem ext4 rw,relatime 0 0
/dev/sda5 /usr/local ext4 rw,relatime 0 0
/dev/sda5 /var/lib/rancher ext4 rw,relatime 0 0
/dev/sda5 /etc/systemd ext4 rw,relatime 0 0
/dev/sda5 /home ext4 rw,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev 0 0
`

func TestMountPointsForDevice(t *testing.T) {
	tests := []struct {
		name   string
		mounts string
		device string
		want   []string
	}{
		{
			name:   "reports every bind mount of the partition, not just the first",
			mounts: procMountsBootedNode,
			device: "/dev/sda5",
			want:   []string{"/usr/local", "/var/lib/rancher", "/etc/systemd", "/home"},
		},
		{
			name:   "keeps the order the kernel lists them in",
			mounts: "/dev/sda5 /home ext4 rw 0 0\n/dev/sda5 /usr/local ext4 rw 0 0\n",
			device: "/dev/sda5",
			want:   []string{"/home", "/usr/local"},
		},
		{
			name:   "reports nothing for a partition that is not mounted",
			mounts: procMountsBootedNode,
			device: "/dev/sda6",
			want:   nil,
		},
		{
			name:   "does not match a device that only shares a prefix",
			mounts: "/dev/sda51 /mnt ext4 rw 0 0\n",
			device: "/dev/sda5",
			want:   nil,
		},
		{
			name:   "decodes the octal escapes the kernel writes for a space",
			mounts: `/dev/sda5 /mnt/my\040disk ext4 rw,relatime 0 0` + "\n",
			device: "/dev/sda5",
			want:   []string{"/mnt/my disk"},
		},
		{
			name:   "skips a line that has no mount point",
			mounts: "/dev/sda5\n/dev/sda5 /usr/local ext4 rw 0 0\n",
			device: "/dev/sda5",
			want:   []string{"/usr/local"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mountPointsForDevice(strings.NewReader(tt.mounts), tt.device)
			if err != nil {
				t.Fatalf("mountPointsForDevice() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("mountPointsForDevice() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("mountPointsForDevice()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// The mount point that has to be unmounted last is the one the old code
// unmounted first: detaching /usr/local leaves every bind holding the device,
// so cryptsetup still refuses to format it.
func TestMountPointsForDeviceUnmountOrder(t *testing.T) {
	got, err := mountPointsForDevice(strings.NewReader(procMountsBootedNode), "/dev/sda5")
	if err != nil {
		t.Fatalf("mountPointsForDevice() error = %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected the partition to have several mount points, got %v", got)
	}
	if got[0] != "/usr/local" {
		t.Errorf("the kernel lists the filesystem before its binds, got %q first", got[0])
	}
	if got[len(got)-1] == "/usr/local" {
		t.Error("/usr/local must not be unmounted first, every bind still holds the device")
	}
}

func TestUnescapeMountField(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  string
	}{
		{name: "leaves a plain path alone", field: "/usr/local", want: "/usr/local"},
		{name: "decodes a space", field: `/mnt/my\040disk`, want: "/mnt/my disk"},
		{name: "decodes a tab", field: `/mnt/a\011b`, want: "/mnt/a\tb"},
		{name: "decodes a newline", field: `/mnt/a\012b`, want: "/mnt/a\nb"},
		{name: "decodes a backslash", field: `/mnt/a\134b`, want: `/mnt/a\b`},
		{name: "decodes several escapes in one field", field: `/mnt/a\040b\040c`, want: "/mnt/a b c"},
		{name: "leaves a backslash that is not an escape alone", field: `/mnt/a\b`, want: `/mnt/a\b`},
		{name: "leaves a trailing backslash alone", field: `/mnt/a\`, want: `/mnt/a\`},
		{name: "leaves a non-octal escape alone", field: `/mnt/a\99x`, want: `/mnt/a\99x`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeMountField(tt.field); got != tt.want {
				t.Errorf("unescapeMountField(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
