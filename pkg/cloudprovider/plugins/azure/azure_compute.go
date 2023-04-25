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

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/cenkalti/backoff/v4"
	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/types"
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
	selectorConfigMap      map[string]*selectorConfig
	configChangedSelectors []*types.NamespacedName
}

// selector struct is meant for storing details about CES configuration, created query string etc.
type selectorConfig struct {
	namespacedName  string
	instanceFilters []*string
	// selectors required for updating resource filters on account config update.
	ces *crdv1alpha1.CloudEntitySelector
}

type selectorLevelCacheSnapshot struct {
	virtualMachines map[internal.InstanceID]*virtualMachineTable
}

type accountLevelCacheSnapshot struct {
	vnets     []armnetwork.VirtualNetwork
	vnetIDs   map[string]struct{}
	vnetPeers map[string][][]string
}

type cacheSnapshot struct {
	// vm resources for each CloudEntitySelector CR.
	vmSnapshot  map[string]*selectorLevelCacheSnapshot
	vpcSnapshot *accountLevelCacheSnapshot
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
		configChangedSelectors: nil,
		selectorConfigMap:      make(map[string]*selectorConfig),
	}

	selectorSnapshot := make(map[string]*selectorLevelCacheSnapshot)
	config.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot, nil})
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

// getCachedVirtualMachines returns virtualMachines from the cache for the subscription.
func (computeCfg *computeServiceConfig) getCachedVirtualMachines(selector *types.NamespacedName) []*virtualMachineTable {
	selectorLevelSnapshot := computeCfg.getSelectorLevelSnapshot()
	if len(selectorLevelSnapshot) == 0 {
		azurePluginLogger().V(4).Info("Selector cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return []*virtualMachineTable{}
	}

	vmSnapshot, found := selectorLevelSnapshot[selector.String()]
	if !found {
		azurePluginLogger().V(4).Info("Vm snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return []*virtualMachineTable{}
	}

	virtualMachines := vmSnapshot.virtualMachines
	instancesToReturn := make([]*virtualMachineTable, 0, len(virtualMachines))
	for _, virtualMachine := range virtualMachines {
		instancesToReturn = append(instancesToReturn, virtualMachine)
	}
	azurePluginLogger().V(1).Info("Cached vm instances", "account", computeCfg.accountNamespacedName,
		"selector", selector, "instances", len(instancesToReturn))
	return instancesToReturn
}

// getManagedVnetIDs returns vnetIDs of vnets containing managed vms.
func (computeCfg *computeServiceConfig) getManagedVnetIDs() map[string]struct{} {
	vnetIDsCopy := make(map[string]struct{})
	vpcSnapshot := computeCfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		azurePluginLogger().Info("Account cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return vnetIDsCopy
	}
	vnetIDsSet := vpcSnapshot.vnetIDs

	for vnetID := range vnetIDsSet {
		vnetIDsCopy[vnetID] = struct{}{}
	}

	return vnetIDsCopy
}

// getManagedVnets returns vnets containing managed vms.
func (computeCfg *computeServiceConfig) getManagedVnets() map[string]armnetwork.VirtualNetwork {
	vnetCopy := make(map[string]armnetwork.VirtualNetwork)
	vpcSnapshot := computeCfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		azurePluginLogger().Info("Account cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return vnetCopy
	}

	for _, vnet := range vpcSnapshot.vnets {
		vnetCopy[strings.ToLower(*vnet.ID)] = vnet
	}

	return vnetCopy
}

func (computeCfg *computeServiceConfig) getVnetPeers(vnetID string) [][]string {
	vpcSnapshot := computeCfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		azurePluginLogger().Info("Account cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	vnetPeersCopy := make([][]string, 0)
	if peers, ok := vpcSnapshot.vnetPeers[vnetID]; ok {
		vnetPeersCopy = deepcopy.Copy(peers).([][]string)
	}
	return vnetPeersCopy
}

// getSelectorConfigsToPoll finds the list of CES for which making a cloud api call is required.
func (computeCfg *computeServiceConfig) getSelectorConfigsToPoll() []*selectorConfig {
	var cesConfigs []*selectorConfig
	if len(computeCfg.configChangedSelectors) == 0 {
		// get all CES for cloud api calls.
		for _, cesConfig := range computeCfg.selectorConfigMap {
			cesConfigs = append(cesConfigs, cesConfig)
		}
	} else {
		for _, namespacedName := range computeCfg.configChangedSelectors {
			if cesConfig, ok := computeCfg.selectorConfigMap[namespacedName.String()]; ok {
				cesConfigs = append(cesConfigs, cesConfig)
			}
		}
		computeCfg.configChangedSelectors = nil
	}
	return cesConfigs
}

// TODO: getVirtualMachines should be able to retrieve VM based on VNET ids as well.
func (computeCfg *computeServiceConfig) getVirtualMachines(updateSnapshot bool) ([]*virtualMachineTable, error) {
	// make cloud inventory calls for CES which are changed, if nothing changed make cloud calls for all CES.
	selectorObjects := computeCfg.getSelectorConfigsToPoll()
	if len(selectorObjects) == 0 {
		azurePluginLogger().V(1).Info("Fetching vm resources from cloud skipped",
			"account", computeCfg.accountNamespacedName, "resource-filters", "not-configured")
		return nil, nil
	}

	var allVirtualMachines []*virtualMachineTable
	// make cloud api call for each selector.
	for _, s := range selectorObjects {
		if len(s.instanceFilters) != 0 {
			azurePluginLogger().V(1).Info("Fetching vm resources from cloud",
				"account", computeCfg.accountNamespacedName, "resource-filters", "configured")
		}

		var subscriptions []*string
		subscriptions = append(subscriptions, &computeCfg.credentials.SubscriptionID)
		var virtualMachines []*virtualMachineTable
		for _, filter := range s.instanceFilters {
			virtualMachineRows, _, err := getVirtualMachineTable(computeCfg.resourceGraphAPIClient, filter, subscriptions)
			if err != nil {
				azurePluginLogger().Error(err, "failed to fetch cloud resources",
					"account", computeCfg.accountNamespacedName, "selector", s.namespacedName)
				return nil, err
			}
			virtualMachines = append(virtualMachines, virtualMachineRows...)
		}
		azurePluginLogger().V(1).Info("Vm instances from cloud",
			"account", computeCfg.accountNamespacedName, "selector", s.namespacedName,
			"instances", len(virtualMachines))

		// update VM snapshot specific to the current selector(CES).
		if updateSnapshot {
			vmIDToInfoMap := make(map[internal.InstanceID]*virtualMachineTable)
			for _, vm := range virtualMachines {
				vmIDToInfoMap[internal.InstanceID(strings.ToLower(*vm.ID))] = vm
			}

			snapshot := computeCfg.resourcesCache.GetSnapshot()
			selectorSnapshot := snapshot.(*cacheSnapshot).vmSnapshot
			vmSnapshot := selectorLevelCacheSnapshot{virtualMachines: vmIDToInfoMap}
			selectorSnapshot[s.namespacedName] = &vmSnapshot
			computeCfg.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot, snapshot.(*cacheSnapshot).vpcSnapshot})
		}
		allVirtualMachines = append(allVirtualMachines, virtualMachines...)
	}

	return allVirtualMachines, nil
}

func (computeCfg *computeServiceConfig) DoResourceInventory() error {
	vnets, err := computeCfg.getVpcs()
	if err != nil {
		azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName)
		return err
	}
	vpcPeers := computeCfg.buildMapVpcPeers(vnets)

	virtualMachines, err := computeCfg.getVirtualMachines(true)
	if err != nil {
		azurePluginLogger().Error(err, "failed to fetch cloud resources", "account", computeCfg.accountNamespacedName)
		return err
	} else {
		vnetIDs := make(map[string]struct{})
		for _, vm := range virtualMachines {
			vnetIDs[*vm.VnetID] = struct{}{}
		}
		// update global snapshot at account level.
		selectorSnapshot := computeCfg.getSelectorLevelSnapshot()
		if selectorSnapshot == nil {
			// Should never happen.
			azurePluginLogger().Info("Selector cache snapshot nil", "account", computeCfg.accountNamespacedName)
			return nil
		}
		vpcSnapshot := &accountLevelCacheSnapshot{vnets, vnetIDs, vpcPeers}
		computeCfg.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot, vpcSnapshot})
	}
	return nil
}

