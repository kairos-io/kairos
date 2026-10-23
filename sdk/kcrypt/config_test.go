package kcrypt

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func TestExtractEncryptOnBootPolicy(t *testing.T) {
	log := logger.NewNullLogger()

	tests := []struct {
		name           string
		values         collector.ConfigValues
		wantEnabled    bool
		wantPartitions []string
	}{
		{
			name:   "empty config yields a disabled policy",
			values: nil,
		},
		{
			name: "enabled flag and partition list",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": true},
				"install": collector.ConfigValues{
					"encrypted_partitions": []interface{}{constants.PersistentLabel},
				},
			},
			wantEnabled:    true,
			wantPartitions: []string{constants.PersistentLabel},
		},
		{
			name: "flag as a cmdline string",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": "true"},
			},
			wantEnabled: true,
		},
		{
			name: "flag as the cmdline spellings 1",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": "1"},
			},
			wantEnabled: true,
		},
		{
			name: "flag as the cmdline spelling yes, any case",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": "YES"},
			},
			wantEnabled: true,
		},
		{
			name: "flag as a yaml integer 1",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": 1},
			},
			wantEnabled: true,
		},
		{
			name: "unrecognised flag strings stay disabled",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": "enable-it"},
			},
		},
		{
			name: "explicit no stays disabled",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": "no"},
			},
		},
		{
			name: "partitions as a comma separated cmdline string",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": true},
				"install": collector.ConfigValues{
					"encrypted_partitions": constants.PersistentLabel + ", MYAPP_DATA",
				},
			},
			wantEnabled:    true,
			wantPartitions: []string{constants.PersistentLabel, "MYAPP_DATA"},
		},
		{
			name: "explicit false stays disabled",
			values: collector.ConfigValues{
				"kcrypt": collector.ConfigValues{"encrypt_on_boot": false},
				"install": collector.ConfigValues{
					"encrypted_partitions": []string{constants.PersistentLabel},
				},
			},
			wantPartitions: []string{constants.PersistentLabel},
		},
		{
			name: "nested maps from a plain yaml unmarshal",
			values: collector.ConfigValues{
				"kcrypt": map[string]interface{}{"encrypt_on_boot": true},
				"install": map[string]interface{}{
					"encrypted_partitions": []interface{}{constants.PersistentLabel, "MYAPP_DATA"},
				},
			},
			wantEnabled:    true,
			wantPartitions: []string{constants.PersistentLabel, "MYAPP_DATA"},
		},
		{
			name: "partition list without the flag stays disabled",
			values: collector.ConfigValues{
				"install": collector.ConfigValues{
					"encrypted_partitions": []interface{}{constants.PersistentLabel},
				},
			},
			wantPartitions: []string{constants.PersistentLabel},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := extractEncryptOnBootPolicy(collector.Config{Values: tt.values}, log)
			if policy.Enabled != tt.wantEnabled {
				t.Fatalf("Enabled = %t, want %t", policy.Enabled, tt.wantEnabled)
			}
			if len(policy.Partitions) != len(tt.wantPartitions) {
				t.Fatalf("Partitions = %v, want %v", policy.Partitions, tt.wantPartitions)
			}
			for i := range policy.Partitions {
				if policy.Partitions[i] != tt.wantPartitions[i] {
					t.Fatalf("Partitions = %v, want %v", policy.Partitions, tt.wantPartitions)
				}
			}
		})
	}
}

