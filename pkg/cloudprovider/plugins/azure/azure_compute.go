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

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/cenkalti/backoff/v4"
	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	nephetypes "antrea.io/nephe/pkg/types"
)

type computeServiceConfig struct {
	accountNamespacedName  types.NamespacedName
	nwIntfAPIClient        azureNwIntfWrapper
	nsgAPIClient           azureNsgWrapper
	asgAPIClient           azureAsgWrapper
	vnetAPIClient          azureVirtualNetworksWrapper
	resourceGraphAPIClient azureResourceGraphWrapper
	resourcesCache         *internal.CloudServiceResourcesCache
	inventoryStats         *internal.CloudServiceStats
	credentials            *azureAccountConfig
	computeFilters         map[types.NamespacedName][]*string
	// selectors required for updating resource filters on account config update.
	selectors map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
}

type computeResourcesCacheSnapshot struct {
	// vm resources for each CloudEntitySelector CR.
	vms            map[types.NamespacedName][]*virtualMachineTable
	vnets          []armnetwork.VirtualNetwork
	managedVnetIDs map[string]struct{}
	vnetPeers      map[string][][]string
}

func newComputeServiceConfig(account types.NamespacedName, service azureServiceClientCreateInterface,
	credentials *azureAccountConfig) (internal.CloudServiceInterface, error) {
	// create compute sdk api client
	nwIntfAPIClient, err := service.networkInterfaces(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating compute sdk api client for account : %v, err: %v", account, err)
	}
	// create security-groups sdk api client
	securityGroupsAPIClient, err := service.securityGroups(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating security-groups sdk api client for account : %v, err: %v", account, err)
	}
	// create application-security-groups sdk api client
	applicationSecurityGroupsAPIClient, err := service.applicationSecurityGroups(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating application-security-groups sdk api client for account : %v, err: %v", account, err)
	}
	// create resource-graph sdk api client
	resourceGraphAPIClient, err := service.resourceGraph()
	if err != nil {
		return nil, fmt.Errorf("error creating resource-graph sdk api client for account : %v, err: %v", account, err)
	}

	// create virtual networks sdk api client
	vnetAPIClient, err := service.virtualNetworks(credentials.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual networks sdk api client for account : %v, err: %v", account, err)
	}

	config := &computeServiceConfig{
		accountNamespacedName:  account,
		nwIntfAPIClient:        nwIntfAPIClient,
		nsgAPIClient:           securityGroupsAPIClient,
		asgAPIClient:           applicationSecurityGroupsAPIClient,
		vnetAPIClient:          vnetAPIClient,
		resourceGraphAPIClient: resourceGraphAPIClient,
		resourcesCache:         &internal.CloudServiceResourcesCache{},
		inventoryStats:         &internal.CloudServiceStats{},
		credentials:            credentials,
		computeFilters:         make(map[types.NamespacedName][]*string),
		selectors:              make(map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector),
	}

	vmSnapshot := make(map[types.NamespacedName][]*virtualMachineTable)
	config.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{vmSnapshot, nil, nil, nil})
	return config, nil
}

