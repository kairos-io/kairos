package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/internal/version"
)

// stubSub replaces the run func of a registered sub for the length of the
// test and records the os.Args it was started with.
func stubSub(t *testing.T, name string) *[]string {
	t.Helper()
	orig, ok := subs[name]
	if !ok {
		t.Fatalf("%s is not a sub-tool; registered: %v", name, keys(subs))
	}
	var got []string
	subs[name] = func() int {
		got = append([]string{}, os.Args...)
		return 0
	}
	origArgs := os.Args
	t.Cleanup(func() {
		subs[name] = orig
		os.Args = origArgs
	})
	return &got
}

func TestSubToolVersionCommandReachesTheSub(t *testing.T) {
	for _, tc := range []struct {
		sub  string
		args []string
		want []string
	}{
		// AuroraBoot's install script runs `kairos-agent version` and
		// parses the bare version the agent prints.
		{"agent", []string{"/usr/bin/kairos-agent", "version"}, []string{"/usr/bin/kairos-agent", "version"}},
		{"agent", []string{"kairos-agent", "version", "--long"}, []string{"kairos-agent", "version", "--long"}},
		{"agent", []string{"kairos", "agent", "version", "--long"}, []string{"agent", "version", "--long"}},
		{"provider", []string{"kairos", "provider", "version"}, []string{"provider", "version"}},
		{"provider", []string{"kairos", "provider", "--version"}, []string{"provider", "--version"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			got := stubSub(t, tc.sub)
			var out bytes.Buffer

			if code := dispatch(tc.args, &out); code != 0 {
				t.Fatalf("exit code %d", code)
			}
			if out.Len() != 0 {
				t.Errorf("dispatcher answered %q itself", out.String())
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("%s ran with %q, want %q", tc.sub, *got, tc.want)
			}
		})
	}
}

// A sub without its own version command must not be started by a version
// query: immucore's default action is the boot flow.
func TestDispatcherAnswersVersionForSubsThatDoNotOwnIt(t *testing.T) {
	for _, args := range [][]string{
		{"kairos", "--version"},
		{"kairos", "version"},
		{"kairos", "-v"},
		{"immucore", "version"},
		{"immucore", "--version"},
		{"kairos", "immucore", "-v"},
		{"kairos", "kcrypt", "version"},
		// The agent has no --version flag, so the dispatcher keeps it.
		{"kairos-agent", "--version"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			ranImmucore := stubSub(t, "immucore")
			ranKcrypt := stubSub(t, "kcrypt")
			ranAgent := stubSub(t, "agent")
			var out bytes.Buffer

			if code := dispatch(args, &out); code != 0 {
				t.Fatalf("exit code %d", code)
			}
			if want := "kairos " + version.Version + "\n"; out.String() != want {
				t.Errorf("printed %q, want %q", out.String(), want)
			}
			if *ranImmucore != nil || *ranKcrypt != nil || *ranAgent != nil {
				t.Errorf("a sub was started: immucore=%q kcrypt=%q agent=%q", *ranImmucore, *ranKcrypt, *ranAgent)
			}
		})
	}
}
