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

package accountmanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloud"
	"antrea.io/nephe/pkg/inventory"
)

const (
	defaultPollTimeout = 60 * time.Second
)

type accountPoller struct {
	client.Client
	log logr.Logger

	PollIntvInSeconds uint
	PollDone          bool
	cloudInterface    cloud.CloudInterface
	namespacedName    *types.NamespacedName
	selector          *crdv1alpha1.CloudEntitySelector
	vmSelector        cache.Indexer
	ch                chan struct{}
	mutex             sync.RWMutex
	inventory         inventory.Interface
}

// initVmSelectorCache inits account poller selector cache and its indexers.
func (p *accountPoller) initVmSelectorCache() {
	p.vmSelector = cache.NewIndexer(
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
}

// addOrUpdateSelector updates account poller with new selectors.
func (p *accountPoller) addOrUpdateSelector(selector *crdv1alpha1.CloudEntitySelector) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if selector.Spec.VMSelector != nil {
		// Indexer does not work with in-place update. Do delete->add.
		for _, vmSelector := range p.vmSelector.List() {
			if err := p.vmSelector.Delete(vmSelector.(*crdv1alpha1.VirtualMachineSelector)); err != nil {
				p.log.Error(err, "unable to delete selector from indexer",
					"VMSelector", vmSelector.(*crdv1alpha1.VirtualMachineSelector))
			}
		}

		for i := range selector.Spec.VMSelector {
			if err := p.vmSelector.Add(&selector.Spec.VMSelector[i]); err != nil {
				p.log.Error(err, "unable to add selector into indexer",
					"VMSelector", selector.Spec.VMSelector[i])
			}
		}
	} else {
		for _, vmSelector := range p.vmSelector.List() {
			if err := p.vmSelector.Delete(vmSelector.(*crdv1alpha1.VirtualMachineSelector)); err != nil {
				p.log.Error(err, "unable to delete selector from indexer",
					"VMSelector", vmSelector.(*crdv1alpha1.VirtualMachineSelector))
			}
		}
	}

	// Store selector to filter cloud resources based on selector.
	p.selector = selector
}

// removeSelector reset selector in account poller.
func (p *accountPoller) removeSelector(accountNamespacedName *types.NamespacedName) {
	p.selector = nil
	// Remove VMs from the cache when selectors are removed.
	_ = p.inventory.DeleteVmsFromCache(accountNamespacedName)
}

// doAccountPolling calls the cloud plugin and fetches the cloud inventory. Once successful poll, updates the cloud
// inventory in the internal cache. It also updates CloudProviderAccount CR, if there are any errors while fetching
// the inventory from cloud.
func (p *accountPoller) doAccountPolling() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.PollDone = false
	// Ignoring error since it is captured in the CloudProviderAccount CR's status field.
	_ = p.cloudInterface.DoInventoryPoll(p.namespacedName)

	defer func() {
		p.PollDone = true
		// Update status on CPA CR after polling.
		p.updateAccountStatus(p.cloudInterface)
	}()

	// TODO: Avoid calling plugin to get VPC inventory from snapshot.
	vpcMap, err := p.cloudInterface.GetVpcInventory(p.namespacedName)
	if err != nil {
		p.log.Error(err, "failed to fetch cloud vpc list from internal snapshot", "account",
			p.namespacedName.String())
		return
	}
	_ = p.inventory.BuildVpcCache(vpcMap, p.namespacedName)

	// Perform VM Operations only when CES is added.
	vmCount := 0
	if p.selector != nil {
		// TODO: Avoid calling plugin to get VM inventory from snapshot.
		virtualMachines := p.getComputeResources(p.cloudInterface)
		// TODO: We are walking thru virtual map twice. Once here and second one in BuildVmCAche.
		// May be expose Add, Delete, Update routine in inventory and we do the calculation here.
		p.updateAgentState(virtualMachines)
		p.inventory.BuildVmCache(virtualMachines, p.namespacedName)
		vmCount = len(virtualMachines)
	}
	p.log.Info("Discovered compute resources statistics", "Account", p.namespacedName,
		"Vpcs", len(vpcMap), "VirtualMachines", vmCount)
}

