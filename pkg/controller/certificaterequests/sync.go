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
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kr/pretty"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	apiutil "github.com/jetstack/cert-manager/pkg/api/util"
	"github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/jetstack/cert-manager/pkg/apis/certmanager/validation"
	"github.com/jetstack/cert-manager/pkg/issuer"
	logf "github.com/jetstack/cert-manager/pkg/logs"
	"github.com/jetstack/cert-manager/pkg/util"
	"github.com/jetstack/cert-manager/pkg/util/pki"
)

const (
	errorIssuerNotFound    = "IssuerNotFound"
	errorIssuerNotReady    = "IssuerNotReady"
	errorIssuerInit        = "IssuerInitError"
	errorSavingCertificate = "SaveCertError"

	errorCertificateSigningRequestNotFound = "CSRNotFound"
	errorCertificateSigningRequestParse    = "CSRParseError"

	errorCertificateNotFound = "CertNotFound"
	errorCertificateParse    = "CertParseError"

	reasonIssuingCertificate = "IssueCert"
	successCertificateIssued = "CertIssued"

	messageErrorSavingCertificate = "Error saving TLS certificate: "

	// staticTemporarySerialNumber is a fixed serial number we check for when
	// updating the status of a certificate.
	// It is used to identify temporarily generated certificates, so that friendly
	// status messages can be displayed to users.
	staticTemporarySerialNumber = 0x1234567890
)

