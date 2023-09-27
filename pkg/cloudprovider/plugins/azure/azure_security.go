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
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"go.uber.org/multierr"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
)

type networkInterfaceInternal struct {
	armnetwork.Interface
	vnetID string
}

// getPerVnetDefaultNsgName returns default NSG name for a given VNET.
func getPerVnetDefaultNsgName(vnetName string) string {
	return cloudresource.ControllerPrefix + "-at-default-" + vnetName
}

// getNetworkInterfacesOfVnet fetch network interfaces for a set of VNET-IDs.
func (computeCfg *computeServiceConfig) getNetworkInterfacesOfVnet(vnetIDSet map[string]struct{}) ([]*networkInterfaceInternal, error) {
	location := computeCfg.credentials.region
	subscriptionID := computeCfg.credentials.SubscriptionID
	tenentID := computeCfg.credentials.TenantID

	var vnetIDs []string
	for vnetID := range vnetIDSet {
		vnetIDs = append(vnetIDs, vnetID)
	}

	// TODO: Change the query to just retrieve interfaces of the VNET and data should just include interface name.
	query, err := getNwIntfsByVnetIDsAndSubscriptionIDsAndTenantIDsAndLocationsMatchQuery(vnetIDs, []string{subscriptionID},
		[]string{tenentID}, []string{location})
	if err != nil {
		return nil, err
	}
	// Required just for vnet-id to interface mapping.
	nwInterfacesFromRGQuery, _, err := getNetworkInterfaceTable(computeCfg.resourceGraphAPIClient, query, []*string{&subscriptionID})
	if err != nil {
		return nil, err
	}

	// Convert to map.
	nwInterfacesMapFromRGQuery := make(map[string]*networkInterfaceTable)
	for _, nwInterface := range nwInterfacesFromRGQuery {
		nwInterfacesMapFromRGQuery[strings.ToLower(*nwInterface.ID)] = nwInterface
	}

	// Fetch all network interfaces from the subscription.
	// TODO: Check if the query can support any filtering in future.
	nwInterfaces, err := computeCfg.nwIntfAPIClient.listAllComplete(context.Background())
	if err != nil {
		return nil, err
	}
	var retNwInterfaces []*networkInterfaceInternal
	for _, nwInterface := range nwInterfaces {
		if rgNwInterface, ok := nwInterfacesMapFromRGQuery[strings.ToLower(*nwInterface.ID)]; ok {
			retNwInterfaces = append(retNwInterfaces, &networkInterfaceInternal{nwInterface, *rgNwInterface.VnetID})
		}
	}
	return retNwInterfaces, err
}

