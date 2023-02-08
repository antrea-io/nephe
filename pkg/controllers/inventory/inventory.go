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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	antreastorage "antrea.io/antrea/pkg/apiserver/storage"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/inventory/common"
	"antrea.io/nephe/pkg/controllers/inventory/store"
	"antrea.io/nephe/pkg/logging"
)

type Inventory struct {
	log      logr.Logger
	vpcStore antreastorage.Interface
}

// InitInventory creates an instance of Inventory struct and initializes inventory with cache indexers.
func InitInventory() *Inventory {
	inventory := &Inventory{
		log: logging.GetLogger("inventory").WithName("Cloud"),
	}
	inventory.vpcStore = store.NewVPCInventoryStore()
	return inventory
}

// BuildVpcCache using vpc list from cloud, update vpc cache(with vpcs applicable for the current cloud account).
func (inventory *Inventory) BuildVpcCache(vpcMap map[string]*runtimev1alpha1.Vpc, namespacedName *types.NamespacedName) error {
	vpcsInCache, _ := inventory.vpcStore.GetByIndex(common.VpcIndexerByAccountNameSpacedName, namespacedName.String())

	// Remove vpcs in vpc cache which are not found in vpc list fetched from cloud.
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		if _, found := vpcMap[vpc.Status.Id]; !found {
			inventory.log.V(1).Info("Deleting vpc from vpc cache", "vpc id", vpc.Status.Id, "account",
				namespacedName.String())
			if err := inventory.vpcStore.Delete(fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[common.VpcLabelAccountName],
				vpc.Status.Id)); err != nil {
				inventory.log.Error(err, "failed to delete vpc from vpc cache", "vpc id", vpc.Status.Id, "account",
					namespacedName.String())
			}
		}
	}

	for _, v := range vpcMap {
		var err error
		key := fmt.Sprintf("%v/%v-%v", v.Namespace, v.Labels[common.VpcLabelAccountName], v.Status.Id)
		if _, found, _ := inventory.vpcStore.Get(key); !found {
			err = inventory.vpcStore.Create(v)
		} else {
			err = inventory.vpcStore.Update(v)
		}
		if err != nil {
			return fmt.Errorf("failed to add vpc into vpc cache, vpc id %s, account %v, error %v",
				v.Status.Id, *namespacedName, err)
		}
	}

	return nil
}

// DeleteVpcCache deletes all entries from vpc cache.
func (inventory *Inventory) DeleteVpcCache(namespacedName *types.NamespacedName) error {
	vpcsInCache, err := inventory.vpcStore.GetByIndex(common.VpcIndexerByAccountNameSpacedName, namespacedName.String())
	if err != nil {
		return err
	}
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		if err := inventory.vpcStore.Delete(fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[common.VpcLabelAccountName],
			vpc.Status.Id)); err != nil {
			return fmt.Errorf("failed to delete entry from VpcIndexer, indexer %v, error %v", *namespacedName, err)
		}
	}
	return nil
}

// GetVpcsFromIndexer returns vpcs matching the indexedValue for the requested index.
func (inventory *Inventory) GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return inventory.vpcStore.GetByIndex(indexName, indexedValue)
}

// GetAllVpcs returns all vpcs from the indexer.
func (inventory *Inventory) GetAllVpcs() []interface{} {
	return inventory.vpcStore.List()
}

// Watch returns a Watch interface of VPC.
func (inventory *Inventory) Watch(ctx context.Context, key string, labelSelector labels.Selector,
	fieldSelector fields.Selector) (watch.Interface, error) {
	return inventory.vpcStore.Watch(ctx, key, labelSelector, fieldSelector)
}
