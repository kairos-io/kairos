package hook

import "testing"

func TestOemFileName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "bare name gets the yaml extension",
			input:    "foo",
			expected: "foo.yaml",
		},
		{
			name:     "an explicit .yaml extension is left alone",
			input:    "foo.yaml",
			expected: "foo.yaml",
		},
		{
			name:     "an explicit .yml extension is left alone",
			input:    "foo.yml",
			expected: "foo.yml",
		},
		{
			name:     "a name that merely contains yaml is not treated as having the extension",
			input:    "foo.yaml.bak",
			expected: "foo.yaml.bak.yaml",
		},
		{
			name:    "empty name is rejected",
			input:   "",
			wantErr: true,
		},
		{
			name:    "dot is rejected",
			input:   ".",
			wantErr: true,
		},
		{
			name:    "dot-dot is rejected",
			input:   "..",
			wantErr: true,
		},
		{
			name:    "a path with a separator is rejected",
			input:   "sub/foo",
			wantErr: true,
		},
		{
			name:    "an absolute path is rejected",
			input:   "/etc/passwd",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := oemFileName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("oemFileName(%q) error = nil, want an error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("oemFileName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Fatalf("oemFileName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
