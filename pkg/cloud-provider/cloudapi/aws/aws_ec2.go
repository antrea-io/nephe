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

	"github.com/mohae/deepcopy"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cenkalti/backoff/v4"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

type ec2ServiceConfig struct {
	accountName    string
	apiClient      awsEC2Wrapper
	resourcesCache *internal.CloudServiceResourcesCache
	inventoryStats *internal.CloudServiceStats
	// instanceFilters has following possible values
	// - empty map indicates no selectors configured for this account. NO cloud api call for inventory will be made.
	// - non-empty map indicates selectors are configured. Cloud api call for inventory will be made.
	//	 - key with nil value indicates no filters. Get all instances for account.
	//   - key with "some-filter-string" value indicates some filter. Get instances matching those filters only.
	instanceFilters map[string][][]*ec2.Filter
}

// ec2ResourcesCacheSnapshot holds the results from querying for all instances.
type ec2ResourcesCacheSnapshot struct {
	instances   map[cloudcommon.InstanceID]*ec2.Instance
	vpcIDs      map[string]struct{}
	vpcNameToID map[string]string
	vpcPeers    map[string][]string
}

func newEC2ServiceConfig(name string, service awsServiceClientCreateInterface) (internal.CloudServiceInterface, error) {
	// create ec2 sdk api client
	apiClient, err := service.compute()
	if err != nil {
		return nil, fmt.Errorf("error creating ec2 sdk api client for account : %v, err: %v", name, err)
	}

	config := &ec2ServiceConfig{
		apiClient:       apiClient,
		accountName:     name,
		resourcesCache:  &internal.CloudServiceResourcesCache{},
		inventoryStats:  &internal.CloudServiceStats{},
		instanceFilters: make(map[string][][]*ec2.Filter),
	}
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
			return fmt.Errorf("inventory for account %v not initialized (waited %v duration)", ec2Cfg.accountName, duration)
		}
		return nil
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration

	return backoff.Retry(operation, b)
}

// getInstanceResourceFilters returns filters to be applied to describeInstances api if filters are configured.
// Otherwise returns (nil, false). false indicates no selectors configured for the account and hence no cloud api needs
// to be made for instance inventory.
func (ec2Cfg *ec2ServiceConfig) getInstanceResourceFilters() ([][]*ec2.Filter, bool) {
	var allFilters [][]*ec2.Filter

	instanceFilters := ec2Cfg.instanceFilters
	if len(instanceFilters) == 0 {
		return nil, false
	}

	for _, filters := range ec2Cfg.instanceFilters {
		// if any selector found with nil filter, skip all other selectors. As nil indicates all
		if len(filters) == 0 {
			return nil, true
		}
		allFilters = append(allFilters, filters...)
	}
	return allFilters, true
}

// getCachedInstances returns instances from the cache for the account.
func (ec2Cfg *ec2ServiceConfig) getCachedInstances() []*ec2.Instance {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("cache snapshot nil", "service", awsComputeServiceNameEC2, "account", ec2Cfg.accountName)
		return []*ec2.Instance{}
	}
	instances := snapshot.(*ec2ResourcesCacheSnapshot).instances
	instancesToReturn := make([]*ec2.Instance, 0, len(instances))
	for _, instance := range instances {
		instancesToReturn = append(instancesToReturn, instance)
	}
	awsPluginLogger().V(1).Info("cached instances", "service", awsComputeServiceNameEC2, "account", ec2Cfg.accountName,
		"instances", len(instancesToReturn))
	return instancesToReturn
}

// getCachedVpcIDs returns vpcIDs from the cache for the account.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcIDs() map[string]struct{} {
	vpcIDsCopy := make(map[string]struct{})
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("cache snapshot nil", "service", awsComputeServiceNameEC2, "account", ec2Cfg.accountName)
		return vpcIDsCopy
	}
	vpcIDsSet := snapshot.(*ec2ResourcesCacheSnapshot).vpcIDs

	for vpcID := range vpcIDsSet {
		vpcIDsCopy[vpcID] = struct{}{}
	}

	return vpcIDsCopy
}

