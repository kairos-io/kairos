package challenger

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-attestation/attest"

	keyserverv1alpha1 "github.com/kairos-io/kairos-challenger/api/v1alpha1"

	"github.com/kairos-io/kairos-challenger/controllers"
	"github.com/kairos-io/kairos-challenger/pkg/attestation"
	"github.com/kairos-io/kairos-challenger/pkg/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gorilla/websocket"
)

// PassphraseRequestData is a struct that holds all the information needed in
// order to lookup a passphrase for a specific tpm hash.
type PassphraseRequestData struct {
	TPMHash    string
	Label      string
	DeviceName string
	UUID       string
}

// ChallengerAttestator implements the attestation.Attestator interface
// and handles selective enrollment logic for the kcrypt-challenger
type ChallengerAttestator struct {
	reconciler *controllers.SealedVolumeReconciler
	kclient    *kubernetes.Clientset
	namespace  string
	logger     logr.Logger
	partition  PartitionInfo // Store partition info from WebSocket headers
}

// NewChallengerAttestator creates a new ChallengerAttestator
func NewChallengerAttestator(reconciler *controllers.SealedVolumeReconciler, kclient *kubernetes.Clientset, namespace string, logger logr.Logger, partition PartitionInfo) *ChallengerAttestator {
	return &ChallengerAttestator{
		reconciler: reconciler,
		kclient:    kclient,
		namespace:  namespace,
		logger:     logger,
		partition:  partition,
	}
}

// IssuePassphrase implements the attestation.Attestator interface
// This method handles the selective enrollment logic and returns the passphrase
func (ca *ChallengerAttestator) IssuePassphrase(ctx context.Context, req attestation.AttestationRequest) ([]byte, error) {
	// Convert PCRs from map[int][]byte to the format expected by the existing logic
	pcrValues := &keyserverv1alpha1.PCRValues{
		PCRs: make(map[string]string),
	}
	for pcrIndex, pcrValue := range req.PCRs {
		pcrValues.PCRs[fmt.Sprintf("%d", pcrIndex)] = fmt.Sprintf("%x", pcrValue)
	}

	// Parse EK from EKPEM
	ek, err := parseEKFromPEM(req.EKPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing EK from EKPEM: %w", err)
	}

	// Use partition info from WebSocket headers
	partition := ca.partition

	// Determine enrollment context
	enrollmentContext, err := determineEnrollmentContext(ca.reconciler, ca.namespace, req.TPMHash, partition, ca.logger)
	if err != nil {
		return nil, fmt.Errorf("determining enrollment context: %w", err)
	}

	// Handle DeferPCREnrollment flag for livecd mode
	// Security rule: Only allow PCR deferral if PCRs are not already set
	shouldDeferPCRs := false
	if req.DeferPCREnrollment {
		if enrollmentContext.IsNewEnrollment {
			// New enrollment - allow PCR deferral
			shouldDeferPCRs = true
			ca.logger.Info("LiveCD mode: Deferring PCR enrollment for new TPM", "tpmHash", req.TPMHash[:8])
		} else if enrollmentContext.SealedVolume != nil && enrollmentContext.SealedVolume.Spec.Attestation != nil {
			// Check if existing PCRs are all empty (re-enrollment mode)
			allPCRsEmpty := true
			if enrollmentContext.SealedVolume.Spec.Attestation.PCRValues != nil &&
				enrollmentContext.SealedVolume.Spec.Attestation.PCRValues.PCRs != nil {
				for _, pcrValue := range enrollmentContext.SealedVolume.Spec.Attestation.PCRValues.PCRs {
					if pcrValue != "" {
						allPCRsEmpty = false
						break
					}
				}
			}

			if allPCRsEmpty {
				shouldDeferPCRs = true
				ca.logger.Info("LiveCD mode: Maintaining deferred PCR enrollment (all PCRs empty)", "tpmHash", req.TPMHash[:8])
			} else {
				ca.logger.Info("Security: Ignoring DeferPCREnrollment flag - PCRs already enrolled", "tpmHash", req.TPMHash[:8])
			}
		}
	}

	// If we should defer PCRs, replace actual PCR values with empty strings
	if shouldDeferPCRs {
		ca.logger.Info("Setting PCR values to empty strings for deferred enrollment", "originalPCRCount", len(pcrValues.PCRs))
		for pcrIndex := range pcrValues.PCRs {
			pcrValues.PCRs[pcrIndex] = ""
		}
	}

	// Check if TPM is quarantined
	if !enrollmentContext.IsNewEnrollment && enrollmentContext.VolumeData != nil && enrollmentContext.VolumeData.Quarantined {
		ca.logger.Info("TPM is quarantined - rejecting attestation", "tpmHash", req.TPMHash)
		return nil, fmt.Errorf("TPM is quarantined")
	}

	// Verify attestation data using selective enrollment
	if err := verifyAttestationData(enrollmentContext, &ClientAttestation{
		EK:        ek,
		PCRValues: pcrValues,
		PCRQuote:  nil, // Not used in enrollment (only PCR values are stored)
	}, ca.logger); err != nil {
		ca.logger.Info("Attestation verification failed", "error", err.Error())
		return nil, fmt.Errorf("attestation verification failed: %w", err)
	}

	// Handle enrollment: initial enrollment for new TPMs, new partitions, or re-enrollment updates for existing ones
	if enrollmentContext.IsNewEnrollment {
		// Perform initial TOFU enrollment for new TPMs
		if err := performInitialEnrollment(enrollmentContext, &ClientAttestation{
			EK:        ek,
			PCRValues: pcrValues,
			PCRQuote:  nil, // Not used in enrollment (only PCR values are stored)
		}, ca.reconciler, ca.kclient, ca.namespace, ca.logger); err != nil {
			return nil, fmt.Errorf("initial enrollment: %w", err)
		}
	} else if enrollmentContext.IsNewPartition {
		// This is a new partition for an existing TPM - add partition to existing SealedVolume
		if err := addPartitionToExistingVolume(enrollmentContext, ca.reconciler, ca.kclient, ca.namespace, ca.logger); err != nil {
			return nil, fmt.Errorf("adding new partition: %w", err)
		}
	} else {
		// Update attestation data for re-enrollment of existing TPMs
		if err := updateEnrollmentData(enrollmentContext, &ClientAttestation{
			EK:        ek,
			PCRValues: pcrValues,
			PCRQuote:  nil, // Not used in enrollment (only PCR values are stored)
		}, ca.reconciler, ca.kclient, ca.namespace, ca.logger); err != nil {
			return nil, fmt.Errorf("re-enrollment data update: %w", err)
		}
	}

	// Retrieve and return passphrase
	if enrollmentContext.VolumeData == nil {
		return nil, fmt.Errorf("no volume data available - enrollment may have failed")
	}

	// Get secret name and path from the enrolled volume data
	secretName, secretPath := enrollmentContext.VolumeData.DefaultSecret()
	ca.logger.Info("Retrieving passphrase", "secretName", secretName, "tpmHash", req.TPMHash[:8])

	// Retrieve the secret
	secret, err := ca.kclient.CoreV1().Secrets(ca.namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("retrieving secret: %w", err)
	}

	secretData, exists := secret.Data[secretPath]
	if !exists {
		return nil, fmt.Errorf("passphrase not found in secret at key: %s", secretPath)
	}

	ca.logger.Info("Passphrase retrieved successfully")
	return secretData, nil
}

