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

package azure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mohae/deepcopy"

	"github.com/cenkalti/backoff/v4"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

type computeServiceConfig struct {
	accountName            string
	nwIntfAPIClient        azureNwIntfWrapper
	nsgAPIClient           azureNsgWrapper
	asgAPIClient           azureAsgWrapper
	vnetAPIClient          azureVirtualNetworksWrapper
	resourceGraphAPIClient azureResourceGraphWrapper
	resourcesCache         *internal.CloudServiceResourcesCache
	inventoryStats         *internal.CloudServiceStats
	credentials            *azureAccountConfig
	computeFilters         map[string][]*string
}

type computeResourcesCacheSnapshot struct {
	virtualMachines map[cloudcommon.InstanceID]*virtualMachineTable
	vnetIDs         map[string]struct{}
	vnetPeers       map[string][][]string
}

func newComputeServiceConfig(name string, service azureServiceClientCreateInterface,
	credentials *azureAccountConfig) (internal.CloudServiceInterface, error) {
	// create compute sdk api client
	nwIntfAPIClient, err := service.networkInterfaces(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating compute sdk api client for account : %v, err: %v", name, err)
	}
	// create security-groups sdk api client
	securityGroupsAPIClient, err := service.securityGroups(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating security-groups sdk api client for account : %v, err: %v", name, err)
	}
	// create application-security-groups sdk api client
	applicationSecurityGroupsAPIClient, err := service.applicationSecurityGroups(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating application-security-groups sdk api client for account : %v, err: %v", name, err)
	}
	// create resource-graph sdk api client
	resourceGraphAPIClient, err := service.resourceGraph()
	if err != nil {
		return nil, fmt.Errorf("error creating resource-graph sdk api client for account : %v, err: %v", name, err)
	}

	// create virtual networks sdk api client
	vnetAPIClient, err := service.virtualNetworks(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual networks sdk api client for account : %v, err: %v", name, err)
	}

	config := &computeServiceConfig{
		accountName:            name,
		nwIntfAPIClient:        nwIntfAPIClient,
		nsgAPIClient:           securityGroupsAPIClient,
		asgAPIClient:           applicationSecurityGroupsAPIClient,
		vnetAPIClient:          vnetAPIClient,
		resourceGraphAPIClient: resourceGraphAPIClient,
		resourcesCache:         &internal.CloudServiceResourcesCache{},
		inventoryStats:         &internal.CloudServiceStats{},
		credentials:            credentials,
		computeFilters:         make(map[string][]*string),
	}
	return config, nil
}

func (computeCfg *computeServiceConfig) waitForInventoryInit(duration time.Duration) error {
	operation := func() error {
		done := computeCfg.inventoryStats.IsInventoryInitialized()
		if !done {
			return fmt.Errorf("inventory for account %v not initialized (waited %v duration)", computeCfg.accountName, duration)
		}
		return nil
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration

	return backoff.Retry(operation, b)
}

// getCachedVirtualMachines returns virtualMachines from the cache for the subscription.
func (computeCfg *computeServiceConfig) getCachedVirtualMachines() []*virtualMachineTable {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("compute service cache snapshot nil", "type", providerType, "account", computeCfg.accountName)
		return []*virtualMachineTable{}
	}
	virtualMachines := snapshot.(*computeResourcesCacheSnapshot).virtualMachines
	instancesToReturn := make([]*virtualMachineTable, 0, len(virtualMachines))
	for _, virtualMachine := range virtualMachines {
		instancesToReturn = append(instancesToReturn, virtualMachine)
	}

	azurePluginLogger().V(1).Info("cached instances", "service", azureComputeServiceNameCompute, "account", computeCfg.accountName,
		"instances", len(instancesToReturn))
	return instancesToReturn
}