// processAppliedToMembership attaches/detaches nics to/from the cloud appliedTo security group.
func (computeCfg *computeServiceConfig) processAppliedToMembership(appliedToGroupIdentifier *cloudresource.CloudResourceID,
	networkInterfaces []*networkInterfaceInternal, rgName string, memberVirtualMachines map[string]struct{},
	memberNetworkInterfaces map[string]struct{}, isPeer bool) error {
	// appliedTo sg has asg as well as nsg created corresponding to it. Hence, update membership for both asg and nsg.
	appliedToGroupOriginalNameToBeUsedAsTag := appliedToGroupIdentifier.GetCloudName(false)
	tokens := strings.Split(appliedToGroupIdentifier.Vpc, "/")
	vnetName := tokens[len(tokens)-1]
	cloudSgNameLowercase := appliedToGroupIdentifier.GetCloudName(isPeer)

	// get NSG and ASG details corresponding to applied to group.
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, getPerVnetDefaultNsgName(vnetName), "")
	if err != nil {
		return err
	}
	asgObj, err := computeCfg.asgAPIClient.get(context.Background(), rgName, cloudSgNameLowercase)
	if err != nil {
		return err
	}

	// find network interfaces which are using or need to use the provided NSG
	nwIntfIDSetNsgToAttach := make(map[string]*armnetwork.Interface)
	nwIntfIDSetNsgToDetach := make(map[string]*armnetwork.Interface)
	for _, networkInterface := range networkInterfaces {
		nwIntfIDLowerCase := strings.ToLower(*networkInterface.ID)
		// 	for network interfaces not attached to any virtual machines, skip processing
		if networkInterface.Properties.VirtualMachine == nil || emptyString(networkInterface.Properties.VirtualMachine.ID) {
			continue
		}

		isNsgAttached := false
		if networkInterface.Properties.NetworkSecurityGroup != nil && !emptyString(networkInterface.Properties.NetworkSecurityGroup.ID) {
			nsgID := strings.ToLower(*networkInterface.Properties.NetworkSecurityGroup.ID)
			_, _, nsgNameLowercase, err := extractFieldsFromAzureResourceID(nsgID)
			if err != nil {
				azurePluginLogger().Error(err, "nsg ID format not valid", "nsgID", nsgID)
				return err
			}

			for _, ipConfigs := range networkInterface.Properties.IPConfigurations {
				if ipConfigs.Properties == nil {
					continue
				}
				for _, asg := range ipConfigs.Properties.ApplicationSecurityGroups {
					asgIDLowerCase := strings.ToLower(*asg.ID)
					// proceed only if network-interface attached to nephe ASG
					_, _, asgName, err := extractFieldsFromAzureResourceID(asgIDLowerCase)
					if err != nil {
						continue
					}
					_, _, isAT := utils.IsNepheControllerCreatedSG(asgName)
					if isAT {
						if strings.Compare(nsgNameLowercase, getPerVnetDefaultNsgName(vnetName)) == 0 &&
							strings.Compare(cloudSgNameLowercase, asgName) == 0 {
							isNsgAttached = true
							break
						}
					}
				}
				// Handling just first available ip-configuration; with an assumption that it will be primary IP always.
				break
			}
		}

		_, isNicAttachedToMemberVM := memberVirtualMachines[strings.ToLower(*networkInterface.Properties.VirtualMachine.ID)]
		_, isNicMemberNetworkInterface := memberNetworkInterfaces[strings.ToLower(*networkInterface.ID)]
		if isNsgAttached {
			if !isNicAttachedToMemberVM && !isNicMemberNetworkInterface {
				nwIntfIDSetNsgToDetach[nwIntfIDLowerCase] = &networkInterface.Interface
			}
		} else {
			if isNicAttachedToMemberVM || isNicMemberNetworkInterface {
				nwIntfIDSetNsgToAttach[nwIntfIDLowerCase] = &networkInterface.Interface
			}
		}
	}

	return computeCfg.processNsgAttachDetachConcurrently(nsgObj, asgObj, nwIntfIDSetNsgToAttach,
		nwIntfIDSetNsgToDetach, appliedToGroupOriginalNameToBeUsedAsTag)
}

// processNsgAttachDetachConcurrently attaches/detaches network interfaces to/from NSG.
func (computeCfg *computeServiceConfig) processNsgAttachDetachConcurrently(nsgObj armnetwork.SecurityGroup,
	asgObj armnetwork.ApplicationSecurityGroup, nwIntfIDSetNsgToAttach map[string]*armnetwork.Interface,
	nwIntfIDSetNsgToDetach map[string]*armnetwork.Interface, nwIntfTagKeyToUpdate string) error {
	ch := make(chan error)
	var err error
	var wg sync.WaitGroup
	wg.Add(len(nwIntfIDSetNsgToAttach) + len(nwIntfIDSetNsgToDetach))
	go func() {
		wg.Wait()
		close(ch)
	}()

	nwIntfAPIClient := computeCfg.nwIntfAPIClient
	for _, nwIntfObj := range nwIntfIDSetNsgToDetach {
		go func(nwIntfObj *armnetwork.Interface, nsgObj armnetwork.SecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceNsg(nwIntfAPIClient, nwIntfObj, nsgObj, asgObj, isAttach, nwIntfTagKeyToUpdate)
		}(nwIntfObj, nsgObj, false, ch)
	}
	for _, nwIntfObj := range nwIntfIDSetNsgToAttach {
		go func(nwIntfObj *armnetwork.Interface, nsgObj armnetwork.SecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceNsg(nwIntfAPIClient, nwIntfObj, nsgObj, asgObj, isAttach, nwIntfTagKeyToUpdate)
		}(nwIntfObj, nsgObj, true, ch)
	}
	for e := range ch {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}
	return err
}