type SealedVolumeData struct {
	Quarantined bool
	SecretName  string
	SecretPath  string

	PartitionLabel string
	VolumeName     string
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (s SealedVolumeData) DefaultSecret() (string, string) {
	secretName := fmt.Sprintf("%s-%s", s.VolumeName, s.PartitionLabel)
	secretPath := "passphrase"
	if s.SecretName != "" {
		secretName = s.SecretName
	}
	if s.SecretPath != "" {
		secretPath = s.SecretPath
	}
	return kube.SafeKubeName(secretName), kube.SafeKubeName(secretPath)
}

// generateTOFUPassphrase creates a cryptographically secure random passphrase for TOFU enrollment
func generateTOFUPassphrase() (string, error) {
	// Generate 32 random bytes (256 bits) for strong passphrase
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("generating random passphrase: %w", err)
	}

	// Encode as base64 for safe storage and transmission
	passphrase := base64.StdEncoding.EncodeToString(randomBytes)
	return passphrase, nil
}

// createOrReuseTOFUSecret creates a Kubernetes secret containing the generated passphrase
// If a secret with the same name already exists, it returns the existing passphrase
// Returns the passphrase that should be used (either new or existing)
func createOrReuseTOFUSecret(kclient *kubernetes.Clientset, namespace, secretName, secretPath, passphrase, tpmHash, partitionLabel string, logger logr.Logger) (string, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kcrypt-challenger",
				"app.kubernetes.io/component": "encryption-secret",
				"kcrypt.kairos.io/tpm-hash":   kube.SafeKubeName(tpmHash),
				"kcrypt.kairos.io/partition":  partitionLabel,
				"kcrypt.kairos.io/managed-by": "kcrypt-challenger", // Additional safety label
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			secretPath: []byte(passphrase),
		},
	}

	_, err := kclient.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Secret exists - this can happen when a SealedVolume was deleted but secret remained
			// Retrieve and return the existing passphrase
			logger.Info("Secret already exists, reusing existing secret", "secretName", secretName, "reason", "previous SealedVolume may have been deleted")

			existingSecret, getErr := kclient.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if getErr != nil {
				return "", fmt.Errorf("retrieving existing secret: %w", getErr)
			}

			existingPassphrase, exists := existingSecret.Data[secretPath]
			if !exists || len(existingPassphrase) == 0 {
				return "", fmt.Errorf("existing secret does not contain expected passphrase data at path %s", secretPath)
			}

			logger.Info("Successfully retrieved passphrase from existing secret", "secretName", secretName)
			return string(existingPassphrase), nil
		}
		return "", fmt.Errorf("creating TOFU secret: %w", err)
	}

	logger.Info("Successfully created new TOFU secret", "secretName", secretName)
	return passphrase, nil
}

// createTOFUSealedVolumeWithAttestation creates a SealedVolume resource with pre-created attestation data
func createTOFUSealedVolumeWithAttestation(reconciler *controllers.SealedVolumeReconciler, namespace, tpmHash, secretName, secretPath string, partition PartitionInfo, attestation *keyserverv1alpha1.AttestationSpec) error {
	volumeName := kube.SafeKubeName(fmt.Sprintf("tofu-%s", tpmHash[:8]))

	sealedVolume := &keyserverv1alpha1.SealedVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volumeName,
			Namespace: namespace,
		},
		Spec: keyserverv1alpha1.SealedVolumeSpec{
			TPMHash: tpmHash,
			Partitions: []keyserverv1alpha1.PartitionSpec{
				{
					Label:      partition.Label,
					DeviceName: partition.DeviceName,
					UUID:       partition.UUID,
					Secret: &keyserverv1alpha1.SecretSpec{
						Name: secretName,
						Path: secretPath,
					},
				},
			},
			Quarantined: false,
			Attestation: attestation,
		},
	}

	return reconciler.Create(context.TODO(), sealedVolume)
}

