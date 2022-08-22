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

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest/to"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
	"antrea.io/nephe/pkg/cloud-provider/utils"
)

const (
	appliedToSecurityGroupNamePerVnet = "per-vnet-default"
)

var (
	mutex sync.Mutex
)

func (computeCfg *computeServiceConfig) getNetworkInterfacesOfVnet(vnetIDSet map[string]struct{}) ([]*networkInterfaceTable, error) {
	location := computeCfg.credentials.region
	subscriptionID := computeCfg.credentials.SubscriptionID
	tenentID := computeCfg.credentials.TenantID

	var vnetIDs []string
	for vnetID := range vnetIDSet {
		vnetIDs = append(vnetIDs, vnetID)
	}

	query, err := getNwIntfsByVnetIDsAndSubscriptionIDsAndTenantIDsAndLocationsMatchQuery(vnetIDs, []string{subscriptionID},
		[]string{tenentID}, []string{location})
	if err != nil {
		return nil, err
	}
	nwIntfs, _, err := getNetworkInterfaceTable(computeCfg.resourceGraphAPIClient, query, []string{subscriptionID})
	return nwIntfs, err
}

func (computeCfg *computeServiceConfig) processAppliedToMembership(addrGroupIdentifier *securitygroup.CloudResourceID,
	networkInterfaces []*networkInterfaceTable, rgName string, memberVirtualMachines map[string]struct{},
	memberNetworkInterfaces map[string]struct{}, isPeer bool) error {
	// appliedTo sg has asg as well as nsg created corresponding to it. Hence update membership for both asg and nsg.
	addrGroupOriginalNameToBeUsedAsTag := addrGroupIdentifier.GetCloudName(false)
	appliedToSG := securitygroup.CloudResourceID{
		Name: appliedToSecurityGroupNamePerVnet,
		Vpc:  addrGroupIdentifier.Vpc,
	}
	tokens := strings.Split(addrGroupIdentifier.Vpc, "/")
	suffix := tokens[len(tokens)-1]
	perVnetNsgNameLowercase := appliedToSG.GetCloudName(false) + "-" + suffix
	cloudSgNameLowercase := addrGroupIdentifier.GetCloudName(isPeer)

	// get NSG and ASG details corresponding to applied to group
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetNsgNameLowercase, "")
	if err != nil {
		return err
	}
	asgObj, err := computeCfg.asgAPIClient.get(context.Background(), rgName, cloudSgNameLowercase)
	if err != nil {
		return err
	}

	// find network interfaces which are using or need to use the provided NSG
	nwIntfIDSetNsgToAttach := make(map[string]struct{})
	nwIntfIDSetNsgToDettach := make(map[string]struct{})
	for _, networkInterface := range networkInterfaces {
		nwIntfIDLowerCase := strings.ToLower(*networkInterface.ID)
		// 	for network interfaces not attached to any virtual machines, skip processing
		vmID := networkInterface.VirtualMachineID
		if vmID == nil || len(*vmID) == 0 {
			continue
		}

		isNsgAttached := false
		if networkInterface.NetworkSecurityGroupID != nil && len(*networkInterface.NetworkSecurityGroupID) > 0 {
			nsgID := strings.ToLower(*networkInterface.NetworkSecurityGroupID)
			_, _, nsgNameLowercase, err := extractFieldsFromAzureResourceID(nsgID)
			if err != nil {
				azurePluginLogger().Error(err, "nsg ID format not valid", "nsgID", nsgID)
				return err
			}
			if len(networkInterface.Tags) > 0 {
				tags := networkInterface.Tags[0]
				_, found := tags[cloudSgNameLowercase]
				if strings.Compare(nsgNameLowercase, perVnetNsgNameLowercase) == 0 && found {
					isNsgAttached = true
				}
			}
		}
		_, isNicAttachedToMemberVM := memberVirtualMachines[strings.ToLower(*vmID)]
		_, isNicMemberNetworkInterface := memberNetworkInterfaces[strings.ToLower(*networkInterface.ID)]
		if isNsgAttached {
			if !isNicAttachedToMemberVM && !isNicMemberNetworkInterface {
				nwIntfIDSetNsgToDettach[nwIntfIDLowerCase] = struct{}{}
			}
		} else {
			if isNicAttachedToMemberVM || isNicMemberNetworkInterface {
				nwIntfIDSetNsgToAttach[nwIntfIDLowerCase] = struct{}{}
			}
		}
	}

	return computeCfg.processNsgAttachDetachConcurrently(nsgObj, asgObj, nwIntfIDSetNsgToAttach,
		nwIntfIDSetNsgToDettach, addrGroupOriginalNameToBeUsedAsTag)
}

