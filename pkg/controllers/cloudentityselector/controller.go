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

	selectorToAccountMap map[types.NamespacedName]types.NamespacedName
	AccManager           accountmanager.Interface
	pendingSyncCount     int
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

	entitySelector := &crdv1alpha1.CloudEntitySelector{}
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

	r.pendingSyncCount = len(cesList.Items)
	if r.pendingSyncCount == 0 {
		sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCES)
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", sync.ControllerTypeCES.String())
	return nil
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *CloudEntitySelectorReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount > 0 {
		r.pendingSyncCount--
		if r.pendingSyncCount == 0 {
			sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCES)
		}
	}
}

func (r *CloudEntitySelectorReconciler) processCreateOrUpdate(selector *crdv1alpha1.CloudEntitySelector,
	selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "create/update")

	accountNamespacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Spec.AccountName,
	}
	r.selectorToAccountMap[*selectorNamespacedName] = *accountNamespacedName
	retry, err := r.AccManager.AddResourceFiltersToAccount(accountNamespacedName, selectorNamespacedName,
		selector, false)
	if err != nil && retry {
		return err
	}
	r.setStatus(selectorNamespacedName, err)
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

// setStatus sets the status on the CloudEntitySelector CR.
func (r *CloudEntitySelectorReconciler) setStatus(namespacedName *types.NamespacedName, err error) {
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	}

	selector := &crdv1alpha1.CloudEntitySelector{}
	if err = r.Get(context.TODO(), *namespacedName, selector); err != nil {
		return
	}
	if selector.Status.Error != errorMsg {
		selector.Status.Error = errorMsg
		r.Log.Info("Setting selector status", "selector", namespacedName, "message", errorMsg)
		if err = r.Client.Status().Update(context.TODO(), selector); err != nil {
			r.Log.Error(err, "failed to update selector status", "selector", namespacedName)
		}
	}
}
