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
	"reflect"

	"github.com/go-logr/logr"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
)

const (
	accountResourceToCreate = "TO_CREATE"
	accountResourceToDelete = "TO_DELETE"
	accountResourceToUpdate = "TO_UPDATE"
)

type accountPoller struct {
	client.Client
	log    logr.Logger
	scheme *runtime.Scheme

	pollIntvInSeconds uint
	cloudType         cloudv1alpha1.CloudProvider
	namespacedName    *types.NamespacedName
	selector          *cloudv1alpha1.CloudEntitySelector
	vmSelector        cache.Indexer
	ch                chan struct{}
}

func (p *accountPoller) doAccountPoller() {
	cloudInterface, e := cloudprovider.GetCloudInterface(common.ProviderType(p.cloudType))
	if e != nil {
		p.log.Info("failed to get cloud interface", "account", p.namespacedName, "error", e)
		return
	}

	account := &cloudv1alpha1.CloudProviderAccount{}
	e = p.Get(context.TODO(), *p.namespacedName, account)
	if e != nil {
		p.log.Info("failed to get account", "account", p.namespacedName, "account", account, "error", e)
	}

	discoveredStatus, e := cloudInterface.GetAccountStatus(p.namespacedName)
	if e != nil {
		p.log.Info("failed to get account status", "account", p.namespacedName, "error", e)
	} else {
		updateAccountStatus(&account.Status, discoveredStatus)
	}

	e = p.Client.Status().Update(context.TODO(), account)
	if e != nil {
		p.log.Info("failed to update account status", "account", p.namespacedName, "err", e)
	}
	virtualMachines := p.getComputeResources(cloudInterface)

	e = p.doVirtualMachineOperations(virtualMachines)
	if e != nil {
		p.log.Info("failed to perform virtual-machine operations", "account", p.namespacedName, "error", e)
	}
}

func (p *accountPoller) getComputeResources(cloudInterface common.CloudInterface) []*cloudv1alpha1.VirtualMachine {
	var e error

	virtualMachines, e := cloudInterface.InstancesGivenProviderAccount(p.namespacedName)
	if e != nil {
		p.log.Info("failed to discover compute resources", "account", p.namespacedName, "error", e)
		return []*cloudv1alpha1.VirtualMachine{}
	}

	p.log.Info("discovered compute resources statistics", "account", p.namespacedName, "virtual-machines",
		len(virtualMachines))

	return virtualMachines
}

func (p *accountPoller) doVirtualMachineOperations(virtualMachines []*cloudv1alpha1.VirtualMachine) error {
	virtualMachinesBasedOnOperation, err := p.findVirtualMachinesByOperation(virtualMachines)
	if err != nil {
		return err
	}

	virtualMachinesToCreate, found := virtualMachinesBasedOnOperation[accountResourceToCreate]
	if found {
		for _, vm := range virtualMachinesToCreate {
			e := p.createVirtualMachineCR(vm)
			if e != nil {
				err = multierr.Append(err, e)
				continue
			}
			p.log.Info("created", "vm-name", vm.Name)
		}
	}

	virtualMachinesToUpdate, found := virtualMachinesBasedOnOperation[accountResourceToUpdate]
	if found {
		for _, vm := range virtualMachinesToUpdate {
			e := p.updateVirtualMachineCR(vm)
			if e != nil {
				err = multierr.Append(err, e)
				continue
			}
			p.log.Info("updated", "vm-name", vm.Name)
		}
	}

	virtualMachinesToDelete, found := virtualMachinesBasedOnOperation[accountResourceToDelete]
	if found {
		for _, vm := range virtualMachinesToDelete {
			e := p.deleteVirtualMachineCR(vm)
			if e != nil {
				err = multierr.Append(err, e)
				continue
			}
			p.log.Info("deleted", "vm-name", vm.Name)
		}
	}

	if len(virtualMachinesToCreate) != 0 || len(virtualMachinesToDelete) != 0 || len(virtualMachinesToUpdate) != 0 {
		p.log.Info("virtual-machine crd statistics", "account", p.namespacedName,
			"created", len(virtualMachinesToCreate), "deleted", len(virtualMachinesToDelete), "updated", len(virtualMachinesToUpdate))
	}

	return err
}