// PartitionInfo holds partition identification data from client headers
type PartitionInfo struct {
	Label      string
	DeviceName string
	UUID       string
}

// ClientAttestation holds all client-provided attestation data
type ClientAttestation struct {
	EK        *attest.EK
	PCRQuote  []byte
	PCRValues *keyserverv1alpha1.PCRValues
}

// EnrollmentContext represents the current enrollment state
type EnrollmentContext struct {
	IsNewEnrollment bool
	IsNewPartition  bool
	SealedVolume    *keyserverv1alpha1.SealedVolume
	VolumeData      *SealedVolumeData
	TPMHash         string
	Partition       PartitionInfo
}

// encodeEKToPEM converts an attest.EK to PEM format for storage
func encodeEKToPEM(ek *attest.EK) (string, error) {
	if ek.Certificate != nil {
		pemBlock := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: ek.Certificate.Raw,
		}
		return string(pem.EncodeToMemory(pemBlock)), nil
	}

	data, err := pubBytesFromKey(ek.Public)
	if err != nil {
		return "", err
	}

	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: data,
	}
	return string(pem.EncodeToMemory(pemBlock)), nil
}

// pubBytesFromKey marshals a public key to DER format
func pubBytesFromKey(pub any) ([]byte, error) {
	data, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("error marshaling public key: %v", err)
	}
	return data, nil
}

// parseEKFromPEM parses EKPEM bytes into an attest.EK with populated Public field
func parseEKFromPEM(ekPEM []byte) (*attest.EK, error) {
	if len(ekPEM) == 0 {
		return nil, fmt.Errorf("EKPEM is empty")
	}

	// Try to parse as PEM first
	block, _ := pem.Decode(ekPEM)
	if block != nil {
		// It's PEM format
		switch block.Type {
		case "CERTIFICATE":
			// EK certificate
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsing EK certificate: %w", err)
			}
			return &attest.EK{
				Certificate: cert,
				Public:      cert.PublicKey,
			}, nil
		case "PUBLIC KEY":
			// EK public key
			pub, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parsing EK public key: %w", err)
			}
			return &attest.EK{Public: pub}, nil
		}
	}

	// Try to parse as raw DER
	pub, err := x509.ParsePKIXPublicKey(ekPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing EK as DER: %w", err)
	}
	return &attest.EK{Public: pub}, nil
}

func Start(ctx context.Context, logger logr.Logger, kclient *kubernetes.Clientset, reconciler *controllers.SealedVolumeReconciler, namespace, address string) {
	logger.Info("Challenger started", "address", address)
	s := http.Server{
		Addr:         address,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	m := http.NewServeMux()

	// TPM Attestation WebSocket endpoint
	m.HandleFunc("/tpm-attestation", func(w http.ResponseWriter, r *http.Request) {
		handleTPMAttestation(w, r, logger, reconciler, kclient, namespace)
	})

	s.Handler = logRequestHandler(logger, m)

	go func() {
		err := s.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = s.Shutdown(ctx)
	}()
}

func findVolumeFor(requestData PassphraseRequestData, volumeList *keyserverv1alpha1.SealedVolumeList) (*SealedVolumeData, *keyserverv1alpha1.SealedVolume) {
	for _, v := range volumeList.Items {
		if requestData.TPMHash == v.Spec.TPMHash {
			for _, p := range v.Spec.Partitions {
				deviceNameMatches := requestData.DeviceName != "" && p.DeviceName == requestData.DeviceName
				uuidMatches := requestData.UUID != "" && p.UUID == requestData.UUID
				labelMatches := requestData.Label != "" && p.Label == requestData.Label

				secretName := ""
				if p.Secret != nil && p.Secret.Name != "" {
					secretName = p.Secret.Name
				}
				secretPath := ""
				if p.Secret != nil && p.Secret.Path != "" {
					secretPath = p.Secret.Path
				}
				if labelMatches || uuidMatches || deviceNameMatches {
					volumeData := &SealedVolumeData{
						Quarantined:    v.Spec.Quarantined,
						SecretName:     secretName,
						SecretPath:     secretPath,
						VolumeName:     v.Name,
						PartitionLabel: p.Label,
					}
					return volumeData, &v
				}
			}
		}
	}

	return nil, nil
}

// errorMessage should be used when an error should be both, printed to the stdout
// and sent over the wire to the websocket client.
func errorMessage(conn *websocket.Conn, logger logr.Logger, theErr error, description string) {
	if theErr == nil {
		return
	}
	logger.Error(theErr, description)

	sendErrorResponse(conn, logger, fmt.Sprintf("%s: %v", description, theErr))
}

// securityRejection should be used for security-related rejections (PCR mismatches, quarantine, etc.)
// These are logged as INFO since they're expected security behavior, not application errors
func securityRejection(conn *websocket.Conn, logger logr.Logger, reason string, details string) {
	logger.Info("Security verification failed - rejecting attestation", "reason", reason, "details", details)
	sendErrorResponse(conn, logger, fmt.Sprintf("%s: %s", reason, details))
}

// sendErrorResponse sends an error response to the client with the error message
func sendErrorResponse(conn *websocket.Conn, logger logr.Logger, errorMsg string) {
	// Send error response as JSON so client can log the specific error
	response := attestation.AttestationResponse{
		Error: errorMsg,
	}

	// Send as JSON using WriteJSON helper (sends as text message)
	if err := conn.WriteJSON(response); err != nil {
		logger.Error(err, "Failed to send error response to client")
	}

	// Also close the connection to signal error condition
	_ = conn.Close()
}

func logRequestHandler(logger logr.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Incoming request", "method", r.Method, "uri", r.URL.String(),
			"referer", r.Header.Get("Referer"), "userAgent", r.Header.Get("User-Agent"))

		h.ServeHTTP(w, r)
	})
}