// processAddressGroupMembership attaches/detaches members from AddressGroup SG.
func (computeCfg *computeServiceConfig) processAddressGroupMembership(addressGroupIdentifier *cloudresource.CloudResourceID,
	networkInterfaces []*networkInterfaceInternal, rgName string, memberVirtualMachines map[string]struct{},
	memberNetworkInterfaces map[string]struct{}) error {
	cloudAsgNameLowercase := addressGroupIdentifier.GetCloudName(true)

	// get ASG details
	asgObj, err := computeCfg.asgAPIClient.get(context.Background(), rgName, cloudAsgNameLowercase)
	if err != nil {
		return err
	}

	// find network interfaces which are using or need to use the provided ASG
	nwIntfIDSetAsgToAttach := make(map[string]*armnetwork.Interface)
	nwIntfIDSetAsgToDetach := make(map[string]*armnetwork.Interface)
	for _, networkInterface := range networkInterfaces {
		nwIntfIDLowerCase := strings.ToLower(*networkInterface.ID)
		// 	for network interfaces not attached to any virtual machines, skip processing
		if networkInterface.Properties.VirtualMachine == nil || emptyString(networkInterface.Properties.VirtualMachine.ID) {
			continue
		}

		isAsgAttached := false
		for _, ipconfig := range networkInterface.Properties.IPConfigurations {
			if ipconfig.Properties == nil {
				continue
			}
			for _, asg := range ipconfig.Properties.ApplicationSecurityGroups {
				_, _, asgNameLowercase, err := extractFieldsFromAzureResourceID(strings.ToLower(*asg.ID))
				if err != nil {
					azurePluginLogger().Error(err, "asg ID format not valid", "asgID", *asg.ID)
					continue
				}
				_, isNepheControllerCreatedAG, _ := utils.IsNepheControllerCreatedSG(asgNameLowercase)
				if !isNepheControllerCreatedAG {
					continue
				}
				if strings.Compare(asgNameLowercase, cloudAsgNameLowercase) == 0 {
					isAsgAttached = true
				}
			}
			// Handling just first available ip-configuration; with an assumption that it will be primary IP always.
			break
		}
		_, isNicAttachedToMemberVM := memberVirtualMachines[strings.ToLower(*networkInterface.Properties.VirtualMachine.ID)]
		_, isNicMemberNetworkInterface := memberNetworkInterfaces[strings.ToLower(*networkInterface.ID)]
		if isAsgAttached {
			if !isNicAttachedToMemberVM && !isNicMemberNetworkInterface {
				nwIntfIDSetAsgToDetach[nwIntfIDLowerCase] = &networkInterface.Interface
			}
		} else {
			if isNicAttachedToMemberVM || isNicMemberNetworkInterface {
				nwIntfIDSetAsgToAttach[nwIntfIDLowerCase] = &networkInterface.Interface
			}
		}
	}

	return computeCfg.processAsgAttachDetachConcurrently(asgObj, nwIntfIDSetAsgToAttach, nwIntfIDSetAsgToDetach)
}

// processAsgAttachDetachConcurrently attaches/detaches network interfaces to/from an ASG.
func (computeCfg *computeServiceConfig) processAsgAttachDetachConcurrently(asgObj armnetwork.ApplicationSecurityGroup,
	nwIntfIDSetAsgToAttach, nwIntfIDSetAsgToDetach map[string]*armnetwork.Interface) error {
	ch := make(chan error)
	var err error
	var wg sync.WaitGroup
	wg.Add(len(nwIntfIDSetAsgToAttach) + len(nwIntfIDSetAsgToDetach))
	go func() {
		wg.Wait()
		close(ch)
	}()

	nwIntfAPIClient := computeCfg.nwIntfAPIClient
	for _, nwIntfObj := range nwIntfIDSetAsgToDetach {
		go func(nwIntfObj *armnetwork.Interface, asgObj armnetwork.ApplicationSecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceAsg(nwIntfAPIClient, nwIntfObj, asgObj, isAttach)
		}(nwIntfObj, asgObj, false, ch)
	}

	for _, nwIntfObj := range nwIntfIDSetAsgToAttach {
		go func(nwIntfObj *armnetwork.Interface, asgObj armnetwork.ApplicationSecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceAsg(nwIntfAPIClient, nwIntfObj, asgObj, isAttach)
		}(nwIntfObj, asgObj, true, ch)
	}
	for e := range ch {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}
	return err
}

