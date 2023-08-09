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

package internal

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	nephetypes "antrea.io/nephe/pkg/types"
)

// CloudServiceInterface needs to be implemented by every cloud-service to be added for a cloud-plugin.
// Once implemented, cloud-service implementation of CloudServiceInterface will get injected into
// plugin-cloud-framework using CloudCommonHelperInterface.
type CloudServiceInterface interface {
	// UpdateServiceConfig updates existing service config with new values. Each service can decide to update one or
	// more fields of the service.
	UpdateServiceConfig(newServiceConfig CloudServiceInterface) error
	// AddResourceFilters will be used by service to get resources from cloud for the service. Each will convert
	// CloudEntitySelector to service understandable filters.
	AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error
	// GetResourceFilters gets all instances resource filters for the service.
	GetResourceFilters() map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
	// RemoveResourceFilters will be used by service to remove configured filter.
	RemoveResourceFilters(selectorNamespacedName *types.NamespacedName)
	// DoResourceInventory performs resource inventory for the cloud service based on configured filters. As part
	// inventory, it is expected to save resources in service cache CloudServiceResourcesCache.
	DoResourceInventory() error
	// GetInventoryStats returns Inventory statistics for the service.
	GetInventoryStats() *CloudServiceStats
	// ResetInventoryCache clears any internal state built by the service as part of cloud resource discovery.
	ResetInventoryCache()
	// GetCloudInventory copies VPCs and VMs stored in internal snapshot(in cloud specific format) to internal format.
	GetCloudInventory() *nephetypes.CloudInventory
}

// CloudServiceResourcesCache is cache used by all services. Each service can maintain
// its resources specific cache by updating the snapshot.
type CloudServiceResourcesCache struct {
	mutex    sync.Mutex
	snapshot interface{}
}

func (cache *CloudServiceResourcesCache) UpdateSnapshot(newSnapshot interface{}) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.snapshot = newSnapshot
}

func (cache *CloudServiceResourcesCache) ClearSnapshot() {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	cache.snapshot = nil
}

func (cache *CloudServiceResourcesCache) GetSnapshot() interface{} {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	return cache.snapshot
}

type CloudServiceStats struct {
	mutex           sync.Mutex
	totalPollCnt    uint64
	successPollCnt  uint64
	lastPollErr     error
	lastPollErrTime time.Time
}

func (s *CloudServiceStats) IsInventoryInitialized() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.successPollCnt > 0
}

func (s *CloudServiceStats) UpdateInventoryPollStats(err error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.totalPollCnt++
	if err == nil {
		s.successPollCnt++
		return
	}
	s.lastPollErrTime = time.Now()
	s.lastPollErr = err
}

func (s *CloudServiceStats) ResetInventoryPollStats() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.totalPollCnt = 0
	s.successPollCnt = 0
	s.lastPollErrTime = time.Time{}
	s.lastPollErr = nil
}
