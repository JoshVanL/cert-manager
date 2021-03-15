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

package app

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	apischeme "k8s.io/client-go/kubernetes/scheme"
	clientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/jetstack/cert-manager/cmd/approver/app/options"
	cmapi "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	"github.com/jetstack/cert-manager/pkg/controller/certificaterequests/approver"
	logf "github.com/jetstack/cert-manager/pkg/logs"
	"github.com/jetstack/cert-manager/pkg/util"
)

const controllerAgentName = "cert-manager-approver"

// This sets the informer's resync period to 10 hours
// following the controller-runtime defaults
//and following discussion: https://github.com/kubernetes-sigs/controller-runtime/pull/88#issuecomment-408500629
var resyncPeriod = 10 * time.Hour

type CertManagerApproverOptions struct {
	ApproverOptions *options.ApproverOptions
}

func NewCertManagerApproverOptions() *CertManagerApproverOptions {
	o := &CertManagerApproverOptions{
		ApproverOptions: options.NewApproverOptions(),
	}

	return o
}

// NewCommandStartCertManagerApprover is a CLI handler for starting cert-manager
func NewCommandStartCertManagerApprover(stopCh <-chan struct{}) *cobra.Command {
	o := NewCertManagerApproverOptions()

	cmd := &cobra.Command{
		Use:   "cert-manager-approver",
		Short: fmt.Sprintf("Approve CertificateRequests (%s) (%s)", util.AppVersion, util.AppGitCommit),
		Long: `
cert-manager-approver is responsible for approving cert-manager
CertificateRequests. Onced approved, they can be signed by the configured
issuer.`,

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Validate(args); err != nil {
				return fmt.Errorf("error validating options: %s", err)
			}

			logf.Log.V(logf.InfoLevel).Info("starting controller", "version", util.AppVersion, "git-commit", util.AppGitCommit)
			return o.RunCertManagerApprover(stopCh)
		},
	}

	flags := cmd.Flags()
	o.ApproverOptions.AddFlags(flags)

	return cmd
}

func (o CertManagerApproverOptions) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.ApproverOptions.Validate())
	return utilerrors.NewAggregate(errors)
}

func (o CertManagerApproverOptions) RunCertManagerApprover(stopCh <-chan struct{}) error {
	rootCtx := util.ContextWithStopCh(context.Background(), stopCh)
	rootCtx = logf.NewContext(rootCtx, nil, "approver")
	log := logf.FromContext(rootCtx)

	scheme := runtime.NewScheme()
	if err := apischeme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := cmapi.AddToScheme(scheme); err != nil {
		return err
	}

	log.V(logf.DebugLevel).Info("creating client")
	restConfig, err := buildClient(o.ApproverOptions)
	if err != nil {
		return err
	}

	// Create a Kubernetes api client
	cl, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating kubernetes client: %s", err)
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logf.WithInfof(log.V(logf.DebugLevel)).Infof)
	eventBroadcaster.StartRecordingToSink(&clientv1.EventSinkImpl{Interface: cl.CoreV1().Events("")})

	log.V(logf.DebugLevel).Info("building controller")

	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Logger:                  log,
		SyncPeriod:              &resyncPeriod,
		LeaderElectionNamespace: o.ApproverOptions.LeaderElectionNamespace,
		LeaderElection:          o.ApproverOptions.LeaderElect,
		LeaderElectionID:        controllerAgentName,
		EventBroadcaster:        eventBroadcaster,
		LeaseDuration:           &o.ApproverOptions.LeaderElectionLeaseDuration,
		RenewDeadline:           &o.ApproverOptions.LeaderElectionRenewDeadline,
		Scheme:                  scheme,
		MetricsBindAddress:      o.ApproverOptions.MetricsListenAddress,
	})
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %s", err)
	}

	recorder := eventBroadcaster.NewRecorder(scheme, corev1.EventSource{Component: controllerAgentName})
	err = ctrl.NewControllerManagedBy(mgr).
		For(new(cmapi.CertificateRequest)).
		Complete(approver.New(log, recorder, mgr.GetClient()))
	if err != nil {
		return err
	}

	return mgr.Start(stopCh)
}

func buildClient(opts *options.ApproverOptions) (*rest.Config, error) {
	// Load the users Kubernetes config
	kubeCfg, err := clientcmd.BuildConfigFromFlags(opts.APIServerHost, opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("error creating rest config: %s", err.Error())
	}

	kubeCfg.QPS = opts.KubernetesAPIQPS
	kubeCfg.Burst = opts.KubernetesAPIBurst

	// Add User-Agent to client
	kubeCfg = rest.AddUserAgent(kubeCfg, util.CertManagerUserAgent)

	return kubeCfg, nil
}