// buildEffectiveNSGSecurityRulesToApply prepares the update rule cloud api payload from internal rules.
func (computeCfg *computeServiceConfig) buildEffectiveNSGSecurityRulesToApply(appliedToGroupID *cloudresource.CloudResourceID,
	addRules, rmRules []*cloudresource.CloudRule, perVnetAppliedToNsgName, rgName string) ([]*armnetwork.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	if nsgObj.Properties == nil {
		return nil, fmt.Errorf("properties field empty in nsg object %s", *nsgObj.ID)
	}

	addIRule, addERule := utils.SplitCloudRulesByDirection(addRules)
	rmIRule, rmERule := utils.SplitCloudRulesByDirection(rmRules)

	agAsgMapByNepheName, atAsgMapByNepheName, err := getNepheControllerCreatedAsgByNameForResourceGroup(computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	addIngressRules, err := convertIngressToNsgSecurityRules(appliedToGroupID, addIRule, agAsgMapByNepheName, atAsgMapByNepheName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	addEgressRules, err := convertEgressToNsgSecurityRules(appliedToGroupID, addERule, agAsgMapByNepheName, atAsgMapByNepheName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	rmIngressRules, err := convertIngressToNsgSecurityRules(appliedToGroupID, rmIRule, agAsgMapByNepheName, atAsgMapByNepheName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	rmEgressRules, err := convertEgressToNsgSecurityRules(appliedToGroupID, rmERule, agAsgMapByNepheName, atAsgMapByNepheName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	var currentNsgIngressRules []*armnetwork.SecurityRule
	var currentNsgEgressRules []*armnetwork.SecurityRule
	currentNsgSecurityRules := nsgObj.Properties.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("Building security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range currentNsgSecurityRules {
		if rule.Properties == nil || *rule.Properties.Priority == vnetToVnetDenyRulePriority {
			continue
		}

		// check if the rule is Nephe rules based on ruleStartPriority and description.
		if *rule.Properties.Priority >= ruleStartPriority {
			_, ok := utils.ExtractCloudDescription(rule.Properties.Description)
			// remove user rule that is in Nephe priority range.
			if !ok {
				continue
			}
			// check if the rule is created by current processing appliedToGroup.
			if isAzureRuleAttachedToAtSg(rule, appliedToGroupNepheControllerName) {
				removeAzureRules := rmIngressRules
				addAzureRules := addIngressRules
				if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
					removeAzureRules = rmEgressRules
					addAzureRules = addEgressRules
				}
				normalizedRule := normalizeAzureSecurityRule(rule)
				// skip the rule if found in remove list.
				idx, found := findSecurityRule(removeAzureRules, normalizedRule)
				if found {
					// remove the rule from remove list to avoid redundant comparison.
					removeAzureRules[idx] = nil
					continue
				}
				// ignore adding rule that already present in cloud.
				idx, found = findSecurityRule(addAzureRules, normalizedRule)
				if found {
					addAzureRules[idx] = nil
				}
			}
		}

		// preserve current rules that do not need to update.
		if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, rule)
		}
	}

	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, addIngressRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, addEgressRules)

	return append(allIngressRules, allEgressRules...), nil
}

// buildEffectivePeerNSGSecurityRulesToApply prepares the update rule cloud api payload from internal rules that require peering.
func (computeCfg *computeServiceConfig) buildEffectivePeerNSGSecurityRulesToApply(appliedToGroupID *cloudresource.CloudResourceID,
	addRules, rmRules []*cloudresource.CloudRule, perVnetAppliedToNsgName, rgName string, ruleIP *string) ([]*armnetwork.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	if nsgObj.Properties == nil {
		return nil, fmt.Errorf("properties field empty in nsg object %s", *nsgObj.ID)
	}

	addIRule, addERule := utils.SplitCloudRulesByDirection(addRules)
	rmIRule, rmERule := utils.SplitCloudRulesByDirection(rmRules)

	agAsgMapByNepheName, _, err := getNepheControllerCreatedAsgByNameForResourceGroup(computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	addIngressRules, err := convertIngressToPeerNsgSecurityRules(appliedToGroupID, addIRule, agAsgMapByNepheName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	addEgressRules, err := convertEgressToPeerNsgSecurityRules(appliedToGroupID, addERule, agAsgMapByNepheName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	rmIngressRules, err := convertIngressToPeerNsgSecurityRules(appliedToGroupID, rmIRule, agAsgMapByNepheName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	rmEgressRules, err := convertEgressToPeerNsgSecurityRules(appliedToGroupID, rmERule, agAsgMapByNepheName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	var currentNsgIngressRules []*armnetwork.SecurityRule
	var currentNsgEgressRules []*armnetwork.SecurityRule
	currentNsgSecurityRules := nsgObj.Properties.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("Building peering security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range currentNsgSecurityRules {
		if rule.Properties == nil || *rule.Properties.Priority == vnetToVnetDenyRulePriority {
			continue
		}
		// check if the rule is Nephe rules based on ruleStartPriority and description.
		if *rule.Properties.Priority >= ruleStartPriority {
			_, ok := utils.ExtractCloudDescription(rule.Properties.Description)
			// remove user rule that is in Nephe priority range.
			if !ok {
				continue
			}
			// check if the rule is created by current processing appliedToGroup.
			if isAzureRuleAttachedToAtSg(rule, appliedToGroupNepheControllerName) {
				removeAzureRules := rmIngressRules
				addAzureRules := addIngressRules
				if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
					removeAzureRules = rmEgressRules
					addAzureRules = addEgressRules
				}
				normalizedRule := normalizeAzureSecurityRule(rule)
				// skip the rule if found in remove list.
				idx, found := findSecurityRule(removeAzureRules, normalizedRule)
				if found {
					// remove the rule from remove list to avoid redundant comparison.
					removeAzureRules[idx] = nil
					continue
				}
				// ignore adding rule that already present in cloud.
				idx, found = findSecurityRule(addAzureRules, normalizedRule)
				if found {
					addAzureRules[idx] = nil
				}
			}
		}

		// preserve rules.
		if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, rule)
		}
	}

	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, addIngressRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, addEgressRules)
	return append(allIngressRules, allEgressRules...), nil
}

// updateSecurityGroupMembers processes cloud appliedTo and address security group members.
func (computeCfg *computeServiceConfig) updateSecurityGroupMembers(securityGroupIdentifier *cloudresource.CloudResourceID,
	computeResourceIdentifier []*cloudresource.CloudResource, membershipOnly bool) error {
	vnetID := securityGroupIdentifier.Vpc
	vnetNetworkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(map[string]struct{}{vnetID: {}})
	if err != nil {
		return err
	}

	// find all network interfaces which needs to be attached to SG
	memberVirtualMachines, memberNetworkInterfaces := utils.FindResourcesBasedOnKind(computeResourceIdentifier)

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(securityGroupIdentifier.Vpc)
	if err != nil {
		return err
	}
	if isPeer := computeCfg.ifPeerProcessing(vnetID); isPeer {
		if membershipOnly {
			err = computeCfg.processAppliedToMembership(securityGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces, true)
		}
	} else {
		if membershipOnly {
			err = computeCfg.processAddressGroupMembership(securityGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces)
		} else {
			err = computeCfg.processAppliedToMembership(securityGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces, false)
		}
	}
	return err
}

// removeReferencesToSecurityGroup removes rules attached to nsg which reference the ASG which is getting deleted.
func (computeCfg *computeServiceConfig) removeReferencesToSecurityGroup(id *cloudresource.CloudResourceID, rgName string,
	location string, membershiponly bool) error {
	tokens := strings.Split(id.Vpc, "/")
	vnetName := tokens[len(tokens)-1]
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, getPerVnetDefaultNsgName(vnetName), "")
	if err != nil {
		return err
	}
	if nsgObj.Properties == nil {
		return fmt.Errorf("properties field empty in nsg %s", *nsgObj.ID)
	}
	if nsgObj.Properties.SecurityRules == nil {
		return nil
	}
	var asgName string
	vnetID := id.Vpc
	if isPeer := computeCfg.ifPeerProcessing(vnetID); isPeer {
		asgName = id.GetCloudName(false)
	} else {
		asgName = id.GetCloudName(membershiponly)
	}
	currentNsgRules := nsgObj.Properties.SecurityRules
	var rulesToKeep []*armnetwork.SecurityRule
	nsgUpdateRequired := false
	for _, rule := range currentNsgRules {
		if rule.Properties == nil {
			continue
		}
		srcAsgUpdated := false
		dstAsgUpdated := false
		srcAsgs := rule.Properties.SourceApplicationSecurityGroups
		if len(srcAsgs) != 0 {
			asgsToKeep, updated := getAsgsToAdd(srcAsgs, asgName)
			if updated {
				srcAsgs = asgsToKeep
				nsgUpdateRequired = true
				srcAsgUpdated = true
			}
		}
		dstAsgs := rule.Properties.DestinationApplicationSecurityGroups
		if len(dstAsgs) != 0 {
			asgsToKeep, updateRequired := getAsgsToAdd(dstAsgs, asgName)
			if updateRequired {
				dstAsgs = asgsToKeep
				nsgUpdateRequired = true
				dstAsgUpdated = true
			}
		}
		if srcAsgUpdated && srcAsgs == nil {
			continue
		}
		if dstAsgUpdated && dstAsgs == nil {
			continue
		}
		rulesToKeep = append(rulesToKeep, rule)
	}

	if !nsgUpdateRequired {
		return nil
	}
	err = updateNetworkSecurityGroupRules(computeCfg.nsgAPIClient, location, rgName, getPerVnetDefaultNsgName(vnetName), rulesToKeep)

	return err
}

// getAsgsToAdd removes the ASG in addrGroupNepheControllerName parameter from the list of ASGs provided.
func getAsgsToAdd(asgs []*armnetwork.ApplicationSecurityGroup, addrGroupNepheControllerName string) (
	[]*armnetwork.ApplicationSecurityGroup, bool) {
	var asgsToKeep []*armnetwork.ApplicationSecurityGroup
	updated := false
	for _, asg := range asgs {
		_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
		if err != nil {
			azurePluginLogger().Error(err, "invalid azure resource ID")
			continue
		}
		if strings.Compare(strings.ToLower(asgName), addrGroupNepheControllerName) == 0 {
			updated = true
			continue
		}
		asgsToKeep = append(asgsToKeep, asg)
	}
	if len(asgsToKeep) == 0 {
		return nil, updated
	}
	return asgsToKeep, updated
}

// isValidAppliedToSg finds if an ASG is a valid nephe created AppliedTo SG and belong to same RG as VNET.
func (computeCfg *computeServiceConfig) isValidAppliedToSg(asgID string, vnetID string) (string, bool) {
	// proceed only if ASG is created by nephe.
	_, asgRgName, cloudAsgName, err := extractFieldsFromAzureResourceID(asgID)
	if err != nil {
		return "", false
	}

	asgName, _, isAT := utils.IsNepheControllerCreatedSG(cloudAsgName)
	if !isAT {
		return "", false
	}

	// proceed only if RG of ASG is same as VNET RG(deletion of ASG uses RG from VNET ID).
	_, vnetRg, _, err := extractFieldsFromAzureResourceID(vnetID)
	if err != nil {
		return "", false
	}

	if strings.Compare(asgRgName, vnetRg) != 0 {
		return "", false
	}

	return asgName, true
}

// processAndBuildATSgView creates synchronization content for AppliedTo SG.
func (computeCfg *computeServiceConfig) processAndBuildATSgView(networkInterfaces []*networkInterfaceInternal) (
	[]cloudresource.SynchronizationContent, error) {
	nepheControllerATSgNameToMemberCloudResourcesMap := make(map[string][]cloudresource.CloudResource)
	perVnetNsgIDToNepheControllerAppliedToSGNameSet := make(map[string]map[string]struct{})
	nsgIDToVnetIDMap := make(map[string]string)
	for _, networkInterface := range networkInterfaces {
		if networkInterface.Properties.VirtualMachine == nil || emptyString(networkInterface.Properties.VirtualMachine.ID) {
			continue
		}
		if networkInterface.Properties.NetworkSecurityGroup == nil || emptyString(networkInterface.Properties.NetworkSecurityGroup.ID) {
			continue
		}
		nsgIDLowerCase := strings.ToLower(*networkInterface.Properties.NetworkSecurityGroup.ID)
		// proceed only if network-interface attached to nephe per-vnet NSG
		_, _, nsgName, err := extractFieldsFromAzureResourceID(nsgIDLowerCase)
		if err != nil {
			continue
		}
		_, _, isAT := utils.IsNepheControllerCreatedSG(nsgName)
		if !isAT {
			continue
		}

		vnetIDLowerCase := strings.ToLower(networkInterface.vnetID)
		nsgIDToVnetIDMap[nsgIDLowerCase] = vnetIDLowerCase

		// from ASGs attached to network interface find nephe AT SG(s) and build membership map
		newNepheControllerAppliedToSGNameSet := make(map[string]struct{})
		for _, ipConfigs := range networkInterface.Properties.IPConfigurations {
			if ipConfigs.Properties == nil {
				continue
			}
			for _, asg := range ipConfigs.Properties.ApplicationSecurityGroups {
				asgID := asg.ID
				asgName, isValidATSg := computeCfg.isValidAppliedToSg(strings.ToLower(*asgID), vnetIDLowerCase)
				if !isValidATSg {
					continue
				}

				cloudResource := cloudresource.CloudResource{
					Type: cloudresource.CloudResourceTypeNIC,
					CloudResourceID: cloudresource.CloudResourceID{
						Name: strings.ToLower(*networkInterface.ID),
						Vpc:  vnetIDLowerCase,
					},
					AccountID:     computeCfg.accountNamespacedName.String(),
					CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
				}
				cloudResources := nepheControllerATSgNameToMemberCloudResourcesMap[asgName]
				cloudResources = append(cloudResources, cloudResource)
				nepheControllerATSgNameToMemberCloudResourcesMap[asgName] = cloudResources

				newNepheControllerAppliedToSGNameSet[asgName] = struct{}{}
			}
			break
		}

		if len(newNepheControllerAppliedToSGNameSet) > 0 {
			existingNepheControllerAppliedToSGNameSet := perVnetNsgIDToNepheControllerAppliedToSGNameSet[nsgIDLowerCase]
			completeSet := mergeSet(existingNepheControllerAppliedToSGNameSet, newNepheControllerAppliedToSGNameSet)
			perVnetNsgIDToNepheControllerAppliedToSGNameSet[nsgIDLowerCase] = completeSet
		}
	}

	appliedToSgEnforcedView, err := computeCfg.getATGroupView(nepheControllerATSgNameToMemberCloudResourcesMap,
		perVnetNsgIDToNepheControllerAppliedToSGNameSet, nsgIDToVnetIDMap)
	return appliedToSgEnforcedView, err
}

// getATGroupView creates synchronization content for NSGs created by nephe under managed VNETs.
func (computeCfg *computeServiceConfig) getATGroupView(nepheControllerATSGNameToCloudResourcesMap map[string][]cloudresource.CloudResource,
	perVnetNsgIDToNepheControllerATSGNameSet map[string]map[string]struct{}, nsgIDToVnetID map[string]string) (
	[]cloudresource.SynchronizationContent, error) {
	networkSecurityGroups, err := computeCfg.nsgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []cloudresource.SynchronizationContent{}, err
	}

	var enforcedSecurityCloudView []cloudresource.SynchronizationContent
	for _, networkSecurityGroup := range networkSecurityGroups {
		nsgIDLowercase := strings.ToLower(*networkSecurityGroup.ID)
		vnetIDLowercase := nsgIDToVnetID[nsgIDLowercase]
		appliedToSgNameSet, found := perVnetNsgIDToNepheControllerATSGNameSet[nsgIDLowercase]
		if !found {
			continue
		}

		if networkSecurityGroup.Properties == nil {
			continue
		}
		nepheControllerATSgNameToIngressRulesMap, nepheControllerATSgNameToEgressRulesMap, removeUserRules :=
			convertToCloudRulesByAppliedToSGName(networkSecurityGroup.Properties.SecurityRules, vnetIDLowercase)

		for atSgName := range appliedToSgNameSet {
			resource := cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: atSgName,
					Vpc:  vnetIDLowercase,
				},
				AccountID:     computeCfg.accountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
			}
			groupSyncObj := cloudresource.SynchronizationContent{
				Resource:       resource,
				MembershipOnly: false,
				Members:        nepheControllerATSGNameToCloudResourcesMap[atSgName],
				IngressRules:   nepheControllerATSgNameToIngressRulesMap[atSgName],
				EgressRules:    nepheControllerATSgNameToEgressRulesMap[atSgName],
			}
			// If there are user rules needs to be removed, trick the sync to trigger a rule update by adding an empty valid rule.
			// In case of no AT or NP for valid rule, it implies Nephe is not actively managing the Vnet, therefore user rules are ignored.
			if removeUserRules {
				if rule := getEmptyCloudRule(&groupSyncObj); rule != nil {
					groupSyncObj.IngressRules = append(groupSyncObj.IngressRules, *rule)
					removeUserRules = false
				}
			}
			enforcedSecurityCloudView = append(enforcedSecurityCloudView, groupSyncObj)
		}
	}

	return enforcedSecurityCloudView, nil
}

