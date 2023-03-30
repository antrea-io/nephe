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
	"sync"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/config"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/controllers/utils"
)

// CloudProviderAccountReconciler reconciles a CloudProviderAccount object.
// nolint:golint
type CloudProviderAccountReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Mgr    *ctrl.Manager

	mutex               sync.Mutex
	accountProviderType map[types.NamespacedName]common.ProviderType
	Inventory           inventory.Interface
	Poller              *Poller
	pendingSyncCount    int
	initialized         bool
	watcher             watch.Interface
	clientset           kubernetes.Interface
}

// nolint:lll
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudprovideraccounts/status,verbs=get;update;patch

func (r *CloudProviderAccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudprovideraccount", req.NamespacedName)

	if !r.initialized {
		if err := GetControllerSyncStatusInstance().waitTillControllerIsInitialized(&r.initialized, initTimeout, ControllerTypeCPA); err != nil {
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
		_ = r.processDelete(&req.NamespacedName)
		return ctrl.Result{}, err
	}

	r.updatePendingSyncCountAndStatus()
	return ctrl.Result{}, nil
}

func (r *CloudProviderAccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.accountProviderType = make(map[types.NamespacedName]common.ProviderType)
	// Client in controller requires reconciler for each object that are under watch. So to avoid reconciler and use only
	// watch, that can be implemented by clientset.
	if r.clientset, err = kubernetes.NewForConfig(ctrl.GetConfigOrDie()); err != nil {
		r.Log.Error(err, "error creating client config")
		return err
	}
	if err := ctrl.NewControllerManagedBy(mgr).For(&crdv1alpha1.CloudProviderAccount{}).Complete(r); err != nil {
		return err
	}
	return mgr.Add(r)
}

// Start performs the initialization of the controller.
// A controller is said to be initialized only when the dependent controllers
// are synced, and controller keeps a count of pending CRs to be reconciled.
func (r *CloudProviderAccountReconciler) Start(ctx context.Context) error {
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
	r.Log.Info("Init done", "controller", ControllerTypeCPA.String())
	return nil
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
	GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeCPA)
	go func() {
		r.setupSecretWatcher()
	}()
}

func (r *CloudProviderAccountReconciler) processCreateOrUpdate(namespacedName *types.NamespacedName,
	account *crdv1alpha1.CloudProviderAccount) error {
	r.Log.Info("Received request", "account", namespacedName, "operation", "create/update")

	accountCloudType, err := utils.GetAccountProviderType(account)
	if err != nil {
		return fmt.Errorf("%s, err: %v", errorMsgAccountAddFail, err)
	}
	cloudType := r.addAccountProviderType(namespacedName, accountCloudType)
	cloudInterface, err := cloudprovider.GetCloudInterface(cloudType)
	if err != nil {
		return err
	}
	if err = cloudInterface.AddProviderAccount(r.Client, account); err != nil {
		return fmt.Errorf("%s, err: %v", errorMsgAccountAddFail, err)
	}

	accPoller, exists := r.Poller.addAccountPoller(accountCloudType, namespacedName, account, r)

	if !exists {
		if r.startPollingThread(namespacedName) {
			r.Log.Info("Creating account poller", "account", namespacedName)
			go wait.Until(accPoller.doAccountPolling, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)
		}
	} else {
		return r.Poller.restartAccountPoller(namespacedName)
	}

	return nil
}

func (r *CloudProviderAccountReconciler) processDelete(namespacedName *types.NamespacedName) error {
	r.Log.Info("Received request", "account", namespacedName, "operation", "delete")

	if err := r.Poller.removeAccountPoller(namespacedName); err != nil {
		return err
	}
	r.Log.V(1).Info("Removed account poller", "account", namespacedName)

	cloudType := r.getAccountProviderType(namespacedName)
	cloudInterface, err := cloudprovider.GetCloudInterface(cloudType)
	if err != nil {
		return err
	}

	if err = cloudInterface.DeleteInventoryPollCache(namespacedName); err != nil {
		return err
	}

	if err = r.Inventory.DeleteVpcsFromCache(namespacedName); err != nil {
		return err
	}

	if err = r.Inventory.DeleteVmsFromCache(namespacedName); err != nil {
		return err
	}

	cloudInterface.RemoveProviderAccount(namespacedName)
	r.removeAccountProviderType(namespacedName)

	return nil
}