// handleTPMAttestation handles the complete TPM attestation flow over a WebSocket connection.
// It performs challenge-response authentication and issues passphrases based on selective enrollment policies.
func handleTPMAttestation(w http.ResponseWriter, r *http.Request, logger logr.Logger, reconciler *controllers.SealedVolumeReconciler, kclient *kubernetes.Clientset, namespace string) {
	// 1. Establish secure connection
	conn, partition, err := establishAttestationConnection(w, r, logger)
	if err != nil {
		return // Error already logged and handled
	}
	defer conn.Close()

	// 2. Create attestation server with ChallengerAttestator
	attestator := NewChallengerAttestator(reconciler, kclient, namespace, logger, partition)
	attestationServer := attestation.NewRemoteAttestationServer(attestator)

	// 3. Perform attestation flow
	// Protocol Step 1: Receive attestation init from client
	logger.Info("Waiting for attestation init from client")
	var init attestation.AttestationInit
	if err := conn.ReadJSON(&init); err != nil {
		errorMessage(conn, logger, fmt.Errorf("reading attestation init: %w", err), "WebSocket read")
		return
	}
	logger.Info("Received attestation init from client")

	// Protocol Step 2: Generate and send challenge
	logger.Info("Generating attestation challenge")
	challenge, secret, err := attestationServer.GenerateChallenge(&init)
	if err != nil {
		errorMessage(conn, logger, fmt.Errorf("generating challenge: %w", err), "Challenge generation")
		return
	}

	logger.Info("Sending challenge to client")
	if err := conn.WriteJSON(challenge); err != nil {
		errorMessage(conn, logger, fmt.Errorf("sending challenge: %w", err), "Challenge send")
		return
	}

	// Protocol Step 3: Receive proof from client
	logger.Info("Waiting for proof from client")
	var proof attestation.AttestationProof
	if err := conn.ReadJSON(&proof); err != nil {
		errorMessage(conn, logger, fmt.Errorf("reading proof: %w", err), "Proof read")
		return
	}
	logger.Info("Received proof from client")

	// Protocol Step 4: Verify proof and issue passphrase
	logger.Info("Verifying proof and issuing passphrase")
	passphrase, err := attestationServer.IssuePassphrase(context.Background(), &init, &proof, secret)
	if err != nil {
		securityRejection(conn, logger, "Attestation verification failed", err.Error())
		return
	}

	// Protocol Step 5: Send passphrase to client
	logger.Info("Sending passphrase to client")
	response := attestation.AttestationResponse{
		Passphrase: passphrase,
	}

	// Send as JSON using WriteJSON helper (sends as text message)
	if err := conn.WriteJSON(response); err != nil {
		errorMessage(conn, logger, fmt.Errorf("sending passphrase: %w", err), "Passphrase send")
		return
	}

	logger.Info("TPM attestation completed successfully")
}

// performInitialEnrollment creates TOFU enrollment for new TPMs
func performInitialEnrollment(ctx *EnrollmentContext, attestation *ClientAttestation, reconciler *controllers.SealedVolumeReconciler, kclient *kubernetes.Clientset, namespace string, logger logr.Logger) error {
	logger.Info("Creating new TOFU enrollment")

	// Generate secret name and path for new enrollment using DefaultSecret logic
	safeVolumeName := kube.SafeKubeName(fmt.Sprintf("tofu-%s", ctx.TPMHash[:8]))
	volumeData := SealedVolumeData{
		PartitionLabel: ctx.Partition.Label,
		VolumeName:     safeVolumeName,
	}
	secretName, secretPath := volumeData.DefaultSecret()

	// Generate secure passphrase for new enrollment
	passphrase, err := generateTOFUPassphrase()
	if err != nil {
		return fmt.Errorf("generating TOFU passphrase: %w", err)
	}

	// Create Kubernetes secret (or reuse if it already exists from a previous enrollment)
	logger.Info("Creating TOFU secret", "secretName", secretName, "secretPath", secretPath)
	actualPassphrase, err := createOrReuseTOFUSecret(kclient, namespace, secretName, secretPath, passphrase, ctx.TPMHash, ctx.Partition.Label, logger)
	if err != nil {
		return fmt.Errorf("creating TOFU secret: %w", err)
	}

	// Create attestation data using initial TOFU logic (stores ALL provided PCRs)
	attestationSpec := createInitialTOFUAttestation(attestation.PCRValues, logger)

	// Extract EK in PEM format for storage
	ekPEM, err := encodeEKToPEM(attestation.EK)
	if err != nil {
		return fmt.Errorf("encoding EK to PEM: %w", err)
	}
	attestationSpec.EKPublicKey = ekPEM

	// Create SealedVolume resource for future attestations
	if err := createTOFUSealedVolumeWithAttestation(reconciler, namespace, ctx.TPMHash, secretName, secretPath, ctx.Partition, attestationSpec); err != nil {
		return fmt.Errorf("creating TOFU SealedVolume: %w", err)
	}

	// Update the enrollment context with volume data for passphrase retrieval
	ctx.VolumeData = &SealedVolumeData{
		Quarantined:    false,
		SecretName:     secretName,
		SecretPath:     secretPath,
		VolumeName:     volumeData.VolumeName,
		PartitionLabel: volumeData.PartitionLabel,
	}

	logger.Info("TOFU enrollment completed", "secretName", secretName, "secretPath", secretPath, "passphraseSource", func() string {
		if actualPassphrase == passphrase {
			return "newly_generated"
		}
		return "reused_existing"
	}())
	return nil
}

