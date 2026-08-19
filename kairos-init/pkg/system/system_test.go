package system

import (
	"testing"

	"github.com/kairos-io/kairos-init/pkg/values"
)

func TestDetectFromReleaseIDs(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		idLike         string
		expectedDistro values.Distro
		expectedFamily values.Family
	}{
		// Direct ID matches
		{name: "ID debian", id: "debian", expectedDistro: values.Debian, expectedFamily: values.DebianFamily},
		{name: "ID ubuntu", id: "ubuntu", expectedDistro: values.Ubuntu, expectedFamily: values.DebianFamily},
		{name: "ID fedora", id: "fedora", expectedDistro: values.Fedora, expectedFamily: values.RedHatFamily},
		{name: "ID rocky", id: "rocky", expectedDistro: values.RockyLinux, expectedFamily: values.RedHatFamily},
		{name: "ID almalinux", id: "almalinux", expectedDistro: values.AlmaLinux, expectedFamily: values.RedHatFamily},
		{name: "ID ol", id: "ol", expectedDistro: values.OracleLinux, expectedFamily: values.RedHatFamily},
		{name: "ID rhel", id: "rhel", expectedDistro: values.RedHat, expectedFamily: values.RedHatFamily},
		{name: "ID arch", id: "arch", expectedDistro: values.Arch, expectedFamily: values.ArchFamily},
		{name: "ID alpine", id: "alpine", expectedDistro: values.Alpine, expectedFamily: values.AlpineFamily},
		{name: "ID opensuse-leap", id: "opensuse-leap", expectedDistro: values.OpenSUSELeap, expectedFamily: values.SUSEFamily},
		{name: "ID opensuse-tumbleweed", id: "opensuse-tumbleweed", expectedDistro: values.OpenSUSETumbleweed, expectedFamily: values.SUSEFamily},
		{name: "ID sles", id: "sles", expectedDistro: values.SLES, expectedFamily: values.SUSEFamily},
		{name: "ID hadron", id: "hadron", expectedDistro: values.Hadron, expectedFamily: values.HadronFamily},
		{name: "ID sle-micro-rancher", id: "sle-micro-rancher", expectedDistro: values.SLEMicroRancher, expectedFamily: values.SUSEFamily},

		// ID takes precedence over ID_LIKE
		{name: "ID precedence over ID_LIKE", id: "ubuntu", idLike: "rhel fedora", expectedDistro: values.Ubuntu, expectedFamily: values.DebianFamily},

		// ID_LIKE single-value fallback via detectFromID
		{name: "ID_LIKE debian", id: "custom", idLike: "debian", expectedDistro: values.Debian, expectedFamily: values.DebianFamily},
		{name: "ID_LIKE fedora", id: "custom", idLike: "fedora", expectedDistro: values.Fedora, expectedFamily: values.RedHatFamily},
		{name: "ID_LIKE rhel", id: "custom", idLike: "rhel", expectedDistro: values.RedHat, expectedFamily: values.RedHatFamily},
		{name: "ID_LIKE arch", id: "custom", idLike: "arch", expectedDistro: values.Arch, expectedFamily: values.ArchFamily},
		{name: "ID_LIKE alpine", id: "custom", idLike: "alpine", expectedDistro: values.Alpine, expectedFamily: values.AlpineFamily},

		// ID_LIKE family-only markers (not valid distro IDs)
		{name: "ID_LIKE suse (family marker)", id: "custom", idLike: "suse", expectedDistro: values.OpenSUSELeap, expectedFamily: values.SUSEFamily},
		{name: "ID_LIKE redhat (family marker)", id: "custom", idLike: "redhat", expectedDistro: values.Fedora, expectedFamily: values.RedHatFamily},

		// ID_LIKE multi-value: first recognized token wins (left-to-right)
		{name: "ID_LIKE multi-value rhel centos fedora", id: "custom", idLike: "rhel centos fedora", expectedDistro: values.RedHat, expectedFamily: values.RedHatFamily},
		{name: "ID_LIKE multi-value ubuntu debian", id: "custom", idLike: "ubuntu debian", expectedDistro: values.Ubuntu, expectedFamily: values.DebianFamily},
		{name: "ID_LIKE ordered: debian before rhel", id: "custom", idLike: "debian rhel", expectedDistro: values.Debian, expectedFamily: values.DebianFamily},
		{name: "ID_LIKE skips unknown then matches", id: "custom", idLike: "custom rhel", expectedDistro: values.RedHat, expectedFamily: values.RedHatFamily},
		{name: "ID_LIKE opensuse-leap suse", id: "custom", idLike: "opensuse-leap suse", expectedDistro: values.OpenSUSELeap, expectedFamily: values.SUSEFamily},
		{name: "ID_LIKE opensuse suse (family fallback)", id: "custom", idLike: "opensuse suse", expectedDistro: values.OpenSUSELeap, expectedFamily: values.SUSEFamily},

		// Fully unknown
		{name: "unknown ID and ID_LIKE", id: "custom", idLike: "custombase", expectedDistro: values.Unknown, expectedFamily: values.UnknownFamily},
		{name: "empty ID and ID_LIKE", id: "", idLike: "", expectedDistro: values.Unknown, expectedFamily: values.UnknownFamily},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			distro, family := detectFromReleaseIDs(tt.id, tt.idLike)
			if distro != tt.expectedDistro {
				t.Errorf("distro: expected %q, got %q", tt.expectedDistro, distro)
			}
			if family != tt.expectedFamily {
				t.Errorf("family: expected %q, got %q", tt.expectedFamily, family)
			}
		})
	}
}
