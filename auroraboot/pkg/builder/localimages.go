package builder

// DockerfileImage is the docker tag a local build gives the image it builds
// from a Dockerfile.
func DockerfileImage(id string) string { return "auroraboot-build:" + id }

// KairosifiedImage is the docker tag a local build gives the image it builds
// by kairosifying a base image.
func KairosifiedImage(id string) string { return "auroraboot-kairos:" + id }

// LocalBuildImages lists every docker tag a local build of id can leave in the
// docker daemon, whether or not the build finished.
func LocalBuildImages(id string) []string {
	return []string{DockerfileImage(id), KairosifiedImage(id)}
}