func (p *accountPoller) findVirtualMachinesByOperation(discoveredVirtualMachines []*cloudv1alpha1.VirtualMachine) (
	map[string][]*cloudv1alpha1.VirtualMachine, error) {
	virtualMachinesByOperation := make(map[string][]*cloudv1alpha1.VirtualMachine)

	currentVirtualMachinesByName, err := p.getCurrentVirtualMachinesByName()
	if err != nil {
		return nil, err
	}

	// if no virtual machines in etcd, all discovered needs to be created.
	if len(currentVirtualMachinesByName) == 0 {
		virtualMachinesByOperation[accountResourceToCreate] = discoveredVirtualMachines
		return virtualMachinesByOperation, nil
	}

	// find virtual machines to be created.
	// And also removed any vm which needs to be created from currentVirtualMachineByName map.
	var virtualMachinesToCreate []*cloudv1alpha1.VirtualMachine
	var virtualMachinesToUpdate []*cloudv1alpha1.VirtualMachine
	for _, discoveredVirtualMachine := range discoveredVirtualMachines {
		currentVirtualMachine, found := currentVirtualMachinesByName[discoveredVirtualMachine.Name]
		if !found {
			virtualMachinesToCreate = append(virtualMachinesToCreate, discoveredVirtualMachine)
		} else {
			delete(currentVirtualMachinesByName, currentVirtualMachine.Name)
			if !areDiscoveredFieldsSameVirtualMachineStatus(currentVirtualMachine.Status, discoveredVirtualMachine.Status) {
				virtualMachinesToUpdate = append(virtualMachinesToUpdate, discoveredVirtualMachine)
			} else if currentVirtualMachine.Status.Agented != p.isVMAgented(&currentVirtualMachine) {
				p.log.Info("vm selector changed, update VM",
					"vm-name", currentVirtualMachine.Name)
				virtualMachinesToUpdate = append(virtualMachinesToUpdate, discoveredVirtualMachine)
			}
		}
	}

	// find virtual machines to be deleted.
	// All entries remaining in currentVirtualMachineByName are to be deleted from etcd
	var virtualMachinesToDelete []*cloudv1alpha1.VirtualMachine
	for _, vmToDelete := range currentVirtualMachinesByName {
		virtualMachinesToDelete = append(virtualMachinesToDelete, vmToDelete.DeepCopy())
	}

	virtualMachinesByOperation[accountResourceToCreate] = virtualMachinesToCreate
	virtualMachinesByOperation[accountResourceToDelete] = virtualMachinesToDelete
	virtualMachinesByOperation[accountResourceToUpdate] = virtualMachinesToUpdate

	return virtualMachinesByOperation, nil
}

func (p *accountPoller) getCurrentVirtualMachinesByName() (map[string]cloudv1alpha1.VirtualMachine, error) {
	currentVirtualMachinesByName := make(map[string]cloudv1alpha1.VirtualMachine)

	currentVirtualMachineList := &cloudv1alpha1.VirtualMachineList{}
	err := p.Client.List(context.TODO(), currentVirtualMachineList, client.InNamespace(p.selector.Namespace))
	if err != nil {
		return nil, err
	}

	ownerSelector := map[string]*cloudv1alpha1.CloudEntitySelector{p.selector.Name: p.selector}
	currentVirtualMachines := currentVirtualMachineList.Items
	for _, currentVirtualMachine := range currentVirtualMachines {
		if !isVirtualMachineOwnedBy(currentVirtualMachine, ownerSelector) {
			continue
		}
		currentVirtualMachinesByName[currentVirtualMachine.Name] = currentVirtualMachine
	}
	return currentVirtualMachinesByName, nil
}

func isVirtualMachineOwnedBy(virtualMachine cloudv1alpha1.VirtualMachine,
	ownerSelector map[string]*cloudv1alpha1.CloudEntitySelector) bool {
	vmOwnerReferences := virtualMachine.OwnerReferences
	for _, vmOwnerReference := range vmOwnerReferences {
		vmOwnerName := vmOwnerReference.Name
		vmOwnerKind := vmOwnerReference.Kind

		if _, found := ownerSelector[vmOwnerName]; found {
			if strings.Compare(vmOwnerKind, reflect.TypeOf(cloudv1alpha1.CloudEntitySelector{}).Name()) == 0 {
				return true
			}
		}
	}
	return false
}

