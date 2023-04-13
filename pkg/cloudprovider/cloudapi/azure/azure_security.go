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
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/go-autorest/autorest/to"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/securitygroup"
)

const (
	appliedToSecurityGroupNamePerVnet = "per-vnet-default"
)

var (
	mutex sync.Mutex
)

type networkInterfaceInternal struct {
	armnetwork.Interface
	vnetID string
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
func (computeCfg *computeServiceConfig) processAppliedToMembership(appliedToGroupIdentifier *securitygroup.CloudResourceID,
	networkInterfaces []*networkInterfaceInternal, rgName string, memberVirtualMachines map[string]struct{},
	memberNetworkInterfaces map[string]struct{}, isPeer bool) error {
	// appliedTo sg has asg as well as nsg created corresponding to it. Hence, update membership for both asg and nsg.
	appliedToGroupOriginalNameToBeUsedAsTag := appliedToGroupIdentifier.GetCloudName(false)
	appliedToSG := securitygroup.CloudResourceID{
		Name: appliedToSecurityGroupNamePerVnet,
		Vpc:  appliedToGroupIdentifier.Vpc,
	}
	tokens := strings.Split(appliedToGroupIdentifier.Vpc, "/")
	suffix := tokens[len(tokens)-1]
	perVnetNsgNameLowercase := appliedToSG.GetCloudName(false) + "-" + suffix
	cloudSgNameLowercase := appliedToGroupIdentifier.GetCloudName(isPeer)

	// get NSG and ASG details corresponding to applied to group.
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetNsgNameLowercase, "")
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
					_, _, isAT := securitygroup.IsNepheControllerCreatedSG(asgName)
					if isAT {
						if strings.Compare(nsgNameLowercase, perVnetNsgNameLowercase) == 0 &&
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
func (computeCfg *computeServiceConfig) processAddressGroupMembership(addressGroupIdentifier *securitygroup.CloudResourceID,
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
				_, isNepheControllerCreatedAG, _ := securitygroup.IsNepheControllerCreatedSG(asgNameLowercase)
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
func (computeCfg *computeServiceConfig) buildEffectiveNSGSecurityRulesToApply(appliedToGroupID *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.CloudRule, egressRules []*securitygroup.CloudRule, perVnetAppliedToNsgName string,
	rgName string) ([]*armnetwork.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	if nsgObj.Properties == nil {
		return nil, fmt.Errorf("properties field empty in nsg object %s", *nsgObj.ID)
	}
	var currentNsgIngressRules []armnetwork.SecurityRule
	var currentNsgEgressRules []armnetwork.SecurityRule
	currentNsgSecurityRules := nsgObj.Properties.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("Building security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range currentNsgSecurityRules {
		if rule.Properties == nil {
			continue
		}
		// Nephe rules will be created from ruleStartPriority and have description.
		if *rule.Properties.Priority < ruleStartPriority || rule.Properties.Description == nil {
			continue
		}

		desc, ok := securitygroup.ExtractCloudDescription(rule.Properties.Description)
		if !ok {
			// Ignore rules that don't have a valid description field.
			azurePluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc, "rule", rule)
			continue
		}
		ruleAddrGroupName := desc.AppliedToGroup
		_, _, isNepheControllerCreatedRule := securitygroup.IsNepheControllerCreatedSG(ruleAddrGroupName)
		if !isNepheControllerCreatedRule {
			continue
		}
		// skip any rules created by current processing appliedToGroup (as we have new rules for this group)
		if strings.Compare(ruleAddrGroupName, appliedToGroupNepheControllerName) == 0 {
			continue
		}
		if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, *rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, *rule)
		}
	}

	agAsgMapByNepheControllerName, atAsgMapByNepheControllerName, err := getNepheControllerCreatedAsgByNameForResourceGroup(
		computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	newIngressSecurityRules, err := convertIngressToNsgSecurityRules(appliedToGroupID, ingressRules,
		agAsgMapByNepheControllerName, atAsgMapByNepheControllerName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	newEgressSecurityRules, err := convertEgressToNsgSecurityRules(appliedToGroupID, egressRules,
		agAsgMapByNepheControllerName, atAsgMapByNepheControllerName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, newIngressSecurityRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, newEgressSecurityRules)

	rules := make([]*armnetwork.SecurityRule, 0)
	for index := range allIngressRules {
		rules = append(rules, &allIngressRules[index])
	}
	for index := range allEgressRules {
		rules = append(rules, &allEgressRules[index])
	}

	return rules, nil
}

// buildEffectivePeerNSGSecurityRulesToApply prepares the update rule cloud api payload from internal rules that require peering.
func (computeCfg *computeServiceConfig) buildEffectivePeerNSGSecurityRulesToApply(appliedToGroupID *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.CloudRule, egressRules []*securitygroup.CloudRule, perVnetAppliedToNsgName string,
	rgName string, ruleIP *string) ([]*armnetwork.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	if nsgObj.Properties == nil {
		return nil, fmt.Errorf("properties field empty in nsg object %s", *nsgObj.ID)
	}
	var currentNsgIngressRules []armnetwork.SecurityRule
	var currentNsgEgressRules []armnetwork.SecurityRule
	currentNsgSecurityRules := nsgObj.Properties.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("Building peering security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range currentNsgSecurityRules {
		if rule.Properties == nil {
			continue
		}
		// Nephe rules will be created from ruleStartPriority and have description.
		if *rule.Properties.Priority < ruleStartPriority || rule.Properties.Description == nil {
			continue
		}

		desc, ok := securitygroup.ExtractCloudDescription(rule.Properties.Description)
		if !ok {
			// Ignore rules that don't have a valid description field.
			azurePluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc, "rule", rule)
			continue
		}
		ruleAddrGroupName := desc.AppliedToGroup
		_, _, isNepheControllerCreatedRule := securitygroup.IsNepheControllerCreatedSG(ruleAddrGroupName)
		if !isNepheControllerCreatedRule {
			continue
		}
		// skip any rules created by current processing appliedToGroup (as we have new rules for this group)
		if strings.Compare(ruleAddrGroupName, appliedToGroupNepheControllerName) == 0 {
			continue
		}
		if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, *rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, *rule)
		}
	}

	agAsgMapByNepheControllerName, _, err := getNepheControllerCreatedAsgByNameForResourceGroup(computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}

	newIngressSecurityRules, err := convertIngressToPeerNsgSecurityRules(appliedToGroupID, ingressRules,
		agAsgMapByNepheControllerName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	newEgressSecurityRules, err := convertEgressToPeerNsgSecurityRules(appliedToGroupID, egressRules,
		agAsgMapByNepheControllerName, ruleIP)
	if err != nil {
		return []*armnetwork.SecurityRule{}, err
	}
	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, newIngressSecurityRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, newEgressSecurityRules)

	var rules []*armnetwork.SecurityRule
	for index := range allIngressRules {
		rules = append(rules, &allIngressRules[index])
	}
	for index := range allEgressRules {
		rules = append(rules, &allEgressRules[index])
	}

	return rules, nil
}

