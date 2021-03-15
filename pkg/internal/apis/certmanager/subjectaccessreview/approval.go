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

package subjectaccessreview

import (
	"context"
	"path/filepath"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	authzclient "k8s.io/client-go/kubernetes/typed/authorization/v1"

	"github.com/jetstack/cert-manager/pkg/apis/certmanager"
	"github.com/jetstack/cert-manager/pkg/internal/api/validation"
	cmapi "github.com/jetstack/cert-manager/pkg/internal/apis/certmanager"
	internalutil "github.com/jetstack/cert-manager/pkg/internal/apis/certmanager/util"
)

type Approval struct {
	sarclient authzclient.SubjectAccessReviewInterface
}

func NewApproval(sarclient authzclient.SubjectAccessReviewInterface) *Approval {
	return &Approval{
		sarclient: sarclient,
	}
}

func (a *Approval) AddToValidationRegistry(reg *validation.Registry) error {
	if err := reg.AddValidateUpdateFunc(&cmapi.CertificateRequest{}, a.Review); err != nil {
		return err
	}

	return nil
}

func (a *Approval) Review(req *admissionv1.AdmissionRequest, oldObj, newObj runtime.Object) field.ErrorList {
	oldCR := oldObj.(*cmapi.CertificateRequest)
	newCR := newObj.(*cmapi.CertificateRequest)

	var (
		ok  bool
		err error
	)

	switch {
	case !reflect.DeepEqual(
		internalutil.GetCertificateRequestCondition(oldCR.Status.Conditions, cmapi.CertificateRequestConditionApproved),
		internalutil.GetCertificateRequestCondition(newCR.Status.Conditions, cmapi.CertificateRequestConditionApproved),
	):
		ok, err = a.reviewRequest(req, newCR, "approve")
	case !reflect.DeepEqual(
		internalutil.GetCertificateRequestCondition(oldCR.Status.Conditions, cmapi.CertificateRequestConditionDenied),
		internalutil.GetCertificateRequestCondition(newCR.Status.Conditions, cmapi.CertificateRequestConditionDenied),
	):
		ok, err = a.reviewRequest(req, newCR, "deny")
	default:
		ok = true
	}

	if err != nil {
		// TODO: log error
		return field.ErrorList{field.InternalError(field.NewPath("status.conditions"), err)}
	}

	if !ok {
		return field.ErrorList{
			field.Forbidden(field.NewPath("status.conditions"), "user does not have permissions to set approved/deny conditions"),
		}
	}

	return nil
}

func (a *Approval) reviewRequest(req *admissionv1.AdmissionRequest, cr *cmapi.CertificateRequest, verb string) (bool, error) {
	extra := make(map[string]authzv1.ExtraValue)
	for k, v := range req.UserInfo.Extra {
		extra[k] = authzv1.ExtraValue(v)
	}

	resp, err := a.sarclient.Create(context.TODO(), &authzv1.SubjectAccessReview{
		Spec: authzv1.SubjectAccessReviewSpec{
			User:   req.UserInfo.Username,
			Groups: req.UserInfo.Groups,
			Extra:  extra,
			UID:    req.UserInfo.UID,

			ResourceAttributes: &authzv1.ResourceAttributes{
				Group:     certmanager.GroupName,
				Resource:  "signers",
				Name:      filepath.Join(cr.Spec.IssuerRef.Group, cr.Spec.IssuerRef.Kind),
				Namespace: cr.Namespace,
				Verb:      verb,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return false, err
	}

	return resp.Status.Allowed, nil
}