func areDiscoveredFieldsSameVirtualMachineStatus(s1, s2 cloudv1alpha1.VirtualMachineStatus) bool {
	if &s1 == &s2 {
		return true
	}
	if s1.Provider != s2.Provider {
		return false
	}
	if s1.State != s2.State {
		return false
	}
	if s1.VirtualPrivateCloud != s2.VirtualPrivateCloud {
		return false
	}
	if len(s1.Tags) != len(s2.Tags) ||
		len(s1.NetworkInterfaces) != len(s2.NetworkInterfaces) {
		return false
	}
	if !areTagsSame(s1.Tags, s2.Tags) {
		return false
	}
	if !areNetworkInterfacesSame(s1.NetworkInterfaces, s2.NetworkInterfaces) {
		return false
	}
	return true
}

func areTagsSame(s1, s2 map[string]string) bool {
	for key1, value1 := range s1 {
		value2, found := s2[key1]
		if !found {
			return false
		}
		if strings.Compare(strings.ToLower(value1), strings.ToLower(value2)) != 0 {
			return false
		}
	}
	return true
}

func areNetworkInterfacesSame(s1, s2 []cloudv1alpha1.NetworkInterface) bool {
	if &s1 == &s2 {
		return true
	}

	if len(s1) != len(s2) {
		return false
	}
	s1NameMap := convertNetworkInterfacesToMap(s1)
	s2NameMap := convertNetworkInterfacesToMap(s2)
	for key1, value1 := range s1NameMap {
		value2, found := s2NameMap[key1]
		if !found {
			return false
		}
		if strings.Compare(strings.ToLower(value1.Name), strings.ToLower(value2.Name)) != 0 {
			return false
		}
		if strings.Compare(strings.ToLower(value1.MAC), strings.ToLower(value2.MAC)) != 0 {
			return false
		}
		if len(value1.IPs) != len(value2.IPs) {
			return false
		}
		if !areIPAddressesSame(value1.IPs, value2.IPs) {
			return false
		}
	}
	return true
}

func areIPAddressesSame(s1, s2 []cloudv1alpha1.IPAddress) bool {
	s1Map := convertAddressToMap(s1)
	s2Map := convertAddressToMap(s2)
	for key1 := range s1Map {
		_, found := s2Map[key1]
		if !found {
			return false
		}
	}
	return true
}

func convertAddressToMap(addresses []cloudv1alpha1.IPAddress) map[string]struct{} {
	ipAddressMap := make(map[string]struct{})
	for _, address := range addresses {
		key := fmt.Sprintf("%v:%v", address.AddressType, address.Address)
		ipAddressMap[key] = struct{}{}
	}
	return ipAddressMap
}

func convertNetworkInterfacesToMap(nwInterfaces []cloudv1alpha1.NetworkInterface) map[string]cloudv1alpha1.NetworkInterface {
	nwInterfaceMap := make(map[string]cloudv1alpha1.NetworkInterface)

	for _, nwIFace := range nwInterfaces {
		nwInterfaceMap[nwIFace.Name] = nwIFace
	}
	return nwInterfaceMap
}

func updateCloudDiscoveredFieldsOfVirtualMachineStatus(current, discovered *cloudv1alpha1.VirtualMachineStatus) {
	current.Provider = discovered.Provider
	current.State = discovered.State
	current.NetworkInterfaces = discovered.NetworkInterfaces
	current.VirtualPrivateCloud = discovered.VirtualPrivateCloud
	current.Tags = discovered.Tags
}

func updateAccountStatus(current, discovered *cloudv1alpha1.CloudProviderAccountStatus) {
	current.Error = discovered.Error
}

// createVirtualMachineCR creates VirtualMachine CR and updates the status.
func (p *accountPoller) createVirtualMachineCR(vm *cloudv1alpha1.VirtualMachine) (err error) {
	err = controllerutil.SetControllerReference(p.selector, vm, p.scheme)
	if err != nil {
		p.log.Info("error setting controller owner reference", "err", err)
		return err
	}
	// save status since create will update VM object and remove status field from it.
	vmStatus := vm.Status
	err = p.Client.Create(context.TODO(), vm)
	if err != nil {
		p.log.Info("virtual machine create failed", "name", vm.Name, "err", err)
		return err
	}
	vmStatus.Agented = p.isVMAgented(vm)
	vm.Status = vmStatus
	err = p.Client.Status().Update(context.TODO(), vm)
	if err != nil {
		p.log.Info("virtual machine status update failed", "account", p.namespacedName, "name", vm.Name, "err", err)
		return err
	}
	return nil
}

