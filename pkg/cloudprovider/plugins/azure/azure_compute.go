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

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
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
	azureConfig            *azureAccountConfig
	computeFilters         map[types.NamespacedName][]*string
	// selectors required for updating resource filters on account config update.
	selectors map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
}

type computeResourcesCacheSnapshot struct {
	// vm resources for each CloudEntitySelector CR.
	vms   map[types.NamespacedName][]*virtualMachineTable
	vnets []armnetwork.VirtualNetwork

	managedVnetIds map[string]struct{}
	vnetPeers      map[string][][]string
	nsgs           map[types.NamespacedName][]*nsgTable
}

// newComputeServiceConfig creates one compute service config for all regions. It returns a region to service config map or error happened.
func newComputeServiceConfig(account types.NamespacedName, service azureServiceClientCreateInterface,
	azureConfig *azureAccountConfig) (map[string]internal.CloudServiceInterface, error) {
	// create compute sdk api client
	nwIntfAPIClient, err := service.networkInterfaces(azureConfig.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating compute sdk api client for account: %v, err: %v", account, err)
	}
	// create security-groups sdk api client
	securityGroupsAPIClient, err := service.securityGroups(azureConfig.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating security-groups sdk api client for account: %v, err: %v", account, err)
	}
	// create application-security-groups sdk api client
	applicationSecurityGroupsAPIClient, err := service.applicationSecurityGroups(azureConfig.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating application-security-groups sdk api client for account: %v, err: %v", account, err)
	}
	// create resource-graph sdk api client
	resourceGraphAPIClient, err := service.resourceGraph()
	if err != nil {
		return nil, fmt.Errorf("error creating resource-graph sdk api client for account: %v, err: %v", account, err)
	}

	// create virtual networks sdk api client
	vnetAPIClient, err := service.virtualNetworks(azureConfig.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error creating virtual networks sdk api client for account: %v, err: %v", account, err)
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
		azureConfig:            azureConfig,
		computeFilters:         make(map[types.NamespacedName][]*string),
		selectors:              make(map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector),
	}

	vmSnapshot := make(map[types.NamespacedName][]*virtualMachineTable)
	config.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{vmSnapshot, nil, nil, nil, nil})

	configs := make(map[string]internal.CloudServiceInterface)
	for _, region := range azureConfig.regions {
		configs[region] = config
	}
	return configs, nil
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

// getManagedVnetIds returns vnet IDs of vnets containing managed vms.
func (computeCfg *computeServiceConfig) getManagedVnetIds() map[string]struct{} {
	vnetIdsCopy := make(map[string]struct{})
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return vnetIdsCopy
	}
	vnetIdsSet := snapshot.(*computeResourcesCacheSnapshot).managedVnetIds

	for vnetId := range vnetIdsSet {
		vnetIdsCopy[vnetId] = struct{}{}
	}

	return vnetIdsCopy
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

func (computeCfg *computeServiceConfig) getVnetPeers(vnetId string) [][]string {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	vnetPeersCopy := make([][]string, 0)
	if peers, ok := snapshot.(*computeResourcesCacheSnapshot).vnetPeers[vnetId]; ok {
		vnetPeersCopy = deepcopy.Copy(peers).([][]string)
	}
	return vnetPeersCopy
}

// getCachedSGs returns SGs from cached snapshot specific to a selector.
func (computeCfg *computeServiceConfig) getCachedSGs(selector *types.NamespacedName) []*nsgTable {
	sgsToReturn := make([]*nsgTable, 0)
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil", "account", computeCfg.accountNamespacedName)
		return sgsToReturn
	}
	nsgs, found := snapshot.(*computeResourcesCacheSnapshot).nsgs[*selector]
	if !found {
		azurePluginLogger().V(4).Info("Security group snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return sgsToReturn
	}
	sgsToReturn = append(sgsToReturn, nsgs...)

	return sgsToReturn
}

// getVirtualMachines gets virtual machines from cloud matching the given selector configuration.
func (computeCfg *computeServiceConfig) getVirtualMachines(namespacedName *types.NamespacedName) ([]*virtualMachineTable, error) {
	filters := computeCfg.computeFilters[*namespacedName]
	var subscriptions []*string
	subscriptions = append(subscriptions, &computeCfg.azureConfig.SubscriptionID)
	var virtualMachines []*virtualMachineTable
	for _, filter := range filters {
		virtualMachineRows, _, err := getVirtualMachineTable(computeCfg.resourceGraphAPIClient, filter, subscriptions)
		if err != nil {
			return nil, err
		}
		virtualMachines = append(virtualMachines, virtualMachineRows...)
	}
	return virtualMachines, nil
}

