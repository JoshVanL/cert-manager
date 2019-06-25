/*
Copyright 2019 The Jetstack cert-manager contributors.

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

package certificaterequests

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coretesting "k8s.io/client-go/testing"
	clock "k8s.io/utils/clock/testing"

	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	testpkg "github.com/jetstack/cert-manager/pkg/controller/test"
	"github.com/jetstack/cert-manager/pkg/issuer"
	"github.com/jetstack/cert-manager/pkg/issuer/fake"
	_ "github.com/jetstack/cert-manager/pkg/issuer/selfsigned"
	"github.com/jetstack/cert-manager/pkg/util/pki"
	"github.com/jetstack/cert-manager/test/unit/gen"
)

var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

func generateCSR(commonName string) ([]byte, error) {
	csr := &x509.CertificateRequest{
		Version:            3,
		SignatureAlgorithm: x509.SHA256WithRSA,
		PublicKeyAlgorithm: x509.RSA,
		Subject: pkix.Name{
			Organization: []string{"my-org"},
			CommonName:   commonName,
		},
		URIs: []*url.URL{
			{
				Scheme: "http",
				Host:   "example.com",
			},
		},
		IPAddresses: []net.IP{
			net.IPv4(8, 8, 8, 8),
		},
	}

	sk, err := pki.GenerateRSAPrivateKey(2048)
	if err != nil {
		return nil, err
	}

	csrBytes, err := pki.EncodeCSR(csr, sk)
	if err != nil {
		return nil, err
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type: "CERTIFICATE REQUEST", Bytes: csrBytes,
	})

	return csrPEM, nil
}

func generatePrivateKey(t *testing.T) *rsa.PrivateKey {
	pk, err := pki.GenerateRSAPrivateKey(2048)
	if err != nil {
		t.Errorf("failed to generate private key: %v", err)
		t.FailNow()
	}
	return pk
}

func generateSelfSignedCert(t *testing.T, cr *cmapi.CertificateRequest, sn *big.Int, key crypto.Signer, notBefore, notAfter time.Time) []byte {
	template, err := pki.GenerateTemplateFromCertificateRequest(cr)
	if err != nil {
		t.Errorf("failed to generate cert template from CSR: %v", err)
		t.FailNow()
	}

	template.NotAfter = notAfter
	template.NotBefore = notBefore

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Errorf("error signing cert: %v", err)
		t.FailNow()
	}

	pemByteBuffer := bytes.NewBuffer([]byte{})
	err = pem.Encode(pemByteBuffer, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err != nil {
		t.Errorf("failed to encode cert: %v", err)
		t.FailNow()
	}

	return pemByteBuffer.Bytes()
}

func TestSync(t *testing.T) {
	nowTime := time.Now()
	nowMetaTime := metav1.NewTime(nowTime)
	fixedClock := clock.NewFakeClock(nowTime)

	csr1, err := generateCSR("csr1")
	if err != nil {
		t.Errorf("failed to generate CSR for testing: %s", err)
		t.FailNow()
	}

	csr2, err := generateCSR("csr2")
	if err != nil {
		t.Errorf("failed to generate CSR for testing: %s", err)
		t.FailNow()
	}

	pk := generatePrivateKey(t)

	exampleCR := gen.CertificateRequest("test",
		gen.SetCertificateRequestIsCA(false),
		gen.SetCertificateRequestIssuer(cmapi.ObjectReference{Name: "test"}),
		gen.SetCertificateRequestCSRPEM(csr1),
	)
	exampleCRNotFoundCondition := gen.CertificateRequestFrom(exampleCR,
		gen.SetCertificateRequestStatusCondition(cmapi.CertificateRequestCondition{
			Type:               cmapi.CertificateRequestConditionReady,
			Status:             cmapi.ConditionFalse,
			Reason:             "NotExists",
			Message:            "Certificate does not exist",
			LastTransitionTime: &nowMetaTime,
		}),
	)

	cert1PEM := generateSelfSignedCert(t, exampleCR, nil, pk, nowTime, nowTime.Add(time.Hour*12))
	certPEMExpired := generateSelfSignedCert(t, exampleCR, nil, pk, nowTime.Add(-time.Hour*13), nowTime.Add(-time.Hour*12))

	exampleSignedCR := exampleCR.DeepCopy()
	exampleSignedCR.Status.Certificate = cert1PEM

	exampleSignedExpiredCR := exampleCR.DeepCopy()
	exampleSignedExpiredCR.Status.Certificate = certPEMExpired

	exampleCRReadyCondition := gen.CertificateRequestFrom(exampleSignedCR,
		gen.SetCertificateRequestStatusCondition(cmapi.CertificateRequestCondition{
			Type:               cmapi.CertificateRequestConditionReady,
			Status:             cmapi.ConditionTrue,
			Reason:             "Ready",
			Message:            "Certificate exists and is signed",
			LastTransitionTime: &nowMetaTime,
		}),
	)

	exampleCRExpiredReadyCondition := exampleSignedExpiredCR
	exampleCRExpiredReadyCondition.Status.Conditions = exampleCRReadyCondition.Status.Conditions

	exampleSignedNotMatchCR := exampleSignedCR.DeepCopy()
	exampleSignedNotMatchCR.Spec.CSRPEM = csr2

	exampleGarbageCertCR := exampleSignedCR.DeepCopy()
	exampleGarbageCertCR.Status.Certificate = []byte("not a certificate")

	tests := map[string]controllerFixture{
		"should update certificate request with NotExists if issuer does not return a response": {
			Issuer: gen.Issuer("test",
				gen.AddIssuerCondition(cmapi.IssuerCondition{
					Type:   cmapi.IssuerConditionReady,
					Status: cmapi.ConditionTrue,
				}),
				gen.SetIssuerSelfSigned(cmapi.SelfSignedIssuer{}),
			),
			CertificateRequest: *exampleCR,
			IssuerImpl: &fake.Issuer{
				FakeSign: func(context.Context, *cmapi.CertificateRequest) (*issuer.IssueResponse, error) {
					// By not returning a response, we trigger a 'no-op' action which
					// causes the certificate request controller to update the status of
					// the CertificateRequest with !Ready - NotExists.
					return nil, nil
				},
			},
			Builder: &testpkg.Builder{
				CertManagerObjects: []runtime.Object{gen.CertificateRequest("test")},
				ExpectedActions: []testpkg.Action{
					testpkg.NewAction(coretesting.NewUpdateAction(
						cmapi.SchemeGroupVersion.WithResource("certificaterequests"),
						gen.DefaultTestNamespace,
						exampleCRNotFoundCondition,
					)),
				},
			},
			CheckFn: func(t *testing.T, s *controllerFixture, args ...interface{}) {
			},
			Err: false,
		},
		"should update the status with a freshly signed certificate only when one doesn't exist": {
			Issuer: gen.Issuer("test",
				gen.AddIssuerCondition(cmapi.IssuerCondition{
					Type:   cmapi.IssuerConditionReady,
					Status: cmapi.ConditionTrue,
				}),
				gen.SetIssuerSelfSigned(cmapi.SelfSignedIssuer{}),
			),
			CertificateRequest: *exampleCR,
			IssuerImpl: &fake.Issuer{
				FakeSign: func(context.Context, *cmapi.CertificateRequest) (*issuer.IssueResponse, error) {
					return &issuer.IssueResponse{
						Certificate: cert1PEM,
					}, nil
				},
			},
			Builder: &testpkg.Builder{
				CertManagerObjects: []runtime.Object{gen.CertificateRequest("test")},
				ExpectedActions: []testpkg.Action{
					testpkg.NewAction(coretesting.NewUpdateAction(
						cmapi.SchemeGroupVersion.WithResource("certificaterequests"),
						gen.DefaultTestNamespace,
						exampleCRReadyCondition,
					)),
				},
			},
			CheckFn: func(t *testing.T, s *controllerFixture, args ...interface{}) {
			},
			Err: false,
		},
		"should not update certificate request if certificate exists, even if out of date": {
			Issuer: gen.Issuer("test",
				gen.AddIssuerCondition(cmapi.IssuerCondition{
					Type:   cmapi.IssuerConditionReady,
					Status: cmapi.ConditionTrue,
				}),
				gen.SetIssuerSelfSigned(cmapi.SelfSignedIssuer{}),
			),
			CertificateRequest: *exampleSignedExpiredCR,
			IssuerImpl: &fake.Issuer{
				FakeSign: func(context.Context, *cmapi.CertificateRequest) (*issuer.IssueResponse, error) {
					return nil, errors.New("unexpected sign call")
				},
			},
			Builder: &testpkg.Builder{
				CertManagerObjects: []runtime.Object{gen.CertificateRequest("test")},
				ExpectedActions:    []testpkg.Action{}, // no update
			},
			CheckFn: func(t *testing.T, s *controllerFixture, args ...interface{}) {
			},
			Err: false,
		},
		"fail if bytes contains no certificate but len > 0": {
			Issuer: gen.Issuer("test",
				gen.AddIssuerCondition(cmapi.IssuerCondition{
					Type:   cmapi.IssuerConditionReady,
					Status: cmapi.ConditionTrue,
				}),
				gen.SetIssuerSelfSigned(cmapi.SelfSignedIssuer{}),
			),
			CertificateRequest: *exampleGarbageCertCR,
			IssuerImpl: &fake.Issuer{
				FakeSign: func(context.Context, *cmapi.CertificateRequest) (*issuer.IssueResponse, error) {
					return nil, errors.New("unexpected sign call")
				},
			},
			Builder: &testpkg.Builder{
				CertManagerObjects: []runtime.Object{gen.CertificateRequest("test")},
				ExpectedActions:    []testpkg.Action{},
			},
			CheckFn: func(t *testing.T, s *controllerFixture, args ...interface{}) {
			},
			Err: true,
		},
	}

	for n, test := range tests {
		t.Run(n, func(t *testing.T) {
			if test.Builder == nil {
				test.Builder = &testpkg.Builder{}
			}
			test.Clock = fixedClock
			test.Setup(t)
			crCopy := test.CertificateRequest.DeepCopy()
			err := test.controller.Sync(test.Ctx, crCopy)
			if err != nil && !test.Err {
				t.Errorf("Expected function to not error, but got: %v", err)
			}
			if err == nil && test.Err {
				t.Errorf("Expected function to get an error, but got: %v", err)
			}
			test.Finish(t, crCopy, err)
		})
	}
}
