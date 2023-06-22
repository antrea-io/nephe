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
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/accountmanager"
	"antrea.io/nephe/pkg/apiserver"
	nephewebhook "antrea.io/nephe/pkg/apiserver/webhook"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/controllers/cloudentityselector"
	"antrea.io/nephe/pkg/controllers/cloudprovideraccount"
	"antrea.io/nephe/pkg/controllers/networkpolicy"
	"antrea.io/nephe/pkg/controllers/sync"
	"antrea.io/nephe/pkg/controllers/virtualmachine"
	"antrea.io/nephe/pkg/inventory"
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
	_ = antreav1alpha1.AddToScheme(scheme)
	_ = antreav1alpha2.AddToScheme(scheme)
	_ = crdv1alpha1.AddToScheme(scheme)
	_ = runtimev1alpha1.AddToScheme(scheme)

	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var enableDebugLog bool

	opts := newOptions()
	flag.StringVar(&opts.configFile, "config", opts.configFile, "The path to the configuration file.")
	flag.StringVar(&metricsAddr, "metrics-addr", defaultMetricsAddress, "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", defaultLeaderElectionFlag,
		"Enable leader election for nephe-controller manager. "+
			"Enabling this will ensure there is only one active nephe-controller manager.")
	flag.BoolVar(&enableDebugLog, "enable-debug-log", defaultDebugLogFlag,
		"Enable debug mode for nephe-controller manager. Enabling this will add debug logs")
	flag.Parse()

	logging.SetDebugLog(enableDebugLog)
	ctrl.SetLogger(logging.GetLogger("setup"))

	if err := opts.complete(); err != nil {
		setupLog.Error(err, "invalid nephe-controller configuration")
		os.Exit(1)
	}

	setupLog.Info("Nephe ConfigMap", "ControllerConfig", opts.config)
	cloudresource.SetCloudResourcePrefix(opts.config.CloudResourcePrefix)

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

	// Initialize vpc inventory cache.
	cloudInventory := inventory.InitInventory()

	accountManager := &accountmanager.AccountManager{
		Client:    mgr.GetClient(),
		Log:       logging.GetLogger("accountManager"), // TODO: Check logging
		Inventory: cloudInventory,
	}
	accountManager.ConfigureAccountManager()

	// Configure controller sync status.
	sync.GetControllerSyncStatusInstance().Configure()

	// Create a VM controller, configure converter, and start it in a separate thread.
	vmController := &virtualmachine.VirtualMachineController{
		Client:    mgr.GetClient(),
		Log:       logging.GetLogger("controllers").WithName("VirtualMachine"),
		Scheme:    mgr.GetScheme(),
		Inventory: cloudInventory,
	}
	vmController.ConfigureConverterAndStart()

	if err = (&cloudentityselector.CloudEntitySelectorReconciler{
		Client:     mgr.GetClient(),
		Log:        logging.GetLogger("controllers").WithName("CloudEntitySelector"),
		Scheme:     mgr.GetScheme(),
		AccManager: accountManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudEntitySelector")
		os.Exit(1)
	}

	if err = (&cloudprovideraccount.CloudProviderAccountReconciler{
		Client:     mgr.GetClient(),
		Log:        logging.GetLogger("controllers").WithName("CloudProviderAccount"),
		Scheme:     mgr.GetScheme(),
		AccManager: accountManager,
		Mgr:        &mgr,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CloudProviderAccount")
		os.Exit(1)
	}

	npController := &networkpolicy.NetworkPolicyReconciler{
		Client:            mgr.GetClient(),
		Log:               logging.GetLogger("controllers").WithName("NetworkPolicy"),
		Scheme:            mgr.GetScheme(),
		CloudSyncInterval: opts.config.CloudSyncInterval,
		Inventory:         cloudInventory,
	}

	if err = npController.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetworkPolicy")
		os.Exit(1)
	}

	if err = (&apiserver.NepheControllerAPIServer{}).SetupWithManager(mgr,
		npController.GetVirtualMachinePolicyIndexer(), cloudInventory, logging.GetLogger("apiServer")); err != nil {
		setupLog.Error(err, "unable to create APIServer")
		os.Exit(1)
	}

	configureWebhooks(mgr)

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func configureWebhooks(mgr ctrl.Manager) {
	// Register webhook for CloudProviderAccount Mutator.
	mgr.GetWebhookServer().Register("/mutate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount",
		&webhook.Admission{Handler: &nephewebhook.CPAMutator{Client: mgr.GetClient(),
			Log: logging.GetLogger("webhook").WithName("CloudProviderAccount")}})

	// Register webhook for CloudProviderAccount Validator.
	mgr.GetWebhookServer().Register("/validate-crd-cloud-antrea-io-v1alpha1-cloudprovideraccount",
		&webhook.Admission{Handler: &nephewebhook.CPAValidator{Client: mgr.GetClient(),
			Log: logging.GetLogger("webhook").WithName("CloudProviderAccount")}})

	// Register webhook for CloudEntitySelector Mutator.
	mgr.GetWebhookServer().Register("/mutate-crd-cloud-antrea-io-v1alpha1-cloudentityselector",
		&webhook.Admission{Handler: &nephewebhook.CESMutator{Client: mgr.GetClient(),
			Sh:  mgr.GetScheme(),
			Log: logging.GetLogger("webhook").WithName("CloudEntitySelector")}})

	// Register webhook for CloudEntitySelector Validator.
	mgr.GetWebhookServer().Register("/validate-crd-cloud-antrea-io-v1alpha1-cloudentityselector",
		&webhook.Admission{Handler: &nephewebhook.CESValidator{Client: mgr.GetClient(),
			Log: logging.GetLogger("webhook").WithName("CloudEntitySelector")}})
}