// getCachedVpcNameToID returns the map vpcNameToID from the cache.
func (ec2Cfg *ec2ServiceConfig) getCachedVpcNameToID() map[string]string {
	vpcNameToIDCopy := make(map[string]string)
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("ec2 service cache snapshot nil", "type", providerType, "account", ec2Cfg.accountName)
		return vpcNameToIDCopy
	}
	vpcNameToID := snapshot.(*ec2ResourcesCacheSnapshot).vpcNameToID

	for k, v := range vpcNameToID {
		vpcNameToIDCopy[k] = v
	}

	return vpcNameToIDCopy
}

// getVpcPeers returns all the peers of a vpc.
func (ec2Cfg *ec2ServiceConfig) getVpcPeers(vpcID string) []string {
	snapshot := ec2Cfg.resourcesCache.GetSnapshot()
	if snapshot == nil {
		awsPluginLogger().V(4).Info("ec2 service cache snapshot nil", "type", providerType, "account", ec2Cfg.accountName)
		return nil
	}
	vpcPeersCopy := make([]string, 0)
	if peers, ok := snapshot.(*ec2ResourcesCacheSnapshot).vpcPeers[vpcID]; ok {
		vpcPeersCopy = deepcopy.Copy(peers).([]string)
	}
	return vpcPeersCopy
}

// getInstances gets instance for the account from aws EC2 API.
func (ec2Cfg *ec2ServiceConfig) getInstances() ([]*ec2.Instance, error) {
	filters, _ := ec2Cfg.getInstanceResourceFilters()
	if filters == nil {
		var validInstanceStateFilters []*ec2.Filter
		validInstanceStateFilters = append(validInstanceStateFilters, buildEc2FilterForValidInstanceStates())
		request := &ec2.DescribeInstancesInput{Filters: validInstanceStateFilters}
		return ec2Cfg.apiClient.pagedDescribeInstancesWrapper(request)
	}

	var instances []*ec2.Instance
	for _, filter := range filters {
		if len(filter) > 0 {
			if *filter[0].Name == awsCustomFilterKeyVPCName {
				filter = buildFilterForVPCIDFromFilterForVPCName(filter, ec2Cfg.getCachedVpcNameToID())
			}
		}
		request := &ec2.DescribeInstancesInput{Filters: filter}
		filterInstances, e := ec2Cfg.apiClient.pagedDescribeInstancesWrapper(request)
		if e != nil {
			return nil, e
		}
		instances = append(instances, filterInstances...)
	}

	awsPluginLogger().V(1).Info("instances from cloud", "service", awsComputeServiceNameEC2, "account", ec2Cfg.accountName,
		"instances", len(instances))

	return instances, nil
}

// doInstancesInventoryWorker gets inventory from cloud for given cloud account.
func (ec2Cfg *ec2ServiceConfig) DoResourceInventory() error {
	instances, e := ec2Cfg.getInstances()
	if e != nil {
		awsPluginLogger().V(0).Info("error fetching ec2 instances", "account", ec2Cfg.accountName, "error", e)
	} else {
		exists := struct{}{}
		vpcIDs := make(map[string]struct{})
		instanceIDs := make(map[cloudcommon.InstanceID]*ec2.Instance)
		vpcNameToID, _ := ec2Cfg.buildMapVpcNameToID()
		vpcPeers, _ := ec2Cfg.buildMapVpcPeers()
		for _, instance := range instances {
			id := cloudcommon.InstanceID(strings.ToLower(aws.StringValue(instance.InstanceId)))
			instanceIDs[id] = instance
			vpcIDs[strings.ToLower(*instance.VpcId)] = exists
		}
		ec2Cfg.resourcesCache.UpdateSnapshot(&ec2ResourcesCacheSnapshot{instanceIDs, vpcIDs, vpcNameToID, vpcPeers})
	}
	ec2Cfg.inventoryStats.UpdateInventoryPollStats(e)

	return e
}

