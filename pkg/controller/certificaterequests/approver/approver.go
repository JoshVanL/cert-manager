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

package approver

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiutil "github.com/jetstack/cert-manager/pkg/api/util"
	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
)

const (
	ApprovedMessage = "Certificate request has been approved by cert-manager.io"
)

// Controller is a CertificateRequest controller which manages the "Approved"
// condition. In the absence of any automated policy engine, this controller
// will _always_ set the "Approved" condition to True. All CertificateRequest
// signing controllers should wait until the "Approved" condition is set to
// True before processing.
type Controller struct {
	client.Client
	log      logr.Logger
	recorder record.EventRecorder
}

func New(log logr.Logger, recorder record.EventRecorder, client client.Client) *Controller {
	return &Controller{
		Client:   client,
		log:      log,
		recorder: recorder,
	}
}

// Reconcile will set the "Approved" condition to True on synced
// CertificateRequests. If the "Denied", "Approved" or "Ready" condition
// already exists, exit early.
func (c *Controller) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := c.log.WithValues("certificaterequest", req.NamespacedName)

	// Fetch the CertificateRequest resource being reconciled
	cr := new(cmapi.CertificateRequest)
	if err := c.Client.Get(ctx, req.NamespacedName, cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	switch {
	case
		// If the CertificateRequest has already been approved, exit early.
		apiutil.CertificateRequestIsApproved(cr),

		// If the CertificateRequest has already been denied, exit early.
		apiutil.CertificateRequestIsDenied(cr),

		// If the CertificateRequest is "Issued" or "Failed", exit early.
		apiutil.CertificateRequestReadyReason(cr) == cmapi.CertificateRequestReasonFailed,
		apiutil.CertificateRequestReadyReason(cr) == cmapi.CertificateRequestReasonIssued:

		return ctrl.Result{}, nil
	}

	// Update the CertificateRequest approved condition to true.
	apiutil.SetCertificateRequestCondition(cr,
		cmapi.CertificateRequestConditionApproved,
		cmmeta.ConditionTrue,
		"cert-manager.io",
		ApprovedMessage,
	)

	// Always retry on Update errors, even if forbidden due to missing RBAC. We
	// may have our RBAC updated before the next sync.
	if err := c.Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	c.recorder.Event(cr, corev1.EventTypeNormal, "cert-manager.io", ApprovedMessage)

	log.Info("approved certificate request")

	return ctrl.Result{}, nil
}
