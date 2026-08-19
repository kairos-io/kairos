/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PCRValues represents Platform Configuration Register values for boot state verification
// Uses a flexible map where keys are PCR indices (as strings) and values are hex-encoded PCR values
type PCRValues struct {
	// PCRs is a flexible map of PCR index (as string) to PCR value (hex-encoded)
	// Example: {"0": "a1b2c3...", "7": "d4e5f6...", "11": "g7h8i9..."}
	// This allows for any combination of PCRs without hardcoding specific indices
	PCRs map[string]string `json:"pcrs,omitempty"`
}

// AttestationSpec defines TPM attestation data for TOFU (Trust On First Use)
// https://en.wikipedia.org/wiki/Trust_on_first_use
// enrollment and verification with transient AK approach,
// only the EK is stored as the trusted identity
type AttestationSpec struct {
	// EKPublicKey stores the Endorsement Key public key in PEM format
	// This is the single trusted identity for the TPM
	EKPublicKey string `json:"ekPublicKey,omitempty"`

	// PCRValues stores the expected PCR values for boot state verification
	PCRValues *PCRValues `json:"pcrValues,omitempty"`

	// EnrolledAt timestamp when this TPM was first enrolled
	EnrolledAt *metav1.Time `json:"enrolledAt,omitempty"`

	// LastVerifiedAt timestamp of the last successful attestation
	LastVerifiedAt *metav1.Time `json:"lastVerifiedAt,omitempty"`
}

// SealedVolumeSpec defines the desired state of SealedVolume
type SealedVolumeSpec struct {
	TPMHash     string           `json:"TPMHash,omitempty"`
	Partitions  []PartitionSpec  `json:"partitions,omitempty"`
	Quarantined bool             `json:"quarantined,omitempty"`
	Attestation *AttestationSpec `json:"attestation,omitempty"`
}

// PartitionSpec defines a Partition. A partition can be identified using
// any of the fields: Label, DeviceName, UUID. The Secret defines the secret
// which decrypts the partition.
type PartitionSpec struct {
	Label      string      `json:"label,omitempty"`
	DeviceName string      `json:"deviceName,omitempty"`
	UUID       string      `json:"uuid,omitempty"`
	Secret     *SecretSpec `json:"secret,omitempty"`
}

type SecretSpec struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SealedVolumeStatus defines the observed state of SealedVolume
type SealedVolumeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SealedVolume is the Schema for the sealedvolumes API
type SealedVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SealedVolumeSpec   `json:"spec,omitempty"`
	Status SealedVolumeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SealedVolumeList contains a list of SealedVolume
type SealedVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SealedVolume `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SealedVolume{}, &SealedVolumeList{})
}