// startPollingThread returns whether a polling thread needs to be started or not.
func (r *CloudProviderAccountReconciler) startPollingThread(namespacedName *types.NamespacedName) bool {
	// Check if a CES CR exits in the same namespace. If it exits, then do not start the polling thread
	// in CPA and let CES controller create the polling thread. This is an optimization done to avoid polling
	// twice for a given account, once for CPA and once for CES. Polling cloud is an expensive operation, so
	// try to avoid calling cloud multiple times for same operation.
	cesList := &crdv1alpha1.CloudEntitySelectorList{}
	listOptions := &client.ListOptions{
		Namespace: namespacedName.Namespace,
	}
	if err := r.Client.List(context.TODO(), cesList, listOptions); err != nil {
		r.Log.V(1).Info("Failed to get CES objects", "namespace", namespacedName.Namespace, "err", err)
		return true
	}

	for _, ces := range cesList.Items {
		if ces.Spec.AccountName == namespacedName.Name {
			r.Log.V(1).Info("Ignoring start of account poller", "account", namespacedName)
			return false
		}
	}
	return true
}

func (r *CloudProviderAccountReconciler) addAccountProviderType(namespacedName *types.NamespacedName,
	provider runtimev1alpha1.CloudProvider) common.ProviderType {
	providerType := common.ProviderType(provider)
	r.accountProviderType[*namespacedName] = providerType
	return providerType
}

func (r *CloudProviderAccountReconciler) removeAccountProviderType(namespacedName *types.NamespacedName) {
	delete(r.accountProviderType, *namespacedName)
}

func (r *CloudProviderAccountReconciler) getAccountProviderType(namespacedName *types.NamespacedName) common.ProviderType {
	return r.accountProviderType[*namespacedName]
}

// getCpaBySecret returns nil only when the Secret is not used by any CloudProvideAccount CR,
// otherwise the dependent CloudProvideAccount CRs will be returned.
func (r *CloudProviderAccountReconciler) getCpaBySecret(s types.NamespacedName) (error, []crdv1alpha1.CloudProviderAccount) {
	cpaList := &crdv1alpha1.CloudProviderAccountList{}
	if err := r.Client.List(context.TODO(), cpaList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("failed to get CloudProviderAccount list, err %v", err), nil
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
	return nil, cpaItems
}

// watchSecret watch the Secret objects.
func (r *CloudProviderAccountReconciler) watchSecret() {
	for {
		event, ok := <-r.watcher.ResultChan()
		if !ok {
			r.resetSecretWatcher()
		} else {
			switch event.Type {
			case watch.Modified:
				r.mutex.Lock()
				secret := event.Object.(*v1.Secret)
				namespacedName := types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}
				r.Log.WithName("Secret").Info("Received request", "Secret", namespacedName, "operation", "update")
				err, cpaItems := r.getCpaBySecret(namespacedName)
				if err != nil {
					r.Log.WithName("Secret").Error(err, "error getting CPA by Secret", "account", namespacedName)
				}
				for _, cpa := range cpaItems {
					accountNamespacedName := types.NamespacedName{Namespace: cpa.Namespace, Name: cpa.Name}
					err := r.processCreateOrUpdate(&accountNamespacedName, &cpa)
					if err != nil {
						r.Log.WithName("Secret").Error(err, "error updating account", "account", accountNamespacedName)
					} else {
						r.Log.WithName("Secret").Info("Done processing secret", "account", accountNamespacedName)
					}
				}
				r.mutex.Unlock()
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
		if r.watcher, err = r.clientset.CoreV1().Secrets(config.GetPodNamespace()).Watch(context.Background(), metav1.ListOptions{}); err != nil {
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
