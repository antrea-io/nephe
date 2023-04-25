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
	sync2 "antrea.io/nephe/pkg/controllers/sync"
)

// CloudEntitySelectorReconciler reconciles a CloudEntitySelector object.
// nolint:golint
type CloudEntitySelectorReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	//mutex  sync.Mutex

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
		if err := sync2.GetControllerSyncStatusInstance().WaitTillControllerIsInitialized(&r.initialized,
			sync2.InitTimeout, sync2.ControllerTypeCES); err != nil {
			return ctrl.Result{}, err
		}
	}
	/*
		r.Log.Info("Test: acquiring lock")
		r.mutex.Lock()
		defer func() {
			r.Log.Info("Test: releaseing lock")
			r.mutex.Unlock()
		}()
	*/

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
	if err := sync2.GetControllerSyncStatusInstance().WaitForControllersToSync(
		[]sync2.ControllerType{sync2.ControllerTypeCPA}, sync2.SyncTimeout); err != nil {
		r.Log.Error(err, "dependent controller sync failed", "controller",
			sync2.ControllerTypeCPA.String())
		return err
	}
	cesList := &crdv1alpha1.CloudEntitySelectorList{}
	if err := r.Client.List(context.TODO(), cesList, &client.ListOptions{}); err != nil {
		return err
	}

	r.pendingSyncCount = len(cesList.Items)
	if r.pendingSyncCount == 0 {
		sync2.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync2.ControllerTypeCES)
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", sync2.ControllerTypeCES.String())
	return nil
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *CloudEntitySelectorReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount > 0 {
		r.pendingSyncCount--
		if r.pendingSyncCount == 0 {
			sync2.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync2.ControllerTypeCES)
		}
	}
}

func (r *CloudEntitySelectorReconciler) processCreateOrUpdate(selector *crdv1alpha1.CloudEntitySelector,
	selectorNamespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "selector", selectorNamespacedName, "operation", "create/update")

	accountNamespacedName := &types.NamespacedName{
		Namespace: selector.Spec.AccountNamespace,
		Name:      selector.Spec.AccountName,
	}
	r.selectorToAccountMap[*selectorNamespacedName] = *accountNamespacedName
	retry, err := r.AccManager.AddResourceFiltersToAccount(accountNamespacedName, selectorNamespacedName,
		selector, false)
	if err != nil && retry {
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
