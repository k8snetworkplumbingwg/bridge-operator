/*
Copyright 2023.

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

package main

import (
	"context"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	bridgeoperatorv1alpha1 "github.com/k8snetworkplumbingwg/bridge-operator/api/bridgeoperator.k8s.cni.npwg.io/v1alpha1"
	"github.com/k8snetworkplumbingwg/bridge-operator/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(bridgeoperatorv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5633f401.k8s.cni.npwg.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.BridgeConfigurationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BridgeConfiguration")
		os.Exit(1)
	}
	if err = (&controllers.BridgeInformationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BridgeInformation")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	daemonSet := getBridgeInformationDaemonSet()
	err = mgr.GetClient().Create(context.TODO(), daemonSet)
	if err != nil {
		setupLog.Error(err, "failed to create operator-daemon")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func getBridgeInformationDaemonSet() *apps.DaemonSet {
	trueP := true
	namespace := os.Getenv("POD_NAMESPACE")
	serviceAccountName := os.Getenv("POD_SERVICE_ACCOUNT")
	daemonImage := os.Getenv("DAEMON_IMAGE")
	return &apps.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bridge-operator-ds",
			Namespace: namespace,
			Labels: map[string]string{
				"tier": "node",
				"app":  "bridge-operator",
				"name": "bridge-operator",
			},
		},
		Spec: apps.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "bridge-operator",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{},
			},
			UpdateStrategy: apps.DaemonSetUpdateStrategy{
				Type: apps.RollingUpdateDaemonSetStrategyType,
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"tier": "node",
						"app":  "bridge-operator",
						"name": "bridge-operator",
					},
				},
				Spec: core.PodSpec{
					HostNetwork: true,
					NodeSelector: map[string]string{
						"kubernetes.io/os": "linux",
					},
					Tolerations: []core.Toleration{
						core.Toleration{
							Operator: core.TolerationOpExists,
							Effect:   core.TaintEffectNoSchedule,
						},
					},
					ServiceAccountName: serviceAccountName,
					Containers: []core.Container{
						core.Container{
							Name:  "bridge-operator-daemon",
							Image: daemonImage,
							Command: []string{
								"/usr/bin/bridge-operator-daemon",
							},
							Resources: core.ResourceRequirements{
								Requests: core.ResourceList{
									core.ResourceCPU:    resource.MustParse("100m"),
									core.ResourceMemory: resource.MustParse("80Mi"),
								},
								Limits: core.ResourceList{
									core.ResourceCPU:    resource.MustParse("100m"),
									core.ResourceMemory: resource.MustParse("150Mi"),
								},
							},
							SecurityContext: &core.SecurityContext{
								Privileged: &trueP,
								Capabilities: &core.Capabilities{
									Add: []core.Capability{
										"SYS_ADMIN",
										"NET_ADMIN",
									},
								},
							},
							VolumeMounts: []core.VolumeMount{
								core.VolumeMount{
									Name:      "host",
									MountPath: "/host",
								},
							},
						},
					},
					Volumes: []core.Volume{
						core.Volume{
							Name: "host",
							VolumeSource: core.VolumeSource{
								HostPath: &core.HostPathVolumeSource{
									Path: "/",
								},
							},
						},
					},
				},
			},
		},
	}
}
