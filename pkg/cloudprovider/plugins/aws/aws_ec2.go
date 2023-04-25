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

package aws

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cenkalti/backoff/v4"
	"github.com/mohae/deepcopy"
	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
)

type ec2ServiceConfig struct {
	accountNamespacedName  types.NamespacedName
	apiClient              awsEC2Wrapper
	credentials            *awsAccountConfig
	resourcesCache         *internal.CloudServiceResourcesCache
	inventoryStats         *internal.CloudServiceStats
	selectorConfigMap      map[string]*selectorConfig
	configChangedSelectors []*types.NamespacedName
}

// selectorConfig struct is meant for storing details about CES configuration, created query string etc.
type selectorConfig struct { // TODO: Azure Come up with a better name.
	namespacedName string
	// instanceFilters has following possible values
	// - empty map indicates no selectors configured for this account. NO cloud api call for inventory will be made.
	// - non-empty map indicates selectors are configured. Cloud api call for inventory will be made.
	// - key with nil value indicates no filters. Get all instances for account.
	// - key with "some-filter-string" value indicates some filter. Get instances matching those filters only.
	instanceFilters [][]*ec2.Filter

	// selectors required for updating resource filters on account config update.
	selector *crdv1alpha1.CloudEntitySelector
}

type selectorLevelCacheSnapshot struct {
	instances map[internal.InstanceID]*ec2.Instance
}

type accountLevelCacheSnapshot struct {
	vpcs        []*ec2.Vpc
	vpcIDs      map[string]struct{}
	vpcNameToID map[string]string
	vpcPeers    map[string][]string
}

type cacheSnapshot struct {
	// TODO: Aws
	// vm resources for each CloudEntitySelector CR.
	// TODO: Delete snaphost when CES is deleted.
	vmSnapshot  map[string]*selectorLevelCacheSnapshot
	vpcSnapshot *accountLevelCacheSnapshot
}