func (computeCfg *computeServiceConfig) processNsgAttachDetachConcurrently(nsgObj network.SecurityGroup,
	asgObj network.ApplicationSecurityGroup, nwIntfIDSetNsgToAttach map[string]struct{},
	nwIntfIDSetNsgToDetach map[string]struct{}, nwIntfTagKeyToUpdate string) error {
	allNwIntfIDs := mergeSet(nwIntfIDSetNsgToAttach, nwIntfIDSetNsgToDetach)

	nwIntfAPIClient := computeCfg.nwIntfAPIClient
	nwIntfIDToObjMap, err1 := getNetworkInterfacesGivenIDs(nwIntfAPIClient, allNwIntfIDs)
	if err1 != nil {
		return nil
	}

	ch := make(chan error)
	var err error
	var wg sync.WaitGroup

	wg.Add(len(allNwIntfIDs))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for _, nwIntfObj := range nwIntfIDToObjMap {
		isAttach := false
		nwIntfIDLowercase := strings.ToLower(*nwIntfObj.ID)
		if _, found := nwIntfIDSetNsgToAttach[nwIntfIDLowercase]; found {
			isAttach = true
		}

		go func(nwIntfObj network.Interface, nsgObj network.SecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceNsg(nwIntfAPIClient, nwIntfObj, nsgObj, asgObj, isAttach, nwIntfTagKeyToUpdate)
		}(nwIntfObj, nsgObj, isAttach, ch)
	}
	for e := range ch {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}

	return err
}

