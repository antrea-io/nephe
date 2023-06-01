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

package inventory

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	antreastorage "antrea.io/antrea/pkg/apiserver/storage"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory/indexer"
	"antrea.io/nephe/pkg/inventory/store"
	nephelabels "antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/logging"
)

type Inventory struct {
	log      logr.Logger
	vpcStore antreastorage.Interface
	vmStore  antreastorage.Interface
	sgStore  antreastorage.Interface
}

// InitInventory creates an instance of Inventory struct and initializes inventory with cache indexers.
func InitInventory() *Inventory {
	inventory := &Inventory{
		log: logging.GetLogger("inventory").WithName("Cloud"),
	}
	inventory.vpcStore = store.NewVPCInventoryStore()
	inventory.vmStore = store.NewVmInventoryStore()
	inventory.sgStore = store.NewSgInventoryStore()
	return inventory
}

// BuildVpcCache builds vpc cache for given account using vpc list fetched from cloud.
func (i *Inventory) BuildVpcCache(discoveredVpcMap map[string]*runtimev1alpha1.Vpc,
	namespacedName *types.NamespacedName) error {
	var numVpcsToAdd, numVpcsToUpdate, numVpcsToDelete int
	// Fetch all vpcs for a given account from the cache and check if it exists in the discovered vpc list.
	vpcsInCache, _ := i.vpcStore.GetByIndex(indexer.VpcByAccountNamespacedName, namespacedName.String())

	// Remove vpcs in vpc cache which are not found in vpc list fetched from cloud.
	for _, object := range vpcsInCache {
		vpc, ok := object.(*runtimev1alpha1.Vpc)
		if !ok {
			continue
		}
		if _, found := discoveredVpcMap[vpc.Status.CloudId]; !found {
			if err := i.vpcStore.Delete(fmt.Sprintf("%v/%v-%v", vpc.Namespace,
				vpc.Labels[nephelabels.CloudAccountName], vpc.Status.CloudId)); err != nil {
				i.log.Error(err, "failed to delete vpc from vpc cache",
					"vpc id", vpc.Status.CloudId, "account", namespacedName.String())
			} else {
				numVpcsToDelete++
			}
		}
	}

	for _, discoveredVpc := range discoveredVpcMap {
		var err error
		key := fmt.Sprintf("%v/%v-%v", discoveredVpc.Namespace,
			discoveredVpc.Labels[nephelabels.CloudAccountName],
			discoveredVpc.Status.CloudId)
		if cachedObj, found, _ := i.vpcStore.Get(key); !found {
			err = i.vpcStore.Create(discoveredVpc)
			if err == nil {
				numVpcsToAdd++
			}
		} else {
			cachedVpc := cachedObj.(*runtimev1alpha1.Vpc)
			if !reflect.DeepEqual(cachedVpc.Status, discoveredVpc.Status) {
				err = i.vpcStore.Update(discoveredVpc)
				if err == nil {
					numVpcsToUpdate++
				}
			}
		}
		if err != nil {
			i.log.Error(err, "failed to update vpc in vpc cache", "vpc id", discoveredVpc.Status.CloudId,
				"account", namespacedName.String())
		}
	}

	if numVpcsToAdd != 0 || numVpcsToUpdate != 0 || numVpcsToDelete != 0 {
		i.log.Info("Vpc poll statistics", "account", namespacedName, "added", numVpcsToAdd,
			"update", numVpcsToUpdate, "delete", numVpcsToDelete)
	}
	return nil
}