// addPartitionToExistingVolume adds a new partition to an existing SealedVolume
func addPartitionToExistingVolume(ctx *EnrollmentContext, reconciler *controllers.SealedVolumeReconciler, kclient *kubernetes.Clientset, namespace string, logger logr.Logger) error {
	logger.Info("Adding new partition to existing SealedVolume")

	// Generate secret name and path for the new partition using DefaultSecret logic
	secretName, secretPath := ctx.VolumeData.DefaultSecret()

	// Generate secure passphrase for the new partition
	passphrase, err := generateTOFUPassphrase()
	if err != nil {
		return fmt.Errorf("generating TOFU passphrase: %w", err)
	}

	// Create Kubernetes secret for the new partition
	logger.Info("Creating TOFU secret for new partition", "secretName", secretName, "secretPath", secretPath)
	actualPassphrase, err := createOrReuseTOFUSecret(kclient, namespace, secretName, secretPath, passphrase, ctx.TPMHash, ctx.Partition.Label, logger)
	if err != nil {
		return fmt.Errorf("creating TOFU secret: %w", err)
	}

	// Add the new partition to the existing SealedVolume
	newPartition := keyserverv1alpha1.PartitionSpec{
		Label:      ctx.Partition.Label,
		DeviceName: ctx.Partition.DeviceName,
		UUID:       ctx.Partition.UUID,
		Secret: &keyserverv1alpha1.SecretSpec{
			Name: secretName,
			Path: secretPath,
		},
	}

	// Add the new partition to the existing SealedVolume
	ctx.SealedVolume.Spec.Partitions = append(ctx.SealedVolume.Spec.Partitions, newPartition)

	// Update the SealedVolume resource
	if err := reconciler.Update(context.TODO(), ctx.SealedVolume); err != nil {
		return fmt.Errorf("updating SealedVolume with new partition: %w", err)
	}

	// Update the enrollment context with volume data for passphrase retrieval
	ctx.VolumeData = &SealedVolumeData{
		Quarantined:    ctx.SealedVolume.Spec.Quarantined,
		SecretName:     secretName,
		SecretPath:     secretPath,
		VolumeName:     ctx.SealedVolume.Name,
		PartitionLabel: ctx.Partition.Label,
	}

	logger.Info("Successfully added new partition to existing SealedVolume",
		"secretName", secretName,
		"secretPath", secretPath,
		"partitionLabel", ctx.Partition.Label,
		"passphraseSource", func() string {
			if actualPassphrase == passphrase {
				return "newly_generated"
			}
			return "reused_existing"
		}())

	return nil
}

// verifyAttestationData verifies EK and PCR data using selective enrollment (transient AK approach)
func verifyAttestationData(ctx *EnrollmentContext, attestation *ClientAttestation, logger logr.Logger) error {
	// Skip verification for new enrollments (TOFU - Trust On First Use)
	if ctx.IsNewEnrollment {
		logger.Info("New enrollment - skipping attestation verification (TOFU)")
		return nil
	}

	// Also skip verification if SealedVolume exists but has no attestation data
	// This supports two scenarios:
	// 1. Static passphrase setup: Operator creates Secret + SealedVolume with TPM hash only,
	//    letting the system learn ALL attestation data (EK, AK, all PCRs) via TOFU
	// 2. Secret reuse: After deleting and recreating a SealedVolume, the system reuses the
	//    existing Secret and re-learns attestation data
	// Note: If operator wants selective PCR tracking, they should create Spec.Attestation
	// with specific PCRs (empty or set), and omit unwanted PCRs from the map
	if ctx.SealedVolume != nil && ctx.SealedVolume.Spec.Attestation == nil {
		logger.Info("SealedVolume exists but has no attestation data - treating as initial TOFU enrollment")
		return nil
	}

	// For existing enrollments, perform security verification
	logger.Info("Existing enrollment - performing security verification")

	// Verify EK public key matches the enrolled one using selective enrollment (transient AK approach)
	if err := verifyEKMatchSelective(ctx.SealedVolume, attestation.EK, logger); err != nil {
		logger.Info("EK verification failed - potential TPM impersonation attempt", "details", err.Error())
		return fmt.Errorf("EK verification failed: %w", err)
	}

	// Note: AK certification is already verified through the challenge-response mechanism
	// The client must activate the credential using the transient AK, which proves the AK
	// is authentic and was generated by the trusted EK

	// Verify PCR values match the enrolled ones using selective enrollment (boot state verification)
	if attestation.PCRValues != nil {
		if ctx.SealedVolume.Spec.Attestation != nil {
			if err := verifyPCRValuesSelective(ctx.SealedVolume.Spec.Attestation.PCRValues, attestation.PCRValues, logger); err != nil {
				logger.Info("PCR verification failed - boot state changed", "details", err.Error())
				return fmt.Errorf("PCR verification failed: %w", err)
			}
		} else {
			logger.Info("No stored attestation data for PCR verification - accepting current PCRs")
		}
	} else {
		logger.Info("No PCR data provided by client")
	}

	logger.Info("All attestation data verification successful")
	return nil
}

