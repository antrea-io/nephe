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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/mohae/deepcopy"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	nephetypes "antrea.io/nephe/pkg/types"
)

type ec2ServiceConfig struct {
	accountNamespacedName types.NamespacedName
	apiClient             awsEC2Wrapper
	resourcesCache        *internal.CloudServiceResourcesCache
	inventoryStats        *internal.CloudServiceStats
	instanceFilters       map[types.NamespacedName][][]*ec2.Filter
	// selectors required for updating resource filters on account config update.
	selectors map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
}

// ec2ResourcesCacheSnapshot holds the results from querying for all instances.
type ec2ResourcesCacheSnapshot struct {
	vms           map[types.NamespacedName][]*ec2.Instance
	vpcs          []*ec2.Vpc
	managedVpcIds map[string]struct{}
	vpcNameToId   map[string]string
	vpcPeers      map[string][]string
	sgs           map[types.NamespacedName][]*ec2.SecurityGroup
}

// newEC2ServiceConfig creates an ec2 service configs for each region. It returns a region to service config map for successful creations
// and multierr for any errors.
func newEC2ServiceConfig(accountNamespacedName types.NamespacedName, services map[string]awsServiceClientCreateInterface) (
	map[string]internal.CloudServiceInterface, error) {
	var retErr error
	configs := make(map[string]internal.CloudServiceInterface)
	for region, service := range services {
		// create ec2 sdk api client.
		apiClient, err := service.compute()
		if err != nil {
			err = fmt.Errorf("error creating ec2 sdk api client for account: %s, region: %s, err: %v", accountNamespacedName.String(),
				region, err)
			retErr = multierr.Append(retErr, err)
			continue
		}

		config := &ec2ServiceConfig{
			apiClient:             apiClient,
			accountNamespacedName: accountNamespacedName,
			resourcesCache:        &internal.CloudServiceResourcesCache{},
			inventoryStats:        &internal.CloudServiceStats{},
			instanceFilters:       make(map[types.NamespacedName][][]*ec2.Filter),
			selectors:             make(map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector),
		}

		vmSnapshot := make(map[types.NamespacedName][]*ec2.Instance)
		config.resourcesCache.UpdateSnapshot(&ec2ResourcesCacheSnapshot{vmSnapshot, nil, nil, nil, nil, nil})
		configs[region] = config
	}
	return configs, retErr
}

// compute returns AWS Compute (ec2) SDK apiClient.
func (p *awsServiceSdkConfigProvider) compute() (awsEC2Wrapper, error) {
	ec2Client := ec2.New(p.session)

	awsEC2 := &awsEC2WrapperImpl{
		ec2: ec2Client,
	}

	return awsEC2, nil
}

// getCachedInstances returns instances from the cache applicable for the given selector.
func (ec2Cfg *ec2ServiceConfig) getCachedInstances(selector *types.NamespacedName) []*ec2.Instance {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil",
			"type", providerType, "account", ec2Cfg.accountNamespacedName)
		return []*ec2.Instance{}
	}
	instances, found := snapshot.(*ec2ResourcesCacheSnapshot).vms[*selector]
	if !found {
		awsPluginLogger().V(4).Info("Vm cache snapshot not found",
			"account", ec2Cfg.accountNamespacedName, "selector", selector)
		return []*ec2.Instance{}
	}
	instancesToReturn := make([]*ec2.Instance, 0, len(instances))
	instancesToReturn = append(instancesToReturn, instances...)
	return instancesToReturn
}

// getManagedVpcIds returns vpcIDs of vpcs containing managed vms from cache.
func (ec2Cfg *ec2ServiceConfig) getManagedVpcIds() map[string]struct{} {
	vpcIdsCopy := make(map[string]struct{})
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil", "type", providerType, "account", ec2Cfg.accountNamespacedName)
		return vpcIdsCopy
	}
	vpcIdsSet := snapshot.(*ec2ResourcesCacheSnapshot).managedVpcIds

	for vpcId := range vpcIdsSet {
		vpcIdsCopy[vpcId] = struct{}{}
	}

	return vpcIdsCopy
}