// DeleteVpcsFromCache deletes all entries from vpc cache for a given account.
func (i *Inventory) DeleteVpcsFromCache(namespacedName *types.NamespacedName) error {
	vpcsInCache, err := i.vpcStore.GetByIndex(indexer.VpcByAccountNamespacedName, namespacedName.String())
	if err != nil {
		return err
	}
	var numVpcsToDelete int
	for _, object := range vpcsInCache {
		vpc, ok := object.(*runtimev1alpha1.Vpc)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[nephelabels.CloudAccountName], vpc.Status.CloudId)
		if err := i.vpcStore.Delete(key); err != nil {
			i.log.Error(err, "failed to delete vpc from vpc cache", "vpc id", vpc.Status.CloudId, "account", *namespacedName)
		} else {
			numVpcsToDelete++
		}
	}

	if numVpcsToDelete != 0 {
		i.log.Info("Vpc poll statistics", "account", namespacedName, "deleted", numVpcsToDelete)
	}
	return nil
}

// GetVpcsFromIndexer returns vpcs matching the indexedValue for the requested indexName.
func (i *Inventory) GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return i.vpcStore.GetByIndex(indexName, indexedValue)
}

// GetAllVpcs returns all the vpcs from the vpc cache.
func (i *Inventory) GetAllVpcs() []interface{} {
	return i.vpcStore.List()
}

// WatchVpcs returns a Watch interface of vpc.
func (i *Inventory) WatchVpcs(ctx context.Context, key string, labelSelector labels.Selector,
	fieldSelector fields.Selector) (watch.Interface, error) {
	return i.vpcStore.Watch(ctx, key, labelSelector, fieldSelector)
}

// BuildVmCache builds vm cache for given account using vm list fetched from cloud.
func (i *Inventory) BuildVmCache(discoveredVmMap map[string]*runtimev1alpha1.VirtualMachine, accountNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) {
	var numVmsToAdd, numVmsToUpdate, numVmsToDelete int

	// Fetch all vms specific to a selector from the inventory cache and check if it exists in the discovered vm list.
	vmsInCache, _ := i.vmStore.GetByIndex(indexer.VirtualMachineBySelectorNamespacedName, selectorNamespacedName.String())
	// Remove vm from vm cache which are not found in vm map fetched from cloud.
	for _, cachedObject := range vmsInCache {
		cachedVm, ok := cachedObject.(*runtimev1alpha1.VirtualMachine)
		if !ok {
			continue
		}
		if _, found := discoveredVmMap[cachedVm.Name]; !found {
			key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
			if err := i.vmStore.Delete(key); err != nil {
				i.log.Error(err, "failed to delete vm from vm cache", "vm", cachedVm.Name, "account",
					*accountNamespacedName, "selector", *selectorNamespacedName)
			} else {
				numVmsToDelete++
			}
		}
	}

	// Add or Update VM
	for _, discoveredVm := range discoveredVmMap {
		var err error
		key := fmt.Sprintf("%v/%v", discoveredVm.Namespace, discoveredVm.Name)
		if cachedObject, found, _ := i.vmStore.Get(key); !found {
			err = i.vmStore.Create(discoveredVm)
			if err == nil {
				numVmsToAdd++
			}
		} else {
			cachedVm := cachedObject.(*runtimev1alpha1.VirtualMachine)
			if !i.compareVirtualMachineObjects(cachedVm.Status, discoveredVm.Status) {
				if cachedVm.Status.Agented != discoveredVm.Status.Agented {
					key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
					err = i.vmStore.Delete(key)
					if err == nil {
						err = i.vmStore.Create(discoveredVm)
					}
				} else {
					err = i.vmStore.Update(discoveredVm)
				}
				if err == nil {
					numVmsToUpdate++
				}
			}
		}
		if err != nil {
			i.log.Error(err, "failed to update vm in vm cache", "vm", discoveredVm.Name,
				"account", accountNamespacedName, "selector", selectorNamespacedName)
		}
	}

	if numVmsToAdd != 0 || numVmsToUpdate != 0 || numVmsToDelete != 0 {
		i.log.Info("Vm poll statistics", "account", accountNamespacedName, "selector", selectorNamespacedName,
			"added", numVmsToAdd, "update", numVmsToUpdate, "delete", numVmsToDelete)
	}
}

