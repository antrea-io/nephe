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

	"antrea.io/nephe/pkg/logging"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/inventory"
)

type accountPoller struct {
	client.Client
	log    logr.Logger
	scheme *runtime.Scheme

	pollIntvInSeconds uint
	pollDone          bool
	cloudType         runtimev1alpha1.CloudProvider
	namespacedName    *types.NamespacedName
	selector          *crdv1alpha1.CloudEntitySelector
	vmSelector        cache.Indexer
	ch                chan struct{}
	mutex             sync.RWMutex
	inventory         inventory.Interface
}

type Poller struct {
	accPollers map[types.NamespacedName]*accountPoller
	mutex      sync.Mutex
	log        logr.Logger
}

// InitPollers function creates an instance of Poller struct and initializes it.
func InitPollers() *Poller {
	poller := &Poller{
		accPollers: make(map[types.NamespacedName]*accountPoller),
		log:        logging.GetLogger("poller").WithName("AccountPoller"),
	}
	return poller
}

// addAccountPoller creates an account poller for a given account and adds it to accPollers map.
func (p *Poller) addAccountPoller(cloudType runtimev1alpha1.CloudProvider, namespacedName *types.NamespacedName,
	account *crdv1alpha1.CloudProviderAccount, r *CloudProviderAccountReconciler) (*accountPoller, bool) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if pollerScope, exists := p.accPollers[*namespacedName]; exists {
		p.log.Info("Poller exists", "account", namespacedName)
		return pollerScope, exists
	}

	poller := &accountPoller{
		Client:            r.Client,
		scheme:            r.Scheme,
		log:               p.log,
		pollIntvInSeconds: *account.Spec.PollIntervalInSeconds,
		cloudType:         cloudType,
		namespacedName:    namespacedName,
		selector:          nil,
		ch:                make(chan struct{}),
		inventory:         r.Inventory,
	}

	poller.vmSelector = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			m := obj.(*crdv1alpha1.VirtualMachineSelector)
			// Create a unique key for each VirtualMachineSelector.
			return fmt.Sprintf("%v-%v-%v", m.Agented, m.VpcMatch, m.VMMatch), nil
		},
		cache.Indexers{
			virtualMachineSelectorMatchIndexerByID: func(obj interface{}) ([]string, error) {
				m := obj.(*crdv1alpha1.VirtualMachineSelector)
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
				m := obj.(*crdv1alpha1.VirtualMachineSelector)
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
				m := obj.(*crdv1alpha1.VirtualMachineSelector)
				if m.VpcMatch != nil && len(m.VpcMatch.MatchID) > 0 {
					return []string{strings.ToLower(m.VpcMatch.MatchID)}, nil
				}
				return nil, nil
			},
		})

	p.accPollers[*namespacedName] = poller
	return poller, false
}

// removeAccountPoller stops account poller thread and removes account poller entry from accPollers map.
func (p *Poller) removeAccountPoller(namespacedName *types.NamespacedName) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if poller, found := p.accPollers[*namespacedName]; found {
		if poller != nil {
			if poller.selector != nil {
				cloudInterface, err := cloudprovider.GetCloudInterface(common.ProviderType(poller.cloudType))
				if err != nil {
					return err
				}
				cloudInterface.RemoveAccountResourcesSelector(namespacedName, poller.selector.Name)
			}

			// Stop go-routine.
			poller.mutex.Lock()
			if poller.ch != nil {
				close(poller.ch)
				poller.ch = nil
			}
			poller.mutex.Unlock()

			// To avoid race condition between CES Delete and CPA Delete wrt account poller workflow
			// remove entry for the account from accPollers map now.
			delete(p.accPollers, *namespacedName)
		}
	}

	return nil
}

// getCloudType fetches cloud provider type from the accountPoller object for a given account.
func (p *Poller) getCloudType(name *types.NamespacedName) (runtimev1alpha1.CloudProvider, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if pollerScope, exists := p.accPollers[*name]; exists {
		return pollerScope.cloudType, nil
	}
	return "", fmt.Errorf("%s %v", errorMsgAccountPollerNotFound, name)
}

