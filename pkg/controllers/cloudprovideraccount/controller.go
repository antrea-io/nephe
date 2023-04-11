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

package cloudprovideraccount

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/accountmanager"
	controllersync "antrea.io/nephe/pkg/controllers/sync"
	"antrea.io/nephe/pkg/util"
	"antrea.io/nephe/pkg/util/env"
)

// CloudProviderAccountReconciler reconciles a CloudProviderAccount object.
// nolint:golint
type CloudProviderAccountReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Mgr    *ctrl.Manager

	mutex            sync.Mutex
	AccManager       accountmanager.Interface
	pendingSyncCount int
	initialized      bool
	watcher          watch.Interface
	clientset        kubernetes.Interface
}

// nolint:lll
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts/status,verbs=get;update;patch

func (r *CloudProviderAccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudprovideraccount", req.NamespacedName)

	if !r.initialized {
		if err := controllersync.GetControllerSyncStatusInstance().WaitTillControllerIsInitialized(&r.initialized, controllersync.InitTimeout,
			controllersync.ControllerTypeCPA); err != nil {
			return ctrl.Result{}, err
		}
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()

	providerAccount := &crdv1alpha1.CloudProviderAccount{}
	if err := r.Get(ctx, req.NamespacedName, providerAccount); err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.processDelete(&req.NamespacedName)
	}

	if err := r.processCreateOrUpdate(&req.NamespacedName, providerAccount); err != nil {
		return ctrl.Result{}, err
	}

	r.updatePendingSyncCountAndStatus()
	return ctrl.Result{}, nil
}

func (r *CloudProviderAccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	// Client in controller requires reconciler for each object that are under watch. So to avoid reconciler and use only
	// watch, that can be implemented by clientset.
	if r.clientset, err = kubernetes.NewForConfig(ctrl.GetConfigOrDie()); err != nil {
		r.Log.Error(err, "error creating client config")
		return err
	}

	// Using GenerationChangedPredicate to allow CPA controller to receive CPA updates
	// for all events except change in status.
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&crdv1alpha1.CloudProviderAccount{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r); err != nil {
		return err
	}
	return mgr.Add(r)
}

// Start performs the initialization of the controller.
// A controller is said to be initialized only when the dependent controllers
// are synced, and controller keeps a count of pending CRs to be reconciled.
func (r *CloudProviderAccountReconciler) Start(_ context.Context) error {
	r.Log.Info("Waiting for shared informer caches to be synced")
	// Blocking call to wait till the informer caches are synced by controller run-time
	// or the context is Done.
	if !(*r.Mgr).GetCache().WaitForCacheSync(context.TODO()) {
		return fmt.Errorf("failed to sync shared informer cache")
	}

	cpaList := &crdv1alpha1.CloudProviderAccountList{}
	if err := r.Client.List(context.TODO(), cpaList, &client.ListOptions{}); err != nil {
		return err
	}

	r.pendingSyncCount = len(cpaList.Items)
	if r.pendingSyncCount == 0 {
		r.setSyncStatusAndSecretWatcher()
	}
	r.initialized = true
	r.Log.Info("Init done", "controller", controllersync.ControllerTypeCPA.String())
	return nil
}

func (r *CloudProviderAccountReconciler) processCreateOrUpdate(namespacedName *types.NamespacedName,
	account *crdv1alpha1.CloudProviderAccount) error {
	r.Log.Info("Received request", "account", namespacedName, "operation", "create/update")
	accountCloudType, err := util.GetAccountProviderType(account)
	if err != nil {
		return fmt.Errorf("%s: %v", accountmanager.ErrorMsgAddOrUpdateAccount, err)
	}
	if err = r.AccManager.AddAccount(namespacedName, accountCloudType, account); err != nil {
		account.Status.Error = err.Error()
		r.Log.Info("Setting account status", "account", namespacedName, "err", err)
		if err = r.Client.Status().Update(context.TODO(), account); err != nil {
			return fmt.Errorf("failed to update account status, account %v err %v", namespacedName, err)
		}
	}
	return nil
}

func (r *CloudProviderAccountReconciler) processDelete(namespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "account", namespacedName, "operation", "delete")
	return r.AccManager.RemoveAccount(namespacedName)
}