// DeleteAllVmsFromCache deletes all entries from vm cache for a given account.
func (i *Inventory) DeleteAllVmsFromCache(accountNamespacedName *types.NamespacedName) error {
	vmsInCache, err := i.vmStore.GetByIndex(indexer.VirtualMachineByAccountNamespacedName, accountNamespacedName.String())
	if err != nil {
		return err
	}
	var numVmsToDelete int
	for _, cachedObject := range vmsInCache {
		cachedVm, ok := cachedObject.(*runtimev1alpha1.VirtualMachine)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
		if err := i.vmStore.Delete(key); err != nil {
			i.log.Error(err, "failed to delete vm from vm cache", "vm", cachedVm.Name, "account", *accountNamespacedName)
		} else {
			numVmsToDelete++
		}
	}

	if numVmsToDelete != 0 {
		i.log.Info("Vm poll statistics", "account", accountNamespacedName, "deleted", numVmsToDelete)
	}
	return nil
}

// DeleteVmsFromCache deletes all entries from vm cache for a given selector.
func (i *Inventory) DeleteVmsFromCache(accountNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) error {
	vmsInCache, err := i.vmStore.GetByIndex(indexer.VirtualMachineBySelectorNamespacedName, selectorNamespacedName.String())
	if err != nil {
		return err
	}
	var numVmsToDelete int
	for _, cachedObject := range vmsInCache {
		cachedVm, ok := cachedObject.(*runtimev1alpha1.VirtualMachine)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
		if err := i.vmStore.Delete(key); err != nil {
			i.log.Error(err, "failed to delete vm from vm cache", "vm", cachedVm.Name, "account",
				*accountNamespacedName, "selector", *selectorNamespacedName)
		} else {
			numVmsToDelete++
		}
	}

	if numVmsToDelete != 0 {
		i.log.Info("Vm poll statistics", "account", accountNamespacedName,
			"selector", selectorNamespacedName, "deleted", numVmsToDelete)
	}
	return nil
}

// GetAllVms returns all the vms from the vm cache.
func (i *Inventory) GetAllVms() []interface{} {
	return i.vmStore.List()
}

// GetVmFromIndexer returns vms matching the indexedValue for the requested indexName.
func (i *Inventory) GetVmFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return i.vmStore.GetByIndex(indexName, indexedValue)
}

// GetVmByKey returns vm from vm cache for a given key (namespace/name).
func (i *Inventory) GetVmByKey(key string) (*runtimev1alpha1.VirtualMachine, bool) {
	cachedObject, found, err := i.vmStore.Get(key)
	if err != nil {
		// Shouldn't happen. Logging it.
		i.log.Error(err, "failed to lookup vm", "vm", key)
		return nil, false
	}
	if !found {
		return nil, false
	}
	return cachedObject.(*runtimev1alpha1.VirtualMachine), true
}

// WatchVms returns a Watch interface of vm cache.
func (i *Inventory) WatchVms(ctx context.Context, key string, labelSelector labels.Selector,
	fieldSelector fields.Selector) (watch.Interface, error) {
	return i.vmStore.Watch(ctx, key, labelSelector, fieldSelector)
}

// compareVirtualMachineObjects compare if two virtual machine objects are the same. Return true if same.
func (i *Inventory) compareVirtualMachineObjects(cached, discovered runtimev1alpha1.VirtualMachineStatus) bool {
	// 1. Check if objects are same.
	if reflect.DeepEqual(cached, discovered) {
		return true
	}
	// 2. Check if NetworkInterface field differ.
	if reflect.DeepEqual(cached.NetworkInterfaces, discovered.NetworkInterfaces) {
		return false
	}

	// 3. Sort NetworkInterface field and re-compare.
	sortInterfacesFunc := func(intfs []runtimev1alpha1.NetworkInterface) {
		sort.Slice(intfs, func(i, j int) bool {
			return strings.Compare(intfs[i].Name, intfs[j].Name) < 0
		})
		for _, intf := range intfs {
			sort.Slice(intf.IPs, func(i, j int) bool {
				return strings.Compare(intf.IPs[i].Address, intf.IPs[j].Address) < 0
			})
		}
	}
	sortInterfacesFunc(cached.NetworkInterfaces)
	dis := discovered.DeepCopy()
	sortInterfacesFunc(dis.NetworkInterfaces)
	return reflect.DeepEqual(cached.NetworkInterfaces, dis.NetworkInterfaces)
}