func TestScanEncryptOnBootPolicy(t *testing.T) {
	log := logger.NewNullLogger()

	t.Run("reads the policy from a config directory", func(t *testing.T) {
		dir := t.TempDir()
		config := `#cloud-config
install:
  encrypted_partitions:
    - COS_PERSISTENT
kcrypt:
  encrypt_on_boot: true
`
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}

		policy, err := ScanEncryptOnBootPolicy(log, dir)
		if err != nil {
			t.Fatal(err)
		}
		if !policy.Enabled {
			t.Fatal("Enabled = false, want true")
		}
		if len(policy.Partitions) != 1 || policy.Partitions[0] != constants.PersistentLabel {
			t.Fatalf("Partitions = %v, want [%s]", policy.Partitions, constants.PersistentLabel)
		}
	})

	t.Run("an empty directory yields a disabled policy", func(t *testing.T) {
		policy, err := ScanEncryptOnBootPolicy(log, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if policy.Enabled {
			t.Fatal("Enabled = true, want false")
		}
	})

	t.Run("a nil collector config yields a disabled policy", func(t *testing.T) {
		policy := EncryptOnBootPolicyFromConfig(nil, log)
		if policy.Enabled || len(policy.Partitions) != 0 {
			t.Fatalf("policy = %+v, want the zero policy", policy)
		}
	})
}

func TestRejectSystemPartitions(t *testing.T) {
	tests := []struct {
		name         string
		partitions   []string
		effectiveOEM string
		wantLabel    string
	}{
		{
			name:       "an empty list is accepted",
			partitions: nil,
		},
		{
			name:       "a data partition is accepted",
			partitions: []string{constants.PersistentLabel, "MYAPP_DATA"},
		},
		{
			name:       "the OEM label is refused",
			partitions: []string{constants.PersistentLabel, constants.OEMLabel},
			wantLabel:  constants.OEMLabel,
		},
		{
			name:         "a renamed OEM label is refused",
			partitions:   []string{"MY_OEM"},
			effectiveOEM: "MY_OEM",
			wantLabel:    "MY_OEM",
		},
		{
			name:       "the state label is refused",
			partitions: []string{constants.StateLabel},
			wantLabel:  constants.StateLabel,
		},
		{
			name:       "the recovery label is refused",
			partitions: []string{constants.RecoveryLabel},
			wantLabel:  constants.RecoveryLabel,
		},
		{
			name:       "the EFI label is refused",
			partitions: []string{constants.EfiLabel},
			wantLabel:  constants.EfiLabel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := EncryptOnBootPolicy{Enabled: true, Partitions: tt.partitions}
			err := policy.RejectSystemPartitions(tt.effectiveOEM)
			if tt.wantLabel == "" {
				if err != nil {
					t.Fatalf("RejectSystemPartitions() = %v, want nil", err)
				}
				return
			}
			var protected *ProtectedPartitionError
			if !errors.As(err, &protected) {
				t.Fatalf("RejectSystemPartitions() = %v, want a *ProtectedPartitionError", err)
			}
			if protected.Label != tt.wantLabel {
				t.Fatalf("Label = %s, want %s", protected.Label, tt.wantLabel)
			}
			if protected.Reason == "" {
				t.Fatal("Reason is empty, want an explanation")
			}
		})
	}
}

// scanPCRBindings writes body as a cloud-config, runs the real collector over
// it, and reads the PCR bindings back the way GetEncryptor does.
func scanPCRBindings(t *testing.T, body string) (bindPCRs, bindPublicPCRs []string, err error) {
	t.Helper()

	dir := t.TempDir()
	if werr := os.WriteFile(filepath.Join(dir, "90_kcrypt.yaml"), []byte(body), 0644); werr != nil {
		t.Fatal(werr)
	}

	o := &collector.Options{NoLogs: true}
	if aerr := o.Apply(collector.Directories(dir)); aerr != nil {
		t.Fatal(aerr)
	}
	c, serr := collector.Scan(o, func(b []byte) ([]byte, error) { return b, nil })
	if serr != nil {
		t.Fatal(serr)
	}

	return extractPCRBindingsFromCollector(*c, logger.NewBufferLogger(&bytes.Buffer{}))
}

