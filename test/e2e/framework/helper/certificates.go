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

package helper

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	apiutil "github.com/jetstack/cert-manager/pkg/api/util"
	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	intscheme "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/scheme"
	"github.com/jetstack/cert-manager/pkg/util"
	"github.com/jetstack/cert-manager/pkg/util/pki"
	"github.com/jetstack/cert-manager/test/e2e/framework/log"
)

// WaitForCertificateReady waits for the certificate resource to enter a Ready
// state.
func (h *Helper) WaitForCertificateReady(ns, name string, timeout time.Duration) (*cmapi.Certificate, error) {
	var certificate *cmapi.Certificate
	err := wait.PollImmediate(time.Second, timeout,
		func() (bool, error) {
			var err error
			log.Logf("Waiting for Certificate %v to be ready", name)
			certificate, err = h.CMClient.CertmanagerV1alpha2().Certificates(ns).Get(name, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf("error getting Certificate %v: %v", name, err)
			}
			isReady := apiutil.CertificateHasCondition(certificate, cmapi.CertificateCondition{
				Type:   cmapi.CertificateConditionReady,
				Status: cmmeta.ConditionTrue,
			})
			if !isReady {
				log.Logf("Expected Certificate to have Ready condition 'true' but it has: %v", certificate.Status.Conditions)
				return false, nil
			}
			return true, nil
		},
	)

	// return certificate even when error to use for debugging
	return certificate, err
}

// WaitForCertificateNotReady waits for the certificate resource to enter a
// non-Ready state.
func (h *Helper) WaitForCertificateNotReady(ns, name string, timeout time.Duration) (*cmapi.Certificate, error) {
	var certificate *cmapi.Certificate
	err := wait.PollImmediate(time.Second, timeout,
		func() (bool, error) {
			var err error
			log.Logf("Waiting for Certificate %v to be ready", name)
			certificate, err = h.CMClient.CertmanagerV1alpha2().Certificates(ns).Get(name, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf("error getting Certificate %v: %v", name, err)
			}
			isReady := apiutil.CertificateHasCondition(certificate, cmapi.CertificateCondition{
				Type:   cmapi.CertificateConditionReady,
				Status: cmmeta.ConditionFalse,
			})
			if !isReady {
				log.Logf("Expected Certificate to have Ready condition 'true' but it has: %v", certificate.Status.Conditions)
				return false, nil
			}
			return true, nil
		},
	)

	// return certificate even when error to use for debugging
	return certificate, err
}

