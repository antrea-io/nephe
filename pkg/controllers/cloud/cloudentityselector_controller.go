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
	"fmt"
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
)

const (
	virtualMachineSelectorMatchIndexerByID   = "virtualmachine.selector.id"
	virtualMachineSelectorMatchIndexerByName = "virtualmachine.selector.name"
	virtualMachineSelectorMatchIndexerByVPC  = "virtualmachine.selector.vpc.id"

	// To poll cloud inventory synchronously.
	defaultPollTimeout = 60 * time.Second
)

// CloudEntitySelectorReconciler reconciles a CloudEntitySelector object.
// nolint:golint
type CloudEntitySelectorReconciler struct {
	client.Client
	Log                  logr.Logger
	Scheme               *runtime.Scheme
	selectorToAccountMap map[types.NamespacedName]types.NamespacedName
	Poller               *Poller
	pendingSyncCount     int
	// indicates whether a controller can reconcile CRs.
	initialized bool
}

// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors/status,verbs=get;update;patch

func (r *CloudEntitySelectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudentityselector", req.NamespacedName)

	if !r.initialized {
		if err := GetControllerSyncStatusInstance().waitTillControllerIsInitialized(&r.initialized, initTimeout, ControllerTypeCES); err != nil {
			return ctrl.Result{}, err
		}
	}

	entitySelector := &cloudv1alpha1.CloudEntitySelector{}
	err := r.Get(ctx, req.NamespacedName, entitySelector)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if errors.IsNotFound(err) {
		err = r.processDelete(&req.NamespacedName)
		return ctrl.Result{}, err
	}

	if err = r.processCreateOrUpdate(entitySelector, &req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}
	r.updatePendingSyncCountAndStatus()
	return ctrl.Result{}, err
}

func (r *CloudEntitySelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.selectorToAccountMap = make(map[types.NamespacedName]types.NamespacedName)
	if err := ctrl.NewControllerManagedBy(mgr).For(&cloudv1alpha1.CloudEntitySelector{}).Complete(r); err != nil {
		return err
	}
	return mgr.Add(r)
}

// Start performs the initialization of the controller.
// A controller is said to be initialized only when the dependent controllers
// are synced, and it keeps a count of pending CRs to be reconciled.
func (r *CloudEntitySelectorReconciler) Start(stop context.Context) error {
	if err := GetControllerSyncStatusInstance().waitForControllersToSync([]controllerType{ControllerTypeCPA}, syncTimeout); err != nil {
		r.Log.Error(err, "dependent controller sync failed", "controller", ControllerTypeCPA.String())
		return err
	}
	cesList := &cloudv1alpha1.CloudEntitySelectorList{}
	if err := r.Client.List(context.TODO(), cesList, &client.ListOptions{}); err != nil {
		return err
	}

	r.pendingSyncCount = len(cesList.Items)
	if r.pendingSyncCount == 0 {
		GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeCES)
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", ControllerTypeCES.String())
	return nil
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *CloudEntitySelectorReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount > 0 {
		r.pendingSyncCount--
		if r.pendingSyncCount == 0 {
			GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeCES)
		}
	}
}

// waitForPollDone waits until account poller has polled the cloud inventory or timeout is reached.
func (r *CloudEntitySelectorReconciler) waitForPollDone(accountNamespacedName *types.NamespacedName, timeout time.Duration) error {
	pollerScope, err := r.Poller.getAccountPoller(accountNamespacedName)
	if err != nil {
		return err
	}

	r.Log.Info("Poll cloud inventory synchronously", "account", *accountNamespacedName)
	if err = wait.PollImmediate(100*time.Millisecond, timeout, func() (done bool, err error) {
		pollerScope.mutex.RLock()
		defer pollerScope.mutex.RUnlock()
		if pollerScope.pollDone {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed to poll cloud inventory, err %v", err)
	}
	return nil
}

func (r *CloudEntitySelectorReconciler) processCreateOrUpdate(selector *cloudv1alpha1.CloudEntitySelector,
	selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "create/update")

	accountNamespacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Spec.AccountName,
	}
	cloudType, err := r.Poller.getCloudType(accountNamespacedName)
	if err != nil {
		return fmt.Errorf("%s, %s %v", err.Error(), errorMsgSelectorAddFail, selectorNamespacedName)
	}

	cloudInterface, err := cloudprovider.GetCloudInterface(common.ProviderType(cloudType))
	if err != nil {
		return fmt.Errorf("%s %s, err: %v", errorMsgSelectorAddFail, selectorNamespacedName.Name, err)
	}

	r.selectorToAccountMap[*selectorNamespacedName] = *accountNamespacedName

	callCESDelete := false
	defer func() {
		if callCESDelete {
			_ = r.processDelete(selectorNamespacedName)
		}
	}()

	if err = cloudInterface.AddAccountResourceSelector(accountNamespacedName, selector); err != nil {
		r.Log.Info(errorMsgSelectorAddFail, "selector", selectorNamespacedName, "error", err)
		callCESDelete = true
		return fmt.Errorf("%s %s, err: %v", errorMsgSelectorAddFail, selectorNamespacedName.Name, err)
	}

	if err = r.Poller.updateAccountPoller(accountNamespacedName, selector); err != nil {
		r.Log.Info(errorMsgSelectorAddFail, "selector", selectorNamespacedName, "error", err)
		callCESDelete = true
		return err
	}

	if err = r.Poller.restartAccountPoller(accountNamespacedName); err != nil {
		r.Log.Info(errorMsgSelectorAddFail, "selector", selectorNamespacedName, "error", err)
		callCESDelete = true
		return err
	}

	return r.waitForPollDone(accountNamespacedName, defaultPollTimeout)
}

func (r *CloudEntitySelectorReconciler) processDelete(selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "delete")

	accountNamespacedName, found := r.selectorToAccountMap[*selectorNamespacedName]
	if !found {
		return fmt.Errorf("%s %s", errorMsgSelectorAccountMapNotFound, selectorNamespacedName.String())
	}
	tempAccountNamespacedName := accountNamespacedName
	delete(r.selectorToAccountMap, *selectorNamespacedName)

	cloudType, err := r.Poller.getCloudType(&tempAccountNamespacedName)
	if err != nil {
		r.Log.Info("account poller is already deleted", "error", err)
		return nil
	}
	cloudInterface, err := cloudprovider.GetCloudInterface(common.ProviderType(cloudType))
	if err != nil {
		return err
	}

	cloudInterface.RemoveAccountResourcesSelector(&tempAccountNamespacedName, selectorNamespacedName.Name)

	if err := r.Poller.restartAccountPoller(&tempAccountNamespacedName); err != nil {
		return err
	}

	return nil
}