// isValidAddressGroupSg finds if an ASG is in managed VNET, and it is a valid nephe created AddressGroup SG.
func (computeCfg *computeServiceConfig) isValidAddressGroupSg(asgIDLowercase string,
	asgIDToVnetID map[string]string) (string, string, bool) {
	// proceed only if ASG is associated with network interfaces of managed VNET.
	vnetID, found := asgIDToVnetID[asgIDLowercase]
	if !found {
		return "", "", false
	}

	// proceed only if ASG is a nephe created AddressGroup SG.
	_, asgRgName, cloudAsgName, err := extractFieldsFromAzureResourceID(asgIDLowercase)
	if err != nil {
		return "", "", false
	}

	asgName, isAG, _ := utils.IsNepheControllerCreatedSG(cloudAsgName)
	if !isAG {
		return "", "", false
	}

	// proceed only if RG of ASG is same as VNET RG(deletion of ASG uses RG from VNET ID).
	_, vnetRg, _, err := extractFieldsFromAzureResourceID(vnetID)
	if err != nil {
		return "", "", false
	}

	if strings.Compare(asgRgName, vnetRg) != 0 {
		return "", "", false
	}

	return asgName, vnetID, true
}

// processAndBuildAGSgView creates synchronization content for AdressGroup SG.
func (computeCfg *computeServiceConfig) processAndBuildAGSgView(
	networkInterfaces []*networkInterfaceInternal) ([]cloudresource.SynchronizationContent, error) {
	nepheControllerAGSgNameToMemberCloudResourcesMap := make(map[string][]cloudresource.CloudResource)
	asgIDToVnetIDMap := make(map[string]string)
	for _, networkInterface := range networkInterfaces {
		if networkInterface.Properties.VirtualMachine == nil || emptyString(networkInterface.Properties.VirtualMachine.ID) {
			continue
		}
		vnetIDLowerCase := strings.ToLower(networkInterface.vnetID)

		for _, ipConfigs := range networkInterface.Properties.IPConfigurations {
			if ipConfigs.Properties == nil {
				continue
			}
			for _, asg := range ipConfigs.Properties.ApplicationSecurityGroups {
				asgIDLowerCase := strings.ToLower(*asg.ID)
				// proceed only if network-interface attached to nephe ASG
				_, _, asgName, err := extractFieldsFromAzureResourceID(asgIDLowerCase)
				if err != nil {
					continue
				}
				sgName, isAG, _ := utils.IsNepheControllerCreatedSG(asgName)
				if !isAG {
					continue
				}

				cloudResource := cloudresource.CloudResource{
					Type: cloudresource.CloudResourceTypeNIC,
					CloudResourceID: cloudresource.CloudResourceID{
						Name: strings.ToLower(*networkInterface.ID),
						Vpc:  vnetIDLowerCase,
					},
					AccountID:     computeCfg.accountNamespacedName.String(),
					CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
				}
				cloudResources := nepheControllerAGSgNameToMemberCloudResourcesMap[sgName]
				cloudResources = append(cloudResources, cloudResource)
				nepheControllerAGSgNameToMemberCloudResourcesMap[sgName] = cloudResources

				asgIDToVnetIDMap[asgIDLowerCase] = vnetIDLowerCase
			}
			break
		}
	}

	addressGroupSgEnforcedView, err := computeCfg.getAGGroupView(nepheControllerAGSgNameToMemberCloudResourcesMap, asgIDToVnetIDMap)
	return addressGroupSgEnforcedView, err
}