// updateSecurityGroupMembers processes cloud appliedTo and address security group members.
func (computeCfg *computeServiceConfig) updateSecurityGroupMembers(securityGroupIdentifier *securitygroup.CloudResourceID,
	computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	vnetID := securityGroupIdentifier.Vpc
	vnetNetworkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(map[string]struct{}{vnetID: {}})
	if err != nil {
		return err
	}

	// find all network interfaces which needs to be attached to SG
	memberVirtualMachines, memberNetworkInterfaces := securitygroup.FindResourcesBasedOnKind(computeResourceIdentifier)

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
func (computeCfg *computeServiceConfig) removeReferencesToSecurityGroup(id *securitygroup.CloudResourceID, rgName string,
	location string, membershiponly bool) error {
	appliedToSG := securitygroup.CloudResourceID{
		Name: appliedToSecurityGroupNamePerVnet,
		Vpc:  id.Vpc,
	}
	tokens := strings.Split(id.Vpc, "/")
	suffix := tokens[len(tokens)-1]
	perVnetNsgNepheControllerName := appliedToSG.GetCloudName(false) + "-" + suffix

	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetNsgNepheControllerName, "")
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
	err = updateNetworkSecurityGroupRules(computeCfg.nsgAPIClient, location, rgName, perVnetNsgNepheControllerName, rulesToKeep)

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

	asgName, _, isAT := securitygroup.IsNepheControllerCreatedSG(cloudAsgName)
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
	[]securitygroup.SynchronizationContent, error) {
	nepheControllerATSgNameToMemberCloudResourcesMap := make(map[string][]securitygroup.CloudResource)
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
		sgName, _, isAT := securitygroup.IsNepheControllerCreatedSG(nsgName)
		if !isAT {
			continue
		}

		vnetIDLowerCase := strings.ToLower(networkInterface.vnetID)
		nsgIDToVnetIDMap[nsgIDLowerCase] = vnetIDLowerCase
		if strings.Contains(strings.ToLower(sgName), appliedToSecurityGroupNamePerVnet) {
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

					cloudResource := securitygroup.CloudResource{
						Type: securitygroup.CloudResourceTypeNIC,
						CloudResourceID: securitygroup.CloudResourceID{
							Name: strings.ToLower(*networkInterface.ID),
							Vpc:  vnetIDLowerCase,
						},
						AccountID:     computeCfg.account.String(),
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
	}

	appliedToSgEnforcedView, err := computeCfg.getATGroupView(nepheControllerATSgNameToMemberCloudResourcesMap,
		perVnetNsgIDToNepheControllerAppliedToSGNameSet, nsgIDToVnetIDMap)
	return appliedToSgEnforcedView, err
}

// getATGroupView creates synchronization content for NSGs created by nephe under managed VNETs.
func (computeCfg *computeServiceConfig) getATGroupView(nepheControllerATSGNameToCloudResourcesMap map[string][]securitygroup.CloudResource,
	perVnetNsgIDToNepheControllerATSGNameSet map[string]map[string]struct{}, nsgIDToVnetID map[string]string) (
	[]securitygroup.SynchronizationContent, error) {
	networkSecurityGroups, err := computeCfg.nsgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []securitygroup.SynchronizationContent{}, err
	}

	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
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
		nepheControllerATSgNameToIngressRulesMap, nepheControllerATSgNameToEgressRulesMap :=
			convertToInternalRulesByAppliedToSGName(networkSecurityGroup.Properties.SecurityRules, vnetIDLowercase)

		for atSgName := range appliedToSgNameSet {
			resource := securitygroup.CloudResource{
				Type: securitygroup.CloudResourceTypeVM,
				CloudResourceID: securitygroup.CloudResourceID{
					Name: atSgName,
					Vpc:  vnetIDLowercase,
				},
				AccountID:     computeCfg.account.String(),
				CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
			}
			groupSyncObj := securitygroup.SynchronizationContent{
				Resource:       resource,
				MembershipOnly: false,
				Members:        nepheControllerATSGNameToCloudResourcesMap[atSgName],
				IngressRules:   nepheControllerATSgNameToIngressRulesMap[atSgName],
				EgressRules:    nepheControllerATSgNameToEgressRulesMap[atSgName],
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

	asgName, isAG, _ := securitygroup.IsNepheControllerCreatedSG(cloudAsgName)
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
	networkInterfaces []*networkInterfaceInternal) ([]securitygroup.SynchronizationContent, error) {
	nepheControllerAGSgNameToMemberCloudResourcesMap := make(map[string][]securitygroup.CloudResource)
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
				sgName, isAG, _ := securitygroup.IsNepheControllerCreatedSG(asgName)
				if !isAG {
					continue
				}

				cloudResource := securitygroup.CloudResource{
					Type: securitygroup.CloudResourceTypeNIC,
					CloudResourceID: securitygroup.CloudResourceID{
						Name: strings.ToLower(*networkInterface.ID),
						Vpc:  vnetIDLowerCase,
					},
					AccountID:     computeCfg.account.String(),
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
func (computeCfg *computeServiceConfig) getAGGroupView(nepheControllerAGSgNameToCloudResourcesMap map[string][]securitygroup.CloudResource,
	asgIDToVnetID map[string]string) ([]securitygroup.SynchronizationContent, error) {
	appSecurityGroups, err := computeCfg.asgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []securitygroup.SynchronizationContent{}, err
	}

	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
	for _, appSecurityGroup := range appSecurityGroups {
		asgName, vnetID, isValidAGSg := computeCfg.isValidAddressGroupSg(strings.ToLower(*appSecurityGroup.ID), asgIDToVnetID)
		if !isValidAGSg {
			continue
		}

		resource := securitygroup.CloudResource{
			Type: securitygroup.CloudResourceTypeVM,
			CloudResourceID: securitygroup.CloudResourceID{
				Name: asgName,
				Vpc:  vnetID,
			},
			AccountID:     computeCfg.account.String(),
			CloudProvider: string(runtimev1alpha1.AzureCloudProvider),
		}
		groupSyncObj := securitygroup.SynchronizationContent{
			Resource:       resource,
			MembershipOnly: true,
			Members:        nepheControllerAGSgNameToCloudResourcesMap[asgName],
		}
		enforcedSecurityCloudView = append(enforcedSecurityCloudView, groupSyncObj)
	}

	return enforcedSecurityCloudView, nil
}

// ////////////////////////////////////////////////////////
//
//	SecurityInterface Implementation
//
// ////////////////////////////////////////////////////////.

// CreateSecurityGroup invokes cloud api and creates the cloud security group based on securityGroupIdentifier.
// For addressGroup it will attempt to create an asg, for appliedTo groups it will attempt to create both nsg and asg.
func (c *azureCloud) CreateSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) (*string, error) {
	mutex.Lock()
	defer mutex.Unlock()
	var cloudSecurityGroupID string

	// find account managing the vnet
	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		azurePluginLogger().Info("Azure account not found managing virtual network", vnetID, "vnetID")
		return nil, fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(securityGroupIdentifier.Vpc)
	if err != nil {
		return nil, err
	}

	// create/get nsg/asg on/from cloud
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return nil, err
	}
	computeService := serviceCfg.(*computeServiceConfig)
	location := computeService.credentials.region

	if !membershipOnly {
		// per vnet only one appliedTo SG will be created. Hence, always use the same pre-assigned name.
		appliedToAddrID := securitygroup.CloudResourceID{
			Name: appliedToSecurityGroupNamePerVnet,
			Vpc:  securityGroupIdentifier.Vpc,
		}
		tokens := strings.Split(securityGroupIdentifier.Vpc, "/")
		suffix := tokens[len(tokens)-1]
		cloudNsgName := appliedToAddrID.GetCloudName(false) + "-" + suffix
		cloudSecurityGroupID, err = createOrGetNetworkSecurityGroup(computeService.nsgAPIClient, location, rgName, cloudNsgName)
		if err != nil {
			return nil, fmt.Errorf("azure per vnet nsg %v create failed for AT sg %v, reason: %w", cloudNsgName, appliedToAddrID.Name, err)
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
func (c *azureCloud) UpdateSecurityGroupRules(appliedToGroupIdentifier *securitygroup.CloudResource,
	_, _, allRules []*securitygroup.CloudRule) error {
	mutex.Lock()
	defer mutex.Unlock()

	ingressRules := make([]*securitygroup.CloudRule, 0)
	egressRules := make([]*securitygroup.CloudRule, 0)

	for _, rule := range allRules {
		switch rule.Rule.(type) {
		case *securitygroup.IngressRule:
			ingressRules = append(ingressRules, rule)
		case *securitygroup.EgressRule:
			egressRules = append(egressRules, rule)
		}
	}

	// find account managing the vnet and get compute service config
	vnetID := appliedToGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&appliedToGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)
	location := computeService.credentials.region

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(appliedToGroupIdentifier.Vpc)
	if err != nil {
		azurePluginLogger().Error(err, "fail to build extract resource-group-name from vnet ID")
		return err
	}

	vnetPeerPairs := computeService.getVnetPeers(vnetID)
	vnetCachedIDs := computeService.getManagedVnetIDs()
	vnetVMs, _ := computeService.getVirtualMachines()
	// ruleIP := vnetVMs[len(vnetVMs)-1].NetworkInterfaces[0].PrivateIps[0]
	// AT sg name per vnet is fixed and predefined. Get azure nsg name for it.
	appliedToSgID := securitygroup.CloudResourceID{
		Name: appliedToSecurityGroupNamePerVnet,
		Vpc:  vnetID,
	}
	tokens := strings.Split(appliedToGroupIdentifier.Vpc, "/")
	suffix := tokens[len(tokens)-1]
	appliedToGroupPerVnetNsgNepheControllerName := appliedToSgID.GetCloudName(false) + "-" + suffix
	// convert to azure security rules and build effective rules to be applied to AT sg azure NSG
	var rules []*armnetwork.SecurityRule
	flag := 0
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]

		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			var ruleIP *string
			for _, vnetVM := range vnetVMs {
				azurePluginLogger().Info("Accessing VM network interfaces", vnetVM.Name)
				if *vnetVM.VnetID == vnetID {
					ruleIP = vnetVM.NetworkInterfaces[0].PrivateIps[0]
				}
				flag = 1
				break
			}
			rules, err = computeService.buildEffectivePeerNSGSecurityRulesToApply(&appliedToGroupIdentifier.CloudResourceID, ingressRules,
				egressRules, appliedToGroupPerVnetNsgNepheControllerName, rgName, ruleIP)
			if err != nil {
				azurePluginLogger().Error(err, "fail to build effective rules to be applied")
				return err
			}
			break
		}
	}
	if flag == 0 {
		rules, err = computeService.buildEffectiveNSGSecurityRulesToApply(&appliedToGroupIdentifier.CloudResourceID, ingressRules,
			egressRules, appliedToGroupPerVnetNsgNepheControllerName, rgName)
		if err != nil {
			azurePluginLogger().Error(err, "fail to build effective rules to be applied")
			return err
		}
	}
	// update network security group with rules
	return updateNetworkSecurityGroupRules(computeService.nsgAPIClient, location, rgName, appliedToGroupPerVnetNsgNepheControllerName, rules)
}

