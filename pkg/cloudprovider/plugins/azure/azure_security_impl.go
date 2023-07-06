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

package azure

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
)

// CreateSecurityGroup invokes cloud api and creates the cloud security group based on securityGroupIdentifier.
// For addressGroup it will attempt to create an asg, for appliedTo groups it will attempt to create both nsg and asg.
func (c *azureCloud) CreateSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) (*string, error) {
	var cloudSecurityGroupID string
	// find account managing the vnet
	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		azurePluginLogger().Info("Azure account not found managing virtual network", vnetID, "vnetID")
		return nil, fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(securityGroupIdentifier.Vpc)
	if err != nil {
		return nil, err
	}

	// create/get nsg/asg on/from cloud
	computeService := accCfg.GetServiceConfig().(*computeServiceConfig)
	location := computeService.credentials.region

	if !membershipOnly {
		// per vnet only one appliedTo SG will be created. Hence, always use the same pre-assigned name.
		tokens := strings.Split(securityGroupIdentifier.Vpc, "/")
		vnetName := tokens[len(tokens)-1]
		cloudNsgName := getPerVnetDefaultNsgName(vnetName)
		cloudSecurityGroupID, err = createOrGetNetworkSecurityGroup(computeService.nsgAPIClient, location, rgName, cloudNsgName)
		if err != nil {
			return nil, fmt.Errorf("azure per vnet nsg %v create failed for AT sg %v, reason: %w", cloudNsgName, securityGroupIdentifier.Name, err)
		}

		// create azure asg corresponding to AT sg.
		cloudAsgName := securityGroupIdentifier.GetCloudName(false)
		_, err = createOrGetApplicationSecurityGroup(computeService.asgAPIClient, location, rgName, cloudAsgName)
		if err != nil {
			return nil, fmt.Errorf("azure asg %v create failed for AT sg %v, reason: %w", cloudAsgName, securityGroupIdentifier.Name, err)
		}
	} else {
		// create azure asg corresponding to AG sg.
		cloudAsgName := securityGroupIdentifier.GetCloudName(true)
		cloudSecurityGroupID, err = createOrGetApplicationSecurityGroup(computeService.asgAPIClient, location, rgName, cloudAsgName)
		if err != nil {
			return nil, fmt.Errorf("azure asg %v create failed for AG sg %v, reason: %w", cloudAsgName, securityGroupIdentifier.Name, err)
		}
	}

	return to.StringPtr(cloudSecurityGroupID), nil
}

// UpdateSecurityGroupRules invokes cloud api and updates cloud security group with allRules.
func (c *azureCloud) UpdateSecurityGroupRules(appliedToGroupIdentifier *cloudresource.CloudResource,
	addRules, rmRules []*cloudresource.CloudRule) error {
	// find account managing the vnet and get compute service config
	vnetID := appliedToGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&appliedToGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	computeService := accCfg.GetServiceConfig().(*computeServiceConfig)
	location := computeService.credentials.region

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(appliedToGroupIdentifier.Vpc)
	if err != nil {
		azurePluginLogger().Error(err, "fail to build extract resource-group-name from vnet ID")
		return err
	}

	vnetPeerPairs := computeService.getVnetPeers(vnetID)
	vnetCachedIDs := computeService.getManagedVnetIds()
	vnetVMs := computeService.getAllCachedVirtualMachines()
	// ruleIP := vnetVMs[len(vnetVMs)-1].NetworkInterfaces[0].PrivateIps[0]
	// AT sg name per vnet is fixed and predefined. Get azure nsg name for it.
	tokens := strings.Split(appliedToGroupIdentifier.Vpc, "/")
	vnetName := tokens[len(tokens)-1]
	appliedToGroupPerVnetNsgName := getPerVnetDefaultNsgName(vnetName)
	// convert to azure security rules and build effective rules to be applied to AT sg azure NSG
	var rules []*armnetwork.SecurityRule
	flag := 0
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]

		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			var ruleIP *string
			for _, vnetVM := range vnetVMs {
				azurePluginLogger().Info("Accessing VM network interfaces", "VM", vnetVM.Name)
				if *vnetVM.VnetID == vnetID {
					ruleIP = vnetVM.NetworkInterfaces[0].PrivateIps[0]
				}
				flag = 1
				break
			}
			rules, err = computeService.buildEffectivePeerNSGSecurityRulesToApply(&appliedToGroupIdentifier.CloudResourceID, addRules,
				rmRules, appliedToGroupPerVnetNsgName, rgName, ruleIP)
			if err != nil {
				azurePluginLogger().Error(err, "fail to build effective rules to be applied")
				return err
			}
			break
		}
	}
	if flag == 0 {
		rules, err = computeService.buildEffectiveNSGSecurityRulesToApply(&appliedToGroupIdentifier.CloudResourceID, addRules,
			rmRules, appliedToGroupPerVnetNsgName, rgName)
		if err != nil {
			azurePluginLogger().Error(err, "fail to build effective rules to be applied")
			return err
		}
	}
	// update network security group with rules
	return updateNetworkSecurityGroupRules(computeService.nsgAPIClient, location, rgName, appliedToGroupPerVnetNsgName, rules)
}