func (computeCfg *computeServiceConfig) AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error {
	subscriptionIDs := []string{computeCfg.credentials.SubscriptionID}
	tenantIDs := []string{computeCfg.credentials.TenantID}
	locations := []string{computeCfg.credentials.region}
	namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	if filters, ok := convertSelectorToComputeQuery(selector, subscriptionIDs, tenantIDs, locations); ok {
		azurePluginLogger().Info("resource query filters created", "selector", namespacedName)
		computeCfg.configChangedSelectors = append(computeCfg.configChangedSelectors, &namespacedName)
		computeCfg.selectorConfigMap[namespacedName.String()] = &selectorConfig{
			namespacedName:  namespacedName.String(),
			instanceFilters: filters,
			ces:             selector.DeepCopy(),
		}
	} else {
		return fmt.Errorf("error creating resource query filters for selector: %v", namespacedName)
	}

	return nil
}

func (computeCfg *computeServiceConfig) RemoveResourceFilters(selectorNamespacedName *types.NamespacedName) {
	delete(computeCfg.selectorConfigMap, selectorNamespacedName.String())
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot != nil {
		selectorSnapshot := snapshot.(*cacheSnapshot).vmSnapshot
		if selectorSnapshot != nil {
			delete(selectorSnapshot, selectorNamespacedName.String())
		}
		computeCfg.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot, snapshot.(*cacheSnapshot).vpcSnapshot})
	}
}