// getCachedVpcsMap returns vpcs from cache in map format.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcsMap() map[string]*ec2.Vpc {
	vpcCopy := make(map[string]*ec2.Vpc)
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil", "type", providerType, "account", ec2Cfg.accountNamespacedName)
		return vpcCopy
	}

	for _, vpc := range snapshot.(*ec2ResourcesCacheSnapshot).vpcs {
		vpcCopy[strings.ToLower(*vpc.VpcId)] = vpc
	}

	return vpcCopy
}

// getCachedVpcNameToId returns the map vpcNameToId from the cache.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcNameToId() map[string]string {
	vpcNameToIdCopy := make(map[string]string)
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil", "type", providerType, "account", ec2Cfg.accountNamespacedName)
		return vpcNameToIdCopy
	}
	vpcNameToId := snapshot.(*ec2ResourcesCacheSnapshot).vpcNameToId

	for k, v := range vpcNameToId {
		vpcNameToIdCopy[k] = v
	}

	return vpcNameToIdCopy
}

// GetCachedVpcs returns VPCs from cached snapshot for the account.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcs() []*ec2.Vpc {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil", "type", providerType, "account", ec2Cfg.accountNamespacedName)
		return []*ec2.Vpc{}
	}
	vpcs := snapshot.(*ec2ResourcesCacheSnapshot).vpcs
	vpcsToReturn := make([]*ec2.Vpc, 0, len(vpcs))
	vpcsToReturn = append(vpcsToReturn, vpcs...)

	return vpcsToReturn
}

// GetCachedSGs returns security groups from cached snapshot for the account.
func (ec2Cfg *ec2ServiceConfig) GetCachedSGs(selector *types.NamespacedName) []*ec2.SecurityGroup {
	sgsToReturn := make([]*ec2.SecurityGroup, 0)
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().Info("Cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return sgsToReturn
	}
	sgs, found := snapshot.(*ec2ResourcesCacheSnapshot).sgs[*selector]
	if !found {
		awsPluginLogger().Info("Security Group Cache snapshot nil", "account", ec2Cfg.accountNamespacedName)
		return sgsToReturn
	}
	sgsToReturn = append(sgsToReturn, sgs...)
	return sgsToReturn
}

// getVpcPeers returns all the peers of a vpc.
func (ec2Cfg *ec2ServiceConfig) getVpcPeers(vpcId string) []string {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("Cache snapshot nil", "type", providerType, "account", ec2Cfg.accountNamespacedName)
		return nil
	}
	vpcPeersCopy := make([]string, 0)
	if peers, ok := snapshot.(*ec2ResourcesCacheSnapshot).vpcPeers[vpcId]; ok {
		vpcPeersCopy = deepcopy.Copy(peers).([]string)
	}
	return vpcPeersCopy
}