// DoResourceInventory gets inventory from cloud for all managed regions in the cloud account.
func (computeCfg *computeServiceConfig) DoResourceInventory() error {
	regions := computeCfg.azureConfig.regions
	vnets, err := computeCfg.getVpcs()
	var subscriptions []*string
	if err != nil {
		azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName,
			"regions", regions)
		return err
	}
	azurePluginLogger().V(1).Info("Vpcs from cloud", "account", computeCfg.accountNamespacedName,
		"regions", regions, "vpcs", len(vnets))
	vnetPeers := computeCfg.buildMapVpcPeers(vnets)
	allVirtualMachines := make(map[types.NamespacedName][]*virtualMachineTable)
	allNsgs := make(map[types.NamespacedName][]*nsgTable)

	// Make cloud API calls for fetching vm inventory for each configured CES.
	if len(computeCfg.selectors) == 0 {
		computeCfg.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{allVirtualMachines, vnets, nil, vnetPeers, allNsgs})
		azurePluginLogger().V(1).Info("Fetching vm resources from cloud skipped", "account", computeCfg.accountNamespacedName,
			"regions", regions, "resource-filters", "not-configured")
		return nil
	}

	allManagedVnetIds := make(map[string]struct{})
	subscriptions = append(subscriptions, &computeCfg.azureConfig.SubscriptionID)
	for namespacedName := range computeCfg.selectors {
		managedVnetIds := make(map[string]struct{})
		virtualMachines, err := computeCfg.getVirtualMachines(&namespacedName)
		if err != nil {
			azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName,
				"regions", regions)
			return err
		}
		azurePluginLogger().V(1).Info("Vm instances from cloud", "account", computeCfg.accountNamespacedName,
			"regions", regions, "selector", namespacedName, "instances", len(virtualMachines))
		for _, vm := range virtualMachines {
			allManagedVnetIds[*vm.VnetID] = struct{}{}
			managedVnetIds[*vm.VnetID] = struct{}{}
		}
		allVirtualMachines[namespacedName] = virtualMachines

		if cloudresource.IsCloudSecurityGroupVisibilityEnabled() {
			if len(managedVnetIds) > 0 {
				nsgFilter, err := getNsgByVnetIDsMatchQuery(managedVnetIds)
				if err != nil {
					azurePluginLogger().Error(err, "failed to create nsg Filter", "account", computeCfg.accountNamespacedName)
					return err
				}
				nsgs, _, err := getNsgTable(computeCfg.resourceGraphAPIClient, nsgFilter, subscriptions)
				if err != nil {
					azurePluginLogger().Error(err, "failed to get nsg from cloud resources", "account", computeCfg.accountNamespacedName)
					return err
				}
				allNsgs[namespacedName] = nsgs
			}
		}
	}
	computeCfg.resourcesCache.UpdateSnapshot(&computeResourcesCacheSnapshot{
		allVirtualMachines, vnets, allManagedVnetIds, vnetPeers, allNsgs})
	return nil
}

// AddResourceFilters add/updates instances resource filter for the service.
func (computeCfg *computeServiceConfig) AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error {
	subscriptionIDs := []string{computeCfg.azureConfig.SubscriptionID}
	tenantIDs := []string{computeCfg.azureConfig.TenantID}
	locations := computeCfg.azureConfig.regions
	namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	if filters, ok := convertSelectorToComputeQuery(selector, subscriptionIDs, tenantIDs, locations); ok {
		computeCfg.computeFilters[namespacedName] = filters
		computeCfg.selectors[namespacedName] = selector.DeepCopy()
	} else {
		return fmt.Errorf("error creating resource query filters")
	}

	return nil
}

// GetResourceFilters gets all instances resource filters for the service.
func (computeCfg *computeServiceConfig) GetResourceFilters() map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector {
	return computeCfg.selectors
}

func (computeCfg *computeServiceConfig) RemoveResourceFilters(selectorNamespacedName *types.NamespacedName) {
	delete(computeCfg.computeFilters, *selectorNamespacedName)
	delete(computeCfg.selectors, *selectorNamespacedName)
}