func (c *Controller) Sync(ctx context.Context, cr *v1alpha1.CertificateRequest) (err error) {
	c.metrics.IncrementSyncCallCount(ControllerName)

	log := logf.FromContext(ctx)
	dbg := log.V(logf.DebugLevel)

	crCopy := cr.DeepCopy()
	defer func() {
		if _, saveErr := c.updateCertificateStatus(ctx, cr, crCopy); saveErr != nil {
			err = utilerrors.NewAggregate([]error{saveErr, err})
		}
	}()

	dbg.Info("Fetching existing certificate signing request and certificate from certificate request",
		"name", crCopy.ObjectMeta.Name)
	if len(cr.Spec.CSRPem) == 0 {
		return errors.New(errorCertificateSigningRequestNotFound)
	}

	block, _ := pem.Decode(cr.Spec.CSRPem)
	if block == nil {
		return errors.New(errorCertificateSigningRequestParse)
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return err
	}

	if len(cr.Status.Certificate) == 0 {
		return errors.New(errorCertificateNotFound)
	}

	block, _ = pem.Decode(cr.Status.Certificate)
	if block == nil {
		return errors.New(errorCertificateParse)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	// TODO: Metrics??
	// update certificate expiry metric
	//defer c.metrics.UpdateCertificateExpiry(crtCopy, c.secretLister)

	dbg.Info("Update certificate request status if required")
	c.setCertificateRequestStatus(crCopy, csr, cert)

	el := validation.ValidateCertificateRequest(crCopy)
	if len(el) > 0 {
		c.Recorder.Eventf(crCopy, corev1.EventTypeWarning, "BadConfig", "Resource validation failed: %v", el.ToAggregate())
		return nil
	}

	// step zero: check if the referenced issuer exists and is ready
	issuerObj, err := c.helper.GetGenericIssuer(crCopy.Spec.IssuerRef, crCopy.Namespace)
	if k8sErrors.IsNotFound(err) {
		c.Recorder.Eventf(crCopy, corev1.EventTypeWarning, errorIssuerNotFound, err.Error())
		return nil
	}
	if err != nil {
		return err
	}
	dbg.Info("Fetched issuer resource referenced by certificate request", "issuer_name", crCopy.Spec.IssuerRef.Name)

	el = validation.ValidateCertificateRequestForIssuer(crCopy, issuerObj)
	if len(el) > 0 {
		c.Recorder.Eventf(crCopy, corev1.EventTypeWarning, "BadConfig", "Resource validation failed: %v", el.ToAggregate())
		return nil
	}

	dbg.Info("Certificate request passed all validation checks")

	issuerReady := apiutil.IssuerHasCondition(issuerObj, v1alpha1.IssuerCondition{
		Type:   v1alpha1.IssuerConditionReady,
		Status: v1alpha1.ConditionTrue,
	})
	if !issuerReady {
		c.Recorder.Eventf(crCopy, corev1.EventTypeWarning, errorIssuerNotReady, "Issuer %s not ready", issuerObj.GetObjectMeta().Name)
		return nil
	}

	i, err := c.issuerFactory.IssuerFor(issuerObj)
	if err != nil {
		c.Recorder.Eventf(crCopy, corev1.EventTypeWarning, errorIssuerInit, "Internal error initialising issuer: %v", err)
		return nil
	}

	if isTemporaryCertificate(cert) {
		dbg.Info("Temporary certificate found - calling 'issue'")
		return c.sign(ctx, i, crCopy)
	}

	if cert == nil {
		dbg.Info("Invoking issue function as existing certificate does not exist")
		return c.sign(ctx, i, crCopy)
	}

	// begin checking if the TLS certificate is valid/needs a re-issue or renew
	matches, matchErrs := c.certificateMatchesSpec(crCopy, csr, cert)
	if !matches {
		dbg.Info("invoking issue function due to certificate not matching spec", "diff", strings.Join(matchErrs, ", "))
		return c.sign(ctx, i, crCopy)
	}

	dbg.Info("Certificate does not need updating.")

	return nil
}

// return an error on failure. If retrieval is succesful, the certificate data
// will be stored in the certificate request status
func (c *Controller) sign(ctx context.Context, issuer issuer.Interface, cr *v1alpha1.CertificateRequest) error {
	log := logf.FromContext(ctx)

	resp, err := issuer.Sign(ctx, cr)
	if err != nil {
		log.Error(err, "error issuing certificate request")
		return err
	}

	// if the issuer has not returned any data, exit early
	if resp == nil {
		return nil
	}

	if len(resp.Certificate) > 0 {
		cr.Status.Certificate = resp.Certificate
		cr.Status.CA = resp.CA

		c.Recorder.Event(cr, corev1.EventTypeNormal, successCertificateIssued, "Certificate issued successfully")
	}

	return nil
}

// setCertificateRequestStatus will update the status subresource of the
// certificate reques. It will not actually submit the resource to the
// apiserver.
func (c *Controller) setCertificateRequestStatus(cr *v1alpha1.CertificateRequest, csr *x509.CertificateRequest, cert *x509.Certificate) {
	if cert == nil {
		apiutil.SetCertificateRequestCondition(cr, v1alpha1.CertificateRequestConditionReady, v1alpha1.ConditionFalse, "NotFound", "Certificate does not exist")
		return
	}

	metaNotAfter := metav1.NewTime(cert.NotAfter)
	cr.Status.NotAfter = &metaNotAfter

	// Derive & set 'Ready' condition on CertificateRequest resource
	matches, matchErrs := c.certificateMatchesSpec(cr, csr, cert)
	ready := v1alpha1.ConditionFalse
	reason := ""
	message := ""
	switch {
	case isTemporaryCertificate(cert):
		reason = "TemporaryCertificate"
		message = "Certificate issuance in progress. Temporary certificate issued."
		// clear the NotAfter field as it is not relevant to the user
		cr.Status.NotAfter = nil
	case cert.NotAfter.Before(c.clock.Now()):
		reason = "Expired"
		message = fmt.Sprintf("Certificate has expired on %s", cert.NotAfter.Format(time.RFC822))
	case !matches:
		reason = "DoesNotMatch"
		message = strings.Join(matchErrs, ", ")
	default:
		ready = v1alpha1.ConditionTrue
		reason = "Ready"
		message = "Certificate is up to date and has not expired"
	}

	apiutil.SetCertificateRequestCondition(cr, v1alpha1.CertificateRequestConditionReady, ready, reason, message)

	return
}

func (c *Controller) certificateMatchesSpec(cr *v1alpha1.CertificateRequest, csr *x509.CertificateRequest, cert *x509.Certificate) (bool, []string) {
	var errs []string

	// validate the common name is correct
	expectedCN := pki.CommonNameForCertificateRequest(csr)
	if expectedCN != cert.Subject.CommonName {
		errs = append(errs, fmt.Sprintf("Common name on TLS certificate not up to date: %q", cert.Subject.CommonName))
	}

	// validate the dns names are correct
	expectedDNSNames := pki.DNSNamesForCertificateRequest(csr)
	if !util.EqualUnsorted(cert.DNSNames, expectedDNSNames) {
		errs = append(errs, fmt.Sprintf("DNS names on TLS certificate not up to date: %q", cert.DNSNames))
	}

	// validate the uris are correct
	if !util.EqualUnsorted(pki.URLsToString(cert.URIs), pki.URLsToString(csr.URIs)) {
		errs = append(errs, fmt.Sprintf("URLs on TLS certificate not up to date: %q", pki.URLsToString(cert.URIs)))
	}

	// validate the ip addresses are correct
	if !util.EqualUnsorted(pki.IPAddressesToString(cert.IPAddresses), pki.IPAddressesToString(csr.IPAddresses)) {
		errs = append(errs, fmt.Sprintf("IP addresses on TLS certificate not up to date: %q", pki.IPAddressesToString(cert.IPAddresses)))
	}

	return len(errs) == 0, errs
}

func isTemporaryCertificate(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	return cert.SerialNumber.Int64() == staticTemporarySerialNumber
}

func (c *Controller) updateCertificateStatus(ctx context.Context, old, new *v1alpha1.CertificateRequest) (*v1alpha1.CertificateRequest, error) {
	log := logf.FromContext(ctx, "updateStatus")
	oldBytes, _ := json.Marshal(old.Status)
	newBytes, _ := json.Marshal(new.Status)
	if reflect.DeepEqual(oldBytes, newBytes) {
		return nil, nil
	}
	log.V(logf.DebugLevel).Info("updating resource due to change in status", "diff", pretty.Diff(string(oldBytes), string(newBytes)))
	// TODO: replace Update call with UpdateStatus. This requires a custom API
	// server with the /status subresource enabled and/or subresource support
	// for CRDs (https://github.com/kubernetes/kubernetes/issues/38113)
	return c.CMClient.CertmanagerV1alpha1().CertificateRequests(new.Namespace).Update(new)
}
