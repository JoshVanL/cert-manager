/*
Copyright 2020 The cert-manager Authors.

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

package venafi

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/Venafi/vcert/v4/pkg/certificate"
	"github.com/Venafi/vcert/v4/pkg/endpoint"
	"github.com/Venafi/vcert/v4/pkg/venafi/fake"

	"github.com/jetstack/cert-manager/pkg/internal/venafi/api"
	internalfake "github.com/jetstack/cert-manager/pkg/internal/venafi/fake"
	"github.com/jetstack/cert-manager/pkg/util"
	"github.com/jetstack/cert-manager/pkg/util/pki"
)

func checkCertificateIssued(t *testing.T, csrPEM []byte, resp []byte) {
	if len(resp) == 0 {
		t.Errorf("expected IssueResponse to be non-nil")
		t.FailNow()
		return
	}

	csr, err := pki.DecodeX509CertificateRequestBytes(csrPEM)
	if err != nil {
		t.Errorf("failed to decode CSR PEM: %s", err)
		return
	}

	crt, err := pki.DecodeX509CertificateBytes(resp)
	if err != nil {
		t.Errorf("unable to decode x509 certificate: %v", err)
		return
	}

	ok, err := pki.PublicKeyMatchesCSR(crt.PublicKey, csr)
	if err != nil {
		t.Errorf("error checking private key: %v", err)
		return
	}
	if !ok {
		t.Errorf("private key does not match certificate")
	}

	// validate the common name is correct
	expectedCN := csr.Subject.CommonName
	if expectedCN != crt.Subject.CommonName {
		t.Errorf("expected common name to be %q but it was %q", expectedCN, crt.Subject.CommonName)
	}

	// validate the dns names are correct
	expectedDNSNames := csr.DNSNames
	if !util.EqualUnsorted(crt.DNSNames, expectedDNSNames) {
		t.Errorf("expected dns names to be %q but it was %q", expectedDNSNames, crt.DNSNames)
	}
}

func generateCSR(t *testing.T, sk crypto.Signer, commonName string, dnsNames []string) []byte {
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: commonName,
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
		DNSNames:           dnsNames,
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &template, sk)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})
	return csr
}

func TestVenafi_RequestCertificate(t *testing.T) {
	privateKey, err := pki.GenerateRSAPrivateKey(2048)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	type args struct {
		csrPEM       []byte
		customFields []api.CustomField
	}
	tests := []struct {
		name         string
		vcertClient  connector
		args         args
		wantPickupID bool
		wantErr      bool
	}{
		{
			name: "error if reading the zone configuration fails",
			args: args{},
			vcertClient: internalfake.Connector{
				ReadZoneConfigurationFunc: func() (*endpoint.ZoneConfiguration, error) {
					return nil, errors.New("zone configuration error")
				},
			}.Default(),
			wantErr: true,
		},
		{
			name: "error if validating the certificate fails",
			args: args{},
			vcertClient: internalfake.Connector{
				ReadZoneConfigurationFunc: func() (*endpoint.ZoneConfiguration, error) {
					return &endpoint.ZoneConfiguration{
						Policy: endpoint.Policy{
							SubjectCNRegexes: []string{"foo"},
						},
					}, nil
				},
			}.Default(),
			wantErr: true,
		},
		{
			name: "error if requesting the certificate fails",
			args: args{},
			vcertClient: internalfake.Connector{
				RequestCertificateFunc: func(*certificate.Request) (string, error) {
					return "", errors.New("request error")
				},
			}.Default(),
			wantErr: true,
		},
		{
			name: "error if a CSR with empty DN",
			args: args{
				csrPEM: generateCSR(t, privateKey, "", []string{"foo.example.com", "bar.example.com"}),
			},
			wantErr: true,
		},
		{
			name: "error if a badly formed CSR",
			args: args{
				csrPEM: []byte("a badly formed CSR"),
			},
			wantErr: true,
		},
		{
			name: "error if no Common Name, DNS Name, or URI SANs in CSR",
			args: args{
				csrPEM: generateCSR(t, privateKey, "", []string{}),
			},
			wantErr: true,
		},
		{
			name: "error if invalid custom field type found the error",
			args: args{
				customFields: []api.CustomField{{Name: "test", Value: "ok", Type: "Bool"}},
			},
			wantErr: true,
		},
		{
			name: "get a success for a certificate with DNS names and CN specified",
			args: args{
				csrPEM: generateCSR(t, privateKey, "common-name", []string{"foo.example.com", "bar.example.com"}),
			},
			wantPickupID: true,
			wantErr:      false,
		},
		{
			name: "get a success for a certificate with custom fields specified",
			args: args{
				customFields: []api.CustomField{{Name: "test", Value: "ok"}},
			},
			vcertClient: internalfake.Connector{
				RetrieveCertificateFunc: func(r *certificate.Request) (*certificate.PEMCollection, error) {
					// we set 1 field by default
					if len(r.CustomFields) <= 1 {
						return nil, errors.New("custom fields not set")
					}
					foundFields := false
					for _, fieldSet := range r.CustomFields {
						if fieldSet.Name != "test" && fieldSet.Value != "ok" {
							foundFields = true
						}
					}
					if !foundFields {
						return nil, errors.New("custom fields content not correct")
					}
					return internalfake.Connector{}.Default().RetrieveCertificate(r) // hack to return to normal
				},
			}.Default(),
			wantPickupID: true,
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.vcertClient == nil {
				tt.vcertClient = fake.NewConnector(true, nil)
			}
			v := &Venafi{
				vcertClient: tt.vcertClient,
			}

			if tt.args.csrPEM == nil {
				tt.args.csrPEM = generateCSR(t, privateKey, "common-name", []string{
					"foo.example.com", "bar.example.com"})
			}

			got, err := v.RequestCertificate(tt.args.csrPEM, time.Minute, tt.args.customFields)
			if (err != nil) != tt.wantErr {
				t.Errorf("RequestCertificate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if (got != "") != tt.wantPickupID {
				t.Errorf("RequestCertificate() got = %v, want empty string", got)
			}
		})
	}
}

func TestVenafi_RetrieveCertificate(t *testing.T) {
	privateKey, err := pki.GenerateRSAPrivateKey(2048)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	type args struct {
		csrPEM       []byte
		duration     time.Duration
		customFields []api.CustomField
	}
	tests := []struct {
		name        string
		vcertClient connector
		args        args
		wantErr     bool
		checkFn     func(*testing.T, []byte, []byte)
	}{
		{
			name: "error if retrieve certificate fails",
			vcertClient: internalfake.Connector{
				RetrieveCertificateFunc: func(*certificate.Request) (*certificate.PEMCollection, error) {
					return nil, errors.New("request error")
				},
			}.Default(),
			args:    args{},
			wantErr: true,
		},
		{
			name:    "successfully retrieve certificate",
			args:    args{},
			wantErr: false,
			checkFn: checkCertificateIssued,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.vcertClient == nil {
				tt.vcertClient = fake.NewConnector(true, nil)
			}
			v := &Venafi{
				vcertClient: tt.vcertClient,
			}

			if tt.args.csrPEM == nil {
				tt.args.csrPEM = generateCSR(t, privateKey, "common-name", []string{
					"foo.example.com", "bar.example.com"})
			}

			// this is needed to provide the fake venafi client with a "valid" pickup id
			// testing errors in this should be done in TestVenafi_RequestCertificate
			// any error returned in these tests is a hard fail
			pickupID, err := v.RequestCertificate(tt.args.csrPEM, tt.args.duration, tt.args.customFields)
			if err != nil {
				t.Errorf("RequestCertificate() should but error but got error = %v", err)
			}
			got, err := v.RetrieveCertificate(pickupID, tt.args.csrPEM, tt.args.duration, tt.args.customFields)
			if (err != nil) != tt.wantErr {
				t.Errorf("RetrieveCertificate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkFn != nil {
				tt.checkFn(t, tt.args.csrPEM, got)
			}
		})
	}
}