// updateVirtualMachineCR updates VirtualMachine CR. When the Agented status
// field is changed, it delete and re-create the VirtualMachine CR.
func (p *accountPoller) updateVirtualMachineCR(vm *cloudv1alpha1.VirtualMachine) (err error) {
	vmNamespacedName := types.NamespacedName{
		Namespace: vm.Namespace,
		Name:      vm.Name,
	}
	currentVM := &cloudv1alpha1.VirtualMachine{}
	err = p.Get(context.TODO(), vmNamespacedName, currentVM)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			p.log.V(1).Info("unable to find to update", "vm-name", vm.Name, "err", err)
			return err
		}
	}
	// Check if status is changed from Agented to Agentless or vice-versa.
	if currentVM.Status.Agented != p.isVMAgented(currentVM) {
		p.log.Info("vm agent status changed", "vm-name", currentVM.Name)
		// Delete the old VM CR, so that any dependent CR's will be deleted.
		err = p.deleteVirtualMachineCR(currentVM)
		if err != nil {
			return err
		}
		// Create a new VM CR, using pre-created VM object.
		// Set the resource version empty.
		vm.ResourceVersion = ""
		err = p.createVirtualMachineCR(vm)
		if err != nil {
			return err
		}
	} else {
		updateCloudDiscoveredFieldsOfVirtualMachineStatus(&currentVM.Status, &vm.Status)
		err = p.Client.Status().Update(context.TODO(), currentVM)
		if err != nil {
			p.log.Info("virtual machine status update failed", "account", p.namespacedName, "name", vm.Name, "err", err)
			return err
		}
	}
	return nil
}

// deleteVirtualMachineCR deletes VirtualMachine CR.
func (p *accountPoller) deleteVirtualMachineCR(vm *cloudv1alpha1.VirtualMachine) (err error) {
	err = p.Delete(context.TODO(), vm)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			p.log.V(1).Info("unable to delete", "vm-name", vm.Name, "err", err)
			return err
		}
	}
	return nil
}

// getVMSelectorMatch returns a VMSelector for a VirtualMachine only if it is agented.
func (p *accountPoller) getVMSelectorMatch(vm *cloudv1alpha1.VirtualMachine) *cloudv1alpha1.VirtualMachineSelector {
	vmSelectors, _ := p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByID, vm.Annotations[common.AnnotationCloudAssignedIDKey])
	for _, i := range vmSelectors {
		vmSelector := i.(*cloudv1alpha1.VirtualMachineSelector)
		return vmSelector
	}

	// VM Name is not unique, hence iterate over all selectors matching the VM Name to see the best matching selector.
	// VM intended to match a selector with vpcMatch and vmMatch selector, falls under exact Match.
	// VM intended to match a selector with only vmMatch selector, falls under partial match.
	var partialMatchSelector *cloudv1alpha1.VirtualMachineSelector = nil
	vmSelectors, _ = p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByName, vm.Annotations[common.AnnotationCloudAssignedNameKey])
	for _, i := range vmSelectors {
		vmSelector := i.(*cloudv1alpha1.VirtualMachineSelector)
		if vmSelector.VpcMatch != nil {
			if vmSelector.VpcMatch.MatchID == vm.Annotations[common.AnnotationCloudAssignedVPCIDKey] {
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

	vmSelectors, _ = p.vmSelector.ByIndex(virtualMachineSelectorMatchIndexerByVPC, vm.Annotations[common.AnnotationCloudAssignedVPCIDKey])
	for _, i := range vmSelectors {
		vmSelector := i.(*cloudv1alpha1.VirtualMachineSelector)
		return vmSelector
	}
	return nil
}

// isVMAgented returns true if a matching VMSelector is found for a VirtualMachine and
// agented flag is enabled for the selector.
func (p *accountPoller) isVMAgented(vm *cloudv1alpha1.VirtualMachine) bool {
	vmSelectorMatch := p.getVMSelectorMatch(vm)
	if vmSelectorMatch == nil {
		return false
	}
	return vmSelectorMatch.Agented
}
