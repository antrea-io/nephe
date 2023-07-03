// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inventory

import (
	"context"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
)

type Interface interface {
	VPCStore
	VMStore
}

type VPCStore interface {
	// BuildVpcCache builds the vpc cache using discoveredVpcMap.
	BuildVpcCache(discoveredVpcMap map[string]*runtimev1alpha1.Vpc, namespacedName *types.NamespacedName) error

	// DeleteVpcsFromCache deletes all vpcs from the cache.
	DeleteVpcsFromCache(namespacedName *types.NamespacedName) error

	// GetVpcsFromIndexer gets all vpcs from the cache that have a matching index value.
	GetVpcsFromIndexer(indexName string, indexedValue string) ([]interface{}, error)

	// GetAllVpcs gets all vpcs from the cache.
	GetAllVpcs() []interface{}

	// WatchVpcs returns a watch interface on the vpc cache for the given selectors.
	WatchVpcs(ctx context.Context, key string, labelSelector labels.Selector, fieldSelector fields.Selector) (watch.Interface, error)
}

type VMStore interface {
	// BuildVmCache builds the vm cache using discoveredVmMap.
	BuildVmCache(discoveredVmMap map[string]*runtimev1alpha1.VirtualMachine, accountNamespacedName *types.NamespacedName,
		selectorNamespacedName *types.NamespacedName)

	// DeleteAllVmsFromCache deletes all vms from the cache for a given account.
	DeleteAllVmsFromCache(accountNamespacedName *types.NamespacedName) error

	// DeleteVmsFromCache deletes all vms from the cache for a given selector.
	DeleteVmsFromCache(accountNamespacedName *types.NamespacedName, selectorNamespacedName *types.NamespacedName) error

	// GetVmFromIndexer gets all vms from the cache that have a matching index value.
	GetVmFromIndexer(indexName string, indexedValue string) ([]interface{}, error)

	// GetAllVms gets all vms from the cache.
	GetAllVms() []interface{}

	// GetVmByKey gets the vm that matches the given key.
	GetVmByKey(key string) (*runtimev1alpha1.VirtualMachine, bool)

	// WatchVms returns a watch interface on the vm cache for the given selectors.
	WatchVms(ctx context.Context, key string, labelSelector labels.Selector, fieldSelector fields.Selector) (watch.Interface, error)
}