// UpdateSecurityGroupMembers invokes cloud api and attaches/detaches nics to/from the cloud security group.
func (c *azureCloud) UpdateSecurityGroupMembers(securityGroupIdentifier *securitygroup.CloudResource,
	computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)

	return computeService.updateSecurityGroupMembers(&securityGroupIdentifier.CloudResourceID, computeResourceIdentifier, membershipOnly)
}

// DeleteSecurityGroup invokes cloud api and deletes the cloud application security group.
func (c *azureCloud) DeleteSecurityGroup(securityGroupIdentifier *securitygroup.CloudResource, membershipOnly bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	vnetID := securityGroupIdentifier.Vpc
	accCfg, found := c.cloudCommon.GetCloudAccountByAccountId(&securityGroupIdentifier.AccountID)
	if !found {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)
	location := computeService.credentials.region

	_ = computeService.updateSecurityGroupMembers(&securityGroupIdentifier.CloudResourceID, nil, membershipOnly)

	var rgName string
	_, rgName, _, err = extractFieldsFromAzureResourceID(securityGroupIdentifier.Vpc)
	if err != nil {
		return err
	}
	err = computeService.removeReferencesToSecurityGroup(&securityGroupIdentifier.CloudResourceID, rgName, location, membershipOnly)
	if err != nil {
		return err
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

func (c *azureCloud) GetEnforcedSecurity() []securitygroup.SynchronizationContent {
	mutex.Lock()
	defer mutex.Unlock()

	inventoryInitWaitDuration := 30 * time.Second

	var accNamespacedNames []types.NamespacedName
	accountConfigs := c.cloudCommon.GetCloudAccounts()
	for _, accCfg := range accountConfigs {
		accNamespacedNames = append(accNamespacedNames, *accCfg.GetNamespacedName())
	}

	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
	var wg sync.WaitGroup
	ch := make(chan []securitygroup.SynchronizationContent)
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

		go func(name *types.NamespacedName, sendCh chan<- []securitygroup.SynchronizationContent) {
			defer wg.Done()

			accCfg, found := c.cloudCommon.GetCloudAccountByName(name)
			if !found {
				azurePluginLogger().Info("Enforced-security-cloud-view GET for account skipped (account no longer exists)", "account", name)
				return
			}

			serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
			if err != nil {
				azurePluginLogger().Error(err, "nforced-security-cloud-view GET for account skipped", "account", accCfg.GetNamespacedName())
				return
			}
			computeService := serviceCfg.(*computeServiceConfig)
			err = computeService.waitForInventoryInit(inventoryInitWaitDuration)
			if err != nil {
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

func (computeCfg *computeServiceConfig) getNepheControllerManagedSecurityGroupsCloudView() []securitygroup.SynchronizationContent {
	vnetIDs := computeCfg.getManagedVnetIDs()
	if len(vnetIDs) == 0 {
		return []securitygroup.SynchronizationContent{}
	}

	networkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(vnetIDs)
	if err != nil {
		return []securitygroup.SynchronizationContent{}
	}

	appliedToSgEnforcedView, err := computeCfg.processAndBuildATSgView(networkInterfaces)
	if err != nil {
		return []securitygroup.SynchronizationContent{}
	}

	addressGroupSgEnforcedView, err := computeCfg.processAndBuildAGSgView(networkInterfaces)
	if err != nil {
		return []securitygroup.SynchronizationContent{}
	}

	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
	enforcedSecurityCloudView = append(enforcedSecurityCloudView, appliedToSgEnforcedView...)
	enforcedSecurityCloudView = append(enforcedSecurityCloudView, addressGroupSgEnforcedView...)

	return enforcedSecurityCloudView
}

func (computeCfg *computeServiceConfig) ifPeerProcessing(vnetID string) bool {
	vnetPeerPairs := computeCfg.getVnetPeers(vnetID)
	vnetCachedIDs := computeCfg.getManagedVnetIDs()
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]
		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			return true
		}
	}
	return false
}
