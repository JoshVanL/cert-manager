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

package ca

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	cmutil "github.com/jetstack/cert-manager/pkg/util"
	"github.com/jetstack/cert-manager/test/e2e/framework"
	"github.com/jetstack/cert-manager/test/e2e/util"
)

var _ = framework.CertManagerDescribe("CA ClusterIssuer", func() {
	f := framework.NewDefaultFramework("create-ca-clusterissuer")
	h := f.Helper()

	issuerName := "test-ca-clusterissuer" + cmutil.RandStringRunes(5)
	secretName := "ca-clusterissuer-signing-keypair-" + cmutil.RandStringRunes(5)

	BeforeEach(func() {
		By("Creating a signing keypair fixture")
		_, err := f.KubeClientSet.CoreV1().Secrets(f.Config.Addons.CertManager.ClusterResourceNamespace).Create(newSigningKeypairSecret(secretName))
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		By("Cleaning up")
		f.KubeClientSet.CoreV1().Secrets(f.Config.Addons.CertManager.ClusterResourceNamespace).Delete(secretName, nil)
		f.CertManagerClientSet.CertmanagerV1alpha2().ClusterIssuers().Delete(issuerName, nil)
	})

	It("should validate a signing keypair", func() {
		By("Creating an Issuer")
		clusterIssuer := util.NewCertManagerCAClusterIssuer(issuerName, secretName)
		_, err := f.CertManagerClientSet.CertmanagerV1alpha2().ClusterIssuers().Create(clusterIssuer)
		Expect(err).NotTo(HaveOccurred())
		By("Waiting for Issuer to become Ready")
		err = h.WaitForClusterIssuerCondition(issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionTrue,
			})
		Expect(err).NotTo(HaveOccurred())
	})
})