func (computeCfg *computeServiceConfig) waitForInventoryInit(duration time.Duration) error {
	operation := func() error {
		done := computeCfg.inventoryStats.IsInventoryInitialized()
		if !done {
			return fmt.Errorf("inventory for account %v not initialized (waited %v duration)",
				computeCfg.accountNamespacedName, duration)
		}
		return nil
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration

	return backoff.Retry(operation, b)
}

// getCachedVirtualMachines returns virtualMachines specific to a selector from the cache for the subscription.
func (computeCfg *computeServiceConfig) getCachedVirtualMachines(selector *types.NamespacedName) []*virtualMachineTable {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return []*virtualMachineTable{}
	}

	virtualMachines, found := snapshot.(*computeResourcesCacheSnapshot).vms[*selector]
	if !found {
		azurePluginLogger().V(4).Info("Vm snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return []*virtualMachineTable{}
	}

	instancesToReturn := make([]*virtualMachineTable, 0, len(virtualMachines))
	instancesToReturn = append(instancesToReturn, virtualMachines...)
	return instancesToReturn
}

// getAllCachedVirtualMachines returns all virtualMachines from the cache for the subscription.
func (computeCfg *computeServiceConfig) getAllCachedVirtualMachines() []*virtualMachineTable {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return []*virtualMachineTable{}
	}

	instancesToReturn := make([]*virtualMachineTable, 0)
	for _, virtualMachines := range snapshot.(*computeResourcesCacheSnapshot).vms {
		instancesToReturn = append(instancesToReturn, virtualMachines...)
	}
	azurePluginLogger().V(1).Info("Cached vm instances", "account", computeCfg.accountNamespacedName,
		"instances", len(instancesToReturn))
	return instancesToReturn
}

// getManagedVnetIDs returns vnetIDs of vnets containing managed vms.
func (computeCfg *computeServiceConfig) getManagedVnetIDs() map[string]struct{} {
	vnetIDsCopy := make(map[string]struct{})
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return vnetIDsCopy
	}
	vnetIDsSet := snapshot.(*computeResourcesCacheSnapshot).managedVnetIDs

	for vnetID := range vnetIDsSet {
		vnetIDsCopy[vnetID] = struct{}{}
	}

	return vnetIDsCopy
}

// getCachedVnetsMap returns vnets from cache in map format.
func (computeCfg *computeServiceConfig) getCachedVnetsMap() map[string]armnetwork.VirtualNetwork {
	vnetCopy := make(map[string]armnetwork.VirtualNetwork)
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return vnetCopy
	}

	for _, vnet := range snapshot.(*computeResourcesCacheSnapshot).vnets {
		vnetCopy[strings.ToLower(*vnet.ID)] = vnet
	}

	return vnetCopy
}

func (computeCfg *computeServiceConfig) getVnetPeers(vnetID string) [][]string {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	vnetPeersCopy := make([][]string, 0)
	if peers, ok := snapshot.(*computeResourcesCacheSnapshot).vnetPeers[vnetID]; ok {
		vnetPeersCopy = deepcopy.Copy(peers).([][]string)
	}
	return vnetPeersCopy
}

// getVirtualMachines gets virtual machines from cloud matching the given selector configuration.
func (computeCfg *computeServiceConfig) getVirtualMachines(namespacedName *types.NamespacedName) ([]*virtualMachineTable, error) {
	filters, found := computeCfg.computeFilters[*namespacedName]
	if found && len(filters) != 0 {
		azurePluginLogger().V(1).Info("Fetching vm resources from cloud",
			"account", computeCfg.accountNamespacedName, "selector", namespacedName, "resource-filters", "configured")
	}
	var subscriptions []*string
	subscriptions = append(subscriptions, &computeCfg.credentials.SubscriptionID)
	var virtualMachines []*virtualMachineTable
	for _, filter := range filters {
		virtualMachineRows, _, err := getVirtualMachineTable(computeCfg.resourceGraphAPIClient, filter, subscriptions)
		if err != nil {
			azurePluginLogger().Error(err, "failed to fetch cloud resources",
				"account", computeCfg.accountNamespacedName, "selector", namespacedName)
			return nil, err
		}
		virtualMachines = append(virtualMachines, virtualMachineRows...)
	}
	azurePluginLogger().V(1).Info("Vm instances from cloud", "account", computeCfg.accountNamespacedName,
		"selector", namespacedName, "instances", len(virtualMachines))

	return virtualMachines, nil
}