// updatePendingSyncCountAndStatus decrements the pendingSyncCount and when
// pendingSyncCount is 0, sets the sync status.
func (r *CloudProviderAccountReconciler) updatePendingSyncCountAndStatus() {
	if r.pendingSyncCount > 0 {
		r.pendingSyncCount--
		if r.pendingSyncCount == 0 {
			r.setSyncStatusAndSecretWatcher()
		}
	}
}

// setSyncStatusAndSecretWatcher sets the controller sync status and watcher.
func (r *CloudProviderAccountReconciler) setSyncStatusAndSecretWatcher() {
	controllersync.GetControllerSyncStatusInstance().SetControllerSyncStatus(controllersync.ControllerTypeCPA)
	go func() {
		r.setupSecretWatcher()
	}()
}

// getCpaBySecret returns nil only when the Secret is not used by any CloudProvideAccount CR,
// otherwise the dependent CloudProvideAccount CRs will be returned.
func (r *CloudProviderAccountReconciler) getCpaBySecret(s types.NamespacedName) ([]crdv1alpha1.CloudProviderAccount, error) {
	cpaList := &crdv1alpha1.CloudProviderAccountList{}
	if err := r.Client.List(context.TODO(), cpaList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("failed to get CloudProviderAccount list, err %v", err)
	}
	var cpaItems []crdv1alpha1.CloudProviderAccount
	for _, cpa := range cpaList.Items {
		if cpa.Spec.AWSConfig != nil {
			if cpa.Spec.AWSConfig.SecretRef.Name == s.Name &&
				cpa.Spec.AWSConfig.SecretRef.Namespace == s.Namespace {
				cpaItems = append(cpaItems, cpa)
			}
		}

		if cpa.Spec.AzureConfig != nil {
			if cpa.Spec.AzureConfig.SecretRef.Name == s.Name &&
				cpa.Spec.AzureConfig.SecretRef.Namespace == s.Namespace {
				cpaItems = append(cpaItems, cpa)
			}
		}
	}
	return cpaItems, nil
}

// watchSecret watch the Secret objects.
func (r *CloudProviderAccountReconciler) watchSecret() {
	logWithSecret := r.Log.WithName("Secret")
	for {
		event, ok := <-r.watcher.ResultChan()
		if !ok {
			r.resetSecretWatcher()
		} else {
			secret := event.Object.(*v1.Secret)
			namespacedName := types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}
			switch event.Type {
			case watch.Modified:
				r.mutex.Lock()
				logWithSecret.Info("Received request", "Secret", namespacedName, "operation", "update")
				cpaItems, err := r.getCpaBySecret(namespacedName)
				if err != nil {
					r.Log.WithName("Secret").Error(err, "error getting CPA by Secret", "account", namespacedName)
				}
				for _, cpa := range cpaItems {
					accountNamespacedName := types.NamespacedName{Namespace: cpa.Namespace, Name: cpa.Name}
					if err := r.processCreateOrUpdate(&accountNamespacedName, &cpa); err != nil {
						logWithSecret.Error(err, "error updating account", "account", accountNamespacedName)
					} else {
						logWithSecret.Info("Done processing Secret update", "account", accountNamespacedName)
					}
				}
				r.mutex.Unlock()
			case watch.Deleted:
				logWithSecret.Info("Received request", "Secret", namespacedName, "operation", "delete")
				cpaItems, err := r.getCpaBySecret(namespacedName)
				if err != nil {
					logWithSecret.Error(err, "error getting CPA by Secret", "account", namespacedName)
				}
				for _, cpa := range cpaItems {
					accountNamespacedName := types.NamespacedName{Namespace: cpa.Namespace, Name: cpa.Name}
					logWithSecret.Info("Done processing Secret delete, update/delete stale account", "account", accountNamespacedName)
				}
			case watch.Error:
				r.resetSecretWatcher()
			}
		}
	}
}

// resetSecretWatcher create a watcher for Secret
func (r *CloudProviderAccountReconciler) resetSecretWatcher() {
	var err error
	for {
		if r.watcher, err = r.clientset.CoreV1().Secrets(env.GetPodNamespace()).Watch(context.Background(), metav1.ListOptions{}); err != nil {
			r.Log.WithName("Secret").Error(err, "error creating Secret watcher")
			time.Sleep(time.Second * 5)
			continue
		}
		break
	}
}

// setupSecretWatcher set up a watcher for Secret objects.
func (r *CloudProviderAccountReconciler) setupSecretWatcher() {
	r.resetSecretWatcher()
	r.watchSecret()
}
