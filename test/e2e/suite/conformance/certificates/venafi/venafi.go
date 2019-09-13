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

package venafi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/jetstack/cert-manager/test/e2e/framework"
	"github.com/jetstack/cert-manager/test/e2e/framework/util/errors"
	"github.com/jetstack/cert-manager/test/e2e/suite/conformance/certificates"
	vaddon "github.com/jetstack/cert-manager/test/e2e/suite/issuers/venafi/addon"
)

var _ = framework.ConformanceDescribe("Certificates", func() {
	// unsupportedFeatures is a list of features that are not supported by the ACME
	// issuer type using HTTP01
	var unsupportedFeatures = certificates.NewFeatureSet(
		certificates.IPAddressFeature,
		certificates.DurationFeature,
	)

	provisioner := new(venafiProvisioner)
	(&certificates.Suite{
		Name:                "Venafi",
		CreateIssuerFunc:    provisioner.create,
		DeleteIssuerFunc:    provisioner.delete,
		UnsupportedFeatures: unsupportedFeatures,
	}).Define()
})

type venafiProvisioner struct {
	tpp *vaddon.VenafiTPP
}

func (v *venafiProvisioner) delete(f *framework.Framework, ref cmapi.ObjectReference) {
	Expect(v.tpp.Deprovision()).NotTo(HaveOccurred(), "failed to deprovision tpp venafi")
}

func (v *venafiProvisioner) create(f *framework.Framework) cmapi.ObjectReference {
	By("Creating a Venafi issuer")

	v.tpp = &vaddon.VenafiTPP{
		Namespace: f.Namespace.Name,
	}

	err := v.tpp.Setup(f.Config)
	if errors.IsSkip(err) {
		framework.Skipf("Skipping test as addon could not be setup: %v", err)
	}
	Expect(err).NotTo(HaveOccurred(), "failed to setup tpp venafi")

	Expect(v.tpp.Provision()).NotTo(HaveOccurred(), "failed to provision tpp venafi")

	issuer := v.tpp.Details().BuildIssuer()
	issuer, err = f.CertManagerClientSet.CertmanagerV1alpha1().Issuers(f.Namespace.Name).Create(issuer)
	Expect(err).NotTo(HaveOccurred(), "failed to create issuer for venafi")

	return cmapi.ObjectReference{
		Group: cmapi.SchemeGroupVersion.Group,
		Kind:  cmapi.IssuerKind,
		Name:  issuer.Name,
	}
}