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

package options

import (
	"time"

	"github.com/spf13/pflag"
)

type ApproverOptions struct {
	APIServerHost      string
	Kubeconfig         string
	KubernetesAPIQPS   float32
	KubernetesAPIBurst int

	Namespace string

	LeaderElect                 bool
	LeaderElectionNamespace     string
	LeaderElectionLeaseDuration time.Duration
	LeaderElectionRenewDeadline time.Duration
	LeaderElectionRetryPeriod   time.Duration

	// The host and port address, separated by a ':', that the Prometheus server
	// should expose metrics on.
	MetricsListenAddress string
}

const (
	defaultAPIServerHost              = ""
	defaultKubeconfig                 = ""
	defaultKubernetesAPIQPS   float32 = 20
	defaultKubernetesAPIBurst         = 50

	defaultNamespace = ""

	defaultLeaderElect                 = true
	defaultLeaderElectionNamespace     = "kube-system"
	defaultLeaderElectionLeaseDuration = 60 * time.Second
	defaultLeaderElectionRenewDeadline = 40 * time.Second
	defaultLeaderElectionRetryPeriod   = 15 * time.Second

	defaultPrometheusMetricsServerAddress = "0.0.0.0:9402"
)

func NewApproverOptions() *ApproverOptions {
	return &ApproverOptions{
		APIServerHost:               defaultAPIServerHost,
		KubernetesAPIQPS:            defaultKubernetesAPIQPS,
		KubernetesAPIBurst:          defaultKubernetesAPIBurst,
		Namespace:                   defaultNamespace,
		LeaderElect:                 defaultLeaderElect,
		LeaderElectionNamespace:     defaultLeaderElectionNamespace,
		LeaderElectionLeaseDuration: defaultLeaderElectionLeaseDuration,
		LeaderElectionRenewDeadline: defaultLeaderElectionRenewDeadline,
		LeaderElectionRetryPeriod:   defaultLeaderElectionRetryPeriod,
	}
}

func (s *ApproverOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.APIServerHost, "master", defaultAPIServerHost, ""+
		"Optional apiserver host address to connect to. If not specified, autoconfiguration "+
		"will be attempted.")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", defaultKubeconfig, ""+
		"Paths to a kubeconfig. Only required if out-of-cluster.")
	fs.Float32Var(&s.KubernetesAPIQPS, "kube-api-qps", defaultKubernetesAPIQPS, "indicates the maximum queries-per-second requests to the Kubernetes apiserver")
	fs.IntVar(&s.KubernetesAPIBurst, "kube-api-burst", defaultKubernetesAPIBurst, "the maximum burst queries-per-second of requests sent to the Kubernetes apiserver")
	fs.StringVar(&s.Namespace, "namespace", defaultNamespace, ""+
		"If set, this limits the scope of cert-manager to a single namespace. If "+
		"not specified, all namespaces will be watched.")
	fs.BoolVar(&s.LeaderElect, "leader-elect", true, ""+
		"If true, cert-manager will perform leader election between instances to ensure no more "+
		"than one instance of cert-manager operates at a time")
	fs.StringVar(&s.LeaderElectionNamespace, "leader-election-namespace", defaultLeaderElectionNamespace, ""+
		"Namespace used to perform leader election. Only used if leader election is enabled")
	fs.DurationVar(&s.LeaderElectionLeaseDuration, "leader-election-lease-duration", defaultLeaderElectionLeaseDuration, ""+
		"The duration that non-leader candidates will wait after observing a leadership "+
		"renewal until attempting to acquire leadership of a led but unrenewed leader "+
		"slot. This is effectively the maximum duration that a leader can be stopped "+
		"before it is replaced by another candidate. This is only applicable if leader "+
		"election is enabled.")
	fs.DurationVar(&s.LeaderElectionRenewDeadline, "leader-election-renew-deadline", defaultLeaderElectionRenewDeadline, ""+
		"The interval between attempts by the acting master to renew a leadership slot "+
		"before it stops leading. This must be less than or equal to the lease duration. "+
		"This is only applicable if leader election is enabled.")
	fs.DurationVar(&s.LeaderElectionRetryPeriod, "leader-election-retry-period", defaultLeaderElectionRetryPeriod, ""+
		"The duration the clients should wait between attempting acquisition and renewal "+
		"of a leadership. This is only applicable if leader election is enabled.")

	fs.StringVar(&s.MetricsListenAddress, "metrics-listen-address", defaultPrometheusMetricsServerAddress, ""+
		"The host and port that the metrics endpoint should listen on.")
}

func (o *ApproverOptions) Validate() error {
	return nil
}