func (computeCfg *computeServiceConfig) processAddressGroupMembership(addressGroupIdentifier *securitygroup.CloudResourceID,
	networkInterfaces []*networkInterfaceTable, rgName string, memberVirtualMachines map[string]struct{},
	memberNetworkInterfaces map[string]struct{}) error {
	cloudAsgNameLowercase := addressGroupIdentifier.GetCloudName(true)

	// get ASG details
	asgObj, err := computeCfg.asgAPIClient.get(context.Background(), rgName, cloudAsgNameLowercase)
	if err != nil {
		return err
	}

	// find network interfaces which are using or need to use the provided ASG
	nwIntfIDSetAsgToAttach := make(map[string]struct{})
	nwIntfIDSetAsgToDettach := make(map[string]struct{})
	for _, networkInterface := range networkInterfaces {
		nwIntfIDLowerCase := strings.ToLower(*networkInterface.ID)
		// 	for network interfaces not attached to any virtual machines, skip processing
		vmID := networkInterface.VirtualMachineID
		if vmID == nil || len(*vmID) == 0 {
			continue
		}

		isAsgAttached := false
		for _, asgID := range networkInterface.ApplicationSecurityGroupIDs {
			_, _, asgNameLowercase, err := extractFieldsFromAzureResourceID(strings.ToLower(*asgID))
			if err != nil {
				azurePluginLogger().Error(err, "asg ID format not valid", "asgID", asgID)
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
		_, isNicAttachedToMemberVM := memberVirtualMachines[strings.ToLower(*vmID)]
		_, isNicMemberNetworkInterface := memberNetworkInterfaces[strings.ToLower(*networkInterface.ID)]
		if isAsgAttached {
			if !isNicAttachedToMemberVM && !isNicMemberNetworkInterface {
				nwIntfIDSetAsgToDettach[nwIntfIDLowerCase] = struct{}{}
			}
		} else {
			if isNicAttachedToMemberVM || isNicMemberNetworkInterface {
				nwIntfIDSetAsgToAttach[nwIntfIDLowerCase] = struct{}{}
			}
		}
	}

	return computeCfg.processAsgAttachDetachConcurrently(asgObj, nwIntfIDSetAsgToAttach, nwIntfIDSetAsgToDettach)
}

func (computeCfg *computeServiceConfig) processAsgAttachDetachConcurrently(asgObj network.ApplicationSecurityGroup,
	nwIntfIDSetAsgToAttach map[string]struct{}, nwIntfIDSetAsgToDetach map[string]struct{}) error {
	allNwIntfIDs := mergeSet(nwIntfIDSetAsgToAttach, nwIntfIDSetAsgToDetach)

	nwIntfAPIClient := computeCfg.nwIntfAPIClient
	nwIntfIDToObjMap, err1 := getNetworkInterfacesGivenIDs(nwIntfAPIClient, allNwIntfIDs)
	if err1 != nil {
		return nil
	}

	ch := make(chan error)
	var err error
	var wg sync.WaitGroup

	wg.Add(len(allNwIntfIDs))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for _, nwIntfObj := range nwIntfIDToObjMap {
		isAttach := false
		nwIntfIDLowercase := strings.ToLower(*nwIntfObj.ID)
		if _, found := nwIntfIDSetAsgToAttach[nwIntfIDLowercase]; found {
			isAttach = true
		}

		go func(nwIntfObj network.Interface, asgObj network.ApplicationSecurityGroup, isAttach bool, ch chan error) {
			defer wg.Done()
			ch <- updateNetworkInterfaceAsg(nwIntfAPIClient, nwIntfObj, asgObj, isAttach)
		}(nwIntfObj, asgObj, isAttach, ch)
	}
	for e := range ch {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}

	return err
}

func (computeCfg *computeServiceConfig) buildEffectiveNSGSecurityRulesToApply(appliedToGroupID *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.IngressRule, egressRules []*securitygroup.EgressRule, perVnetAppliedToNsgName string,
	rgName string) ([]network.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []network.SecurityRule{}, err
	}

	var currentNsgIngressRules []network.SecurityRule
	var currentNsgEgressRules []network.SecurityRule
	currentNsgSecurityRules := nsgObj.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("building security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range *currentNsgSecurityRules {
		// skip any rules not created by nephe
		if rule.Description == nil {
			continue
		}
		ruleAddrGroupName := *rule.Description
		_, _, isNepheControllerCreatedRule := securitygroup.IsNepheControllerCreatedSG(ruleAddrGroupName)
		if !isNepheControllerCreatedRule {
			continue
		}
		// skip any rules created by current processing appliedToGroup (as we have new rules for this group)
		if strings.Compare(ruleAddrGroupName, appliedToGroupNepheControllerName) == 0 {
			continue
		}
		if rule.Direction == network.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, rule)
		}
	}

	agAsgMapByNepheControllerName, atAsgMapByNepheControllerName, err := getNepheControllerCreatedAsgByNameForResourceGroup(
		computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []network.SecurityRule{}, err
	}

	newIngressSecurityRules, err := convertIngressToAzureNsgSecurityRules(appliedToGroupID, ingressRules,
		agAsgMapByNepheControllerName, atAsgMapByNepheControllerName)
	if err != nil {
		return []network.SecurityRule{}, err
	}
	newEgressSecurityRules, err := convertEgressToAzureNsgSecurityRules(appliedToGroupID, egressRules,
		agAsgMapByNepheControllerName, atAsgMapByNepheControllerName)
	if err != nil {
		return []network.SecurityRule{}, err
	}
	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, newIngressSecurityRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, newEgressSecurityRules)

	var rules []network.SecurityRule
	rules = append(rules, allIngressRules...)
	rules = append(rules, allEgressRules...)

	return rules, nil
}

