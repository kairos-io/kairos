package certs

import (
	"crypto/x509"
	"crypto/x509/pkix"
)

// CertList provides a list of certs on the system from the Efivars and properly parsed
type CertList struct {
	PK  []CertDetail
	KEK []CertDetail
	DB  []CertDetail
}

// CertListFull provides a list of FULL certs, including raw cert data
type CertListFull struct {
	PK  []*x509.Certificate
	KEK []*x509.Certificate
	DB  []*x509.Certificate
}

type CertDetail struct {
	Owner  pkix.Name
	Issuer pkix.Name
}

// EfiCerts is a simplified version of a CertList which only provides the Common names for the certs
type EfiCerts struct {
	PK  []string
	KEK []string
	DB  []string
}
