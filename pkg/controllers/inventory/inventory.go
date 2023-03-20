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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	antreastorage "antrea.io/antrea/pkg/apiserver/storage"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/inventory/common"
	"antrea.io/nephe/pkg/controllers/inventory/store"
	"antrea.io/nephe/pkg/controllers/utils"
	"antrea.io/nephe/pkg/logging"
)

type Inventory interface {
	VPCStore
	VMStore
}

type VMStore interface {
	BuildVmCache(discoveredVmMap map[string]*runtimev1alpha1.VirtualMachine, namespacedName *types.NamespacedName)
	DeleteVmsFromCache(namespacedName *types.NamespacedName) error
	GetAllVms() []interface{}
	GetVmFromIndexer(indexName string, indexedValue string) ([]interface{}, error)
	GetVmBykey(key string) (*runtimev1alpha1.VirtualMachine, bool)
	WatchVms(ctx context.Context, key string, labelSelector labels.Selector, fieldSelector fields.Selector) (watch.Interface, error)
}

type VPCStore interface {
	BuildVpcCache(discoveredVpcMap map[string]*runtimev1alpha1.Vpc, namespacedName *types.NamespacedName) error
	DeleteVpcsFromCache(namespacedName *types.NamespacedName) error
	GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error)
	GetAllVpcs() []interface{}
	WatchVpcs(ctx context.Context, key string, labelSelector labels.Selector, fieldSelector fields.Selector) (watch.Interface, error)
}

type InventoryImpl struct {
	log      logr.Logger
	vpcStore antreastorage.Interface
	vmStore  antreastorage.Interface
}

// InitInventory creates an instance of InventoryImpl struct and initializes inventory with cache indexers.
func InitInventory() *InventoryImpl {
	inventory := &InventoryImpl{
		log: logging.GetLogger("inventory").WithName("Cloud"),
	}
	inventory.vpcStore = store.NewVPCInventoryStore()
	inventory.vmStore = store.NewVmInventoryStore()
	return inventory
}

// BuildVpcCache builds vpc cache for given account using vpc list fetched from cloud.
func (inventory *InventoryImpl) BuildVpcCache(discoveredVpcMap map[string]*runtimev1alpha1.Vpc,
	namespacedName *types.NamespacedName) error {
	var numVpcsToAdd, numVpcsToUpdate, numVpcsToDelete int
	// Fetch all vpcs for a given account from the cache and check if it exists in the discovered vpc list.
	vpcsInCache, _ := inventory.vpcStore.GetByIndex(common.VpcIndexerByNameSpacedAccountName, namespacedName.String())

	// Remove vpcs in vpc cache which are not found in vpc list fetched from cloud.
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		if _, found := discoveredVpcMap[vpc.Status.Id]; !found {
			if err := inventory.vpcStore.Delete(fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[common.VpcLabelAccountName],
				vpc.Status.Id)); err != nil {
				inventory.log.Error(err, "failed to delete vpc from vpc cache", "vpc id", vpc.Status.Id, "account",
					namespacedName.String())
			} else {
				numVpcsToDelete++
			}
		}
	}

	for _, discoveredVpc := range discoveredVpcMap {
		var err error
		key := fmt.Sprintf("%v/%v-%v", discoveredVpc.Namespace, discoveredVpc.Labels[common.VpcLabelAccountName], discoveredVpc.Status.Id)
		if cachedObj, found, _ := inventory.vpcStore.Get(key); !found {
			err = inventory.vpcStore.Create(discoveredVpc)
			if err == nil {
				numVpcsToAdd++
			}
		} else {
			cachedVpc := cachedObj.(*runtimev1alpha1.Vpc)
			if !reflect.DeepEqual(cachedVpc.Status, discoveredVpc.Status) {
				err = inventory.vpcStore.Update(discoveredVpc)
				if err == nil {
					numVpcsToUpdate++
				}
			}
		}
		if err != nil {
			return fmt.Errorf("failed to add vpc into vpc cache, vpc id: %s, error: %v",
				discoveredVpc.Status.Id, err)
		}
	}

	if numVpcsToAdd != 0 || numVpcsToUpdate != 0 || numVpcsToDelete != 0 {
		inventory.log.Info("Vpc poll statistics", "account", namespacedName, "added", numVpcsToAdd,
			"update", numVpcsToUpdate, "delete", numVpcsToDelete)
	}
	return nil
}

// DeleteVpcsFromCache deletes all entries from vpc cache for a given account.
func (inventory *InventoryImpl) DeleteVpcsFromCache(namespacedName *types.NamespacedName) error {
	vpcsInCache, err := inventory.vpcStore.GetByIndex(common.VpcIndexerByNameSpacedAccountName, namespacedName.String())
	if err != nil {
		return err
	}
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		key := fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[common.VpcLabelAccountName], vpc.Status.Id)
		if err := inventory.vpcStore.Delete(key); err != nil {
			return fmt.Errorf("failed to delete vpc from vpc cache %s:%s, error %v",
				*namespacedName, vpc.Status.Id, err)
		}
	}
	return nil
}