func (computeCfg *computeServiceConfig) buildEffectivePeerNSGSecurityRulesToApply(appliedToGroupID *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.IngressRule, egressRules []*securitygroup.EgressRule, perVnetAppliedToNsgName string,
	rgName string, ruleIP *string) ([]network.SecurityRule, error) {
	// get current rules for applied to SG azure NSG
	nsgObj, err := computeCfg.nsgAPIClient.get(context.Background(), rgName, perVnetAppliedToNsgName, "")
	if err != nil {
		return []network.SecurityRule{}, err
	}

	var currentNsgIngressRules []network.SecurityRule
	var currentNsgEgressRules []network.SecurityRule
	currentNsgSecurityRules := nsgObj.SecurityRules
	appliedToGroupNepheControllerName := appliedToGroupID.GetCloudName(false)
	azurePluginLogger().Info("building peering security rules", "applied to security group", appliedToGroupNepheControllerName)
	for _, rule := range *currentNsgSecurityRules {
		// skip any rules not created by nephe
		if rule.Description == nil {
			continue
		}
		ruleAddrGroupName := *rule.Description
		_, _, isNepheControllerCreatedRule := securitygroup.IsNepheControllerCreatedSG(ruleAddrGroupName)
		if !isNepheControllerCreatedRule {
			continue
		}
		// skip any rules created by current processing appliedToGroup (as we have new rules for this group)
		if strings.Compare(ruleAddrGroupName, appliedToGroupNepheControllerName) == 0 {
			continue
		}
		if rule.Direction == network.SecurityRuleDirectionInbound {
			currentNsgIngressRules = append(currentNsgIngressRules, rule)
		} else {
			currentNsgEgressRules = append(currentNsgEgressRules, rule)
		}
	}

	agAsgMapByNepheControllerName, _, err := getNepheControllerCreatedAsgByNameForResourceGroup(computeCfg.asgAPIClient, rgName)
	if err != nil {
		return []network.SecurityRule{}, err
	}

	newIngressSecurityRules, err := convertIngressToAzurePeerNsgSecurityRules(appliedToGroupID, ingressRules,
		agAsgMapByNepheControllerName, ruleIP)
	if err != nil {
		return []network.SecurityRule{}, err
	}
	newEgressSecurityRules, err := convertEgressToAzurePeerNsgSecurityRules(appliedToGroupID, egressRules,
		agAsgMapByNepheControllerName, ruleIP)
	if err != nil {
		return []network.SecurityRule{}, err
	}
	allIngressRules := updateSecurityRuleNameAndPriority(currentNsgIngressRules, newIngressSecurityRules)
	allEgressRules := updateSecurityRuleNameAndPriority(currentNsgEgressRules, newEgressSecurityRules)

	var rules []network.SecurityRule
	rules = append(rules, allIngressRules...)
	rules = append(rules, allEgressRules...)

	return rules, nil
}

