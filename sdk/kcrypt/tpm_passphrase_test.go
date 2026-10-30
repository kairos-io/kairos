package kcrypt

import (
	"testing"
)

func TestResolveLocalTPMHandles(t *testing.T) {
	tests := []struct {
		name                         string
		nvIndex, cIndex, tpmDevice   string
		wantStoreIndex, wantKeyIndex string
		wantDevice                   string
	}{
		{
			name:           "defaults the NV index when none is configured",
			wantStoreIndex: DefaultLocalPassphraseNVIndex,
		},
		{
			name:           "keeps the configured NV index",
			nvIndex:        "0x1500010",
			wantStoreIndex: "0x1500010",
		},
		{
			// The key handle and the NV index are different objects. A
			// configured c_index must never leak into the NV index or the
			// other way round.
			name:           "keeps the key handle separate from the NV index",
			nvIndex:        "0x1500010",
			cIndex:         "0x81000010",
			wantStoreIndex: "0x1500010",
			wantKeyIndex:   "0x81000010",
		},
		{
			name:           "passes the TPM device through",
			cIndex:         "0x81000010",
			tpmDevice:      "/dev/tpm0",
			wantStoreIndex: DefaultLocalPassphraseNVIndex,
			wantKeyIndex:   "0x81000010",
			wantDevice:     "/dev/tpm0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveLocalTPMHandles(tt.nvIndex, tt.cIndex, tt.tpmDevice)
			if got.storeIndex != tt.wantStoreIndex {
				t.Errorf("storeIndex = %q, want %q", got.storeIndex, tt.wantStoreIndex)
			}
			if got.keyIndex != tt.wantKeyIndex {
				t.Errorf("keyIndex = %q, want %q", got.keyIndex, tt.wantKeyIndex)
			}
			if got.device != tt.wantDevice {
				t.Errorf("device = %q, want %q", got.device, tt.wantDevice)
			}
		})
	}
}