func (computeCfg *computeServiceConfig) getCachedVnetIDs() map[string]struct{} {
	vnetIDsCopy := make(map[string]struct{})
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("compute service cache snapshot nil", "type", providerType, "account", computeCfg.accountName)
		return vnetIDsCopy
	}
	vnetIDsSet := snapshot.(*computeResourcesCacheSnapshot).vnetIDs

	for vnetID := range vnetIDsSet {
		vnetIDsCopy[vnetID] = struct{}{}
	}

	return vnetIDsCopy
}

func (computeCfg *computeServiceConfig) getVnetPeers(vnetID string) [][]string {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("compute service cache snapshot nil", "type", providerType, "account", computeCfg.accountName)
		return nil
	}
	vnetPeersCopy := make([][]string, 0)
	if peers, ok := snapshot.(*computeResourcesCacheSnapshot).vnetPeers[vnetID]; ok {
		vnetPeersCopy = deepcopy.Copy(peers).([][]string)
	}
	return vnetPeersCopy
}

func (computeCfg *computeServiceConfig) getVirtualMachines() ([]*virtualMachineTable, error) {
	var subscriptions []string
	subscriptions = append(subscriptions, computeCfg.credentials.SubscriptionID)

	var virtualMachines []*virtualMachineTable
	filters, _ := computeCfg.getComputeResourceFilters()
	for _, filter := range filters {
		virtualMachineRows, _, err := getVirtualMachineTable(computeCfg.resourceGraphAPIClient, filter, subscriptions)
		if err != nil {
			return nil, err
		}
		virtualMachines = append(virtualMachines, virtualMachineRows...)
	}

	azurePluginLogger().V(1).Info("instances from cloud", "service", azureComputeServiceNameCompute, "account", computeCfg.accountName,
		"instances", len(virtualMachines))

	return virtualMachines, nil
}

func (computeCfg *computeServiceConfig) getComputeResourceFilters() ([]*string, bool) {
	var allFilters []*string

	computeFilters := computeCfg.computeFilters
	if len(computeFilters) == 0 {
		return nil, false
	}

	for _, filters := range computeCfg.computeFilters {
		// if any selector found with nil filter, skip all other selectors. As nil indicates all
		if len(filters) == 0 {
			var queries []*string
			subscriptionIDs := []string{computeCfg.credentials.SubscriptionID}
			tenantIDs := []string{computeCfg.credentials.TenantID}
			locations := []string{computeCfg.credentials.region}
			queryStr, err := getVMsBySubscriptionIDsAndTenantIDsAndLocationsMatchQuery(subscriptionIDs, tenantIDs, locations)
			if err != nil {
				azurePluginLogger().Error(err, "query string creation failed", "account", computeCfg.accountName)
				return nil, false
			}
			queries = append(queries, queryStr)
			return queries, true
		}
		allFilters = append(allFilters, filters...)
	}
	return allFilters, true
}

func (computeCfg *computeServiceConfig) DoResourceInventory() error {
	virtualMachines, err := computeCfg.getVirtualMachines()
	if err == nil {
		exists := struct{}{}
		vnetIDs := make(map[string]struct{})
		vpcPeers, _ := computeCfg.buildMapVpcPeers()
		vmIDToInfoMap := make(map[cloudcommon.InstanceID]*virtualMachineTable)
		for _, vm := range virtualMachines {
			id := cloudcommon.InstanceID(strings.ToLower(*vm.ID))
			vmIDToInfoMap[id] = vm
			vnetIDs[*vm.VnetID] = exists
		}
		computeCfg.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{vmIDToInfoMap, vnetIDs, vpcPeers})
	}

	return err
}

func (computeCfg *computeServiceConfig) SetResourceFilters(selector *v1alpha1.CloudEntitySelector) {
	subscriptionIDs := []string{computeCfg.credentials.SubscriptionID}
	tenantIDs := []string{computeCfg.credentials.TenantID}
	locations := []string{computeCfg.credentials.region}
	if filters, found := convertSelectorToComputeQuery(selector, subscriptionIDs, tenantIDs, locations); found {
		computeCfg.computeFilters[selector.GetName()] = filters
	} else {
		if selector != nil {
			delete(computeCfg.computeFilters, selector.GetName())
		}
		computeCfg.resourcesCache.UpdateSnapshot(nil)
	}
}