// ValidateIssuedCertificate will ensure that the given Certificate has a
// certificate issued for it, and that the details on the x509 certificate are
// correct as defined by the Certificate's spec.
func (h *Helper) ValidateIssuedCertificate(certificate *cmapi.Certificate, rootCAPEM []byte) (*x509.Certificate, error) {
	log.Logf("Getting the TLS certificate Secret resource")
	secret, err := h.KubeClient.CoreV1().Secrets(certificate.Namespace).Get(certificate.Spec.SecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if !(len(secret.Data) == 2 || len(secret.Data) == 3) {
		return nil, fmt.Errorf("Expected 2 keys in certificate secret, but there was %d", len(secret.Data))
	}

	keyBytes, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("No private key data found for Certificate %q (secret %q)", certificate.Name, certificate.Spec.SecretName)
	}
	key, err := pki.DecodePrivateKeyBytes(keyBytes)
	if err != nil {
		return nil, err
	}

	// validate private key is of the correct type (rsa or ecdsa)
	switch certificate.Spec.KeyAlgorithm {
	case cmapi.KeyAlgorithm(""),
		cmapi.RSAKeyAlgorithm:
		_, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("Expected private key of type RSA, but it was: %T", key)
		}
	case cmapi.ECDSAKeyAlgorithm:
		_, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("Expected private key of type ECDSA, but it was: %T", key)
		}
	default:
		return nil, fmt.Errorf("unrecognised requested private key algorithm %q", certificate.Spec.KeyAlgorithm)
	}

	// TODO: validate private key KeySize

	// check the provided certificate is valid
	expectedOrganization := pki.OrganizationForCertificate(certificate)
	expectedDNSNames := certificate.Spec.DNSNames
	uris, err := pki.URIsForCertificate(certificate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URIs: %s", err)
	}

	expectedURIs := pki.URLsToString(uris)

	certBytes, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("No certificate data found for Certificate %q (secret %q)", certificate.Name, certificate.Spec.SecretName)
	}

	cert, err := pki.DecodeX509CertificateBytes(certBytes)
	if err != nil {
		return nil, err
	}

	commonNameCorrect := true
	expectedCN := certificate.Spec.CommonName
	if len(expectedCN) == 0 && len(cert.Subject.CommonName) > 0 {
		if !util.Contains(cert.DNSNames, cert.Subject.CommonName) {
			commonNameCorrect = false
		}
	} else if expectedCN != cert.Subject.CommonName {
		commonNameCorrect = false
	}

	if !commonNameCorrect || !util.Subset(cert.DNSNames, expectedDNSNames) || !util.EqualUnsorted(pki.URLsToString(cert.URIs), expectedURIs) ||
		!(len(cert.Subject.Organization) == 0 || util.EqualUnsorted(cert.Subject.Organization, expectedOrganization)) {
		return nil, fmt.Errorf("Expected certificate valid for CN %q, O %v, dnsNames %v, uriSANs %v,but got a certificate valid for CN %q, O %v, dnsNames %v, uriSANs %v",
			expectedCN, expectedOrganization, expectedDNSNames, expectedURIs, cert.Subject.CommonName, cert.Subject.Organization, cert.DNSNames, cert.URIs)
	}

	if certificate.Status.NotAfter == nil {
		return nil, fmt.Errorf("No certificate expiration found for Certificate %q", certificate.Name)
	}
	if !cert.NotAfter.Equal(certificate.Status.NotAfter.Time) {
		return nil, fmt.Errorf("Expected certificate expiry date to be %v, but got %v", certificate.Status.NotAfter, cert.NotAfter)
	}

	label, ok := secret.Annotations[cmapi.CertificateNameKey]
	if !ok {
		return nil, fmt.Errorf("Expected secret to have certificate-name label, but had none")
	}

	if label != certificate.Name {
		return nil, fmt.Errorf("Expected secret to have certificate-name label with a value of %q, but got %q", certificate.Name, label)
	}

	certificateKeyUsages, certificateExtKeyUsages, err := pki.BuildKeyUsages(certificate.Spec.Usages, certificate.Spec.IsCA)
	if err != nil {
		return nil, fmt.Errorf("failed to build key usages from certificate: %s", err)
	}

	defaultCertKeyUsages, defaultCertExtKeyUsages, err := h.defaultKeyUsagesToAdd(certificate.Namespace, &certificate.Spec.IssuerRef)
	if err != nil {
		return nil, err
	}

	certificateKeyUsages |= defaultCertKeyUsages
	certificateExtKeyUsages = append(certificateExtKeyUsages, defaultCertExtKeyUsages...)

	// If using ECDSA then ignore key encipherment
	if certificate.Spec.KeyAlgorithm == cmapi.ECDSAKeyAlgorithm {
		certificateKeyUsages &^= x509.KeyUsageKeyEncipherment
		cert.KeyUsage &^= x509.KeyUsageKeyEncipherment
	}

	certificateExtKeyUsages = h.deduplicateExtKeyUsages(certificateExtKeyUsages)

	if !h.keyUsagesMatch(cert.KeyUsage, cert.ExtKeyUsage,
		certificateKeyUsages, certificateExtKeyUsages) {
		return nil, fmt.Errorf("key usages and extended key usages do not match: exp=%s got=%s exp=%s got=%s",
			apiutil.KeyUsageStrings(certificateKeyUsages), apiutil.KeyUsageStrings(cert.KeyUsage),
			apiutil.ExtKeyUsageStrings(certificateExtKeyUsages), apiutil.ExtKeyUsageStrings(cert.ExtKeyUsage))
	}

	var dnsName string
	if len(expectedDNSNames) > 0 {
		dnsName = expectedDNSNames[0]
	}

	// TODO: move this verification step out of this function
	if rootCAPEM != nil {
		rootCertPool := x509.NewCertPool()
		rootCertPool.AppendCertsFromPEM(rootCAPEM)
		intermediateCertPool := x509.NewCertPool()
		intermediateCertPool.AppendCertsFromPEM(certBytes)
		opts := x509.VerifyOptions{
			DNSName:       dnsName,
			Intermediates: intermediateCertPool,
			Roots:         rootCertPool,
		}

		if _, err := cert.Verify(opts); err != nil {
			return nil, err
		}
	}

	return cert, nil
}

