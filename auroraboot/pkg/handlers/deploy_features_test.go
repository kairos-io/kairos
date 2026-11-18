package handlers

import (
	"reflect"
	"testing"
)

// TestSortedFeatures verifies the inspect-response feature projection: only
// present (true) features are returned, in a stable sorted order, so the JSON
// response is deterministic.
func TestSortedFeatures(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]bool
		want []string
	}{
		{name: "nil map", in: nil, want: []string{}},
		{name: "empty map", in: map[string]bool{}, want: []string{}},
		{
			name: "sorted and present-only",
			in:   map[string]bool{"UEFI": true, "SecureBoot": true, "Legacy": false},
			want: []string{"SecureBoot", "UEFI"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedFeatures(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("sortedFeatures(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