// updateEnrollmentData updates attestation data for re-enrollment of existing TPMs
func updateEnrollmentData(ctx *EnrollmentContext, attestation *ClientAttestation, reconciler *controllers.SealedVolumeReconciler, kclient *kubernetes.Clientset, namespace string, logger logr.Logger) error {
	// If no attestation data exists, create initial TOFU attestation
	// This handles the case where an operator creates a SealedVolume without attestation data
	// (e.g., for static passphrase setup or after SealedVolume recreation)
	if ctx.SealedVolume.Spec.Attestation == nil {
		logger.Info("No attestation data in SealedVolume - initializing TOFU attestation")

		// Check if we also need to create a secret (when partition has no secret reference)
		needsSecretCreation := ctx.VolumeData.SecretName == "" && ctx.VolumeData.SecretPath == ""

		if needsSecretCreation {
			// This is like a new enrollment - create secret + attestation
			// WARN: This is an unusual scenario - normally operators create SealedVolume without attestation
			// when they want to pre-define the passphrase (static secret). Creating both secret + attestation
			// is more of a "deferred TOFU" scenario (operator creates empty SealedVolume, lets system generate everything)
			logger.Info("WARNING: Unusual enrollment scenario detected - creating both secret and attestation",
				"scenario", "deferred-TOFU",
				"sealedVolume", ctx.SealedVolume.Name,
				"reason", "SealedVolume has no attestation AND no secret reference",
				"action", "auto-generating passphrase and learning all attestation data",
				"recommendation", "If you intended to use a static passphrase, pre-create a Secret and reference it in the partition spec")

			// Generate secret name and path using DefaultSecret logic
			volumeData := SealedVolumeData{
				PartitionLabel: ctx.Partition.Label,
				VolumeName:     ctx.SealedVolume.Name,
			}
			secretName, secretPath := volumeData.DefaultSecret()

			// Generate secure passphrase for enrollment
			passphrase, err := generateTOFUPassphrase()
			if err != nil {
				return fmt.Errorf("generating TOFU passphrase: %w", err)
			}

			// Create Kubernetes secret (or reuse if it already exists)
			logger.Info("Creating TOFU secret for SealedVolume without secret reference", "secretName", secretName, "secretPath", secretPath)
			_, err = createOrReuseTOFUSecret(kclient, namespace, secretName, secretPath, passphrase, ctx.TPMHash, ctx.Partition.Label, logger)
			if err != nil {
				return fmt.Errorf("creating TOFU secret: %w", err)
			}

			// Update the partition in SealedVolume to reference the new secret
			for i := range ctx.SealedVolume.Spec.Partitions {
				p := &ctx.SealedVolume.Spec.Partitions[i]
				if p.Label == ctx.Partition.Label {
					p.Secret = &keyserverv1alpha1.SecretSpec{
						Name: secretName,
						Path: secretPath,
					}
					break
				}
			}

			// Update VolumeData so the caller can retrieve the passphrase
			ctx.VolumeData.SecretName = secretName
			ctx.VolumeData.SecretPath = secretPath
		}

		// Create attestation data using initial TOFU logic (stores ALL provided PCRs)
		// Note: If operator wants selective PCR tracking, they should pre-create Spec.Attestation
		// with only desired PCRs, leaving unwanted PCRs omitted from the map
		attestationSpec := createInitialTOFUAttestation(attestation.PCRValues, logger)

		// Extract EK in PEM format for storage
		ekPEM, err := encodeEKToPEM(attestation.EK)
		if err != nil {
			return fmt.Errorf("encoding EK to PEM: %w", err)
		}
		attestationSpec.EKPublicKey = ekPEM

		// Update the SealedVolume with the new attestation data (and possibly secret reference)
		ctx.SealedVolume.Spec.Attestation = attestationSpec
		if err := reconciler.Update(context.TODO(), ctx.SealedVolume); err != nil {
			return fmt.Errorf("updating SealedVolume with initial attestation data: %w", err)
		}

		logger.Info("Successfully initialized attestation data for existing SealedVolume")
		return nil
	}

	// Update any re-enrollment mode fields (empty values)
	// Note: VolumeData should already be set by determineEnrollmentContext from the existing partition
	logger.Info("Updating attestation data for re-enrollment mode fields")

	if err := updateAttestationDataSelective(ctx.SealedVolume.Spec.Attestation, attestation, logger); err != nil {
		return fmt.Errorf("updating selective attestation data: %w", err)
	}

	// Update the SealedVolume resource if changes were made
	if err := reconciler.Update(context.TODO(), ctx.SealedVolume); err != nil {
		return fmt.Errorf("updating SealedVolume with new attestation data: %w", err)
	}

	logger.Info("Successfully updated attestation data")
	return nil
}

// updateLastVerificationTimestamp updates the last verification time for an existing SealedVolume
func updateLastVerificationTimestamp(reconciler *controllers.SealedVolumeReconciler, namespace, tpmHash string) error {
	// This would need to be implemented in the reconciler to update the LastVerifiedAt field
	// For now, we'll log that it should be updated
	// NOTE: Reconciler method to update verification timestamps needs implementation
	return nil
}