func (h *Helper) deduplicateExtKeyUsages(us []x509.ExtKeyUsage) []x509.ExtKeyUsage {
	extKeyUsagesMap := make(map[x509.ExtKeyUsage]bool)
	for _, e := range us {
		extKeyUsagesMap[e] = true
	}

	us = make([]x509.ExtKeyUsage, 0)
	for e, ok := range extKeyUsagesMap {
		if ok {
			us = append(us, e)
		}
	}

	return us
}

func (h *Helper) WaitCertificateIssuedValid(ns, name string, timeout time.Duration) error {
	return h.WaitCertificateIssuedValidTLS(ns, name, timeout, nil)
}

func (h *Helper) defaultKeyUsagesToAdd(ns string, issuerRef *cmmeta.ObjectReference) (x509.KeyUsage, []x509.ExtKeyUsage, error) {
	var issuerSpec *cmapi.IssuerSpec
	switch issuerRef.Kind {
	case "ClusterIssuer":
		issuerObj, err := h.CMClient.CertmanagerV1alpha2().ClusterIssuers().Get(issuerRef.Name, metav1.GetOptions{})
		if err != nil {
			return 0, nil, fmt.Errorf("failed to find referenced ClusterIssuer %v: %s",
				issuerRef, err)
		}

		issuerSpec = &issuerObj.Spec
	default:
		issuerObj, err := h.CMClient.CertmanagerV1alpha2().Issuers(ns).Get(issuerRef.Name, metav1.GetOptions{})
		if err != nil {
			return 0, nil, fmt.Errorf("failed to find referenced Issuer %v: %s",
				issuerRef, err)
		}

		issuerSpec = &issuerObj.Spec
	}

	var keyUsages x509.KeyUsage
	var extKeyUsages []x509.ExtKeyUsage

	// Vault and ACME issuers will add server auth and client auth extended key
	// usages by default so we need to add them to the list of expected usages
	if issuerSpec.ACME != nil || issuerSpec.Vault != nil {
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	}

	// Vault issuers will add key agreement key usage
	if issuerSpec.Vault != nil {
		keyUsages |= x509.KeyUsageKeyAgreement
	}

	// Venafi issue adds server auth key usage
	if issuerSpec.Venafi != nil {
		extKeyUsages = append(extKeyUsages, x509.ExtKeyUsageServerAuth)
	}

	return keyUsages, extKeyUsages, nil
}

func (h *Helper) keyUsagesMatch(aKU x509.KeyUsage, aEKU []x509.ExtKeyUsage,
	bKU x509.KeyUsage, bEKU []x509.ExtKeyUsage) bool {
	if aKU != bKU {
		return false
	}

	if len(aEKU) != len(bEKU) {
		return false
	}

	sort.SliceStable(aEKU, func(i, j int) bool {
		return aEKU[i] < aEKU[j]
	})

	sort.SliceStable(bEKU, func(i, j int) bool {
		return bEKU[i] < bEKU[j]
	})

	for i := range aEKU {
		if aEKU[i] != bEKU[i] {
			return false
		}
	}

	return true
}