func newEC2ServiceConfig(accountNamespacedName types.NamespacedName, service awsServiceClientCreateInterface,
	credentials *awsAccountConfig) (internal.CloudServiceInterface, error) {
	// create ec2 sdk api client.
	apiClient, err := service.compute()
	if err != nil {
		return nil, fmt.Errorf("error creating ec2 sdk api client for account : %v, err: %v", accountNamespacedName.String(), err)
	}

	config := &ec2ServiceConfig{
		apiClient:              apiClient,
		accountNamespacedName:  accountNamespacedName,
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

// compute returns AWS Compute (ec2) SDK apiClient.
func (p *awsServiceSdkConfigProvider) compute() (awsEC2Wrapper, error) {
	ec2Client := ec2.New(p.session)

	awsEC2 := &awsEC2WrapperImpl{
		ec2: ec2Client,
	}

	return awsEC2, nil
}

func (ec2Cfg *ec2ServiceConfig) waitForInventoryInit(duration time.Duration) error {
	operation := func() error {
		done := ec2Cfg.inventoryStats.IsInventoryInitialized()
		if !done {
			return fmt.Errorf("inventory for account %v not initialized (waited %v duration)", ec2Cfg.accountNamespacedName, duration)
		}
		return nil
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration

	return backoff.Retry(operation, b)
}

// getInstanceResourceFilters returns filters to be applied to describeInstances api if filters are configured.
// Otherwise, returns (nil, false). false indicates no selectors configured for the account and hence no cloud api needs
// to be made for instance inventory.
func (ec2Cfg *ec2ServiceConfig) getInstanceResourceFilters(s *selectorConfig) ([][]*ec2.Filter, bool) {
	for _, filters := range s.instanceFilters {
		// if any selector found with nil filter, skip all other selectors, as nil indicates all.
		if len(filters) == 0 {
			return nil, true
		}
	}
	return s.instanceFilters, true
}

// getCachedInstances returns instances from the cache for the account.
func (ec2Cfg *ec2ServiceConfig) getCachedInstances(selector *types.NamespacedName) []*ec2.Instance {
	selectorSnapshot := ec2Cfg.getSelectorLevelSnapshot()
	if selectorSnapshot == nil {
		awsPluginLogger().V(4).Info("Selector cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return []*ec2.Instance{}
	}

	vmSnapshot, found := selectorSnapshot[selector.String()]
	if !found {
		awsPluginLogger().V(4).Info("Vm cache snapshot not found",
			"account", ec2Cfg.accountNamespacedName, "selector", selector.String())
		return []*ec2.Instance{}
	}
	instances := vmSnapshot.instances
	instancesToReturn := make([]*ec2.Instance, 0, len(instances))
	for _, instance := range instances {
		instancesToReturn = append(instancesToReturn, instance)
	}
	return instancesToReturn
}

// getManagedVpcIDs returns vpcIDs of vpcs containing managed vms.
func (ec2Cfg *ec2ServiceConfig) getManagedVpcIDs() map[string]struct{} {
	vpcSnapshot := ec2Cfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}

	vpcIDsCopy := make(map[string]struct{})
	vpcIDsSet := vpcSnapshot.vpcIDs

	for vpcID := range vpcIDsSet {
		vpcIDsCopy[vpcID] = struct{}{}
	}

	return vpcIDsCopy
}

// getManagedVpcs returns vpcs containing managed vms.
func (ec2Cfg *ec2ServiceConfig) getManagedVpcs() map[string]*ec2.Vpc {
	vpcSnapshot := ec2Cfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}

	vpcCopy := make(map[string]*ec2.Vpc)
	for _, vpc := range vpcSnapshot.vpcs {
		vpcCopy[strings.ToLower(*vpc.VpcId)] = vpc
	}

	return vpcCopy
}

// getCachedVpcNameToID returns the map vpcNameToID from the cache.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcNameToID() map[string]string {
	vpcSnapshot := ec2Cfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}

	vpcNameToIDCopy := make(map[string]string)
	vpcNameToID := vpcSnapshot.vpcNameToID

	for k, v := range vpcNameToID {
		vpcNameToIDCopy[k] = v
	}

	return vpcNameToIDCopy
}

// GetCachedVpcs returns VPCs from cached snapshot for the account.
func (ec2Cfg *ec2ServiceConfig) GetCachedVpcs() []*ec2.Vpc {
	vpcSnapshot := ec2Cfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}
	vpcs := vpcSnapshot.vpcs
	vpcsToReturn := make([]*ec2.Vpc, 0, len(vpcs))
	vpcsToReturn = append(vpcsToReturn, vpcs...)

	return vpcsToReturn
}

// getVpcPeers returns all the peers of a vpc.
func (ec2Cfg *ec2ServiceConfig) getVpcPeers(vpcID string) []string {
	vpcSnapshot := ec2Cfg.getAccountLevelSnapshot()
	if vpcSnapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}

	vpcPeersCopy := make([]string, 0)
	if peers, ok := vpcSnapshot.vpcPeers[vpcID]; ok {
		vpcPeersCopy = deepcopy.Copy(peers).([]string)
	}
	return vpcPeersCopy
}