func (computeCfg *computeServiceConfig) GetInternalResourceObjects(accountNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) map[string]*runtimev1alpha1.VirtualMachine {
	virtualMachines := computeCfg.getCachedVirtualMachines(selectorNamespacedName)
	vnets := computeCfg.getManagedVnets()

	vmObjects := map[string]*runtimev1alpha1.VirtualMachine{}

	for _, virtualMachine := range virtualMachines {
		// build runtimev1alpha1 VirtualMachine object.
		vmObject := computeInstanceToInternalVirtualMachineObject(virtualMachine, vnets, selectorNamespacedName,
			accountNamespacedName, computeCfg.credentials.region)
		vmObjects[vmObject.Name] = vmObject
	}

	azurePluginLogger().V(1).Info("Internal resource objects", "account", computeCfg.accountNamespacedName,
		"selector", selectorNamespacedName, "VirtualMachine objects", len(vmObjects))

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

	for _, cesConfig := range computeCfg.selectorConfigMap {
		if err := computeCfg.AddResourceFilters(cesConfig.ces); err != nil {
			return err
		}
	}

	return nil
}

// getVpcs invokes cloud API to fetch the list of vnets.
func (computeCfg *computeServiceConfig) getVpcs() ([]armnetwork.VirtualNetwork, error) {
	return computeCfg.vnetAPIClient.listAllComplete(context.Background())
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

// GetVpcInventory generates vpc object for the vpcs stored in snapshot(in cloud format) and return a map of vpc runtime objects.
func (computeCfg *computeServiceConfig) GetVpcInventory() map[string]*runtimev1alpha1.Vpc {
	vnetIDs := computeCfg.getManagedVnetIDs()
	vpcSnapshot := computeCfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		azurePluginLogger().Info("Account cache snapshot nil",
			"type", providerType, "account", computeCfg.accountNamespacedName)
		return nil
	}

	// Convert to kubernetes object and return a map indexed using VnetID.
	vpcMap := map[string]*runtimev1alpha1.Vpc{}
	for _, vpc := range vpcSnapshot.vnets {
		if strings.EqualFold(*vpc.Location, computeCfg.credentials.region) {
			managed := false
			if _, ok := vnetIDs[strings.ToLower(*vpc.ID)]; ok {
				managed = true
			}
			vpcObj := ComputeVpcToInternalVpcObject(&vpc, computeCfg.accountNamespacedName.Namespace,
				computeCfg.accountNamespacedName.Name, strings.ToLower(computeCfg.credentials.region), managed)
			vpcMap[strings.ToLower(*vpc.ID)] = vpcObj
		}
	}
	azurePluginLogger().V(1).Info("Cached vpcs", "account", computeCfg.accountNamespacedName,
		"vpc objects", len(vpcMap))

	return vpcMap
}

func (computeCfg *computeServiceConfig) getAccountLevelSnapshot() *accountLevelCacheSnapshot {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("Account cache snapshot nil", "account", computeCfg.accountNamespacedName)
		return nil
	}
	return snapshot.(*cacheSnapshot).vpcSnapshot
}

func (computeCfg *computeServiceConfig) getSelectorLevelSnapshot() map[string]*selectorLevelCacheSnapshot {
	snapshot := computeCfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		azurePluginLogger().V(4).Info("Account cache snapshot nil", "account", computeCfg.accountNamespacedName)
		return nil
	}
	return snapshot.(*cacheSnapshot).vmSnapshot
}
