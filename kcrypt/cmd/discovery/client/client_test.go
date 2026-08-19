package client

import "testing"

func TestAttestationEndpointURL(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		want      string
	}{
		{
			name:      "no trailing slash",
			serverURL: "http://foo.example:8082/challenge",
			want:      "http://foo.example:8082/challenge/tpm-attestation",
		},
		{
			name:      "trailing slash is normalized",
			serverURL: "http://foo.example:8082/challenge/",
			want:      "http://foo.example:8082/challenge/tpm-attestation",
		},
		{
			name:      "multiple trailing slashes are normalized",
			serverURL: "http://foo.example:8082/challenge///",
			want:      "http://foo.example:8082/challenge/tpm-attestation",
		},
		{
			name:      "bare host without path",
			serverURL: "http://foo.example:8082",
			want:      "http://foo.example:8082/tpm-attestation",
		},
		{
			name:      "bare host with trailing slash",
			serverURL: "http://foo.example:8082/",
			want:      "http://foo.example:8082/tpm-attestation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := attestationEndpointURL(tc.serverURL)
			if got != tc.want {
				t.Errorf("attestationEndpointURL(%q) = %q, want %q", tc.serverURL, got, tc.want)
			}
		})
	}
}