// GetVpcsFromIndexer returns vpcs matching the indexedValue for the requested indexName.
func (inventory *InventoryImpl) GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return inventory.vpcStore.GetByIndex(indexName, indexedValue)
}

// GetAllVpcs returns all the vpcs from the vpc cache.
func (inventory *InventoryImpl) GetAllVpcs() []interface{} {
	return inventory.vpcStore.List()
}

// WatchVpcs returns a Watch interface of VPC.
func (inventory *InventoryImpl) WatchVpcs(ctx context.Context, key string, labelSelector labels.Selector,
	fieldSelector fields.Selector) (watch.Interface, error) {
	return inventory.vpcStore.Watch(ctx, key, labelSelector, fieldSelector)
}

// BuildVmCache builds vm cache for given account using vm list fetched from cloud.
func (inventory *InventoryImpl) BuildVmCache(discoveredVmMap map[string]*runtimev1alpha1.VirtualMachine,
	namespacedName *types.NamespacedName) {
	// Fetch all vms for a given account from the cache and check if it exists in the discovered vm list.
	vmsInCache, _ := inventory.vmStore.GetByIndex(common.IndexerByNamespace, namespacedName.Namespace)
	// Remove vm from vm cache which are not found in vm map fetched from cloud.
	for _, cachedObject := range vmsInCache {
		cachedVm := cachedObject.(*runtimev1alpha1.VirtualMachine)
		if _, found := discoveredVmMap[cachedVm.Name]; !found {
			inventory.log.V(1).Info("Deleting vm from vm cache", "vm", cachedVm.Name, "account",
				namespacedName.String())
			key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
			if err := inventory.vmStore.Delete(key); err != nil {
				inventory.log.Error(err, "failed to delete vm from vm cache", "vm", cachedVm.Name, "account",
					namespacedName.String())
			}
		}
	}

	// TODO: Create a counter for add, delete and update and log it at the end

	// Add or Update VM
	for _, discoveredVm := range discoveredVmMap {
		var err error
		key := fmt.Sprintf("%v/%v", discoveredVm.Namespace, discoveredVm.Name)
		if cachedObject, found, _ := inventory.vmStore.Get(key); !found {
			err = inventory.vmStore.Create(discoveredVm)
		} else {
			cachedVm := cachedObject.(*runtimev1alpha1.VirtualMachine)
			if !utils.AreDiscoveredFieldsSameVirtualMachineStatus(cachedVm.Status, discoveredVm.Status) {
				if cachedVm.Status.Agented != discoveredVm.Status.Agented {
					key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
					err = inventory.vmStore.Delete(key)
					if err == nil {
						err = inventory.vmStore.Create(discoveredVm)
					}
				} else {
					err = inventory.vmStore.Update(discoveredVm)
				}
			}
		}
		if err != nil {
			inventory.log.Error(err, "failed to update vm in vm cache", "vm", discoveredVm.Name,
				"account", namespacedName.String())
		}
	}
}

// DeleteVmsFromCache deletes all entries from vm cache for a given account.
func (inventory *InventoryImpl) DeleteVmsFromCache(namespacedName *types.NamespacedName) error {
	vmsInCache, err := inventory.vmStore.GetByIndex(common.VirtualMachineIndexerByAccountID, namespacedName.String())
	if err != nil {
		return err
	}
	for _, cachedObject := range vmsInCache {
		cachedVm := cachedObject.(*runtimev1alpha1.VirtualMachine)
		key := fmt.Sprintf("%v/%v", cachedVm.Namespace, cachedVm.Name)
		if err := inventory.vmStore.Delete(key); err != nil {
			return fmt.Errorf("failed to delete vm from vm cache %s:%s, error %v",
				*namespacedName, cachedVm.Name, err)
		}
	}
	return nil
}

// GetAllVms returns all the vms from the vm cache.
func (inventory *InventoryImpl) GetAllVms() []interface{} {
	return inventory.vmStore.List()
}

// GetVmFromIndexer returns vms matching the indexedValue for the requested indexName.
func (inventory *InventoryImpl) GetVmFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return inventory.vmStore.GetByIndex(indexName, indexedValue)
}

// GetVmBykey returns vm from vm cache for a given key (namespace/name).
func (inventory *InventoryImpl) GetVmBykey(key string) (*runtimev1alpha1.VirtualMachine, bool) {
	cachedObject, found, err := inventory.vmStore.Get(key)
	if err != nil {
		// Shouldn't happen. Logging it.
		inventory.log.Error(err, "failed to lookup vm", "vm", key)
		return nil, false
	}
	if !found {
		return nil, false
	}
	return cachedObject.(*runtimev1alpha1.VirtualMachine), true
}

// WatchVms returns a Watch interface of vm cache.
func (inventory *InventoryImpl) WatchVms(ctx context.Context, key string, labelSelector labels.Selector,
	fieldSelector fields.Selector) (watch.Interface, error) {
	return inventory.vmStore.Watch(ctx, key, labelSelector, fieldSelector)
}