// UpdateSecurityGroupMembers invokes cloud api and attaches/detaches nics to/from the cloud security group.
func (c *azureCloud) UpdateSecurityGroupMembers(securityGroupIdentifier *cloudresource.CloudResource,
	computeResourceIdentifier []*cloudresource.CloudResource, membershipOnly bool) error {
	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	computeService := accCfg.GetServiceConfig().(*computeServiceConfig)
	return computeService.updateSecurityGroupMembers(&securityGroupIdentifier.CloudResourceID, computeResourceIdentifier, membershipOnly)
}

// DeleteSecurityGroup invokes cloud api and deletes the cloud application security group.
func (c *azureCloud) DeleteSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) error {
	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	computeService := accCfg.GetServiceConfig().(*computeServiceConfig)
	location := computeService.credentials.region

	_ = computeService.updateSecurityGroupMembers(&securityGroupIdentifier.CloudResourceID, nil, membershipOnly)

	var rgName string
	_, rgName, _, err := extractFieldsFromAzureResourceID(securityGroupIdentifier.Vpc)
	if err != nil {
		return err
	}
	// remove attached rules for appliedTo sg but not address sg. This behavior is consistent with AWS.
	if !membershipOnly {
		err = computeService.removeReferencesToSecurityGroup(&securityGroupIdentifier.CloudResourceID, rgName, location, membershipOnly)
		if err != nil {
			return err
		}
	}

	var cloudAsgName string
	if isPeer := computeService.ifPeerProcessing(vnetID); isPeer {
		cloudAsgName = securityGroupIdentifier.GetCloudName(false)
	} else {
		cloudAsgName = securityGroupIdentifier.GetCloudName(membershipOnly)
	}
	err = computeService.asgAPIClient.delete(context.Background(), rgName, cloudAsgName)

	return err
}

func (c *azureCloud) GetEnforcedSecurity() []cloudresource.SynchronizationContent {
	var accNamespacedNames []types.NamespacedName
	accountConfigs := c.cloudCommon.GetCloudAccounts()
	for _, accCfg := range accountConfigs {
		accNamespacedNames = append(accNamespacedNames, *accCfg.GetNamespacedName())
	}

	var enforcedSecurityCloudView []cloudresource.SynchronizationContent
	var wg sync.WaitGroup
	ch := make(chan []cloudresource.SynchronizationContent)
	wg.Add(len(accNamespacedNames))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for _, accNamespacedName := range accNamespacedNames {
		accNamespacedNameCopy := &types.NamespacedName{
			Namespace: accNamespacedName.Namespace,
			Name:      accNamespacedName.Name,
		}

		go func(name *types.NamespacedName, sendCh chan<- []cloudresource.SynchronizationContent) {
			defer wg.Done()

			accCfg, found := c.cloudCommon.GetCloudAccountByName(name)
			if !found {
				azurePluginLogger().Info("Enforced-security-cloud-view GET for account skipped (account no longer exists)", "account", name)
				return
			}

			computeService := accCfg.GetServiceConfig().(*computeServiceConfig)
			if err := computeService.waitForInventoryInit(internal.InventoryInitWaitDuration); err != nil {
				azurePluginLogger().Error(err, "enforced-security-cloud-view GET for account skipped", "account", accCfg.GetNamespacedName())
				return
			}
			sendCh <- computeService.getNepheControllerManagedSecurityGroupsCloudView()
		}(accNamespacedNameCopy, ch)
	}

	for val := range ch {
		if val != nil {
			enforcedSecurityCloudView = append(enforcedSecurityCloudView, val...)
		}
	}
	return enforcedSecurityCloudView
}

func (computeCfg *computeServiceConfig) getNepheControllerManagedSecurityGroupsCloudView() []cloudresource.SynchronizationContent {
	vnetIDs := computeCfg.getManagedVnetIds()
	if len(vnetIDs) == 0 {
		return []cloudresource.SynchronizationContent{}
	}

	networkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(vnetIDs)
	if err != nil {
		return []cloudresource.SynchronizationContent{}
	}

	appliedToSgEnforcedView, err := computeCfg.processAndBuildATSgView(networkInterfaces)
	if err != nil {
		return []cloudresource.SynchronizationContent{}
	}

	addressGroupSgEnforcedView, err := computeCfg.processAndBuildAGSgView(networkInterfaces)
	if err != nil {
		return []cloudresource.SynchronizationContent{}
	}

	var enforcedSecurityCloudView []cloudresource.SynchronizationContent
	enforcedSecurityCloudView = append(enforcedSecurityCloudView, appliedToSgEnforcedView...)
	enforcedSecurityCloudView = append(enforcedSecurityCloudView, addressGroupSgEnforcedView...)

	return enforcedSecurityCloudView
}

func (computeCfg *computeServiceConfig) ifPeerProcessing(vnetID string) bool {
	vnetPeerPairs := computeCfg.getVnetPeers(vnetID)
	vnetCachedIDs := computeCfg.getManagedVnetIds()
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]
		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			return true
		}
	}
	return false
}
