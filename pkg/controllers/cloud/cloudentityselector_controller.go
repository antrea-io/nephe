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
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/utils"
)

const (
	virtualMachineIndexerByCloudAccount      = "virtualmachine.cloudaccount"
	virtualMachineSelectorMatchIndexerByID   = "virtualmachine.selector.id"
	virtualMachineSelectorMatchIndexerByName = "virtualmachine.selector.name"
	virtualMachineSelectorMatchIndexerByVPC  = "virtualmachine.selector.vpc.id"
)

// CloudEntitySelectorReconciler reconciles a CloudEntitySelector object.
// nolint:golint
type CloudEntitySelectorReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	mutex      sync.Mutex
	accPollers map[types.NamespacedName]*accountPoller
}

// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=crd.cloud.antrea.io,resources=cloudentityselectors/status,verbs=get;update;patch

func (r *CloudEntitySelectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("cloudentityselector", req.NamespacedName)

	entitySelector := &cloudv1alpha1.CloudEntitySelector{}
	err := r.Get(ctx, req.NamespacedName, entitySelector)
	if err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if errors.IsNotFound(err) {
		err = r.processDelete(&req.NamespacedName)
		return ctrl.Result{}, err
	}

	err = r.processCreateOrUpdate(entitySelector, &req.NamespacedName)

	return ctrl.Result{}, err
}

func (r *CloudEntitySelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.accPollers = make(map[types.NamespacedName]*accountPoller)
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &cloudv1alpha1.VirtualMachine{},
		virtualMachineIndexerByCloudAccount, func(obj client.Object) []string {
			vm := obj.(*cloudv1alpha1.VirtualMachine)
			owner := vm.GetOwnerReferences()
			if len(owner) == 0 {
				return nil
			}
			return []string{owner[0].Name}
		}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&cloudv1alpha1.CloudEntitySelector{}).
		Complete(r)
}

func (r *CloudEntitySelectorReconciler) processCreateOrUpdate(selector *cloudv1alpha1.CloudEntitySelector,
	selectorNamespacedName *types.NamespacedName) error {
	accPoller, preExists := r.addAccountPoller(selector)

	if selector.Spec.VMSelector != nil {
		// Indexer does not work with in-place update. Do delete->add.
		for _, vmSelector := range accPoller.vmSelector.List() {
			if err := accPoller.vmSelector.Delete(vmSelector.(*cloudv1alpha1.VirtualMachineSelector)); err != nil {
				r.Log.Error(err, "unable to delete selector from indexer",
					"VMSelector", vmSelector.(*cloudv1alpha1.VirtualMachineSelector))
			}
		}

		for i := range selector.Spec.VMSelector {
			if err := accPoller.vmSelector.Add(&selector.Spec.VMSelector[i]); err != nil {
				r.Log.Error(err, "unable to add selector into indexer",
					"VMSelector", selector.Spec.VMSelector[i])
			}
		}

		vmList := &cloudv1alpha1.VirtualMachineList{}
		if err := r.List(context.TODO(), vmList, client.MatchingFields{
			virtualMachineIndexerByCloudAccount: selector.Name}, client.InNamespace(selector.Namespace)); err != nil {
			r.Log.Error(err, "unable to get virtual machines for external entity selector",
				"Name", selector.Name)
		} else {
			var vms []*cloudv1alpha1.VirtualMachine
			for i := range vmList.Items {
				vm := &vmList.Items[i]
				// vm selector changed, trigger to recompute.
				vms = append(vms, vm)
			}
			if err := accPoller.doVirtualMachineOperations(vms); err != nil {
				r.Log.Error(err, "unable to update virtual machines")
			}
		}
	}

	cloudInterface, err := cloudprovider.GetCloudInterface(common.ProviderType(accPoller.cloudType))
	if err != nil {
		if !preExists {
			_ = r.processDelete(selectorNamespacedName)
		}
		return err
	}

	err = cloudInterface.AddAccountResourceSelector(accPoller.namespacedName, selector)
	if err != nil {
		if !preExists {
			_ = r.processDelete(selectorNamespacedName)
		}
		r.Log.Info("selector add failed", "selector", selectorNamespacedName,
			"poller-exists", preExists)
		return err
	}

	if !preExists {
		go wait.Until(accPoller.doAccountPoller, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)
	}
	return nil
}

