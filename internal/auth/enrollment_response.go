package auth

import (
	"errors"
	"strings"

	"arx-mdm/internal/pki"
)

// EnrollAPIResponse is the JSON body returned by POST /v1/enroll on success.
type EnrollAPIResponse struct {
	ClientCert string `json:"client_cert"`
	RootCA     string `json:"root_ca"`
}

// BuildEnrollAPIResponse maps embedded CA signing output to the wire contract.
func BuildEnrollAPIResponse(res *pki.IssueClientCertificateResult) (EnrollAPIResponse, error) {
	if res == nil {
		return EnrollAPIResponse{}, ErrEnrollmentSignEmpty
	}
	if strings.TrimSpace(res.ClientChainPEM) == "" || strings.TrimSpace(res.RootCAPEM) == "" {
		return EnrollAPIResponse{}, errors.New("auth: enrollment signing result missing PEM material")
	}
	return EnrollAPIResponse{
		ClientCert: res.ClientChainPEM,
		RootCA:     res.RootCAPEM,
	}, nil
}
