// Copyright 2023 Antrea Authors.
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
	"sync"

	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
)

// CreateSecurityGroup invokes cloud api and creates the cloud security group based on securityGroupIdentifier.
func (c *awsCloud) CreateSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) (*string, error) {
	vpcID := securityGroupIdentifier.Vpc
	accCfg, err := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if err != nil {
		return nil, fmt.Errorf("%v, virtual private cloud: %v", err, vpcID)
	}

	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	cloudSgName := securityGroupIdentifier.GetCloudName(membershipOnly)
	region := accCfg.FindRegion(vpcID)
	if region == "" {
		return nil, fmt.Errorf("region not found for vpc %s", vpcID)
	}
	ec2Service := accCfg.GetServiceConfig(region).(*ec2ServiceConfig)
	resp, err := ec2Service.createOrGetSecurityGroups(securityGroupIdentifier.Vpc, map[string]struct{}{cloudSgName: {}})
	if err != nil {
		return nil, err
	}
	securityGroupObj := resp[cloudSgName]

	return securityGroupObj.GroupId, nil
}

// UpdateSecurityGroupRules invokes cloud api and updates cloud security group with addRules and rmRules.
func (c *awsCloud) UpdateSecurityGroupRules(appliedToGroupIdentifier *cloudresource.CloudResource,
	addRules, rmRules []*cloudresource.CloudRule) error {
	addIRule, addERule := utils.SplitCloudRulesByDirection(addRules)
	rmIRule, rmERule := utils.SplitCloudRulesByDirection(rmRules)

	vpcID := appliedToGroupIdentifier.Vpc
	accCfg, err := c.cloudCommon.GetCloudAccountByAccountId(&appliedToGroupIdentifier.AccountID)
	if err != nil {
		return fmt.Errorf("%v, virtual private cloud: %v", err, vpcID)
	}

	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	// build from addressGroups, cloudSgNames from rules
	cloudSgNames := buildEc2CloudSgNamesFromRules(&appliedToGroupIdentifier.CloudResourceID, append(addIRule, rmIRule...),
		append(addERule, rmERule...))

	// make sure all required security groups pre-exist
	region := accCfg.FindRegion(vpcID)
	if region == "" {
		return fmt.Errorf("region not found for vpc %s", vpcID)
	}
	ec2Service := accCfg.GetServiceConfig(region).(*ec2ServiceConfig)
	vpcIDs := []string{vpcID}
	vpcPeerIDs := ec2Service.getVpcPeers(vpcID)
	vpcIDs = append(vpcIDs, vpcPeerIDs...)
	cloudSGNameToCloudSGObj, err := ec2Service.getCloudSecurityGroupsWithNameFromCloud(vpcIDs, cloudSgNames)
	if err != nil {
		return err
	}
	if len(cloudSGNameToCloudSGObj) != len(cloudSgNames) {
		return fmt.Errorf("failed to find security groups")
	}

	cloudSGObjToAddRules := cloudSGNameToCloudSGObj[appliedToGroupIdentifier.GetCloudName(false)]
	cloudSGObjToAddRules.IpPermissions = normalizeIpPermissions(cloudSGObjToAddRules.IpPermissions)
	cloudSGObjToAddRules.IpPermissionsEgress = normalizeIpPermissions(cloudSGObjToAddRules.IpPermissionsEgress)

	addIngressRules, err := convertIngressToIpPermission(addIRule, cloudSGNameToCloudSGObj)
	if err != nil {
		return err
	}
	removeIngressRules, err := convertIngressToIpPermission(rmIRule, cloudSGNameToCloudSGObj)
	if err != nil {
		return err
	}
	addEgressRules, err := convertEgressToIpPermission(addERule, cloudSGNameToCloudSGObj)
	if err != nil {
		return err
	}
	removeEgressRules, err := convertEgressToIpPermission(rmERule, cloudSGNameToCloudSGObj)
	if err != nil {
		return err
	}

	addIngressRules = dedupIpPermissions(addIngressRules, cloudSGObjToAddRules.IpPermissions)
	addEgressRules = dedupIpPermissions(addEgressRules, cloudSGObjToAddRules.IpPermissionsEgress)

	// rollback operation for cloud api failures
	rollbackRmIngress := false
	rollbackAddIngress := false
	rollbackRmEgress := false
	defer func() {
		if rollbackRmIngress {
			_ = ec2Service.realizeIngressIPPermissions(cloudSGObjToAddRules, removeIngressRules, false)
		}
		if rollbackAddIngress {
			_ = ec2Service.realizeIngressIPPermissions(cloudSGObjToAddRules, addIngressRules, true)
		}
		if rollbackRmEgress {
			_ = ec2Service.realizeEgressIPPermissions(cloudSGObjToAddRules, removeEgressRules, false)
		}
	}()

	// realize security group ingress and egress permissions
	if err = ec2Service.realizeIngressIPPermissions(cloudSGObjToAddRules, removeIngressRules, true); err != nil {
		return err
	}
	if err = ec2Service.realizeIngressIPPermissions(cloudSGObjToAddRules, addIngressRules, false); err != nil {
		rollbackRmIngress = true
		return err
	}
	if err = ec2Service.realizeEgressIPPermissions(cloudSGObjToAddRules, removeEgressRules, true); err != nil {
		rollbackRmIngress = true
		rollbackAddIngress = true
		return err
	}
	if err = ec2Service.realizeEgressIPPermissions(cloudSGObjToAddRules, addEgressRules, false); err != nil {
		rollbackRmIngress = true
		rollbackAddIngress = true
		rollbackRmEgress = true
		return err
	}

	return nil
}

