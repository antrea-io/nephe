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
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/logging"
)

type Inventory struct {
	log      logr.Logger
	vpcCache cache.Indexer
}

const (
	VpcIndexerByAccountNameSpacedName = "namespace.accountname"
	VpcIndexerByVpcNamespacedName     = "namespace.vpcname"
	VpcIndexerByNamespace             = "vpc.namespace"
	VpcIndexerByNamespacedRegion      = "namespace.region"

	VpcLabelAccountName = "account-name"
	VpcLabelRegion      = "region"
)

// InitInventory creates an instance of Inventory struct and initializes inventory with cache indexers.
func InitInventory() *Inventory {
	inventory := &Inventory{
		log: logging.GetLogger("inventory").WithName("Cloud"),
	}
	inventory.vpcCache = cache.NewIndexer(
		func(obj interface{}) (string, error) {
			vpc := obj.(*runtimev1alpha1.Vpc)
			return fmt.Sprintf("%v/%v-%v", vpc.Namespace, vpc.Labels[VpcLabelAccountName], vpc.Info.Id), nil
		},
		cache.Indexers{
			VpcIndexerByAccountNameSpacedName: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Namespace + "/" + vpc.Labels[VpcLabelAccountName]}, nil
			},
			VpcIndexerByVpcNamespacedName: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Namespace + "/" + vpc.Name}, nil
			},
			VpcIndexerByNamespacedRegion: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Namespace + "/" + vpc.Info.Region}, nil
			},
			VpcIndexerByNamespace: func(obj interface{}) ([]string, error) {
				vpc := obj.(*runtimev1alpha1.Vpc)
				return []string{vpc.Namespace}, nil
			},
		})
	return inventory
}

// BuildVpcCache using vpc list from cloud, update vpc cache(with vpcs applicable for the current cloud account).
func (inventory *Inventory) BuildVpcCache(vpcMap map[string]*runtimev1alpha1.Vpc, namespacedName *types.NamespacedName) error {
	vpcsInCache, _ := inventory.vpcCache.ByIndex(VpcIndexerByAccountNameSpacedName, namespacedName.String())

	// Remove vpcs in vpc cache which are not found in vpc list fetched from cloud.
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		if _, found := vpcMap[vpc.Info.Id]; !found {
			inventory.log.V(1).Info("Deleting a vpc from vpc cache", "vpc id", vpc.Info.Id, "account",
				namespacedName.String())
			if err := inventory.vpcCache.Delete(vpc); err != nil {
				inventory.log.Error(err, "failed to delete entry from VpcIndexer", "vpc id", vpc.Info.Id, "account",
					namespacedName.String())
			}
		}
	}

	for _, v := range vpcMap {
		err := inventory.vpcCache.Add(v)
		if err != nil {
			return fmt.Errorf("failed to add entry into VpcIndexer, vpc id %s, account %v, error %v",
				v.Info.Id, *namespacedName, err)
		}
	}

	return nil
}

// DeleteVpcCache deletes all entries from vpc cache.
func (inventory *Inventory) DeleteVpcCache(namespacedName *types.NamespacedName) error {
	vpcsInCache, err := inventory.vpcCache.ByIndex(VpcIndexerByAccountNameSpacedName, namespacedName.String())
	if err != nil {
		return err
	}
	for _, i := range vpcsInCache {
		vpc := i.(*runtimev1alpha1.Vpc)
		if err := inventory.vpcCache.Delete(vpc); err != nil {
			return fmt.Errorf("failed to delete entry from VpcIndexer, indexer %v, error %v", *namespacedName, err)
		}
	}
	return nil
}

// GetVpcsFromIndexer returns vpcs matching the indexedValue for the requested index.
func (inventory *Inventory) GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error) {
	return inventory.vpcCache.ByIndex(indexName, indexedValue)
}

// GetAllVpcs returns all vpcs from the indexer.
func (inventory *Inventory) GetAllVpcs() []interface{} {
	return inventory.vpcCache.List()
}
