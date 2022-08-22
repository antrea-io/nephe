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

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

type CloudServiceName string
type CloudServiceType string

const (
	CloudServiceTypeCompute = CloudServiceType("Compute")
)

type CloudServiceCommon struct {
	mutex sync.Mutex

	serviceInterface CloudServiceInterface
}

// CloudServiceInterface needs to be implemented by every cloud-service to be added for a cloud-plugin.
// Once implemented, cloud-service implementation of CloudServiceInterface will get injected into
// plugin-common-framework using CloudCommonHelperInterface.
type CloudServiceInterface interface {
	// UpdateServiceConfig updates existing service config with new values. Each service can decide to update one or
	// more fields of the service.
	UpdateServiceConfig(newServiceConfig CloudServiceInterface)
	// SetResourceFilters will be used by service to get resources from cloud for the service. Each will convert
	// CloudEntitySelector to service understandable filters.
	SetResourceFilters(selector *cloudv1alpha1.CloudEntitySelector)
	// HasFiltersConfigured returns if service has filters configured and if the configured filters are nil or not.
	HasFiltersConfigured() (bool, bool)
	// DoResourceInventory performs resource inventory for the cloud service based on configured filters. As part
	// inventory, it is expected to save resources in service cache CloudServiceResourcesCache.
	DoResourceInventory() error
	// GetInventoryStats returns Inventory statistics for the service.
	GetInventoryStats() *CloudServiceStats
	// GetResourceCRDs returns Service resource saved in CloudServiceResourcesCache in terms of CRD.
	GetResourceCRDs(namespace string) *CloudServiceResourceCRDs
	// GetName returns cloud name of the service.
	GetName() CloudServiceName
	// GetType returns service type (compute, any other type etc.)
	GetType() CloudServiceType
	// ResetCachedState clears any internal state build by the service as part of cloud resource discovery.
	ResetCachedState()
}

func (cfg *CloudServiceCommon) updateServiceConfig(newConfig CloudServiceInterface) {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	cfg.serviceInterface.UpdateServiceConfig(newConfig)
}

func (cfg *CloudServiceCommon) setResourceFilters(selector *cloudv1alpha1.CloudEntitySelector) {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	cfg.serviceInterface.SetResourceFilters(selector)
}

func (cfg *CloudServiceCommon) hasFiltersConfigured() (bool, bool) {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	return cfg.serviceInterface.HasFiltersConfigured()
}

func (cfg *CloudServiceCommon) doResourceInventory() error {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	return cfg.serviceInterface.DoResourceInventory()
}

func (cfg *CloudServiceCommon) getInventoryStats() *CloudServiceStats {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	return cfg.serviceInterface.GetInventoryStats()
}

func (cfg *CloudServiceCommon) getResourceCRDs(namespace string) *CloudServiceResourceCRDs {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	return cfg.serviceInterface.GetResourceCRDs(namespace)
}

func (cfg *CloudServiceCommon) getName() CloudServiceName {
	return cfg.serviceInterface.GetName()
}

func (cfg *CloudServiceCommon) getType() CloudServiceType {
	return cfg.serviceInterface.GetType()
}

func (cfg *CloudServiceCommon) resetCachedState() {
	cfg.mutex.Lock()
	defer cfg.mutex.Unlock()

	cfg.serviceInterface.ResetCachedState()
}

type CloudServiceResourceCRDs struct {
	virtualMachines []*cloudv1alpha1.VirtualMachine
}

// SetComputeResourceCRDs sets Service resource CRDs for accessing it from cloudCommon interface.
func (s *CloudServiceResourceCRDs) SetComputeResourceCRDs(vms []*cloudv1alpha1.VirtualMachine) {
	s.virtualMachines = vms
}

// CloudServiceResourcesCache is cache used by all services. Each service can maintain its resources specific cache by
// updating the snapshot.
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