func (computeCfg *computeServiceConfig) updateSecurityGroupMembers(addressGroupIdentifier *securitygroup.CloudResourceID,
	computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	vnetID := addressGroupIdentifier.Vpc
	vnetNetworkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(map[string]struct{}{vnetID: {}})
	if err != nil {
		return err
	}

	// find all network interfaces which needs to be attached to SG
	memberVirtualMachines, memberNetworkInterfaces := securitygroup.FindResourcesBasedOnKind(computeResourceIdentifier)

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(addressGroupIdentifier.Vpc)
	if err != nil {
		return err
	}
	if isPeer := computeCfg.ifPeerProcessing(vnetID); isPeer {
		if membershipOnly {
			err = computeCfg.processAppliedToMembership(addressGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces, true)
		}
	} else {
		if membershipOnly {
			err = computeCfg.processAddressGroupMembership(addressGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces)
		} else {
			err = computeCfg.processAppliedToMembership(addressGroupIdentifier, vnetNetworkInterfaces, rgName,
				memberVirtualMachines, memberNetworkInterfaces, false)
		}
	}
	return err
}

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
	if nsgObj.SecurityRules == nil {
		return nil
	}
	var asgName string
	vnetID := id.Vpc
	if isPeer := computeCfg.ifPeerProcessing(vnetID); isPeer {
		asgName = id.GetCloudName(false)
	} else {
		asgName = id.GetCloudName(membershiponly)
	}
	currentNsgRules := *nsgObj.SecurityRules
	var rulesToKeep []network.SecurityRule
	nsgUpdateRequired := false
	for _, rule := range currentNsgRules {
		srcAsgUpdated := false
		dstAsgUpdated := false
		srcAsgs := rule.SourceApplicationSecurityGroups
		if srcAsgs != nil && len(*srcAsgs) != 0 {
			asgsToKeep, updated := getAsgsToAdd(srcAsgs, asgName)
			if updated {
				srcAsgs = asgsToKeep
				nsgUpdateRequired = true
				srcAsgUpdated = true
			}
		}
		dstAsgs := rule.DestinationApplicationSecurityGroups
		if dstAsgs != nil && len(*dstAsgs) != 0 {
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

func getAsgsToAdd(asgs *[]network.ApplicationSecurityGroup, addrGroupNepheControllerName string) (
	*[]network.ApplicationSecurityGroup, bool) {
	var asgsToKeep []network.ApplicationSecurityGroup
	updated := false
	for _, asg := range *asgs {
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
	return &asgsToKeep, updated
}

func (computeCfg *computeServiceConfig) processAndBuildAGSgView(networkInterfaces []*networkInterfaceTable,
	antreaATSgNameSet map[string]struct{}) ([]securitygroup.SynchronizationContent, error) {
	nepheControllerAGSgNameToMemberCloudResourcesMap := make(map[string][]securitygroup.CloudResource)
	asgIDToVnetIDMap := make(map[string]string)
	for _, networkInterface := range networkInterfaces {
		if networkInterface.VirtualMachineID == nil {
			continue
		}
		vnetIDLowerCase := strings.ToLower(*networkInterface.VnetID)
		attachedAsgIDs := networkInterface.ApplicationSecurityGroupIDs
		for _, asgID := range attachedAsgIDs {
			asgIDLowerCase := strings.ToLower(*asgID)
			// proceed only if network-interface attached to nephe ASG
			_, _, nsgName, err := extractFieldsFromAzureResourceID(asgIDLowerCase)
			if err != nil {
				continue
			}
			SgName, isAG, _ := securitygroup.IsNepheControllerCreatedSG(nsgName)
			if !isAG {
				continue
			}

			cloudResource := securitygroup.CloudResource{
				Type: securitygroup.CloudResourceTypeNIC,
				Name: securitygroup.CloudResourceID{
					Name: utils.GenerateShortResourceIdentifier(*networkInterface.ID, common.NetworkInterfaceCRDKind),
					Vpc:  vnetIDLowerCase,
				},
			}
			cloudResources := nepheControllerAGSgNameToMemberCloudResourcesMap[SgName]
			cloudResources = append(cloudResources, cloudResource)
			nepheControllerAGSgNameToMemberCloudResourcesMap[SgName] = cloudResources

			asgIDToVnetIDMap[asgIDLowerCase] = vnetIDLowerCase
		}
	}

	addressGroupSgEnforcedView, err := computeCfg.getAGGroupView(nepheControllerAGSgNameToMemberCloudResourcesMap, asgIDToVnetIDMap,
		antreaATSgNameSet)
	return addressGroupSgEnforcedView, err
}

func (computeCfg *computeServiceConfig) processAndBuildATSgView(networkInterfaces []*networkInterfaceTable) (
	[]securitygroup.SynchronizationContent, map[string]struct{}, error) {
	nepheControllerATSgNameToMemberCloudResourcesMap := make(map[string][]securitygroup.CloudResource)
	perVnetNsgIDToNepheControllerAppliedToSGNameSet := make(map[string]map[string]struct{})
	nsgIDToVnetIDMap := make(map[string]string)
	for _, networkInterface := range networkInterfaces {
		if networkInterface.VirtualMachineID == nil {
			continue
		}
		if networkInterface.NetworkSecurityGroupID == nil {
			continue
		}
		nsgIDLowerCase := strings.ToLower(*networkInterface.NetworkSecurityGroupID)
		// proceed only if network-interface attached to nephe per-vnet NSG
		_, _, nsgName, err := extractFieldsFromAzureResourceID(nsgIDLowerCase)
		if err != nil {
			continue
		}
		sgName, _, isAT := securitygroup.IsNepheControllerCreatedSG(nsgName)
		if !isAT {
			continue
		}
		vnetIDLowerCase := strings.ToLower(*networkInterface.VnetID)
		nsgIDToVnetIDMap[nsgIDLowerCase] = vnetIDLowerCase
		if strings.Compare(sgName, strings.ToLower(appliedToSecurityGroupNamePerVnet)) == 0 {
			// from tags find nephe AT SG(s) and build membership map
			newNepheControllerAppliedToSGNameSet := make(map[string]struct{})
			for key := range networkInterface.Tags[0] {
				ATSgName, _, isATSG := securitygroup.IsNepheControllerCreatedSG(key)
				if !isATSG {
					continue
				}
				cloudResource := securitygroup.CloudResource{
					Type: securitygroup.CloudResourceTypeNIC,
					Name: securitygroup.CloudResourceID{
						Name: utils.GenerateShortResourceIdentifier(*networkInterface.ID, common.NetworkInterfaceCRDKind),
						Vpc:  vnetIDLowerCase,
					},
				}
				cloudResources := nepheControllerATSgNameToMemberCloudResourcesMap[ATSgName]
				cloudResources = append(cloudResources, cloudResource)
				nepheControllerATSgNameToMemberCloudResourcesMap[ATSgName] = cloudResources

				newNepheControllerAppliedToSGNameSet[ATSgName] = struct{}{}
			}
			if len(newNepheControllerAppliedToSGNameSet) > 0 {
				existingNepheControllerAppliedToSGNameSet := perVnetNsgIDToNepheControllerAppliedToSGNameSet[nsgIDLowerCase]
				completeSet := mergeSet(existingNepheControllerAppliedToSGNameSet, newNepheControllerAppliedToSGNameSet)
				perVnetNsgIDToNepheControllerAppliedToSGNameSet[nsgIDLowerCase] = completeSet
			}
		}
	}

	appliedToSgEnforcedView, antreaAtSgNameSet, err := computeCfg.getATGroupView(nepheControllerATSgNameToMemberCloudResourcesMap,
		perVnetNsgIDToNepheControllerAppliedToSGNameSet, nsgIDToVnetIDMap)
	return appliedToSgEnforcedView, antreaAtSgNameSet, err
}

func (computeCfg *computeServiceConfig) getATGroupView(nepheControllerATSGNameToCloudResourcesMap map[string][]securitygroup.CloudResource,
	perVnetNsgIDToNepheControllerATSGNameSet map[string]map[string]struct{}, nsgIDToVnetID map[string]string) (
	[]securitygroup.SynchronizationContent, map[string]struct{}, error) {
	networkSecurityGroups, err := computeCfg.nsgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []securitygroup.SynchronizationContent{}, nil, err
	}

	nepheControllerATSgNameSet := make(map[string]struct{})
	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
	for _, networkSecurityGroup := range networkSecurityGroups {
		nsgIDLowercase := strings.ToLower(*networkSecurityGroup.ID)
		vnetIDLowercase := nsgIDToVnetID[nsgIDLowercase]
		appliedToSgNameSet, found := perVnetNsgIDToNepheControllerATSGNameSet[nsgIDLowercase]
		if !found {
			continue
		}
		nepheControllerATSgNameToIngressRulesMap, nepheControllerATSgNameToEgressRulesMap :=
			convertToNepheControllerRulesByAppliedToSGName(networkSecurityGroup.SecurityRules, vnetIDLowercase)

		for atSgName := range appliedToSgNameSet {
			resource := securitygroup.CloudResourceID{
				Name: atSgName,
				Vpc:  vnetIDLowercase,
			}
			groupSyncObj := securitygroup.SynchronizationContent{
				Resource:       resource,
				MembershipOnly: false,
				Members:        nepheControllerATSGNameToCloudResourcesMap[atSgName],
				IngressRules:   nepheControllerATSgNameToIngressRulesMap[atSgName],
				EgressRules:    nepheControllerATSgNameToEgressRulesMap[atSgName],
			}
			enforcedSecurityCloudView = append(enforcedSecurityCloudView, groupSyncObj)

			nepheControllerATSgNameSet[atSgName] = struct{}{}
		}
	}

	return enforcedSecurityCloudView, nepheControllerATSgNameSet, nil
}

func (computeCfg *computeServiceConfig) getAGGroupView(nepheControllerAGSgNameToCloudResourcesMap map[string][]securitygroup.CloudResource,
	asgIDToVnetID map[string]string, nepheControllerATSgNameSet map[string]struct{}) ([]securitygroup.SynchronizationContent, error) {
	appSecurityGroups, err := computeCfg.asgAPIClient.listAllComplete(context.Background())
	if err != nil {
		return []securitygroup.SynchronizationContent{}, err
	}

	var enforcedSecurityCloudView []securitygroup.SynchronizationContent
	for _, appSecurityGroup := range appSecurityGroups {
		asgIDLowercase := strings.ToLower(*appSecurityGroup.ID)

		_, _, cloudAsgName, err := extractFieldsFromAzureResourceID(asgIDLowercase)
		if err != nil {
			continue
		}

		AGSgName, isAG, _ := securitygroup.IsNepheControllerCreatedSG(cloudAsgName)
		if !isAG {
			continue
		}

		// skip asg if it belongs to AT SG
		_, found := nepheControllerATSgNameSet[AGSgName]
		if found {
			continue
		}

		vnetID := asgIDToVnetID[asgIDLowercase]
		resource := securitygroup.CloudResourceID{
			Name: AGSgName,
			Vpc:  vnetID,
		}
		groupSyncObj := securitygroup.SynchronizationContent{
			Resource:       resource,
			MembershipOnly: true,
			Members:        nepheControllerAGSgNameToCloudResourcesMap[AGSgName],
		}
		enforcedSecurityCloudView = append(enforcedSecurityCloudView, groupSyncObj)
	}

	return enforcedSecurityCloudView, nil
}

// ////////////////////////////////////////////////////////
// 	SecurityInterface Implementation
// ////////////////////////////////////////////////////////.
func (c *azureCloud) CreateSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) (*string, error) {
	mutex.Lock()
	defer mutex.Unlock()
	var cloudSecurityGroupID string

	// find account managing the vnet
	vnetID := addressGroupIdentifier.Vpc
	accCfg := c.getVnetAccount(vnetID)
	if accCfg == nil {
		azurePluginLogger().Info("azure account not found managing virtual network", vnetID, "vnetID")
		return nil, fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(addressGroupIdentifier.Vpc)
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
		// per vnet only one appliedTo SG will be created. Hence always use the same pre-assigned name.
		appliedToAddrID := securitygroup.CloudResourceID{
			Name: appliedToSecurityGroupNamePerVnet,
			Vpc:  addressGroupIdentifier.Vpc,
		}
		tokens := strings.Split(addressGroupIdentifier.Vpc, "/")
		suffix := tokens[len(tokens)-1]
		cloudNsgName := appliedToAddrID.GetCloudName(false) + "-" + suffix
		cloudSecurityGroupID, err = createOrGetNetworkSecurityGroup(computeService.nsgAPIClient, location, rgName, cloudNsgName)
		if err != nil {
			return nil, fmt.Errorf("azure per vnet nsg %v create failed for AT sg %v, reason: %w", cloudNsgName, appliedToAddrID.Name, err)
		}

		// create azure asg corresponding to AT sg.
		cloudAsgName := addressGroupIdentifier.GetCloudName(false)
		_, err = createOrGetApplicationSecurityGroup(computeService.asgAPIClient, location, rgName, cloudAsgName)
		if err != nil {
			return nil, fmt.Errorf("azure asg %v create failed for AT sg %v, reason: %w", cloudAsgName, addressGroupIdentifier.Name, err)
		}
	} else {
		// create azure asg corresponding to AG sg.
		cloudAsgName := addressGroupIdentifier.GetCloudName(true)
		cloudSecurityGroupID, err = createOrGetApplicationSecurityGroup(computeService.asgAPIClient, location, rgName, cloudAsgName)
		if err != nil {
			return nil, fmt.Errorf("azure asg %v create failed for AG sg %v, reason: %w", cloudAsgName, addressGroupIdentifier.Name, err)
		}
	}

	return to.StringPtr(cloudSecurityGroupID), nil
}

func (c *azureCloud) UpdateSecurityGroupRules(addressGroupIdentifier *securitygroup.CloudResourceID,
	ingressRules []*securitygroup.IngressRule, egressRules []*securitygroup.EgressRule) error {
	mutex.Lock()
	defer mutex.Unlock()

	// find account managing the vnet and get compute service config
	vnetID := addressGroupIdentifier.Vpc
	accCfg := c.getVnetAccount(vnetID)
	if accCfg == nil {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)
	location := computeService.credentials.region

	// extract resource-group-name from vnet ID
	_, rgName, _, err := extractFieldsFromAzureResourceID(addressGroupIdentifier.Vpc)
	if err != nil {
		azurePluginLogger().Error(err, "fail to build extract resource-group-name from vnet ID")
		return err
	}

	vnetPeerPairs := computeService.getVnetPeers(vnetID)
	vnetCachedIDs := computeService.getCachedVnetIDs()
	vnetVMs, _ := computeService.getVirtualMachines()
	// ruleIP := vnetVMs[len(vnetVMs)-1].NetworkInterfaces[0].PrivateIps[0]
	// AT sg name per vnet is fixed and predefined. Get azure nsg name for it.
	appliedToSgID := securitygroup.CloudResourceID{
		Name: appliedToSecurityGroupNamePerVnet,
		Vpc:  vnetID,
	}
	tokens := strings.Split(addressGroupIdentifier.Vpc, "/")
	suffix := tokens[len(tokens)-1]
	appliedToGroupPerVnetNsgNepheControllerName := appliedToSgID.GetCloudName(false) + "-" + suffix
	// convert to azure security rules and build effective rules to be applied to AT sg azure NSG
	rules := []network.SecurityRule{}
	flag := 0
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]

		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			var ruleIP *string
			for _, vnetVM := range vnetVMs {
				if *vnetVM.VnetID == vnetID {
					ruleIP = vnetVM.NetworkInterfaces[0].PrivateIps[0]
				}
				flag = 1
				break
			}
			rules, err = computeService.buildEffectivePeerNSGSecurityRulesToApply(addressGroupIdentifier, ingressRules, egressRules,
				appliedToGroupPerVnetNsgNepheControllerName, rgName, ruleIP)
			if err != nil {
				azurePluginLogger().Error(err, "fail to build effective rules to be applied")
				return err
			}
			break
		}
	}
	if flag == 0 {
		rules, err = computeService.buildEffectiveNSGSecurityRulesToApply(addressGroupIdentifier, ingressRules, egressRules,
			appliedToGroupPerVnetNsgNepheControllerName, rgName)
		if err != nil {
			azurePluginLogger().Error(err, "fail to build effective rules to be applied")
			return err
		}
	}
	// update network security group with rules
	err = updateNetworkSecurityGroupRules(computeService.nsgAPIClient, location, rgName, appliedToGroupPerVnetNsgNepheControllerName, rules)
	if err != nil {
		return err
	}
	return nil
}