func (computeCfg *computeServiceConfig) DoResourceInventory() error {
	vnets, err := computeCfg.getVpcs()
	if err != nil {
		azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName)
		return err
	}
	azurePluginLogger().V(1).Info("Vpcs from cloud", "account", computeCfg.accountNamespacedName,
		"vpcs", len(vnets))
	vnetPeers := computeCfg.buildMapVpcPeers(vnets)
	allVirtualMachines := make(map[types.NamespacedName][]*virtualMachineTable)

	// Make cloud API calls for fetching vm inventory for each configured CES.
	if len(computeCfg.selectors) == 0 {
		computeCfg.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{allVirtualMachines, vnets, nil, vnetPeers})
		azurePluginLogger().V(1).Info("Fetching vm resources from cloud skipped",
			"account", computeCfg.accountNamespacedName, "resource-filters", "not-configured")
		return nil
	}

	managedVnetIDs := make(map[string]struct{})
	for namespacedName := range computeCfg.selectors {
		virtualMachines, err := computeCfg.getVirtualMachines(&namespacedName)
		if err != nil {
			azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName)
			return err
		}
		for _, vm := range virtualMachines {
			managedVnetIDs[*vm.VnetID] = struct{}{}
		}
		allVirtualMachines[namespacedName] = virtualMachines
	}
	computeCfg.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{allVirtualMachines, vnets, managedVnetIDs, vnetPeers})
	return nil
}

func (computeCfg *computeServiceConfig) AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error {
	subscriptionIDs := []string{computeCfg.credentials.SubscriptionID}
	tenantIDs := []string{computeCfg.credentials.TenantID}
	locations := []string{computeCfg.credentials.region}
	namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	if filters, ok := convertSelectorToComputeQuery(selector, subscriptionIDs, tenantIDs, locations); ok {
		computeCfg.computeFilters[namespacedName] = filters
		computeCfg.selectors[namespacedName] = selector.DeepCopy()
	} else {
		return fmt.Errorf("error creating resource query filters")
	}

	return nil
}

func (computeCfg *computeServiceConfig) RemoveResourceFilters(selectorNamespacedName *types.NamespacedName) {
	delete(computeCfg.computeFilters, *selectorNamespacedName)
	delete(computeCfg.selectors, *selectorNamespacedName)
}

// getVirtualMachineObjects converts cached virtual machines in cloud format to internal runtimev1alpha1.VirtualMachine format.
func (computeCfg *computeServiceConfig) getVirtualMachineObjects(accountNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) map[string]*runtimev1alpha1.VirtualMachine {
	virtualMachines := computeCfg.getCachedVirtualMachines(selectorNamespacedName)
	vnets := computeCfg.getCachedVnetsMap()
	vmObjects := map[string]*runtimev1alpha1.VirtualMachine{}
	for _, virtualMachine := range virtualMachines {
		// build runtimev1alpha1 VirtualMachine object.
		vmObject := computeInstanceToInternalVirtualMachineObject(virtualMachine, vnets, selectorNamespacedName,
			accountNamespacedName, computeCfg.credentials.region)
		vmObjects[vmObject.Name] = vmObject
	}

	return vmObjects
}

func (computeCfg *computeServiceConfig) GetInventoryStats() *internal.CloudServiceStats {
	return computeCfg.inventoryStats
}

func (computeCfg *computeServiceConfig) ResetInventoryCache() {
	computeCfg.resourcesCache.UpdateSnapshot(nil)
	computeCfg.inventoryStats.ResetInventoryPollStats()
}

func (computeCfg *computeServiceConfig) UpdateServiceConfig(newConfig internal.CloudServiceInterface) error {
	newComputeServiceConfig := newConfig.(*computeServiceConfig)
	computeCfg.nwIntfAPIClient = newComputeServiceConfig.nwIntfAPIClient
	computeCfg.nsgAPIClient = newComputeServiceConfig.nsgAPIClient
	computeCfg.asgAPIClient = newComputeServiceConfig.asgAPIClient
	computeCfg.vnetAPIClient = newComputeServiceConfig.vnetAPIClient
	computeCfg.resourceGraphAPIClient = newComputeServiceConfig.resourceGraphAPIClient
	computeCfg.credentials = newComputeServiceConfig.credentials
	for _, selector := range computeCfg.selectors {
		if err := computeCfg.AddResourceFilters(selector); err != nil {
			return err
		}
	}
	return nil
}