// updateAccountPoller updates accountPoller object with CES specific information.
func (p *Poller) updateAccountPoller(name *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	accPoller, found := p.accPollers[*name]
	if !found {
		return fmt.Errorf("%s %v", errorMsgAccountPollerNotFound, name)
	}

	if selector.Spec.VMSelector != nil {
		// Indexer does not work with in-place update. Do delete->add.
		for _, vmSelector := range accPoller.vmSelector.List() {
			if err := accPoller.vmSelector.Delete(vmSelector.(*crdv1alpha1.VirtualMachineSelector)); err != nil {
				p.log.Error(err, "unable to delete selector from indexer",
					"VMSelector", vmSelector.(*crdv1alpha1.VirtualMachineSelector))
			}
		}

		for i := range selector.Spec.VMSelector {
			if err := accPoller.vmSelector.Add(&selector.Spec.VMSelector[i]); err != nil {
				p.log.Error(err, "unable to add selector into indexer",
					"VMSelector", selector.Spec.VMSelector[i])
			}
		}
	} else {
		for _, vmSelector := range accPoller.vmSelector.List() {
			if err := accPoller.vmSelector.Delete(vmSelector.(*crdv1alpha1.VirtualMachineSelector)); err != nil {
				p.log.Error(err, "unable to delete selector from indexer",
					"VMSelector", vmSelector.(*crdv1alpha1.VirtualMachineSelector))
			}
		}
	}

	// Populate selector specific fields in the accPoller created by CPA, needed for setting owner reference in VM CR.
	accPoller.selector = selector.DeepCopy()

	return nil
}

// restartAccountPoller stops and starts a goroutine making sure there is only one account poller goroutine at a time.
func (p *Poller) restartAccountPoller(name *types.NamespacedName) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	accPoller, found := p.accPollers[*name]
	if !found {
		return fmt.Errorf("%s %v", errorMsgAccountPollerNotFound, name)
	}

	// Acquire mutex lock to make sure doAccountPolling is not running while new goroutine is started.
	accPoller.mutex.Lock()
	if accPoller.ch != nil {
		close(accPoller.ch)
		accPoller.ch = nil
	}
	accPoller.mutex.Unlock()

	p.log.Info("Restarting account poller", "account", name)
	accPoller.ch = make(chan struct{})
	go wait.Until(accPoller.doAccountPolling, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)

	return nil
}

// getAccountPoller returns the account poller matching the nameSpacedName
func (p *Poller) getAccountPoller(name *types.NamespacedName) (*accountPoller, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	accPoller, found := p.accPollers[*name]
	if !found {
		return nil, fmt.Errorf("%s %v", errorMsgAccountPollerNotFound, name)
	}
	return accPoller, nil
}

func (p *accountPoller) doAccountPolling() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.pollDone = false
	cloudInterface, e := cloudprovider.GetCloudInterface(common.ProviderType(p.cloudType))
	if e != nil {
		p.log.V(1).Info("Failed to get cloud interface", "account", p.namespacedName, "err", e)
		return
	}

	e = cloudInterface.DoInventoryPoll(p.namespacedName)
	if e != nil {
		p.log.Error(e, "failed to poll cloud inventory", "account", p.namespacedName)
	}

	p.updateAccountStatus(cloudInterface)

	// TODO: Avoid calling plugin to get VPC inventory from snapshot.
	vpcMap, e := cloudInterface.GetVpcInventory(p.namespacedName)
	if e != nil {
		p.log.Error(e, "failed to fetch cloud vpc list from internal snapshot", "account",
			p.namespacedName.String())
		return
	}

	vpcCount := len(vpcMap)
	e = p.inventory.BuildVpcCache(vpcMap, p.namespacedName)
	if e != nil {
		p.log.Error(e, "failed to build vpc cache", "account", p.namespacedName.String())
	}

	// Perform VM Operations only when CES is added.
	vmCount := 0
	if p.selector != nil {
		// TODO: Avoid calling plugin to get VM inventory from snapshot.
		virtualMachines := p.getComputeResources(cloudInterface)
		// TODO: We are walking thru virtual map twice. Once here and second one in BuildVmCAche.
		// May be expose Add, Delete, Update routine in inventory and we do the calculation here.
		p.updateAgentState(virtualMachines)
		p.inventory.BuildVmCache(virtualMachines, p.namespacedName)
		vmCount = len(virtualMachines)
	}
	p.log.Info("Discovered compute resources statistics", "Account", p.namespacedName,
		"Vpcs", vpcCount, "VirtualMachines", vmCount)
	p.pollDone = true
}