// typedPCRBindings reads the same document through the typed config, which is
// the other reader of these two keys.
func typedPCRBindings(t *testing.T, body string) (*sdkConfig.Config, error) {
	t.Helper()

	var c sdkConfig.Config
	err := yaml.Unmarshal([]byte(body), &c)
	return &c, err
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestPCRBindingsAgreeWithTypedConfig is the invariant: a document the typed
// config accepts has to reach systemd-cryptenroll with the same PCR indices.
// bind-pcrs has no default, so a binding dropped here is a partition enrolled
// with no PCR policy at all.
func TestPCRBindingsAgreeWithTypedConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"quoted indices", "#cloud-config\nbind-pcrs:\n  - \"7\"\nbind-public-pcrs:\n  - \"11\"\n"},
		{"bare numbers", "#cloud-config\nbind-pcrs:\n  - 7\nbind-public-pcrs:\n  - 11\n"},
		{"inline numbers", "#cloud-config\nbind-pcrs: [7, 8]\n"},
		{"mixed", "#cloud-config\nbind-pcrs: [7, \"8\"]\nbind-public-pcrs: [11]\n"},
		{"neither key", "#cloud-config\ninstall:\n  device: /dev/sda\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			typed, terr := typedPCRBindings(t, tc.body)
			if terr != nil {
				t.Fatalf("the typed config rejected this document, so it is not a case this test can compare: %v", terr)
			}

			bindPCRs, bindPublicPCRs, err := scanPCRBindings(t, tc.body)
			if err != nil {
				t.Fatalf("kcrypt refused a document the typed config accepts: %v", err)
			}

			if !equalStrings(bindPCRs, typed.BindPCRs) {
				t.Errorf("bind-pcrs: kcrypt read %#v, the typed config read %#v", bindPCRs, typed.BindPCRs)
			}
			if !equalStrings(bindPublicPCRs, typed.BindPublicPCRs) {
				t.Errorf("bind-public-pcrs: kcrypt read %#v, the typed config read %#v", bindPublicPCRs, typed.BindPublicPCRs)
			}
		})
	}
}

// TestPCRBindingsReportAnUnreadableValue pins the second half: a value that
// cannot be read as a list of PCR indices is an error, not an empty list that
// silently drops the policy.
func TestPCRBindingsReportAnUnreadableValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a scalar", "#cloud-config\nbind-pcrs: 7\n"},
		{"a boolean", "#cloud-config\nbind-pcrs: true\n"},
		{"a mapping", "#cloud-config\nbind-pcrs:\n  a: 1\n"},
		{"a nested list", "#cloud-config\nbind-pcrs:\n  - [7]\n"},
		{"a fractional index", "#cloud-config\nbind-pcrs: [7.5]\n"},
		{"an unreadable public list", "#cloud-config\nbind-public-pcrs:\n  - a: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bindPCRs, bindPublicPCRs, err := scanPCRBindings(t, tc.body)
			if err == nil {
				t.Fatalf("expected an error, got bind-pcrs=%#v bind-public-pcrs=%#v", bindPCRs, bindPublicPCRs)
			}
			if bindPCRs != nil || bindPublicPCRs != nil {
				t.Errorf("an error has to come with no bindings, got %#v and %#v", bindPCRs, bindPublicPCRs)
			}
		})
	}
}

// TestPCRBindingsBindWhatWasAsked states the user-visible outcome directly, so
// the suite still fails if the agreement test above ever goes vacuous.
func TestPCRBindingsBindWhatWasAsked(t *testing.T) {
	bindPCRs, bindPublicPCRs, err := scanPCRBindings(t, "#cloud-config\nbind-pcrs:\n  - 7\nbind-public-pcrs:\n  - 11\n")
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(bindPCRs, []string{"7"}) {
		t.Errorf("bind-pcrs: got %#v, want [7]", bindPCRs)
	}
	if !equalStrings(bindPublicPCRs, []string{"11"}) {
		t.Errorf("bind-public-pcrs: got %#v, want [11]", bindPublicPCRs)
	}
}