// updateAccountStatus updates status of a CPA object when it's changed.
func (p *accountPoller) updateAccountStatus(cloudInterface cloud.CloudInterface) {
	discoveredStatus := crdv1alpha1.CloudProviderAccountStatus{}
	status, err := cloudInterface.GetAccountStatus(p.namespacedName)
	if err != nil {
		discoveredStatus.Error = fmt.Sprintf("failed to get account status, err %v", err)
	} else if status != nil {
		discoveredStatus = *status
	}

	updateStatusFunc := func() error {
		account := &crdv1alpha1.CloudProviderAccount{}
		if err := p.Get(context.TODO(), *p.namespacedName, account); err != nil {
			return nil
		}
		if account.Status != discoveredStatus {
			account.Status.Error = discoveredStatus.Error
			p.log.Info("Setting CPA status", "account", p.namespacedName, "message", discoveredStatus.Error)
			if err = p.Client.Status().Update(context.TODO(), account); err != nil {
				p.log.Error(err, "failed to update CPA status, retrying", "account", p.namespacedName)
				return err
			}
		}
		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, updateStatusFunc); err != nil {
		p.log.Error(err, "failed to update CPA status", "account", p.namespacedName)
	}
}

// updateAgentState sets the Agented field in a VM object.
func (p *accountPoller) updateAgentState(vms map[string]*runtimev1alpha1.VirtualMachine) {
	for _, vm := range vms {
		vm.Status.Agented = p.isVmAgented(vm)
	}
}

func (p *accountPoller) getComputeResources(cloudInterface cloud.CloudInterface) map[string]*runtimev1alpha1.VirtualMachine {
	virtualMachines, e := cloudInterface.InstancesGivenProviderAccount(p.namespacedName)
	if e != nil {
		p.log.Error(e, "failed to discover compute resources", "account", p.namespacedName)
		return map[string]*runtimev1alpha1.VirtualMachine{}
	}
	return virtualMachines
}

// getVmSelectorMatch returns a VMSelector for a VirtualMachine only if it is agented.
func (p *accountPoller) getVmSelectorMatch(vm *runtimev1alpha1.VirtualMachine) *crdv1alpha1.VirtualMachineSelector {
	vmSelectors, _ := p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByID, vm.Status.CloudId)
	for _, obj := range vmSelectors {
		vmSelector, ok := obj.(*crdv1alpha1.VirtualMachineSelector)
		if !ok {
			continue
		}
		return vmSelector
	}

	// VM Name is not unique, hence iterate over all selectors matching the VM Name to see the best matching selector.
	// VM intended to match a selector with vpcMatch and vmMatch selector, falls under exact Match.
	// VM intended to match a selector with only vmMatch selector, falls under partial match.
	var partialMatchSelector *crdv1alpha1.VirtualMachineSelector = nil
	vmSelectors, _ = p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByName, vm.Status.CloudName)
	for _, obj := range vmSelectors {
		vmSelector, ok := obj.(*crdv1alpha1.VirtualMachineSelector)
		if !ok {
			continue
		}
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
	for _, obj := range vmSelectors {
		vmSelector, ok := obj.(*crdv1alpha1.VirtualMachineSelector)
		if !ok {
			continue
		}
		return vmSelector
	}
	return nil
}

// isVmAgented returns true if a matching VMSelector is found for a VirtualMachine and
// agented flag is enabled for the selector.
func (p *accountPoller) isVmAgented(vm *runtimev1alpha1.VirtualMachine) bool {
	vmSelectorMatch := p.getVmSelectorMatch(vm)
	if vmSelectorMatch == nil {
		return false
	}
	return vmSelectorMatch.Agented
}

// waitForPollDone waits until account poller has completed polling cloud inventory.
func (p *accountPoller) waitForPollDone(accountNamespacedName *types.NamespacedName) error {
	p.log.Info("Waiting for inventory poll to complete", "account", *accountNamespacedName)
	if err := wait.PollImmediate(100*time.Millisecond, defaultPollTimeout, func() (done bool, err error) {
		p.mutex.RLock()
		defer p.mutex.RUnlock()
		if p.PollDone {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed to poll cloud inventory, err %v", err)
	}
	return nil
}

// restartPoller restarts account poller thread.
func (p *accountPoller) restartPoller(name *types.NamespacedName) {
	// Wait for existing thread to complete its execution.
	p.mutex.Lock()
	if p.ch != nil {
		close(p.ch)
		p.ch = nil
	}
	p.mutex.Unlock()

	p.log.Info("Restarting account poller", "account", name)
	p.ch = make(chan struct{})
	go wait.Until(p.doAccountPolling, time.Duration(p.PollIntvInSeconds)*time.Second, p.ch)
}

// stopPoller stops account poller thread if it's running.
func (p *accountPoller) stopPoller() {
	// Wait for existing thread to complete its execution.
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.ch != nil {
		close(p.ch)
		p.ch = nil
	}
}