// getVpcs invokes cloud API to fetch the list of vnets.
func (computeCfg *computeServiceConfig) getVpcs() ([]armnetwork.VirtualNetwork, error) {
	vnets := make([]armnetwork.VirtualNetwork, 0)
	allVnets, err := computeCfg.vnetAPIClient.listAllComplete(context.Background())
	if err != nil {
		return vnets, err
	}
	// Store the vnets which are in the configured region, discard the rest.
	for _, vnet := range allVnets {
		if strings.EqualFold(*vnet.Location, computeCfg.credentials.region) {
			vnets = append(vnets, vnet)
		}
	}
	return vnets, nil
}

func (computeCfg *computeServiceConfig) buildMapVpcPeers(results []armnetwork.VirtualNetwork) map[string][][]string {
	vpcPeers := make(map[string][][]string)

	for _, result := range results {
		if result.Properties == nil {
			continue
		}
		properties := result.Properties
		if len(properties.VirtualNetworkPeerings) > 0 {
			for _, peerConn := range properties.VirtualNetworkPeerings {
				var requesterID, destinationID, sourceID string
				accepterID := strings.ToLower(*result.ID)
				peerProperties := peerConn.Properties
				if peerProperties != nil && peerProperties.RemoteVirtualNetwork != nil {
					requesterID = strings.ToLower(*peerConn.Properties.RemoteVirtualNetwork.ID)
				}

				if peerProperties != nil && peerProperties.RemoteAddressSpace != nil &&
					len(peerProperties.RemoteAddressSpace.AddressPrefixes) > 0 {
					destinationID = strings.ToLower(*peerProperties.RemoteAddressSpace.AddressPrefixes[0])
				}
				if properties.AddressSpace != nil && len(properties.AddressSpace.AddressPrefixes) > 0 {
					sourceID = strings.ToLower(*properties.AddressSpace.AddressPrefixes[0])
				}

				vpcPeers[accepterID] = append(vpcPeers[accepterID], []string{requesterID, destinationID, sourceID})
			}
		}
	}
	return vpcPeers
}

// GetCloudInventory fetches VM and VPC inventory from stored snapshot and converts from cloud format to internal format.
func (computeCfg *computeServiceConfig) GetCloudInventory() *nephetypes.CloudInventory {
	cloudInventory := nephetypes.CloudInventory{
		VmMap:  map[types.NamespacedName]map[string]*runtimev1alpha1.VirtualMachine{},
		VpcMap: map[string]*runtimev1alpha1.Vpc{},
	}
	cloudInventory.VpcMap = computeCfg.getVpcObjects()
	for ns := range computeCfg.selectors {
		cloudInventory.VmMap[ns] = computeCfg.getVirtualMachineObjects(&computeCfg.accountNamespacedName, &ns)
	}

	return &cloudInventory
}

// getVpcObjects generates vpc object for the vpcs stored in snapshot(in cloud format) and return a map of vpc runtime objects.
func (computeCfg *computeServiceConfig) getVpcObjects() map[string]*runtimev1alpha1.Vpc {
	managedVnetIDs := computeCfg.getManagedVnetIDs()
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	// Convert to kubernetes object and return a map indexed using VnetID.
	vpcMap := map[string]*runtimev1alpha1.Vpc{}
	for _, vpc := range snapshot.(*computeResourcesCacheSnapshot).vnets {
		managed := false
		if _, ok := managedVnetIDs[strings.ToLower(*vpc.ID)]; ok {
			managed = true
		}
		vpcObj := ComputeVpcToInternalVpcObject(&vpc, computeCfg.accountNamespacedName.Namespace,
			computeCfg.accountNamespacedName.Name, strings.ToLower(computeCfg.credentials.region), managed)
		vpcMap[strings.ToLower(*vpc.ID)] = vpcObj
	}

	return vpcMap
}