// setInstanceFilters add/updates instances resource filter for the service.
func (ec2Cfg *ec2ServiceConfig) SetResourceFilters(selector *v1alpha1.CloudEntitySelector) {
	if filters, found := convertSelectorToEC2InstanceFilters(selector); found {
		ec2Cfg.instanceFilters[selector.GetName()] = filters
	} else {
		if selector != nil {
			delete(ec2Cfg.instanceFilters, selector.GetName())
		}
		ec2Cfg.resourcesCache.UpdateSnapshot(nil)
	}
}

func (ec2Cfg *ec2ServiceConfig) GetResourceCRDs(namespace string) *internal.CloudServiceResourceCRDs {
	instances := ec2Cfg.getCachedInstances()
	vmCRDs := make([]*v1alpha1.VirtualMachine, 0, len(instances))
	for _, instance := range instances {
		// build VirtualMachine CRD
		vmCRD := ec2InstanceToVirtualMachineCRD(instance, namespace)
		vmCRDs = append(vmCRDs, vmCRD)
	}

	awsPluginLogger().V(1).Info("CRDs", "service", awsComputeServiceNameEC2, "account", ec2Cfg.accountName,
		"virtual-machine CRDs", len(vmCRDs))

	serviceResourceCRDs := &internal.CloudServiceResourceCRDs{}
	serviceResourceCRDs.SetComputeResourceCRDs(vmCRDs)

	return serviceResourceCRDs
}

func (ec2Cfg *ec2ServiceConfig) HasFiltersConfigured() (bool, bool) {
	filters, found := ec2Cfg.getInstanceResourceFilters()

	return found, filters == nil
}

func (ec2Cfg *ec2ServiceConfig) GetName() internal.CloudServiceName {
	return awsComputeServiceNameEC2
}

func (ec2Cfg *ec2ServiceConfig) GetType() internal.CloudServiceType {
	return internal.CloudServiceTypeCompute
}

func (ec2Cfg *ec2ServiceConfig) GetInventoryStats() *internal.CloudServiceStats {
	return ec2Cfg.inventoryStats
}

func (ec2Cfg *ec2ServiceConfig) ResetCachedState() {
	ec2Cfg.SetResourceFilters(nil)
	ec2Cfg.inventoryStats.ResetInventoryPollStats()
}

func (ec2Cfg *ec2ServiceConfig) UpdateServiceConfig(newConfig internal.CloudServiceInterface) {
	newEc2ServiceConfig := newConfig.(*ec2ServiceConfig)
	ec2Cfg.apiClient = newEc2ServiceConfig.apiClient
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcNameToID() (map[string]string, error) {
	vpcNameToID := make(map[string]string)
	result, err := ec2Cfg.apiClient.describeVpcsWrapper(nil)
	if err != nil {
		awsPluginLogger().V(0).Info("error describing vpcs", "error", err)
		return vpcNameToID, err
	}
	for _, vpc := range result.Vpcs {
		if len(vpc.Tags) == 0 {
			awsPluginLogger().V(4).Info("vpc name not found", "account", ec2Cfg.accountName, "vpc", vpc)
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
	return vpcNameToID, nil
}

func (ec2Cfg *ec2ServiceConfig) buildMapVpcPeers() (map[string][]string, error) {
	vpcPeers := make(map[string][]string)
	result, err := ec2Cfg.apiClient.describeVpcPeeringConnectionsWrapper(nil)
	if err != nil {
		awsPluginLogger().V(0).Info("error getting peering connections", "error", err)
		return nil, err
	}
	for _, peerConn := range result.VpcPeeringConnections {
		accepterID, requesterID := *peerConn.AccepterVpcInfo.VpcId, *peerConn.RequesterVpcInfo.VpcId
		vpcPeers[accepterID] = append(vpcPeers[accepterID], requesterID)
		vpcPeers[requesterID] = append(vpcPeers[requesterID], accepterID)
	}
	return vpcPeers, nil
}
