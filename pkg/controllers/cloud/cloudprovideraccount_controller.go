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

package cloud

import (
	"context"
	"k8s.io/apimachinery/pkg/util/wait"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
)

// CloudProviderAccountReconciler reconciles a CloudProviderAccount object.
// nolint:golint
type CloudProviderAccountReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	mutex               sync.Mutex
	accountProviderType map[types.NamespacedName]common.ProviderType
	accPollers          map[types.NamespacedName]*accountPoller
}

// nolint:lll
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts/status,verbs=get;update;patch

func (r *CloudProviderAccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudprovideraccount", req.NamespacedName)

	providerAccount := &cloudv1alpha1.CloudProviderAccount{}
	err := r.Get(ctx, req.NamespacedName, providerAccount)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if errors.IsNotFound(err) {
		err := r.processDelete(&req.NamespacedName)
		return ctrl.Result{}, err
	}

	err = r.processCreate(&req.NamespacedName, providerAccount)
	if err != nil {
		_ = r.processDelete(&req.NamespacedName)
	}

	return ctrl.Result{}, err
}

func (r *CloudProviderAccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	DefineIndexers(r.Log)
	r.accPollers = make(map[types.NamespacedName]*accountPoller)
	r.accountProviderType = make(map[types.NamespacedName]common.ProviderType)
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudv1alpha1.CloudProviderAccount{}).
		Complete(r)
}

func (r *CloudProviderAccountReconciler) processCreate(namespacedName *types.NamespacedName,
	account *cloudv1alpha1.CloudProviderAccount) error {
	accountCloudType, err := account.GetAccountProviderType()
	if err != nil {
		return err
	}
	cloudType := r.addAccountProviderType(namespacedName, accountCloudType)
	cloudInterface, err := cloudprovider.GetCloudInterface(cloudType)
	if err != nil {
		return err
	}
	err = cloudInterface.AddProviderAccount(r.Client, account)
	if err != nil {
		return err
	}

	err = cloudInterface.AddInventoryPoller(namespacedName)
	if err != nil {
		return err
	}
	accPoller, preExists := r.addAccountPoller(accountCloudType, namespacedName, account)
	/*
		// Whenever there is a CPA add, update global vpcCache(with vpc list applicable for this account).
		err = BuildVpcInventory(cloudInterface, r.Log, account.Namespace, namespacedName, string(account.UID))
		if err != nil {
			return err
		}
	*/

	if !preExists {
		go wait.Until(accPoller.doAccountPoller, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)
	}

	return nil
}

func (r *CloudProviderAccountReconciler) processDelete(namespacedName *types.NamespacedName) error {
	cloudType := r.getAccountProviderType(namespacedName)
	cloudInterface, err := cloudprovider.GetCloudInterface(cloudType)
	if err != nil {
		return err
	}
	err = cloudInterface.DeleteInventoryPoller(namespacedName)
	if err != nil {
		return err
	}
	cloudInterface.RemoveProviderAccount(namespacedName)
	r.removeAccountProviderType(namespacedName)

	r.removeAccountPoller(namespacedName)
	DeleteVpcInventory(r.Log, namespacedName)

	return nil
}

func (r *CloudProviderAccountReconciler) addAccountProviderType(namespacedName *types.NamespacedName,
	provider cloudv1alpha1.CloudProvider) common.ProviderType {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	providerType := common.ProviderType(provider)
	r.accountProviderType[*namespacedName] = providerType

	return providerType
}

func (r *CloudProviderAccountReconciler) removeAccountProviderType(namespacedName *types.NamespacedName) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.accountProviderType, *namespacedName)
}

func (r *CloudProviderAccountReconciler) getAccountProviderType(namespacedName *types.NamespacedName) common.ProviderType {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.accountProviderType[*namespacedName]
}

func (r *CloudProviderAccountReconciler) addAccountPoller(cloudType cloudv1alpha1.CloudProvider, namespacedName *types.NamespacedName,
	account *cloudv1alpha1.CloudProviderAccount) (*accountPoller, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.Log.Info("Test: inside addAccountPoller")
	if pollerScope, exists := r.accPollers[*namespacedName]; exists {
		r.Log.Info("poller exists", "account", namespacedName)
		return pollerScope, exists
	}

	poller := &accountPoller{
		Client:            r.Client,
		scheme:            r.Scheme,
		log:               r.Log,
		pollIntvInSeconds: *account.Spec.PollIntervalInSeconds,
		cloudType:         cloudType,
		namespacedName:    namespacedName,
		//selector:          selector.DeepCopy(),
		ch: make(chan struct{}),
	}

	r.accPollers[*namespacedName] = poller

	r.Log.Info("poller will be created", "account", namespacedName)
	return poller, false
}

func (r *CloudProviderAccountReconciler) removeAccountPoller(namespacedName *types.NamespacedName) *accountPoller {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	poller, found := r.accPollers[*namespacedName]
	if found {
		close(poller.ch)
		delete(r.accPollers, *namespacedName)
	}
	return poller
}

func (r *CloudProviderAccountReconciler) GetAccountPoller(name *types.NamespacedName) (*accountPoller, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if pollerScope, exists := r.accPollers[*name]; exists {
		r.Log.Info("poller exists", "account", name)
		return pollerScope, true
	}
	return nil, false
}
