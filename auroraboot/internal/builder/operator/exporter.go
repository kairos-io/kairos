package operator

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// curlUploaderImage is the container image the exporter Job runs to PUT
	// each finished artifact to AuroraBoot's upload endpoint. Pinned to a
	// specific curl release so a distro repush cannot change the behaviour
	// underneath a build.
	curlUploaderImage = "curlimages/curl:8.15.0"

	// exporterBackoffLimit lets the exporter retry a few times before the
	// Job is marked failed, so a transient network blip between the cluster
	// and the AuroraBoot host does not lose an otherwise-successful build.
	exporterBackoffLimit int32 = 3

	// uploadURLKey and uploadTokenKey are the Secret data keys the exporter
	// container reads via envFrom. Names are uppercase to match the shell
	// convention the inline script assumes.
	uploadURLKey   = "AURORABOOT_URL"
	uploadTokenKey = "AURORABOOT_UPLOAD_TOKEN"
)

func uploadSecretName(id string) string { return id + "-upload" }

// uploadExporter returns the batchv1.JobSpec the operator's checkExport step
// runs to ship finished artifacts back to AuroraBoot. The exporter iterates
// every file under /artifacts and PUTs it to AuroraBoot's per-build upload
// endpoint, authenticated by the token minted in the Create handler and
// mounted via envFrom on the upload Secret.
//
// The operator itself injects the "artifacts" Volume (backed by the build's
// artifacts PVC, read-only); we only declare the VolumeMount so the container
// can see /artifacts.
func uploadExporter(id string) batchv1.JobSpec {
	backoff := exporterBackoffLimit
	return batchv1.JobSpec{
		BackoffLimit: &backoff,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{
						Name:    "upload-to-auroraboot",
						Image:   curlUploaderImage,
						Command: []string{"sh", "-ec"},
						Args: []string{`
for f in /artifacts/*; do
  [ -f "$f" ] || continue
  base=$(basename "$f")
  echo "Uploading $base to $AURORABOOT_URL"
  curl -fsS --retry 3 --connect-timeout 30 --max-time 3600 -X PUT \
    -H "Authorization: Bearer $AURORABOOT_UPLOAD_TOKEN" \
    --upload-file "$f" \
    "$AURORABOOT_URL/api/v1/artifacts/$BUILD_ID/upload/$base" || exit 1
done
echo "Upload done"
`},
						Env: []corev1.EnvVar{
							{Name: "BUILD_ID", Value: id},
						},
						EnvFrom: []corev1.EnvFromSource{
							{SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: uploadSecretName(id),
								},
							}},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "artifacts", MountPath: "/artifacts", ReadOnly: true},
						},
					},
				},
			},
		},
	}
}