func (r *CloudEntitySelectorReconciler) processDelete(selectorNamespacedName *types.NamespacedName) error {
	poller := r.removeAccountPoller(selectorNamespacedName)
	if poller == nil {
		return nil
	}
	cloudInterface, err := cloudprovider.GetCloudInterface(common.ProviderType(poller.cloudType))
	if err != nil {
		return err
	}
	cloudInterface.RemoveAccountResourcesSelector(poller.namespacedName, selectorNamespacedName.Name)

	return nil
}

func (r *CloudEntitySelectorReconciler) addAccountPoller(selector *cloudv1alpha1.CloudEntitySelector) (*accountPoller, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	selectorNamespacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Name,
	}

	if pollerScope, exists := r.accPollers[*selectorNamespacedName]; exists {
		r.Log.Info("poller exists", "selector", selectorNamespacedName)
		return pollerScope, exists
	}

	accountNamespacedName := &types.NamespacedName{
		Namespace: selector.Namespace,
		Name:      selector.Spec.AccountName,
	}
	account := &cloudv1alpha1.CloudProviderAccount{}
	_ = r.Get(context.TODO(), *accountNamespacedName, account)
	accountCloudType, err := utils.GetAccountProviderType(account)
	if err != nil {
		return nil, false
	}
	poller := &accountPoller{
		Client:            r.Client,
		scheme:            r.Scheme,
		log:               r.Log,
		pollIntvInSeconds: *account.Spec.PollIntervalInSeconds,
		cloudType:         accountCloudType,
		namespacedName:    accountNamespacedName,
		selector:          selector.DeepCopy(),
		ch:                make(chan struct{}),
	}
	poller.vmSelector = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			m := obj.(*cloudv1alpha1.VirtualMachineSelector)
			// Create a unique key for each VirtualMachineSelector.
			return fmt.Sprintf("%v-%v-%v", m.Agented, m.VpcMatch, m.VMMatch), nil
		},
		cache.Indexers{
			virtualMachineSelectorMatchIndexerByID: func(obj interface{}) ([]string, error) {
				m := obj.(*cloudv1alpha1.VirtualMachineSelector)
				if len(m.VMMatch) == 0 {
					return nil, nil
				}
				var match []string
				for _, vmMatch := range m.VMMatch {
					if len(vmMatch.MatchID) > 0 {
						match = append(match, strings.ToLower(vmMatch.MatchID))
					}
				}
				return match, nil
			},
			virtualMachineSelectorMatchIndexerByName: func(obj interface{}) ([]string, error) {
				m := obj.(*cloudv1alpha1.VirtualMachineSelector)
				if len(m.VMMatch) == 0 {
					return nil, nil
				}
				var match []string
				for _, vmMatch := range m.VMMatch {
					if len(vmMatch.MatchName) > 0 {
						match = append(match, strings.ToLower(vmMatch.MatchName))
					}
				}
				return match, nil
			},
			virtualMachineSelectorMatchIndexerByVPC: func(obj interface{}) ([]string, error) {
				m := obj.(*cloudv1alpha1.VirtualMachineSelector)
				if m.VpcMatch != nil && len(m.VpcMatch.MatchID) > 0 {
					return []string{strings.ToLower(m.VpcMatch.MatchID)}, nil
				}
				return nil, nil
			},
		})

	r.accPollers[*selectorNamespacedName] = poller

	r.Log.Info("poller will be created", "selector", selectorNamespacedName)
	return poller, false
}

func (r *CloudEntitySelectorReconciler) removeAccountPoller(selectorNamespacedName *types.NamespacedName) *accountPoller {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	poller, found := r.accPollers[*selectorNamespacedName]
	if found {
		close(poller.ch)
		delete(r.accPollers, *selectorNamespacedName)
	}
	return poller
}