// updateAgentState sets the Agented field in a VM object.
func (p *accountPoller) updateAgentState(vms map[string]*runtimev1alpha1.VirtualMachine) {
	for _, vm := range vms {
		vm.Status.Agented = p.isVMAgented(vm)
	}
}

// updateAccountStatus updates status of a CPA object when it's changed.
func (p *accountPoller) updateAccountStatus(cloudInterface common.CloudInterface) {
	account := &crdv1alpha1.CloudProviderAccount{}
	e := p.Get(context.TODO(), *p.namespacedName, account)
	if e != nil {
		p.log.Error(e, "failed to get account", "account", p.namespacedName)
		return
	}

	discoveredStatus := crdv1alpha1.CloudProviderAccountStatus{}
	status, e := cloudInterface.GetAccountStatus(p.namespacedName)
	if e != nil {
		discoveredStatus.Error = fmt.Sprintf("failed to get status, err %v", e)
	} else if status != nil {
		discoveredStatus = *status
	}

	if account.Status != discoveredStatus {
		account.Status.Error = discoveredStatus.Error
		e = p.Client.Status().Update(context.TODO(), account)
		if e != nil {
			p.log.Error(e, "failed to update account status", "account", p.namespacedName)
		}
	}
}

func (p *accountPoller) getComputeResources(cloudInterface common.CloudInterface) map[string]*runtimev1alpha1.VirtualMachine {
	virtualMachines, e := cloudInterface.InstancesGivenProviderAccount(p.namespacedName)
	if e != nil {
		p.log.Error(e, "failed to discover compute resources", "account", p.namespacedName)
		return map[string]*runtimev1alpha1.VirtualMachine{}
	}
	return virtualMachines
}

// getVMSelectorMatch returns a VMSelector for a VirtualMachine only if it is agented.
func (p *accountPoller) getVMSelectorMatch(vm *runtimev1alpha1.VirtualMachine) *crdv1alpha1.VirtualMachineSelector {
	vmSelectors, _ := p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByID, vm.Status.CloudId)
	for _, i := range vmSelectors {
		vmSelector := i.(*crdv1alpha1.VirtualMachineSelector)
		return vmSelector
	}

	// VM Name is not unique, hence iterate over all selectors matching the VM Name to see the best matching selector.
	// VM intended to match a selector with vpcMatch and vmMatch selector, falls under exact Match.
	// VM intended to match a selector with only vmMatch selector, falls under partial match.
	var partialMatchSelector *crdv1alpha1.VirtualMachineSelector = nil
	vmSelectors, _ = p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByName, vm.Status.CloudName)
	for _, i := range vmSelectors {
		vmSelector := i.(*crdv1alpha1.VirtualMachineSelector)
		if vmSelector.VpcMatch != nil {
			if vmSelector.VpcMatch.MatchID == vm.Status.CloudVpcId {
				// Prioritize exact match(along with vpcMatch) over VM name only match.
				return vmSelector
			}
		} else {
			partialMatchSelector = vmSelector
		}
	}
	if partialMatchSelector != nil {
		return partialMatchSelector
	}

	vmSelectors, _ = p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByVPC, vm.Status.CloudVpcId)
	for _, i := range vmSelectors {
		vmSelector := i.(*crdv1alpha1.VirtualMachineSelector)
		return vmSelector
	}
	return nil
}

// isVMAgented returns true if a matching VMSelector is found for a VirtualMachine and
// agented flag is enabled for the selector.
func (p *accountPoller) isVMAgented(vm *runtimev1alpha1.VirtualMachine) bool {
	vmSelectorMatch := p.getVMSelectorMatch(vm)
	if vmSelectorMatch == nil {
		return false
	}
	return vmSelectorMatch.Agented
}