// UpdateVm updates virtual machine object in vm cache.
func (i *Inventory) UpdateVm(vm *runtimev1alpha1.VirtualMachine) error {
	i.log.Info("Updating virtual machine", "namespace", vm.Namespace, "name", vm.Name)
	return i.vmStore.Update(vm)
}

// BuildSgCache builds SG cache for given account using security group list fetched from cloud.
func (i *Inventory) BuildSgCache(discoveredSgMap map[string]*runtimev1alpha1.SecurityGroup,
	namespacedName *types.NamespacedName) error {
	var numSgsAdded, numSgsUpdated, numSgsDeleted int
	// Fetch all security groups for a given account from the cache and check if it exists in the discovered vpc list.
	sgsInCache, _ := i.sgStore.GetByIndex(indexer.SecurityGroupByAccountNamespacedName, namespacedName.String())

	// Remove security groups in SG cache which are not found in security group list fetched from cloud.
	for _, object := range sgsInCache {
		sg := object.(*runtimev1alpha1.SecurityGroup)
		if _, found := discoveredSgMap[sg.Name]; !found {
			if err := i.vpcStore.Delete(fmt.Sprintf("%v/%v", sg.Namespace, sg.Name)); err != nil {
				i.log.Error(err, "failed to delete sg from sg cache",
					"sg", sg.Name, "account", namespacedName.String())
			} else {
				numSgsDeleted++
			}
		}
	}

	// Add or Update Security Group
	for _, discoveredSg := range discoveredSgMap {
		var err error
		key := fmt.Sprintf("%v/%v", discoveredSg.Namespace, discoveredSg.Name)
		if cachedObject, found, _ := i.sgStore.Get(key); !found {
			err = i.sgStore.Create(discoveredSg)
			if err == nil {
				numSgsAdded++
			}
		} else {
			cachedSg := cachedObject.(*runtimev1alpha1.SecurityGroup)
			if !reflect.DeepEqual(cachedSg.Status, discoveredSg.Status) {
				err = i.sgStore.Update(discoveredSg)
				if err == nil {
					numSgsUpdated++
				}
			}
		}
		if err != nil {
			i.log.Error(err, "failed to update sg in sg cache", "vm", discoveredSg.Name,
				"account", namespacedName.String())
		}
	}

	if numSgsAdded != 0 || numSgsUpdated != 0 || numSgsDeleted != 0 {
		i.log.Info("Security group poll statistics", "account", namespacedName, "added", numSgsAdded,
			"update", numSgsUpdated, "delete", numSgsDeleted)
	}
	return nil
}

// GetAllSgs returns all the security groups from the SG cache.
func (i *Inventory) GetAllSgs() []interface{} {
	return i.sgStore.List()
}

// GetSgsFromIndexer returns security group matching the indexedValue for the requested indexName.
func (i *Inventory) GetSgsFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return i.sgStore.GetByIndex(indexName, indexedValue)
}

// GetSgByKey returns security group from sg cache for a given key (namespace/name).
func (i *Inventory) GetSgByKey(key string) (*runtimev1alpha1.SecurityGroup, bool) {
	cachedObject, found, err := i.sgStore.Get(key)
	if err != nil {
		// Shouldn't happen. Logging it.
		i.log.Error(err, "failed to lookup sg", "vm", key)
		return nil, false
	}
	if !found {
		return nil, false
	}
	return cachedObject.(*runtimev1alpha1.SecurityGroup), true
}
