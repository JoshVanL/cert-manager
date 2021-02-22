/*
Copyright 2021 The cert-manager Authors.

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

package issuers

import (
	"fmt"

	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	cminformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions/certmanager/v1"
	cmlisters "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1"
)

type Getter interface {
	Issuer(ref cmmeta.ObjectReference, ns string) (cmapi.GenericIssuer, error)
}

// Type Getter provides a set of commonly useful functions for use when building
// a cert-manager controller.
// An instance of Getter is made available as part of a controller's context.
type getterImpl struct {
	issuerLister        cmlisters.IssuerLister
	clusterIssuerLister cmlisters.ClusterIssuerLister
}

var _ Getter = &getterImpl{}

// NewGetter will construct a new instance of a Getter using values supplied on
// the provided controller context.
func NewGetter(informers cminformers.Interface) Getter {
	return &getterImpl{
		issuerLister:        informers.Issuers().Lister(),
		clusterIssuerLister: informers.ClusterIssuers().Lister(),
	}
}

// Issuer will return an Issuer for the given IssuerRef.
// The namespace parameter must be provided if an 'Issuer' is referenced.
// This namespace will be used to read the Issuer resource.
// In most cases, the ns parameter should be set to the namespace of the resource
// that defines the IssuerRef (i.e. the namespace of the Certificate resource).
func (h *getterImpl) Issuer(ref cmmeta.ObjectReference, ns string) (cmapi.GenericIssuer, error) {
	switch ref.Kind {
	case "", cmapi.IssuerKind:
		return h.issuerLister.Issuers(ns).Get(ref.Name)
	case cmapi.ClusterIssuerKind:
		// handle edge case where the ClusterIssuerLister is not set.
		// this isn't actually a supported operating mode right now, nor is it
		// exposed to users.
		// we include it here in case we do allow this mode of operation again
		// in future.
		if h.clusterIssuerLister == nil {
			return nil, fmt.Errorf("cannot get ClusterIssuer named %q as cert-manager is scoped to a single namespace", ref.Name)
		}
		return h.clusterIssuerLister.Get(ref.Name)
	default:
		return nil, fmt.Errorf(`invalid value %q for issuerRef.kind. Must be empty, %q or %q`, ref.Kind, cmapi.IssuerKind, cmapi.ClusterIssuerKind)
	}
}