// getInstances gets instances from cloud matching the given selector configuration.
func (ec2Cfg *ec2ServiceConfig) getInstances(namespacedName *types.NamespacedName) ([]*ec2.Instance, error) {
	var instances []*ec2.Instance
	filters := ec2Cfg.instanceFilters[*namespacedName]
	for _, filter := range filters {
		if len(filter) > 0 {
			if *filter[0].Name == awsCustomFilterKeyVPCName {
				filter = buildFilterForVPCIDFromFilterForVPCName(filter, ec2Cfg.getCachedVpcNameToId())
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
	return instances, nil
}

// DoResourceInventory gets inventory from cloud for a given ec2 service config in the cloud account.
func (ec2Cfg *ec2ServiceConfig) DoResourceInventory() error {
	region := ec2Cfg.apiClient.getRegion()
	vpcs, err := ec2Cfg.getVpcs()
	if err != nil {
		awsPluginLogger().Error(err, "failed to fetch cloud resources", "account", ec2Cfg.accountNamespacedName,
			"region", region)
		return err
	}
	awsPluginLogger().V(1).Info("Vpcs from cloud", "account", ec2Cfg.accountNamespacedName, "region", region,
		"vpcs", len(vpcs))
	vpcNameToId := ec2Cfg.buildMapVpcNameToId(vpcs)
	vpcPeers, _ := ec2Cfg.buildMapVpcPeers()
	allInstances := make(map[types.NamespacedName][]*ec2.Instance)
	allSgs := make(map[types.NamespacedName][]*ec2.SecurityGroup)

	// Call cloud APIs for the configured CloudEntitySelectors CRs.
	if len(ec2Cfg.selectors) == 0 {
		awsPluginLogger().V(1).Info("Fetching vm resources from cloud skipped", "account", ec2Cfg.accountNamespacedName,
			"region", region, "resource-filters", "not-configured")
		ec2Cfg.resourcesCache.UpdateSnapshot(&ec2ResourcesCacheSnapshot{allInstances, vpcs, nil, vpcNameToId, vpcPeers, allSgs})
		return nil
	}

	allManagedVpcIds := make(map[string]struct{})
	for namespacedName := range ec2Cfg.selectors {
		managedVpcIds := make(map[string]struct{})
		instances, err := ec2Cfg.getInstances(&namespacedName)
		if err != nil {
			awsPluginLogger().Error(err, "failed to fetch cloud resources", "account", ec2Cfg.accountNamespacedName,
				"region", region)
			return err
		}
		awsPluginLogger().V(1).Info("Vm instances from cloud", "account", ec2Cfg.accountNamespacedName,
			"selector", namespacedName, "region", region, "instances", len(instances))
		for _, instance := range instances {
			allManagedVpcIds[strings.ToLower(*instance.VpcId)] = struct{}{}
			managedVpcIds[strings.ToLower(*instance.VpcId)] = struct{}{}
		}
		allInstances[namespacedName] = instances

		// get security groups for managed vpcs.
		if cloudresource.IsCloudSecurityGroupVisibilityEnabled() {
			if len(managedVpcIds) > 0 {
				sgs, err := ec2Cfg.getSecurityGroupsOfVpc(managedVpcIds)
				if err != nil {
					awsPluginLogger().Error(err, "failed to fetch cloud resources", "account", ec2Cfg.accountNamespacedName)
					return err
				}
				allSgs[namespacedName] = sgs
			}
		}
	}
	ec2Cfg.resourcesCache.UpdateSnapshot(&ec2ResourcesCacheSnapshot{allInstances, vpcs, allManagedVpcIds, vpcNameToId, vpcPeers, allSgs})

	return nil
}

// AddResourceFilters add/updates instances resource filter for the service.
func (ec2Cfg *ec2ServiceConfig) AddResourceFilters(selector *crdv1alpha1.CloudEntitySelector) error {
	namespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
	if filters, ok := convertSelectorToEC2InstanceFilters(selector); ok {
		ec2Cfg.instanceFilters[namespacedName] = filters
		ec2Cfg.selectors[namespacedName] = selector.DeepCopy()
	} else {
		return fmt.Errorf("error creating resource query filters")
	}
	return nil
}

// GetResourceFilters gets all instances resource filters for the service.
func (ec2Cfg *ec2ServiceConfig) GetResourceFilters() map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector {
	return ec2Cfg.selectors
}

func (ec2Cfg *ec2ServiceConfig) RemoveResourceFilters(namespacedName *types.NamespacedName) {
	delete(ec2Cfg.instanceFilters, *namespacedName)
	delete(ec2Cfg.selectors, *namespacedName)
}

// getVirtualMachineObjects converts cached virtual machines in cloud format to internal runtimev1alpha1.VirtualMachine format.
func (ec2Cfg *ec2ServiceConfig) getVirtualMachineObjects(selector *types.NamespacedName) map[string]*runtimev1alpha1.VirtualMachine {
	instances := ec2Cfg.getCachedInstances(selector)
	vpcs := ec2Cfg.getCachedVpcsMap()

	vmObjects := map[string]*runtimev1alpha1.VirtualMachine{}
	for _, instance := range instances {
		// build runtime.v1alpha1.VirtualMachine object.
		vmObject := ec2InstanceToInternalVirtualMachineObject(instance, vpcs, selector, &ec2Cfg.accountNamespacedName,
			ec2Cfg.apiClient.getRegion())
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

// UpdateServiceConfig updates the service config according to the new service config.
func (ec2Cfg *ec2ServiceConfig) UpdateServiceConfig(newConfig internal.CloudServiceInterface) error {
	newEc2ServiceConfig := newConfig.(*ec2ServiceConfig)
	ec2Cfg.apiClient = newEc2ServiceConfig.apiClient
	return nil
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcNameToId(vpcs []*ec2.Vpc) map[string]string {
	vpcNameToId := make(map[string]string)
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
		vpcNameToId[vpcName] = *vpc.VpcId
	}
	return vpcNameToId
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcPeers() (map[string][]string, error) {
	vpcPeers := make(map[string][]string)
	result, err := ec2Cfg.apiClient.describeVpcPeeringConnectionsWrapper(nil)
	if err != nil {
		awsPluginLogger().V(0).Info("Failed to get peering connections", "error", err)
		return nil, err
	}
	for _, peerConn := range result.VpcPeeringConnections {
		accepterId, requesterId := *peerConn.AccepterVpcInfo.VpcId, *peerConn.RequesterVpcInfo.VpcId
		vpcPeers[accepterId] = append(vpcPeers[accepterId], requesterId)
		vpcPeers[requesterId] = append(vpcPeers[requesterId], accepterId)
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

// getVpcObjects generates vpc object for the vpcs stored in snapshot(in cloud format) and return a map of vpc runtime objects.
func (ec2Cfg *ec2ServiceConfig) getVpcObjects() map[string]*runtimev1alpha1.Vpc {
	vpcs := ec2Cfg.getCachedVpcs()
	managedVpcIds := ec2Cfg.getManagedVpcIds()
	// Convert to kubernetes object and return a map indexed using VPC ID.
	vpcMap := map[string]*runtimev1alpha1.Vpc{}
	for _, vpc := range vpcs {
		managed := false
		if _, ok := managedVpcIds[*vpc.VpcId]; ok {
			managed = true
		}
		vpcObj := ec2VpcToInternalVpcObject(vpc, ec2Cfg.accountNamespacedName.Namespace, ec2Cfg.accountNamespacedName.Name,
			strings.ToLower(ec2Cfg.apiClient.getRegion()), managed)
		vpcMap[strings.ToLower(*vpc.VpcId)] = vpcObj
	}
	return vpcMap
}

// getSecurityGroupObjects generates security group object for the sgs stored in snapshot and return a map of sg runtime objects.
func (ec2Cfg *ec2ServiceConfig) getSecurityGroupObjects(
	selectorNamespacedName *types.NamespacedName) map[string]*runtimev1alpha1.SecurityGroup {
	sgs := ec2Cfg.GetCachedSGs(selectorNamespacedName)
	sgMap := map[string]*runtimev1alpha1.SecurityGroup{}
	for _, sg := range sgs {
		sgObj := ec2SgToInternalSgObject(sg, selectorNamespacedName, &ec2Cfg.accountNamespacedName, strings.ToLower(ec2Cfg.apiClient.getRegion()))
		sgMap[strings.ToLower(*sg.GroupId)] = sgObj
	}
	return sgMap
}

// GetCloudInventory fetches VM and VPC inventory from stored snapshot and converts from cloud format to internal format.
func (ec2Cfg *ec2ServiceConfig) GetCloudInventory() *nephetypes.CloudInventory {
	cloudInventory := nephetypes.CloudInventory{
		VmMap:  map[types.NamespacedName]map[string]*runtimev1alpha1.VirtualMachine{},
		VpcMap: map[string]*runtimev1alpha1.Vpc{},
		SgMap:  map[types.NamespacedName]map[string]*runtimev1alpha1.SecurityGroup{},
	}
	cloudInventory.VpcMap = ec2Cfg.getVpcObjects()
	for namespacedName := range ec2Cfg.selectors {
		cloudInventory.VmMap[namespacedName] = ec2Cfg.getVirtualMachineObjects(&namespacedName)
		cloudInventory.SgMap[namespacedName] = ec2Cfg.getSecurityGroupObjects(&namespacedName)
	}

	return &cloudInventory
}
