package e2e_test

import "testing"

// The warning that made run 39292052264 fail arrived after the output of the
// command, not before it, so the cases below put it on both sides.
func TestWithoutSudoDiagnostics(t *testing.T) {
	for name, tc := range map[string]struct {
		out  string
		want string
	}{
		"warning after the output": {
			out:  "ext4\nsudo: unable to resolve host kairos: Temporary failure in name resolution\n",
			want: "ext4\n",
		},
		"warning before the output": {
			out:  "sudo: unable to resolve host kairos: Temporary failure in name resolution\next4\n",
			want: "ext4\n",
		},
		"output with no warning is untouched": {
			out:  "ext4\n",
			want: "ext4\n",
		},
		"a line that only mentions sudo is kept": {
			out:  "[    0.1] kernel: sudo: not a prefix\n",
			want: "[    0.1] kernel: sudo: not a prefix\n",
		},
		"the output of the command keeps its own blank lines": {
			out:  "boot: active_boot\n\nsudo: unable to resolve host kairos\noem: /oem\n",
			want: "boot: active_boot\n\noem: /oem\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := withoutSudoDiagnostics(tc.out); got != tc.want {
				t.Errorf("withoutSudoDiagnostics(%q) = %q, want %q", tc.out, got, tc.want)
			}
		})
	}
}