// TODO: Azure getVirtualMachines should be able to retrieve VM based on VNET ids as well.
// getInstances gets instance for the account from aws EC2 API.
func (ec2Cfg *ec2ServiceConfig) getInstances() ([]*ec2.Instance, error) {
	// Call cloud APIs only for CloudEntitySelectors which are changed. Default is to call for all CloudEntitySelectors.
	cesConfigs := ec2Cfg.getSelectorConfigsToPoll()
	if len(cesConfigs) == 0 {
		awsPluginLogger().V(1).Info("Fetching vm resources from cloud skipped",
			"account", ec2Cfg.accountNamespacedName, "resource-filters", "not-configured")
		return nil, nil
	}

	var allVirtualMachines []*ec2.Instance
	for _, cesConfig := range cesConfigs {
		var instances []*ec2.Instance
		var err error
		filters, _ := ec2Cfg.getInstanceResourceFilters(cesConfig)
		if len(filters) == 0 {
			// TODO: Should we support this use-case?
			awsPluginLogger().V(1).Info("Fetching vm resources from cloud",
				"account", ec2Cfg.accountNamespacedName, "resource-filters", "all(nil)")
			var validInstanceStateFilters []*ec2.Filter
			validInstanceStateFilters = append(validInstanceStateFilters, buildEc2FilterForValidInstanceStates())
			request := &ec2.DescribeInstancesInput{
				MaxResults: aws.Int64(internal.MaxCloudResourceResponse),
				Filters:    validInstanceStateFilters,
			}
			// If a filter is not set for a selector, then return all the VMs.
			instances, err = ec2Cfg.apiClient.pagedDescribeInstancesWrapper(request)
			if err != nil {
				return nil, err
			}
		} else {
			awsPluginLogger().V(1).Info("Fetching vm resources from cloud",
				"account", ec2Cfg.accountNamespacedName, "resource-filters", "configured")
			for _, filter := range filters {
				if len(filter) > 0 {
					if *filter[0].Name == awsCustomFilterKeyVPCName {
						filter = buildFilterForVPCIDFromFilterForVPCName(filter, ec2Cfg.getCachedVpcNameToID())
					}
				}
				request := &ec2.DescribeInstancesInput{
					MaxResults: aws.Int64(internal.MaxCloudResourceResponse),
					Filters:    filter,
				}
				filterInstances, err := ec2Cfg.apiClient.pagedDescribeInstancesWrapper(request)
				if err != nil {
					return nil, err
				}
				instances = append(instances, filterInstances...)
			}
		}

		// update VM snapshot of the current CloudEntitySelector.
		snapshot := ec2Cfg.resourcesCache.GetSnapshot()
		if snapshot == nil {
			return nil, fmt.Errorf("account cache snapshot nil")
		}
		selectorSnapshot := snapshot.(*cacheSnapshot).vmSnapshot
		if selectorSnapshot == nil {
			return nil, fmt.Errorf("selector cache snapshot nil")
		}
		vmIDToInfoMap := make(map[internal.InstanceID]*ec2.Instance)
		for _, vm := range instances {
			vmIDToInfoMap[internal.InstanceID(strings.ToLower(*vm.InstanceId))] = vm
		}
		vmSnapshot := &selectorLevelCacheSnapshot{instances: vmIDToInfoMap}
		selectorSnapshot[cesConfig.namespacedName] = vmSnapshot
		ec2Cfg.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot,
			snapshot.(*cacheSnapshot).vpcSnapshot})

		allVirtualMachines = append(allVirtualMachines, instances...)
		// TODO: Azure Should we log instances count per ces and dump ces info? i.e. when CES is changed.
		awsPluginLogger().Info("Vm instances from cloud", "account", ec2Cfg.accountNamespacedName,
			"selector", cesConfig.namespacedName, "instances", len(allVirtualMachines))
	}

	return allVirtualMachines, nil
}

// getSelectorConfigsToPoll returns a list of selector configurations to poll the cloud.
func (ec2Cfg *ec2ServiceConfig) getSelectorConfigsToPoll() []*selectorConfig {
	var cesConfigs []*selectorConfig
	if len(ec2Cfg.configChangedSelectors) == 0 {
		// return all the available selector configs for this account.
		for _, cesConfig := range ec2Cfg.selectorConfigMap {
			cesConfigs = append(cesConfigs, cesConfig)
		}
	} else {
		for _, namespacedName := range ec2Cfg.configChangedSelectors {
			if cesConfig, ok := ec2Cfg.selectorConfigMap[namespacedName.String()]; ok {
				cesConfigs = append(cesConfigs, cesConfig)
			}
		}
		ec2Cfg.configChangedSelectors = nil
	}
	return cesConfigs
}