// getVirtualMachineObjects converts cached virtual machines in cloud format to internal runtimev1alpha1.VirtualMachine format.
func (computeCfg *computeServiceConfig) getVirtualMachineObjects(
	selectorNamespacedName *types.NamespacedName) map[string]*runtimev1alpha1.VirtualMachine {
	virtualMachines := computeCfg.getCachedVirtualMachines(selectorNamespacedName)
	vnets := computeCfg.getCachedVnetsMap()
	vmObjects := map[string]*runtimev1alpha1.VirtualMachine{}
	for _, virtualMachine := range virtualMachines {
		// build runtimev1alpha1 VirtualMachine object.
		vmObject := computeInstanceToInternalVirtualMachineObject(virtualMachine, vnets, selectorNamespacedName,
			&computeCfg.accountNamespacedName)
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
	computeCfg.azureConfig = newComputeServiceConfig.azureConfig
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
	// Store the vnets which are in the configured regions, discard the rest.
	targetRegions := make(map[string]struct{})
	for _, region := range computeCfg.azureConfig.regions {
		targetRegions[region] = struct{}{}
	}
	for _, vnet := range allVnets {
		if _, found := targetRegions[strings.ToLower(*vnet.Location)]; found {
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
				var requesterId, destinationId, sourceId string
				accepterId := strings.ToLower(*result.ID)
				peerProperties := peerConn.Properties
				if peerProperties != nil && peerProperties.RemoteVirtualNetwork != nil {
					requesterId = strings.ToLower(*peerConn.Properties.RemoteVirtualNetwork.ID)
				}

				if peerProperties != nil && peerProperties.RemoteAddressSpace != nil &&
					len(peerProperties.RemoteAddressSpace.AddressPrefixes) > 0 {
					destinationId = strings.ToLower(*peerProperties.RemoteAddressSpace.AddressPrefixes[0])
				}
				if properties.AddressSpace != nil && len(properties.AddressSpace.AddressPrefixes) > 0 {
					sourceId = strings.ToLower(*properties.AddressSpace.AddressPrefixes[0])
				}

				vpcPeers[accepterId] = append(vpcPeers[accepterId], []string{requesterId, destinationId, sourceId})
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
		SgMap:  map[types.NamespacedName]map[string]*runtimev1alpha1.SecurityGroup{},
	}
	cloudInventory.VpcMap = computeCfg.getVpcObjects()
	for ns := range computeCfg.selectors {
		cloudInventory.VmMap[ns] = computeCfg.getVirtualMachineObjects(&ns)
		cloudInventory.SgMap[ns] = computeCfg.getSecurityGroupObjects(&ns)
	}

	return &cloudInventory
}

// getVpcObjects generates vpc object for the vpcs stored in snapshot(in cloud format) and return a map of vpc runtime objects.
func (computeCfg *computeServiceConfig) getVpcObjects() map[string]*runtimev1alpha1.Vpc {
	managedVnetIds := computeCfg.getManagedVnetIds()
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().Info("Cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	// Convert to kubernetes object and return a map indexed using vnet ID.
	vpcMap := map[string]*runtimev1alpha1.Vpc{}
	for _, vpc := range snapshot.(*computeResourcesCacheSnapshot).vnets {
		managed := false
		if _, ok := managedVnetIds[strings.ToLower(*vpc.ID)]; ok {
			managed = true
		}
		vpcObj := ComputeVpcToInternalVpcObject(&vpc, computeCfg.accountNamespacedName.Namespace,
			computeCfg.accountNamespacedName.Name, managed)
		vpcMap[strings.ToLower(*vpc.ID)] = vpcObj
	}

	return vpcMap
}

// getSecurityGroupObjects generates security group object for the sgs stored in snapshot and return a map of sg runtime objects.
func (computeCfg *computeServiceConfig) getSecurityGroupObjects(
	selectorNamespacedName *types.NamespacedName) map[string]*runtimev1alpha1.SecurityGroup {
	sgs := computeCfg.getCachedSGs(selectorNamespacedName)
	sgMap := map[string]*runtimev1alpha1.SecurityGroup{}
	for _, sg := range sgs {
		sgObj := computeSgToInternalSgObject(sg, selectorNamespacedName, &computeCfg.accountNamespacedName)
		sgMap[strings.ToLower(*sg.ID)] = sgObj
	}
	return sgMap
}
