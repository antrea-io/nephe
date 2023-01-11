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
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/controllers/utils"
)

// CloudProviderAccountReconciler reconciles a CloudProviderAccount object.
// nolint:golint
type CloudProviderAccountReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	mutex               sync.Mutex
	accountProviderType map[types.NamespacedName]common.ProviderType
	Inventory           *inventory.Inventory
	Poller              *Poller
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
	r.accountProviderType = make(map[types.NamespacedName]common.ProviderType)
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudv1alpha1.CloudProviderAccount{}).
		Complete(r)
}

func (r *CloudProviderAccountReconciler) processCreate(namespacedName *types.NamespacedName,
	account *cloudv1alpha1.CloudProviderAccount) error {
	accountCloudType, err := utils.GetAccountProviderType(account)
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

	accPoller, preExists := r.Poller.addAccountPoller(accountCloudType, namespacedName, account, r)

	if !preExists {
		go wait.Until(accPoller.doAccountPoller, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)
	}

	return nil
}

func (r *CloudProviderAccountReconciler) processDelete(namespacedName *types.NamespacedName) error {
	r.Log.V(1).Info("remove account poller", "account", namespacedName.String())
	err := r.Poller.removeAccountPoller(namespacedName)
	if err != nil {
		return err
	}

	err = r.Inventory.DeleteVpcCache(namespacedName)
	if err != nil {
		return err
	}

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