// DoResourceInventory gets inventory from cloud for a given cloud account.
func (ec2Cfg *ec2ServiceConfig) DoResourceInventory() error {
	vpcs, err := ec2Cfg.getVpcs()
	if err != nil {
		awsPluginLogger().Error(err, "failed to fetch cloud resources", "account", ec2Cfg.accountNamespacedName)
		return err
	}

	// TODO: Avoid updating snapshot twice i.e. once for vm and the other for vpc.
	instances, err := ec2Cfg.getInstances()
	if err != nil {
		awsPluginLogger().Error(err, "failed to fetch cloud resources", "account", ec2Cfg.accountNamespacedName)
		return err
	} else {
		vpcIDs := make(map[string]struct{})
		instanceIDs := make(map[internal.InstanceID]*ec2.Instance)
		vpcNameToID := ec2Cfg.buildMapVpcNameToID(vpcs)
		vpcPeers, _ := ec2Cfg.buildMapVpcPeers()
		for _, instance := range instances {
			id := internal.InstanceID(strings.ToLower(aws.StringValue(instance.InstanceId)))
			instanceIDs[id] = instance
			vpcIDs[strings.ToLower(*instance.VpcId)] = struct{}{}
		}

		selectorSnapshot := ec2Cfg.getSelectorLevelSnapshot()
		if selectorSnapshot == nil {
			// Should never happen.
			awsPluginLogger().Info("Selector cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
			return nil
		}
		// update vpc info in the snapshot.
		vpcSnapshot := &accountLevelCacheSnapshot{vpcs, vpcIDs, vpcNameToID, vpcPeers}
		ec2Cfg.resourcesCache.UpdateSnapshot(&cacheSnapshot{selectorSnapshot, vpcSnapshot})
	}

	return nil
}

// AddResourceFilters add/updates instances resource filter for the service.
func (ec2Cfg *ec2ServiceConfig) AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error {
	namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	if filters, ok := convertSelectorToEC2InstanceFilters(selector); ok {
		ec2Cfg.configChangedSelectors = append(ec2Cfg.configChangedSelectors, &namespacedName)
		ec2Cfg.selectorConfigMap[namespacedName.String()] = &selectorConfig{
			namespacedName:  namespacedName.String(),
			instanceFilters: filters,
			selector:        selector.DeepCopy(),
		}
	} else {
		return fmt.Errorf("error creating resource query filters for selector: %v", namespacedName)
	}
	return nil
}

func (ec2Cfg *ec2ServiceConfig) RemoveResourceFilters(namespacedName *types.NamespacedName) {
	delete(ec2Cfg.selectorConfigMap, namespacedName.String())
	selectorSnapshot := ec2Cfg.getSelectorLevelSnapshot()
	if selectorSnapshot != nil {
		delete(selectorSnapshot, namespacedName.String())
	}
	// TODO: update snapshot.
}

func (ec2Cfg *ec2ServiceConfig) GetInternalResourceObjects(accountNamespacedName *types.NamespacedName,
	selector *types.NamespacedName) map[string]*runtimev1alpha1.VirtualMachine {
	instances := ec2Cfg.getCachedInstances(selector)
	vpcs := ec2Cfg.getManagedVpcs()

	vmObjects := map[string]*runtimev1alpha1.VirtualMachine{}
	for _, instance := range instances {
		// build runtime.v1alpha1.VirtualMachine object.
		vmObject := ec2InstanceToInternalVirtualMachineObject(instance, vpcs, selector,
			accountNamespacedName, ec2Cfg.credentials.region)
		vmObjects[vmObject.Name] = vmObject
	}

	return vmObjects
}

func (ec2Cfg *ec2ServiceConfig) GetInventoryStats() *internal.CloudServiceStats {
	return ec2Cfg.inventoryStats
}

func (ec2Cfg *ec2ServiceConfig) ResetInventoryCache() {
	ec2Cfg.resourcesCache.UpdateSnapshot(nil)
	ec2Cfg.inventoryStats.ResetInventoryPollStats()
}

func (ec2Cfg *ec2ServiceConfig) UpdateServiceConfig(newConfig internal.CloudServiceInterface) error {
	newEc2ServiceConfig := newConfig.(*ec2ServiceConfig)
	ec2Cfg.apiClient = newEc2ServiceConfig.apiClient
	ec2Cfg.credentials = newEc2ServiceConfig.credentials
	return nil
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcNameToID(vpcs []*ec2.Vpc) map[string]string {
	vpcNameToID := make(map[string]string)
	for _, vpc := range vpcs {
		if len(vpc.Tags) == 0 {
			awsPluginLogger().V(4).Info("Vpc name not found", "account", ec2Cfg.accountNamespacedName, "vpc", vpc)
			continue
		}
		var vpcName string
		for _, tag := range vpc.Tags {
			if *(tag.Key) == "Name" {
				vpcName = *(tag.Value)
				break
			}
		}
		vpcNameToID[vpcName] = *vpc.VpcId
	}
	return vpcNameToID
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcPeers() (map[string][]string, error) {
	vpcPeers := make(map[string][]string)
	result, err := ec2Cfg.apiClient.describeVpcPeeringConnectionsWrapper(nil)
	if err != nil {
		awsPluginLogger().V(0).Info("Failed to get peering connections", "error", err)
		return nil, err
	}
	for _, peerConn := range result.VpcPeeringConnections {
		accepterID, requesterID := *peerConn.AccepterVpcInfo.VpcId, *peerConn.RequesterVpcInfo.VpcId
		vpcPeers[accepterID] = append(vpcPeers[accepterID], requesterID)
		vpcPeers[requesterID] = append(vpcPeers[requesterID], accepterID)
	}
	return vpcPeers, nil
}

// getVpcs invokes cloud API to fetch the list of vpcs.
func (ec2Cfg *ec2ServiceConfig) getVpcs() ([]*ec2.Vpc, error) {
	result, err := ec2Cfg.apiClient.describeVpcsWrapper(nil)
	if err != nil {
		return nil, err
	}
	return result.Vpcs, nil
}

// GetVpcInventory generates vpc object for the vpcs stored in snapshot(in cloud format) and return a map of vpc runtime objects.
func (ec2Cfg *ec2ServiceConfig) GetVpcInventory() map[string]*runtimev1alpha1.Vpc {
	vpcs := ec2Cfg.GetCachedVpcs()
	vpcIDs := ec2Cfg.getManagedVpcIDs()
	// Convert to kubernetes object and return a map indexed using VPC ID.
	vpcMap := map[string]*runtimev1alpha1.Vpc{}
	for _, vpc := range vpcs {
		managed := false
		if _, ok := vpcIDs[*vpc.VpcId]; ok {
			managed = true
		}
		vpcObj := ec2VpcToInternalVpcObject(vpc, ec2Cfg.accountNamespacedName.Namespace, ec2Cfg.accountNamespacedName.Name,
			strings.ToLower(ec2Cfg.credentials.region), managed)
		vpcMap[strings.ToLower(*vpc.VpcId)] = vpcObj
	}
	return vpcMap
}

func (ec2Cfg *ec2ServiceConfig) getAccountLevelSnapshot() *accountLevelCacheSnapshot {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}
	return snapshot.(*cacheSnapshot).vpcSnapshot
}

func (ec2Cfg *ec2ServiceConfig) getSelectorLevelSnapshot() map[string]*selectorLevelCacheSnapshot {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Account cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return nil
	}
	return snapshot.(*cacheSnapshot).vmSnapshot
}