func (h *Helper) WaitCertificateIssuedValidTLS(ns, name string, timeout time.Duration, rootCAPEM []byte) error {
	certificate, err := h.WaitForCertificateReady(ns, name, timeout)
	if err != nil {
		log.Logf("Error waiting for Certificate to become Ready: %v", err)
		h.Kubectl(ns).DescribeResource("certificate", name)
		h.Kubectl(ns).Describe("order", "challenge")
		h.describeCertificateRequestFromCertificate(ns, certificate)
		return err
	}

	_, err = h.ValidateIssuedCertificate(certificate, rootCAPEM)
	if err != nil {
		log.Logf("Error validating issued certificate: %v", err)
		h.Kubectl(ns).DescribeResource("certificate", name)
		h.Kubectl(ns).Describe("order", "challenge")
		h.describeCertificateRequestFromCertificate(ns, certificate)
		return err
	}

	return nil
}

func (h *Helper) describeCertificateRequestFromCertificate(ns string, certificate *cmapi.Certificate) {
	if certificate == nil {
		return
	}

	crName, err := apiutil.ComputeCertificateRequestName(certificate)
	if err != nil {
		log.Logf("Failed to compute CertificateRequest name from certificate: %s", err)
		return
	}
	h.Kubectl(ns).DescribeResource("certificaterequest", crName)
}

// WaitForCertificateCondition waits for the status of the named Certificate to contain
// a condition whose type and status matches the supplied one.
func (h *Helper) WaitForCertificateCondition(ns, name string, condition cmapi.CertificateCondition, timeout time.Duration) error {
	pollErr := wait.PollImmediate(500*time.Millisecond, timeout,
		func() (bool, error) {
			log.Logf("Waiting for Certificate %v condition %#v", name, condition)
			certificate, err := h.CMClient.CertmanagerV1alpha2().Certificates(ns).Get(name, metav1.GetOptions{})
			if nil != err {
				return false, fmt.Errorf("error getting Certificate %v: %v", name, err)
			}

			return apiutil.CertificateHasCondition(certificate, condition), nil
		},
	)
	return h.wrapErrorWithCertificateStatusCondition(pollErr, ns, name, condition.Type)
}

// WaitForCertificateEvent waits for an event on the named Certificate to contain
// an event reason matches the supplied one.
func (h *Helper) WaitForCertificateEvent(cert *cmapi.Certificate, reason string, timeout time.Duration) error {
	return wait.PollImmediate(500*time.Millisecond, timeout,
		func() (bool, error) {
			log.Logf("Waiting for Certificate event %v reason %#v", cert.Name, reason)
			evts, err := h.KubeClient.CoreV1().Events(cert.Namespace).Search(intscheme.Scheme, cert)
			if err != nil {
				return false, fmt.Errorf("error getting Certificate %v: %v", cert.Name, err)
			}

			return hasEvent(evts, reason), nil
		},
	)
}

func hasEvent(events *corev1.EventList, reason string) bool {
	for _, evt := range events.Items {
		if evt.Reason == reason {
			return true
		}
	}
	return false
}

// try to retrieve last condition to help diagnose tests.
func (h *Helper) wrapErrorWithCertificateStatusCondition(pollErr error, ns, name string, conditionType cmapi.CertificateConditionType) error {
	if pollErr == nil {
		return nil
	}

	certificate, err := h.CMClient.CertmanagerV1alpha2().Certificates(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return pollErr
	}

	for _, cond := range certificate.Status.Conditions {
		if cond.Type == conditionType {
			return fmt.Errorf("%s: Last Status: '%s' Reason: '%s', Message: '%s'", pollErr.Error(), cond.Status, cond.Reason, cond.Message)
		}
	}

	return pollErr
}

// WaitForCertificateToExist waits for the named certificate to exist
func (h *Helper) WaitForCertificateToExist(ns, name string, timeout time.Duration) error {
	return wait.PollImmediate(500*time.Millisecond, timeout,
		func() (bool, error) {
			log.Logf("Waiting for Certificate %v to exist", name)
			_, err := h.CMClient.CertmanagerV1alpha2().Certificates(ns).Get(name, metav1.GetOptions{})
			if k8sErrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("error getting Certificate %v: %v", name, err)
			}

			return true, nil
		},
	)
}
