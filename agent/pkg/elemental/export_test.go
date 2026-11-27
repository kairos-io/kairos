package elemental

import (
	"context"
	"os"

	"github.com/kairos-io/kairos/v4/agent/pkg/sshimage"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// SetPartLabelDevice replaces the /dev/disk/by-partlabel lookup used by
// PartitionAndFormatDevice and returns a restorer for the host lookup.
func SetPartLabelDevice(fn func(name string) (string, error)) func() {
	prev := partLabelDevice
	partLabelDevice = fn
	return func() { partLabelDevice = prev }
}

// SetFetchSSH replaces the SSH image fetch and returns a restorer.
func SetFetchSSH(fn func(ctx context.Context, o sshimage.Options, t sdkImages.SSHTarget, dst *os.File) (sshimage.Header, error)) func() {
	prev := fetchSSH
	fetchSSH = fn
	return func() { fetchSSH = prev }
}
