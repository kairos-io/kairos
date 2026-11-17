package operator

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

const (
	cloudConfigSecretKey = "cloud-config"
	dockerfileSecretKey  = "Dockerfile"

	buildIDLabel = "auroraboot.io/build-id"
)

func cloudConfigSecretName(id string) string { return id + "-cloud-config" }
func dockerfileSecretName(id string) string  { return id + "-dockerfile" }

// materializeSecrets returns the Secret objects that must exist alongside the
// OSArtifact CR for the operator to consume its cloud-config, Dockerfile,
// and (when the exporter is wired) the AuroraBoot upload target + token.
// Namespace and owner references are left for the caller to stamp, since the
// OSArtifact must be created first to have a UID.
func materializeSecrets(id, namespace, uploadURL string, opts builder.BuildOptions) []corev1.Secret {
	var out []corev1.Secret
	labels := map[string]string{buildIDLabel: id}

	if opts.CloudConfig != "" {
		out = append(out, corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cloudConfigSecretName(id),
				Namespace: namespace,
				Labels:    labels,
			},
			Data: map[string][]byte{
				cloudConfigSecretKey: []byte(opts.CloudConfig),
			},
		})
	}
	if opts.Dockerfile != "" {
		out = append(out, corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dockerfileSecretName(id),
				Namespace: namespace,
				Labels:    labels,
			},
			Data: map[string][]byte{
				dockerfileSecretKey: []byte(opts.Dockerfile),
			},
		})
	}
	// Upload Secret: carries the AuroraBoot URL and per-build token that the
	// exporter Job reads via envFrom to PUT artifacts back to the AuroraBoot
	// upload endpoint. Only emit when both are populated - a caller building
	// without a return channel (e.g. unit tests) leaves them empty and gets
	// no upload wiring.
	if uploadURL != "" && opts.UploadToken != "" {
		out = append(out, corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      uploadSecretName(id),
				Namespace: namespace,
				Labels:    labels,
			},
			Data: map[string][]byte{
				uploadURLKey:   []byte(uploadURL),
				uploadTokenKey: []byte(opts.UploadToken),
			},
		})
	}
	return out
}