func (c *azureCloud) UpdateSecurityGroupMembers(addressGroupIdentifier *securitygroup.CloudResourceID,
	computeResourceIdentifier []*securitygroup.CloudResource, membershipOnly bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	vnetID := addressGroupIdentifier.Vpc
	accCfg := c.getVnetAccount(vnetID)
	if accCfg == nil {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)

	return computeService.updateSecurityGroupMembers(addressGroupIdentifier, computeResourceIdentifier, membershipOnly)
}

func (c *azureCloud) DeleteSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	vnetID := addressGroupIdentifier.Vpc
	accCfg := c.getVnetAccount(vnetID)
	if accCfg == nil {
		return fmt.Errorf("azure account not found managing virtual network [%v]", vnetID)
	}
	serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	if err != nil {
		return err
	}
	computeService := serviceCfg.(*computeServiceConfig)
	location := computeService.credentials.region

	_ = computeService.updateSecurityGroupMembers(addressGroupIdentifier, nil, membershipOnly)

	var rgName string
	_, rgName, _, err = extractFieldsFromAzureResourceID(addressGroupIdentifier.Vpc)
	if err != nil {
		return err
	}
	err = computeService.removeReferencesToSecurityGroup(addressGroupIdentifier, rgName, location, membershipOnly)
	if err != nil {
		return err
	}

	var cloudAsgName string
	if isPeer := computeService.ifPeerProcessing(vnetID); isPeer {
		cloudAsgName = addressGroupIdentifier.GetCloudName(false)
	} else {
		cloudAsgName = addressGroupIdentifier.GetCloudName(membershipOnly)
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
				azurePluginLogger().Info("enforced-security-cloud-view GET for account skipped (account no longer exists)", "account", name)
				return
			}

			serviceCfg, err := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
			if err != nil {
				azurePluginLogger().Error(err, "enforced-security-cloud-view GET for account skipped", "account", accCfg.GetNamespacedName())
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
	vnetIDs := computeCfg.getCachedVnetIDs()
	if len(vnetIDs) == 0 {
		return []securitygroup.SynchronizationContent{}
	}

	networkInterfaces, err := computeCfg.getNetworkInterfacesOfVnet(vnetIDs)
	if err != nil {
		return []securitygroup.SynchronizationContent{}
	}

	appliedToSgEnforcedView, antreaATSgNameSet, err := computeCfg.processAndBuildATSgView(networkInterfaces)
	if err != nil {
		return []securitygroup.SynchronizationContent{}
	}

	addressGroupSgEnforcedView, err := computeCfg.processAndBuildAGSgView(networkInterfaces, antreaATSgNameSet)
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
	vnetCachedIDs := computeCfg.getCachedVnetIDs()
	for _, vnetPeerPair := range vnetPeerPairs {
		vnetPeerID, _, _ := vnetPeerPair[0], vnetPeerPair[1], vnetPeerPair[2]
		if _, ok := vnetCachedIDs[vnetPeerID]; ok {
			return true
		}
	}
	return false
}