func (computeCfg *computeServiceConfig) GetResourceCRDs(namespace string) *internal.CloudServiceResourceCRDs {
	virtualMachines := computeCfg.getCachedVirtualMachines()
	vmCRDs := make([]*v1alpha1.VirtualMachine, 0, len(virtualMachines))

	for _, virtualMachine := range virtualMachines {
		// build VirtualMachine CRD
		vmCRD := computeInstanceToVirtualMachineCRD(virtualMachine, namespace)
		vmCRDs = append(vmCRDs, vmCRD)
	}

	azurePluginLogger().V(1).Info("CRDs", "service", azureComputeServiceNameCompute, "account", computeCfg.accountName,
		"virtual-machine CRDs", len(vmCRDs))

	serviceResourceCRDs := &internal.CloudServiceResourceCRDs{}
	serviceResourceCRDs.SetComputeResourceCRDs(vmCRDs)

	return serviceResourceCRDs
}

func (computeCfg *computeServiceConfig) HasFiltersConfigured() (bool, bool) {
	filters, found := computeCfg.getComputeResourceFilters()
	return found, filters == nil
}

func (computeCfg *computeServiceConfig) GetName() internal.CloudServiceName {
	return azureComputeServiceNameCompute
}

func (computeCfg *computeServiceConfig) GetType() internal.CloudServiceType {
	return internal.CloudServiceTypeCompute
}

func (computeCfg *computeServiceConfig) GetInventoryStats() *internal.CloudServiceStats {
	return computeCfg.inventoryStats
}

func (computeCfg *computeServiceConfig) ResetCachedState() {
	computeCfg.SetResourceFilters(nil)
	computeCfg.inventoryStats.ResetInventoryPollStats()
}

func (computeCfg *computeServiceConfig) UpdateServiceConfig(newConfig internal.CloudServiceInterface) {
	newComputeServiceConfig := newConfig.(*computeServiceConfig)
	computeCfg.nwIntfAPIClient = newComputeServiceConfig.nwIntfAPIClient
	computeCfg.nsgAPIClient = newComputeServiceConfig.nsgAPIClient
	computeCfg.asgAPIClient = newComputeServiceConfig.asgAPIClient
	computeCfg.vnetAPIClient = newComputeServiceConfig.vnetAPIClient
	computeCfg.resourceGraphAPIClient = newComputeServiceConfig.resourceGraphAPIClient
	computeCfg.credentials = newComputeServiceConfig.credentials
}

func (computeCfg *computeServiceConfig) buildMapVpcPeers() (map[string][][]string, error) {
	vpcPeers := make(map[string][][]string)
	results, err := computeCfg.vnetAPIClient.listAllComplete(context.Background())
	if err != nil {
		azurePluginLogger().V(0).Info("error getting peering connections", "error", err)
		return nil, err
	}
	for _, result := range results {
		if len(*result.VirtualNetworkPropertiesFormat.VirtualNetworkPeerings) > 0 {
			for _, peerConn := range *result.VirtualNetworkPropertiesFormat.VirtualNetworkPeerings {
				accepterID := strings.ToLower(*result.ID)
				requesterID := strings.ToLower(*peerConn.VirtualNetworkPeeringPropertiesFormat.RemoteVirtualNetwork.ID)
				AddressPrefixes := *result.VirtualNetworkPropertiesFormat.AddressSpace.AddressPrefixes
				sourceID := strings.ToLower(AddressPrefixes[0])
				AddressPrefixes = *peerConn.VirtualNetworkPeeringPropertiesFormat.RemoteAddressSpace.AddressPrefixes
				destinationID := strings.ToLower(AddressPrefixes[0])
				vpcPeers[accepterID] = append(vpcPeers[accepterID], []string{requesterID, destinationID, sourceID})
			}
		}
	}
	return vpcPeers, nil
}