// getAGGroupView creates synchronization content for ASGs created by nephe under managed VNETs.
func (computeCfg *computeServiceConfig) getAGGroupView(nepheControllerAGSgNameToCloudResourcesMap map[string][]cloudresource.CloudResource,
	asgIDToVnetID map[string]string) ([]cloudresource.SynchronizationContent, error) {
	appSecurityGroups, err := computeCfg.asgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []cloudresource.SynchronizationContent{}, err
	}

	var enforcedSecurityCloudView []cloudresource.SynchronizationContent
	for _, appSecurityGroup := range appSecurityGroups {
		asgName, vnetID, isValidAGSg := computeCfg.isValidAddressGroupSg(strings.ToLower(*appSecurityGroup.ID), asgIDToVnetID)
		if !isValidAGSg {
			continue
		}

		resource := cloudresource.CloudResource{
			Type: cloudresource.CloudResourceTypeVM,
			CloudResourceID: cloudresource.CloudResourceID{
				Name: asgName,
				Vpc:  vnetID,
			},
			AccountID:     computeCfg.accountNamespacedName.String(),
			CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
		}
		groupSyncObj := cloudresource.SynchronizationContent{
			Resource:       resource,
			MembershipOnly: true,
			Members:        nepheControllerAGSgNameToCloudResourcesMap[asgName],
		}
		enforcedSecurityCloudView = append(enforcedSecurityCloudView, groupSyncObj)
	}

	return enforcedSecurityCloudView, nil
}
