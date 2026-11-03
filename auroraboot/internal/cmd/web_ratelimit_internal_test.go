package cmd

import (
	"testing"

	"github.com/urfave/cli/v2"
)

// TestWebRateLimitBurstFlags drives the real WebCMD flag set, because the bug
// this covers was that server.Config.NodeRateLimitBurst and
// RegisterRateLimitBurst were wired into the limiters but reachable from no
// flag, so they were always 0 in production. Parsing WebCMD.Flags rather than a
// hand-built flag set is what makes a dropped flag fail here.
func TestWebRateLimitBurstFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantNodeBurst int
		wantRegBurst  int
	}{
		{name: "unset -> 0, so the limiters apply their own default", args: []string{"web"}},
		{
			name:          "both set",
			args:          []string{"web", "--node-rate-limit-burst", "7", "--register-rate-limit-burst", "3"},
			wantNodeBurst: 7,
			wantRegBurst:  3,
		},
		{
			name:          "only the node burst set",
			args:          []string{"web", "--node-rate-limit-burst", "11"},
			wantNodeBurst: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotNode, gotReg int
			app := &cli.App{
				Commands: []*cli.Command{{
					Name:  "web",
					Flags: WebCMD.Flags,
					Action: func(c *cli.Context) error {
						gotNode = c.Int("node-rate-limit-burst")
						gotReg = c.Int("register-rate-limit-burst")
						return nil
					},
				}},
			}
			if err := app.Run(append([]string{"auroraboot"}, tt.args...)); err != nil {
				t.Fatalf("parsing %v: %v", tt.args, err)
			}
			if gotNode != tt.wantNodeBurst {
				t.Errorf("node-rate-limit-burst = %d, want %d", gotNode, tt.wantNodeBurst)
			}
			if gotReg != tt.wantRegBurst {
				t.Errorf("register-rate-limit-burst = %d, want %d", gotReg, tt.wantRegBurst)
			}
		})
	}
}
