// Copyright 2023 Antrea Authors.
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

package cloudentityselector

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/accountmanager"
	"antrea.io/nephe/pkg/controllers/sync"
)

// CloudEntitySelectorReconciler reconciles a CloudEntitySelector object.
// nolint:golint
type CloudEntitySelectorReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	AccManager accountmanager.Interface

	selectorToAccountMap map[types.NamespacedName]types.NamespacedName
	pendingCesSyncMap    map[types.NamespacedName]struct{}

	// indicates whether a controller can reconcile CRs.
	initialized bool
}

// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors/status,verbs=get;update;patch

func (r *CloudEntitySelectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudentityselector", req.NamespacedName)
	if !r.initialized {
		if err := sync.GetControllerSyncStatusInstance().WaitTillControllerIsInitialized(&r.initialized,
			sync.InitTimeout, sync.ControllerTypeCES); err != nil {
			return ctrl.Result{}, err
		}
	}

	defer r.updatePendingCesSyncAndStatus(req.NamespacedName)

	entitySelector := &crdv1alpha1.CloudEntitySelector{}
	err := r.Get(ctx, req.NamespacedName, entitySelector)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if errors.IsNotFound(err) {
		return ctrl.Result{}, r.processDelete(&req.NamespacedName)
	}

	if err = r.processCreateOrUpdate(entitySelector, &req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
}

func (r *CloudEntitySelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.selectorToAccountMap = make(map[types.NamespacedName]types.NamespacedName)
	r.pendingCesSyncMap = make(map[types.NamespacedName]struct{})
	// Using GenerationChangedPredicate to allow CES controller to receive CES updates
	// for all events except change in status.
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&crdv1alpha1.CloudEntitySelector{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r); err != nil {
		return err
	}

	return mgr.Add(r)
}

// Start performs the initialization of the controller.
// A controller is said to be initialized only when the dependent controllers
// are synced, and it keeps a count of pending CRs to be reconciled.
func (r *CloudEntitySelectorReconciler) Start(_ context.Context) error {
	if err := sync.GetControllerSyncStatusInstance().WaitForControllersToSync(
		[]sync.ControllerType{sync.ControllerTypeCPA}, sync.SyncTimeout); err != nil {
		r.Log.Error(err, "dependent controller sync failed", "controller",
			sync.ControllerTypeCPA.String())
		return err
	}
	cesList := &crdv1alpha1.CloudEntitySelectorList{}
	if err := r.Client.List(context.TODO(), cesList, &client.ListOptions{}); err != nil {
		return err
	}

	for _, ces := range cesList.Items {
		key := types.NamespacedName{Namespace: ces.Namespace, Name: ces.Name}
		r.pendingCesSyncMap[key] = struct{}{}
	}

	if len(r.pendingCesSyncMap) == 0 {
		r.setSyncStatusAndSyncInventory()
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", sync.ControllerTypeCES.String())
	return nil
}

// updatePendingCesSyncAndStatus updates pendingSyncCesMap and when length of pendingSyncCesMap is 0,
// sets the controller sync status and sync inventory.
func (r *CloudEntitySelectorReconciler) updatePendingCesSyncAndStatus(namespacedName types.NamespacedName) {
	if sync.GetControllerSyncStatusInstance().IsControllerSynced(sync.ControllerTypeCES) {
		return
	}
	delete(r.pendingCesSyncMap, namespacedName)
	if len(r.pendingCesSyncMap) == 0 {
		r.setSyncStatusAndSyncInventory()
	}
}

// setSyncStatusAndSyncInventory syncs inventory for all accounts and marks CES controller as synced.
func (r *CloudEntitySelectorReconciler) setSyncStatusAndSyncInventory() {
	r.AccManager.SyncAllAccounts()
	sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCES)
}

func (r *CloudEntitySelectorReconciler) processCreateOrUpdate(selector *crdv1alpha1.CloudEntitySelector,
	selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "create/update")

	var accountNamespace = selector.Spec.AccountNamespace
	// For upgrade purpose, use selector namespace as account namespace when not available, remove later.
	if accountNamespace == "" {
		accountNamespace = selector.Namespace
	}
	accountNamespacedName := &types.NamespacedName{
		Namespace: accountNamespace,
		Name:      selector.Spec.AccountName,
	}
	r.selectorToAccountMap[*selectorNamespacedName] = *accountNamespacedName
	retryCes, err := r.AccManager.AddResourceFiltersToAccount(accountNamespacedName, selectorNamespacedName,
		selector)
	if err != nil && retryCes {
		return err
	}
	r.updateStatus(selectorNamespacedName, err)
	return nil
}

func (r *CloudEntitySelectorReconciler) processDelete(selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "delete")
	accountNamespacedName, found := r.selectorToAccountMap[*selectorNamespacedName]
	if !found {
		return fmt.Errorf("failed to find account for selector %s", selectorNamespacedName.String())
	}
	delete(r.selectorToAccountMap, *selectorNamespacedName)
	_ = r.AccManager.RemoveResourceFiltersFromAccount(&accountNamespacedName, selectorNamespacedName)
	return nil
}

// updateStatus updates the status on the CloudEntitySelector CR.
func (r *CloudEntitySelectorReconciler) updateStatus(namespacedName *types.NamespacedName, err error) {
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	}

	updateStatusFunc := func() error {
		selector := &crdv1alpha1.CloudEntitySelector{}
		if err = r.Get(context.TODO(), *namespacedName, selector); err != nil {
			return nil
		}
		if selector.Status.Error != errorMsg {
			selector.Status.Error = errorMsg
			r.Log.Info("Setting CES status", "selector", namespacedName, "message", errorMsg)
			if err = r.Client.Status().Update(context.TODO(), selector); err != nil {
				r.Log.Error(err, "failed to update CES status, retrying", "selector", namespacedName)
				return err
			}
		}
		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, updateStatusFunc); err != nil {
		r.Log.Error(err, "failed to update CES status", "selector", namespacedName)
	}
}
