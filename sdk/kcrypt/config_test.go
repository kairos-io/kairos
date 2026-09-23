package kcrypt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
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