// verifyEKMatchSelective compares the current EK public key with the enrolled one using selective enrollment logic (transient AK approach)
func verifyEKMatchSelective(sealedVolume *keyserverv1alpha1.SealedVolume, currentEK *attest.EK, logger logr.Logger) error {
	// Get the stored EK from the SealedVolume's attestation spec
	if sealedVolume.Spec.Attestation == nil {
		return fmt.Errorf("no attestation data in SealedVolume for verification")
	}

	storedEKPEM := sealedVolume.Spec.Attestation.EKPublicKey

	// Empty stored EK = re-enrollment mode, accept any current EK
	if storedEKPEM == "" {
		logger.Info("EK re-enrollment mode: accepting any EK value")
		return nil
	}

	// Non-empty stored EK = enforcement mode, require exact match
	currentEKPEM, err := encodeEKToPEM(currentEK)
	if err != nil {
		return fmt.Errorf("encoding current EK to PEM: %w", err)
	}

	if storedEKPEM != currentEKPEM {
		logger.Info("EK mismatch detected in enforcement mode",
			"storedEKLength", len(storedEKPEM),
			"currentEKLength", len(currentEKPEM))
		return fmt.Errorf("EK public key does not match enrolled key - potential TPM impersonation")
	}

	logger.Info("EK verification successful - matches enrolled key")
	return nil
}

// verifyPCRValuesSelective compares current PCR values against stored expected values using selective enrollment logic
func verifyPCRValuesSelective(stored, current *keyserverv1alpha1.PCRValues, logger logr.Logger) error {
	// No stored values = accept any current values (first enrollment or no requirements)
	if stored == nil || stored.PCRs == nil {
		logger.Info("No expected PCR values stored, accepting current values")
		return nil
	}

	// No current values provided
	if current == nil || current.PCRs == nil {
		// Check if any stored PCRs are actually required (non-empty)
		for pcrIndex, storedValue := range stored.PCRs {
			if storedValue != "" {
				return fmt.Errorf("no current PCR values provided but PCR%s is required", pcrIndex)
			}
		}
		logger.Info("No current PCR values but all stored PCRs are in re-enrollment mode")
		return nil
	}

	// Compare each stored PCR value using selective enrollment logic
	for pcrIndex, storedValue := range stored.PCRs {
		if storedValue == "" {
			// Empty stored value = re-enrollment mode, accept any current value
			logger.V(1).Info("PCR re-enrollment mode", "pcr", pcrIndex)
			continue
		}

		// Non-empty stored value = enforcement mode, require exact match
		currentValue, exists := current.PCRs[pcrIndex]
		if !exists || currentValue == "" {
			return fmt.Errorf("PCR%s mismatch: expected %s, but not provided in current values", pcrIndex, storedValue)
		}

		if storedValue != currentValue {
			return fmt.Errorf("PCR%s changed - boot state verification failed: expected %s, got %s", pcrIndex, storedValue, currentValue)
		}

		logger.V(1).Info("PCR enforcement mode verification passed", "pcr", pcrIndex)
	}

	logger.Info("PCR verification successful using selective enrollment")
	return nil
}

// updateAttestationDataSelective updates empty attestation fields with current values during selective enrollment
// This handles both EK and PCR re-enrollment according to the selective enrollment policy:
// - Empty string ("") = re-enrollment mode, accept and store current value
// - Set value = enforcement mode, already verified to match
// - Omitted (nil/not in map) = skip entirely
func updateAttestationDataSelective(attestation *keyserverv1alpha1.AttestationSpec, clientAttestation *ClientAttestation, logger logr.Logger) error {
	updated := false

	// Update EK if it's in re-enrollment mode (empty string)
	if attestation.EKPublicKey == "" {
		ekPEM, err := encodeEKToPEM(clientAttestation.EK)
		if err != nil {
			return fmt.Errorf("encoding EK to PEM: %w", err)
		}
		attestation.EKPublicKey = ekPEM
		logger.Info("Updated EK during selective enrollment (was in re-enrollment mode)")
		updated = true
	}

	// Update PCR values if empty (re-enrollment mode)
	currentPCRs := clientAttestation.PCRValues
	if attestation.PCRValues != nil && currentPCRs != nil && currentPCRs.PCRs != nil {
		if attestation.PCRValues.PCRs == nil {
			attestation.PCRValues.PCRs = make(map[string]string)
		}

		for pcrIndex, currentValue := range currentPCRs.PCRs {
			// Only update if stored value exists AND is empty (re-enrollment mode)
			// AND the current value is not empty (skip deferred PCR updates)
			// Omitted PCRs (not in the map) should be skipped entirely per spec
			if storedValue, exists := attestation.PCRValues.PCRs[pcrIndex]; exists && storedValue == "" && currentValue != "" {
				attestation.PCRValues.PCRs[pcrIndex] = currentValue
				logger.Info("Updated PCR value during selective enrollment", "pcr", pcrIndex)
				updated = true
			}
		}
	}

	if updated {
		// Update timestamps
		now := metav1.Now()
		attestation.LastVerifiedAt = &now
		logger.Info("Selective enrollment update completed")
	}

	return nil
}

