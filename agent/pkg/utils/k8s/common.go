package k8s

import (
	"os"

	sdkFS "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

// GetHostDirForK8s returns the base dir where the running system is
// mounted. Under Kubernetes the agent usually runs in a container with the
// node's root mounted at HOST_DIR (/host by default, as kairos-operator and
// system-upgrade-controller mount it), so the node's configs and
// kairos-release live under that dir and the plain paths are the
// container's own.
//
// It returns an empty string outside Kubernetes, and also when the dir does
// not exist. A pod that runs `chroot /host kairos-agent ...`, as the
// documented Trusted Boot upgrade does, keeps its environment, but inside
// the chroot the node's files are at the plain paths and there is no
// HOST_DIR. An empty string leaves any filepath.Join unchanged.
func GetHostDirForK8s(fs sdkFS.KairosFS) string {
	if _, underKubernetes := os.LookupEnv("KUBERNETES_SERVICE_HOST"); !underKubernetes {
		return ""
	}
	hostDir := os.Getenv("HOST_DIR")
	if hostDir == "" {
		hostDir = "/host"
	}
	if info, err := fs.Stat(hostDir); err != nil || !info.IsDir() {
		return ""
	}
	return hostDir
}