// UpdateSecurityGroupMembers invokes cloud api and attaches/detaches nics to/from the cloud security group.
func (c *awsCloud) UpdateSecurityGroupMembers(securityGroupIdentifier *cloudresource.CloudResource,
	cloudResourceIdentifiers []*cloudresource.CloudResource, membershipOnly bool) error {
	vpcID := securityGroupIdentifier.Vpc
	accCfg, err := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if err != nil {
		return fmt.Errorf("%v, virtual private cloud: %v", err, vpcID)
	}

	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	// get addressGroup cloudSgID
	region := accCfg.FindRegion(vpcID)
	if region == "" {
		return fmt.Errorf("region not found for vpc %s", vpcID)
	}
	cloudSgName := securityGroupIdentifier.GetCloudName(membershipOnly)
	ec2Service := accCfg.GetServiceConfig(region).(*ec2ServiceConfig)
	vpcIDs := []string{vpcID}
	cloudSgNames := map[string]struct{}{cloudSgName: {}}
	out, err := ec2Service.getCloudSecurityGroupsWithNameFromCloud(vpcIDs, cloudSgNames)
	if err != nil {
		return err
	}
	if len(out) != len(cloudSgNames) {
		return fmt.Errorf("failed to find cloud sg (%v) corresponding to address group (%v)",
			securityGroupIdentifier.Name, cloudSgName)
	}
	cloudSgID := out[cloudSgName].GroupId

	err = ec2Service.updateSecurityGroupMembers(cloudSgID, cloudSgName, vpcID, cloudResourceIdentifiers, membershipOnly)
	if err != nil {
		return err
	}

	return nil
}

// DeleteSecurityGroup invokes cloud api and deletes the cloud security group. Any attached resource will be moved to default sg.
func (c *awsCloud) DeleteSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) error {
	vpcID := securityGroupIdentifier.Vpc
	accCfg, err := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if err != nil {
		return fmt.Errorf("%v, virtual private cloud: %v", err, vpcID)
	}

	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	// check if sg exists in cloud and get its cloud sg id to delete
	vpcIDs := []string{vpcID}
	cloudSgNameToDelete := securityGroupIdentifier.GetCloudName(membershipOnly)
	region := accCfg.FindRegion(vpcID)
	if region == "" {
		return fmt.Errorf("region not found for vpc %s", vpcID)
	}
	ec2Service := accCfg.GetServiceConfig(region).(*ec2ServiceConfig)
	out, err := ec2Service.getCloudSecurityGroupsWithNameFromCloud(vpcIDs, map[string]struct{}{cloudSgNameToDelete: {}})
	if err != nil || len(out) == 0 {
		return err
	}

	// Detach security group from interfaces before deleting.
	cloudSgIDToDelete := out[cloudSgNameToDelete].GroupId
	err = ec2Service.updateSecurityGroupMembers(cloudSgIDToDelete, cloudSgNameToDelete, vpcID, nil, membershipOnly)
	if err != nil {
		return err
	}

	// delete security group
	input := &ec2.DeleteSecurityGroupInput{
		GroupId: cloudSgIDToDelete,
	}
	_, err = ec2Service.apiClient.deleteSecurityGroup(input)
	if err != nil {
		return err
	}

	return nil
}

// GetEnforcedSecurity gets the current security groups and security rules in the cloud.
func (c *awsCloud) GetEnforcedSecurity() []cloudresource.SynchronizationContent {
	var accNamespacedNames []types.NamespacedName
	accountConfigs := c.cloudCommon.GetCloudAccounts()
	for _, accCfg := range accountConfigs {
		accNamespacedNames = append(accNamespacedNames, *accCfg.GetNamespacedName())
	}

	var enforcedSecurityCloudView []cloudresource.SynchronizationContent
	// wait group to close the sync channel and finish the GetEnforcedSecurity call.
	var wg sync.WaitGroup
	ch := make(chan []cloudresource.SynchronizationContent)

	for _, accNamespacedName := range accNamespacedNames {
		accCfg, err := c.cloudCommon.GetCloudAccountByName(&accNamespacedName)
		if err != nil {
			awsPluginLogger().V(1).Info("Account is invalid")
			continue
		}
		accCfg.LockMutex()

		ec2Services := accCfg.GetAllServiceConfigs()

		// wait group to release account mutex after all service config operation finishes.
		var accWg sync.WaitGroup
		accWg.Add(len(ec2Services))
		wg.Add(len(ec2Services))
		go func() {
			accWg.Wait()
			accCfg.UnlockMutex()
		}()

		for _, s := range ec2Services {
			ec2Service := s.(*ec2ServiceConfig)

			go func(ec2Service *ec2ServiceConfig, sendCh chan<- []cloudresource.SynchronizationContent) {
				defer wg.Done()
				defer accWg.Done()

				sendCh <- ec2Service.getNepheControllerManagedSecurityGroupsCloudView()
			}(ec2Service, ch)
		}
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	for val := range ch {
		if val != nil {
			enforcedSecurityCloudView = append(enforcedSecurityCloudView, val...)
		}
	}
	return enforcedSecurityCloudView
}
