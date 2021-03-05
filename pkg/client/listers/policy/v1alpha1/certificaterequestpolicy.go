/*
Copyright The cert-manager Authors.

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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/jetstack/cert-manager/pkg/apis/policy/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// CertificateRequestPolicyLister helps list CertificateRequestPolicies.
// All objects returned here must be treated as read-only.
type CertificateRequestPolicyLister interface {
	// List lists all CertificateRequestPolicies in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.CertificateRequestPolicy, err error)
	// CertificateRequestPolicies returns an object that can list and get CertificateRequestPolicies.
	CertificateRequestPolicies(namespace string) CertificateRequestPolicyNamespaceLister
	CertificateRequestPolicyListerExpansion
}

// certificateRequestPolicyLister implements the CertificateRequestPolicyLister interface.
type certificateRequestPolicyLister struct {
	indexer cache.Indexer
}

// NewCertificateRequestPolicyLister returns a new CertificateRequestPolicyLister.
func NewCertificateRequestPolicyLister(indexer cache.Indexer) CertificateRequestPolicyLister {
	return &certificateRequestPolicyLister{indexer: indexer}
}

// List lists all CertificateRequestPolicies in the indexer.
func (s *certificateRequestPolicyLister) List(selector labels.Selector) (ret []*v1alpha1.CertificateRequestPolicy, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.CertificateRequestPolicy))
	})
	return ret, err
}

// CertificateRequestPolicies returns an object that can list and get CertificateRequestPolicies.
func (s *certificateRequestPolicyLister) CertificateRequestPolicies(namespace string) CertificateRequestPolicyNamespaceLister {
	return certificateRequestPolicyNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// CertificateRequestPolicyNamespaceLister helps list and get CertificateRequestPolicies.
// All objects returned here must be treated as read-only.
type CertificateRequestPolicyNamespaceLister interface {
	// List lists all CertificateRequestPolicies in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.CertificateRequestPolicy, err error)
	// Get retrieves the CertificateRequestPolicy from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.CertificateRequestPolicy, error)
	CertificateRequestPolicyNamespaceListerExpansion
}

// certificateRequestPolicyNamespaceLister implements the CertificateRequestPolicyNamespaceLister
// interface.
type certificateRequestPolicyNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all CertificateRequestPolicies in the indexer for a given namespace.
func (s certificateRequestPolicyNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.CertificateRequestPolicy, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.CertificateRequestPolicy))
	})
	return ret, err
}

// Get retrieves the CertificateRequestPolicy from the indexer for a given namespace and name.
func (s certificateRequestPolicyNamespaceLister) Get(name string) (*v1alpha1.CertificateRequestPolicy, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("certificaterequestpolicy"), name)
	}
	return obj.(*v1alpha1.CertificateRequestPolicy), nil
}