// createInitialTOFUAttestation creates attestation spec for initial TOFU enrollment, storing ALL provided PCRs
func createInitialTOFUAttestation(currentPCRs *keyserverv1alpha1.PCRValues, logger logr.Logger) *keyserverv1alpha1.AttestationSpec {
	currentTime := metav1.Now()

	attestation := &keyserverv1alpha1.AttestationSpec{
		EnrolledAt:     &currentTime,
		LastVerifiedAt: &currentTime,
	}

	// Store ALL provided PCRs without filtering
	if currentPCRs != nil && currentPCRs.PCRs != nil {
		attestation.PCRValues = &keyserverv1alpha1.PCRValues{
			PCRs: make(map[string]string),
		}

		// Copy all PCRs - don't filter any out
		maps.Copy(attestation.PCRValues.PCRs, currentPCRs.PCRs)

		logger.Info("Stored ALL PCR values for initial TOFU enrollment",
			"pcrCount", len(attestation.PCRValues.PCRs),
			"pcrs", attestation.PCRValues.PCRs)
	}

	return attestation
}

// establishAttestationConnection upgrades HTTP to WebSocket and extracts partition info
func establishAttestationConnection(w http.ResponseWriter, r *http.Request, logger logr.Logger) (*websocket.Conn, PartitionInfo, error) {
	logger.V(1).Info("Debug: Attempting to upgrade HTTP connection to WebSocket", "remoteAddr", r.RemoteAddr)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(err, "upgrading connection for TPM attestation")
		return nil, PartitionInfo{}, err
	}

	logger.Info("Starting TPM attestation WebSocket flow")

	// Get partition details from headers (sent by client)
	partition := PartitionInfo{
		Label:      r.Header.Get("label"),
		DeviceName: r.Header.Get("name"),
		UUID:       r.Header.Get("uuid"),
	}
	logger.Info("Partition details from client", "label", partition.Label, "name", partition.DeviceName, "uuid", partition.UUID)

	return conn, partition, nil
}

// findVolumeForTPM finds a SealedVolume by TPM hash, regardless of partition
func findVolumeForTPM(tpmHash string, volumeList *keyserverv1alpha1.SealedVolumeList) *keyserverv1alpha1.SealedVolume {
	for _, v := range volumeList.Items {
		if tpmHash == v.Spec.TPMHash {
			return &v
		}
	}
	return nil
}

// findPartitionInVolume checks if a specific partition exists in a SealedVolume
func findPartitionInVolume(volume *keyserverv1alpha1.SealedVolume, partition PartitionInfo) (*SealedVolumeData, bool) {
	for _, p := range volume.Spec.Partitions {
		deviceNameMatches := partition.DeviceName != "" && p.DeviceName == partition.DeviceName
		uuidMatches := partition.UUID != "" && p.UUID == partition.UUID
		labelMatches := partition.Label != "" && p.Label == partition.Label

		if labelMatches || uuidMatches || deviceNameMatches {
			secretName := ""
			if p.Secret != nil && p.Secret.Name != "" {
				secretName = p.Secret.Name
			}
			secretPath := ""
			if p.Secret != nil && p.Secret.Path != "" {
				secretPath = p.Secret.Path
			}
			volumeData := &SealedVolumeData{
				Quarantined:    volume.Spec.Quarantined,
				SecretName:     secretName,
				SecretPath:     secretPath,
				VolumeName:     volume.Name,
				PartitionLabel: p.Label,
			}
			return volumeData, true
		}
	}
	return nil, false
}

// determineEnrollmentContext checks for existing enrollment and creates context
func determineEnrollmentContext(reconciler *controllers.SealedVolumeReconciler, namespace, tpmHash string, partition PartitionInfo, logger logr.Logger) (*EnrollmentContext, error) {
	volumeList := &keyserverv1alpha1.SealedVolumeList{}
	err := reconciler.List(context.TODO(), volumeList, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("listing sealed volumes: %w", err)
	}

	// First, check if there's any SealedVolume for this TPM hash
	existingSealedVolume := findVolumeForTPM(tpmHash, volumeList)

	var existingVolume *SealedVolumeData
	var isNewEnrollment bool
	var isNewPartition bool

	if existingSealedVolume != nil {
		// TPM is already enrolled - check if this specific partition exists
		volumeData, partitionExists := findPartitionInVolume(existingSealedVolume, partition)
		if partitionExists {
			// This is an existing partition for an enrolled TPM
			existingVolume = volumeData
			isNewEnrollment = false
			isNewPartition = false
		} else {
			// This is a new partition for an enrolled TPM
			// Create basic volume data from the existing SealedVolume
			existingVolume = &SealedVolumeData{
				Quarantined:    existingSealedVolume.Spec.Quarantined,
				VolumeName:     existingSealedVolume.Name,
				PartitionLabel: partition.Label, // Use the new partition label
			}
			isNewEnrollment = false
			isNewPartition = true
		}
	} else {
		// No SealedVolume exists for this TPM - this is a completely new enrollment
		isNewEnrollment = true
		isNewPartition = false
	}

	logger.Info("Determined enrollment context",
		"isNewEnrollment", isNewEnrollment,
		"isNewPartition", isNewPartition,
		"tpmHash", tpmHash,
		"partitionLabel", partition.Label,
		"partitionDeviceName", partition.DeviceName,
		"partitionUUID", partition.UUID,
		"foundVolumes", len(volumeList.Items))

	return &EnrollmentContext{
		IsNewEnrollment: isNewEnrollment,
		IsNewPartition:  isNewPartition,
		SealedVolume:    existingSealedVolume,
		VolumeData:      existingVolume,
		TPMHash:         tpmHash,
		Partition:       partition,
	}, nil
}
