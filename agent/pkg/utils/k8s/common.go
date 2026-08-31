package k8s

import "os"

// GetHostDirForK8s returns the base dir where the system is
// This is because under k8s, we mount the actual system under a dir
// So we need to know which paths we need to read the configs from
// if we read them from the root directly, we are actually reading the
// configs of the upgrade container
// If not found returns an empty string
func GetHostDirForK8s() string {
	_, underKubernetes := os.LookupEnv("KUBERNETES_SERVICE_HOST")
	// Try to get the HOST_DIR in case we are not using the default one
	hostDirEnv := os.Getenv("HOST_DIR")
	// If we are under kubernetes but the HOST_DIR var is empty, default to /host as system-upgrade-controller mounts
	// the host in that dir by default
	if underKubernetes {
		if hostDirEnv != "" {
			return hostDirEnv
		} else {
			return "/host"
		}
	} else {
		// We return an empty string so any filepath.join does not alter the paths
		return ""
	}
}
