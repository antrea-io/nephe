// Copyright 2022 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/apiserver"
	controllers "antrea.io/nephe/pkg/controllers/cloud"
	"antrea.io/nephe/pkg/logging"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = antreanetworking.AddToScheme(scheme)
	_ = antreatypes.AddToScheme(scheme)
	_ = crdv1alpha1.AddToScheme(scheme)
	_ = runtimev1alpha1.AddToScheme(scheme)

	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableDebugLog bool

	flag.StringVar(&metricsAddr, "metrics-addr", defaultMetricsAddress, "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", defaultLeaderElectionFlag,
		"Enable leader election for nephe-controller manager. "+
			"Enabling this will ensure there is only one active nephe-controller manager.")
	flag.BoolVar(&enableDebugLog, "enable-debug-log", defaultDebugLogFlag,
		"Enable debug mode for nephe-controller manager. Enabling this will add debug logs")
	flag.Parse()

	logging.SetDebugLog(enableDebugLog)
	ctrl.SetLogger(logging.GetLogger("setup"))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   electionID,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.CloudEntitySelectorReconciler{
		Client: mgr.GetClient(),
		Log:    logging.GetLogger("controllers").WithName("CloudEntitySelector"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudEntitySelector")
		os.Exit(1)
	}

	if err = (&controllers.CloudProviderAccountReconciler{
		Client: mgr.GetClient(),
		Log:    logging.GetLogger("controllers").WithName("CloudProviderAccount"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudProviderAccount")
		os.Exit(1)
	}
	if err = (&controllers.ExternalEntityReconciler{
		Client: mgr.GetClient(),
		Log:    logging.GetLogger("controllers").WithName("ExternalEntity"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ExternalEntity")
		os.Exit(1)
	}

	npController := &controllers.NetworkPolicyReconciler{
		Client: mgr.GetClient(),
		Log:    logging.GetLogger("controllers").WithName("NetworkPolicy"),
		Scheme: mgr.GetScheme(),
	}

	if err = npController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetworkPolicy")
		os.Exit(1)
	}

	vmManager := &controllers.VirtualMachineReconciler{
		Client: mgr.GetClient(),
		Log:    logging.GetLogger("controllers").WithName("VirtualMachine"),
		Scheme: mgr.GetScheme(),
	}
	if err = vmManager.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VirtualMachine")
		os.Exit(1)
	}
	if err = (&crdv1alpha1.VirtualMachine{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VirtualMachine")
		os.Exit(1)
	}
	if err = (&crdv1alpha1.CloudEntitySelector{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudEntitySelector")
		os.Exit(1)
	}

	if err = (&crdv1alpha1.CloudProviderAccount{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "CloudProviderAccount")
		os.Exit(1)
	}

	if err = (&apiserver.NepheControllerAPIServer{}).SetupWithManager(mgr,
		npController.GetVirtualMachinePolicyIndexer(), logging.GetLogger("apiServer")); err != nil {
		setupLog.Error(err, "unable to create APIServer")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
