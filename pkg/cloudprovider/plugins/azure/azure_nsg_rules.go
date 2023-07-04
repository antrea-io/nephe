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
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
)

const (
	// Nephe rule priority range is 2000 to 4096.
	ruleStartPriority           = 2000
	vnetToVnetDenyRulePriority  = 4096
	emptyPort                   = "*"
	virtualnetworkAddressPrefix = "VirtualNetwork"
)

var protoNumAzureNameMap = map[int]armnetwork.SecurityRuleProtocol{
	1:  armnetwork.SecurityRuleProtocolIcmp,
	6:  armnetwork.SecurityRuleProtocolTCP,
	17: armnetwork.SecurityRuleProtocolUDP,
}

var azureProtoNameToNumMap = map[string]int{
	strings.ToLower(string(armnetwork.SecurityRuleProtocolIcmp)): 1,
	strings.ToLower(string(armnetwork.SecurityRuleProtocolTCP)):  6,
	strings.ToLower(string(armnetwork.SecurityRuleProtocolUDP)):  17,
}

func getDefaultDenyRuleName() string {
	return cloudresource.ControllerPrefix + "-default-deny"
}

// isAzureRuleAttachedToAtSg check if the given Azure security rule is attached to the specified appliedTo sg.
func isAzureRuleAttachedToAtSg(rule *armnetwork.SecurityRule, asg string) bool {
	atSgs := rule.Properties.DestinationApplicationSecurityGroups
	if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
		atSgs = rule.Properties.SourceApplicationSecurityGroups
	}
	for _, atSg := range atSgs {
		_, _, name, _ := extractFieldsFromAzureResourceID(*atSg.ID)
		_, _, isNepheControllerCreatedRule := utils.IsNepheControllerCreatedSG(name)
		if isNepheControllerCreatedRule && strings.Compare(name, asg) == 0 {
			return true
		}
	}
	return false
}

// getEmptyCloudRule returns a *securitygroup.CloudRule object with valid fields based on sync content, except nil for the rule field.
// If no valid options are available, nil is returned.
func getEmptyCloudRule(syncContent *cloudresource.SynchronizationContent) *cloudresource.CloudRule {
	for _, rule := range append(syncContent.IngressRules, syncContent.EgressRules...) {
		if rule.NpNamespacedName != "" {
			emptyRule := &cloudresource.CloudRule{
				NpNamespacedName: rule.NpNamespacedName,
				AppliedToGrp:     rule.AppliedToGrp,
			}
			emptyRule.Hash = emptyRule.GetHash()
			return emptyRule
		}
	}
	return nil
}

// getUnusedPriority finds and returns the first unused priority starting from startPriority.
func getUnusedPriority(existingRulePriority map[int32]struct{}, startPriority int32) int32 {
	_, ok := existingRulePriority[startPriority]
	for ok {
		startPriority++
		_, ok = existingRulePriority[startPriority]
	}
	return startPriority
}

// updateSecurityRuleNameAndPriority updates rule name and priority for new security rules based on existing security rules
// and returns them combined.
func updateSecurityRuleNameAndPriority(existingRules []*armnetwork.SecurityRule,
	newRules []*armnetwork.SecurityRule) []*armnetwork.SecurityRule {
	var rules []*armnetwork.SecurityRule
	existingRulePriority := make(map[int32]struct{})
	rulePriority := int32(ruleStartPriority)

	for _, rule := range existingRules {
		if rule.Properties == nil {
			continue
		}
		// record priority for existing rules in Nephe priority range.
		if *rule.Properties.Priority >= ruleStartPriority {
			existingRulePriority[*rule.Properties.Priority] = struct{}{}
		}
		rules = append(rules, rule)
	}

	for _, rule := range newRules {
		if rule.Properties == nil {
			continue
		}
		// skip priority update for default 4096 deny rule. newRules consists of only Nephe rules.
		if rule.Properties.Priority != nil && *rule.Properties.Priority == vnetToVnetDenyRulePriority {
			rules = append(rules, rule)
			continue
		}

		// update priority for new rules.
		rulePriority = getUnusedPriority(existingRulePriority, rulePriority)
		rule.Properties.Priority = to.Int32Ptr(rulePriority)
		ruleName := fmt.Sprintf("%v-%v", rulePriority, *rule.Properties.Direction)
		rule.Name = &ruleName

		rules = append(rules, rule)
		rulePriority++
	}

	return rules
}

// convertIngressToNsgSecurityRules converts ingress rules from securitygroup.CloudRule to azure rules.
func convertIngressToNsgSecurityRules(appliedToGroupID *cloudresource.CloudResourceID, rules []*cloudresource.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) ([]*armnetwork.SecurityRule, error) {
	var securityRules []*armnetwork.SecurityRule

	asg, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []*armnetwork.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}
	// only resource id is needed. other fields are ignored to match Azure returned format so rules can be compared.
	dstAsgObj := armnetwork.ApplicationSecurityGroup{ID: asg.ID}

	for _, obj := range rules {
		rule := obj.Rule.(*cloudresource.IngressRule)
		if rule == nil {
			continue
		}
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description,
					armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, &securityRule)
			}
		}

		srcApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}
		if len(srcApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
				&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description,
				armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, &securityRule)
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, &securityRule)

	return securityRules, nil
}

// convertIngressToPeerNsgSecurityRules converts ingress rules that require peering from securitygroup.CloudRule to azure rules.
func convertIngressToPeerNsgSecurityRules(appliedToGroupID *cloudresource.CloudResourceID, rules []*cloudresource.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	ruleIP *string) ([]*armnetwork.SecurityRule, error) {
	var securityRules []*armnetwork.SecurityRule

	for _, obj := range rules {
		rule := obj.Rule.(*cloudresource.IngressRule)
		if rule == nil {
			continue
		}
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
					armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, &securityRule)
			}
		}
		flag := 0
		for _, fromSecurityGroup := range rule.FromSecurityGroups {
			if fromSecurityGroup.Vpc == appliedToGroupID.Vpc {
				srcApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
				if err != nil {
					return []*armnetwork.SecurityRule{}, err
				}
				if len(srcApplicationSecurityGroups) != 0 {
					securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionInbound,
						to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
						&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
						armnetwork.SecurityRuleAccessAllow)
					securityRules = append(securityRules, &securityRule)
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), ruleIP, nil, nil,
				&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
				armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, &securityRule)
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, &securityRule)

	return securityRules, nil
}

// convertEgressToNsgSecurityRules converts egress rules from securitygroup.CloudRule to azure rules.
func convertEgressToNsgSecurityRules(appliedToGroupID *cloudresource.CloudResourceID, rules []*cloudresource.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) ([]*armnetwork.SecurityRule, error) {
	var securityRules []*armnetwork.SecurityRule

	asg, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []*armnetwork.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}
	// only resource id is needed. other fields are ignored to match Azure returned format so rules can be compared.
	srcAsgObj := armnetwork.ApplicationSecurityGroup{ID: asg.ID}

	for _, obj := range rules {
		rule := obj.Rule.(*cloudresource.EgressRule)
		if rule == nil {
			continue
		}
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, &securityRule)
			}
		}

		dstApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}
		if len(dstApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
				&dstPort, nil, nil, dstApplicationSecurityGroups, &description, armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, &securityRule)
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, &securityRule)

	return securityRules, nil
}

// convertEgressToPeerNsgSecurityRules converts egress rules that require peering from securitygroup.CloudRule to azure rules.
func convertEgressToPeerNsgSecurityRules(appliedToGroupID *cloudresource.CloudResourceID, rules []*cloudresource.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	ruleIP *string) ([]*armnetwork.SecurityRule, error) {
	var securityRules []*armnetwork.SecurityRule

	for _, obj := range rules {
		rule := obj.Rule.(*cloudresource.EgressRule)
		if rule == nil {
			continue
		}
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, &securityRule)
			}
		}
		flag := 0
		for _, toSecurityGroup := range rule.ToSecurityGroups {
			if toSecurityGroup.Vpc == appliedToGroupID.Vpc {
				dstApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
				if err != nil {
					return []*armnetwork.SecurityRule{}, err
				}
				if len(dstApplicationSecurityGroups) != 0 {
					securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionOutbound,
						to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
						&dstPort, nil, nil, dstApplicationSecurityGroups, &description, armnetwork.SecurityRuleAccessAllow)
					securityRules = append(securityRules, &securityRule)
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildSecurityRule(nil, protoName, armnetwork.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
				&dstPort, ruleIP, nil, nil, &description, armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, &securityRule)
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, &securityRule)

	return securityRules, nil
}

// nolint:whitespace
// suppress whitespace linter to keep the function in a more readable format.
// buildSecurityRule builds Azure security rule with given parameters.
func buildSecurityRule(rulePriority *int32, protoName armnetwork.SecurityRuleProtocol, direction armnetwork.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes []*string, srcASGs []*armnetwork.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes []*string, dstASGs []*armnetwork.ApplicationSecurityGroup,
	description *string, access armnetwork.SecurityRuleAccess) armnetwork.SecurityRule {

	securityRule := armnetwork.SecurityRule{
		Properties: &armnetwork.SecurityRulePropertiesFormat{
			Protocol:                             &protoName,
			SourcePortRange:                      srcPort,
			SourceAddressPrefix:                  srcAddrPrefix,
			SourceAddressPrefixes:                srcAddrPrefixes,
			SourceApplicationSecurityGroups:      srcASGs,
			DestinationPortRange:                 dstPort,
			DestinationAddressPrefix:             dstAddrPrefix,
			DestinationAddressPrefixes:           dstAddrPrefixes,
			DestinationApplicationSecurityGroups: dstASGs,
			Access:                               &access,
			Priority:                             rulePriority,
			Direction:                            &direction,
			Description:                          description,
		},
	}

	if rulePriority != nil {
		securityRule.Name = to.StringPtr(fmt.Sprintf("%v-%v", *rulePriority, direction))
	}

	return securityRule
}

// findSecurityRule finds the security rule in the given slice and return the index with boolean indicating found or not.
func findSecurityRule(rule *armnetwork.SecurityRule, ruleList []*armnetwork.SecurityRule) (int, bool) {
	for idx, newRule := range ruleList {
		if newRule != nil && reflect.DeepEqual(rule.Properties, newRule.Properties) {
			return idx, true
		}
	}
	return -1, false
}

// normalizeAzureSecurityRule normalizes and ignores certain Azure rule properties, allowing easy comparison with Nephe rules.
func normalizeAzureSecurityRule(rule *armnetwork.SecurityRule) *armnetwork.SecurityRule {
	property := *rule.Properties
	normalizedProtocolNum := azureProtoNameToNumMap[strings.ToLower(string(*rule.Properties.Protocol))]
	normalizedProtocol := protoNumAzureNameMap[normalizedProtocolNum]

	property.Protocol = &normalizedProtocol
	property.Priority = nil
	property.ProvisioningState = nil
	property.SourcePortRanges = nil
	property.DestinationPortRanges = nil
	if len(property.SourceAddressPrefixes) == 0 {
		property.SourceAddressPrefixes = nil
	}
	if len(property.DestinationAddressPrefixes) == 0 {
		property.DestinationAddressPrefixes = nil
	}

	return &armnetwork.SecurityRule{Properties: &property}
}

// convertToAzureApplicationSecurityGroups converts Nephe security groups to Azure Asgs based on sg cloud resource id.
func convertToAzureApplicationSecurityGroups(securityGroups []*cloudresource.CloudResourceID,
	asgByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) ([]*armnetwork.ApplicationSecurityGroup, error) {
	var asgsToReturn []*armnetwork.ApplicationSecurityGroup
	for _, securityGroup := range securityGroups {
		asg, found := asgByNepheControllerName[strings.ToLower(securityGroup.Name)]
		if !found {
			return nil, fmt.Errorf("asg not found for sg %s", securityGroup.Name)
		}
		newAsg := armnetwork.ApplicationSecurityGroup{ID: asg.ID}
		asgsToReturn = append(asgsToReturn, &newAsg)
	}

	return asgsToReturn, nil
}

func convertToAzureProtocolName(protoNum *int) (armnetwork.SecurityRuleProtocol, error) {
	if protoNum == nil {
		return armnetwork.SecurityRuleProtocolAsterisk, nil
	}

	protocolName, found := protoNumAzureNameMap[*protoNum]
	if !found {
		return "", fmt.Errorf("unsupported protocol number %v", *protoNum)
	}

	return protocolName, nil
}

func convertToAzurePortRange(port *int) string {
	if port == nil {
		return emptyPort
	}
	return strconv.Itoa(*port)
}

func convertToAzureAddressPrefix(ruleIPs []*net.IPNet) (*string, []*string) {
	var prefixes []*string
	for _, ip := range ruleIPs {
		ipStr := ip.String()
		prefixes = append(prefixes, &ipStr)
	}

	var addressPrefix *string
	var addressPrefixes []*string
	if len(prefixes) == 0 {
		addressPrefix = to.StringPtr(emptyPort)
	} else {
		addressPrefixes = prefixes
	}
	return addressPrefix, addressPrefixes
}

// convertToCloudRulesByAppliedToSGName converts Azure rules to securitygroup.CloudRule and split them by security group names.
// It also returns a boolean as the third value indicating whether there are user rules in Nephe priority range or not.
func convertToCloudRulesByAppliedToSGName(azureSecurityRules []*armnetwork.SecurityRule,
	vnetID string) (map[string][]cloudresource.CloudRule, map[string][]cloudresource.CloudRule, bool) {
	nepheControllerATSgNameToIngressRules := make(map[string][]cloudresource.CloudRule)
	nepheControllerATSgNameToEgressRules := make(map[string][]cloudresource.CloudRule)
	removeUserRules := false
	for _, azureSecurityRule := range azureSecurityRules {
		if azureSecurityRule.Properties == nil || *azureSecurityRule.Properties.Priority == vnetToVnetDenyRulePriority {
			continue
		}

		// Nephe inbound rule implies destination is AT sg. Nephe outbound rule implies source is AT sg.
		atAsgs := azureSecurityRule.Properties.DestinationApplicationSecurityGroups
		ruleMap := nepheControllerATSgNameToIngressRules
		convertFunc := convertFromAzureIngressSecurityRuleToCloudRule
		if *azureSecurityRule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
			atAsgs = azureSecurityRule.Properties.SourceApplicationSecurityGroups
			ruleMap = nepheControllerATSgNameToEgressRules
			convertFunc = convertFromAzureEgressSecurityRuleToCloudRule
		}
		isInNephePriorityRange := *azureSecurityRule.Properties.Priority >= ruleStartPriority

		// Nephe rule has correct description.
		desc, ok := utils.ExtractCloudDescription(azureSecurityRule.Properties.Description)
		if !ok {
			removeUserRules = removeUserRules || isInNephePriorityRange
			// Skip converting user rule that is in Nephe priority range, as they will be removed.
			if isInNephePriorityRange {
				continue
			}
		}

		// Nephe rule has AT sg. We skip syncing rules that doesn't have a Nephe AT sg, as they will not conflict with any Nephe rules.
		if len(atAsgs) == 0 {
			removeUserRules = removeUserRules || isInNephePriorityRange
			continue
		}
		for _, asg := range atAsgs {
			// Nephe rule has the correct AT sg naming format.
			_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
			if err != nil {
				azurePluginLogger().Error(err, "failed to extract asg name from resource id", "id", *asg.ID)
				removeUserRules = removeUserRules || isInNephePriorityRange
				continue
			}
			sgName, _, isATSg := utils.IsNepheControllerCreatedSG(asgName)
			if !isATSg {
				removeUserRules = removeUserRules || isInNephePriorityRange
				continue
			}

			sgID := cloudresource.CloudResourceID{
				Name: sgName,
				Vpc:  vnetID,
			}

			rule, err := convertFunc(*azureSecurityRule, sgID.String(), vnetID, desc)
			if err != nil {
				azurePluginLogger().Error(err, "failed to convert to cloud rule",
					"direction", azureSecurityRule.Properties.Direction, "ruleName", azureSecurityRule.Name)
				removeUserRules = removeUserRules || isInNephePriorityRange
				continue
			}

			rules := ruleMap[sgName]
			rules = append(rules, rule...)
			ruleMap[sgName] = rules
		}
	}

	return nepheControllerATSgNameToIngressRules, nepheControllerATSgNameToEgressRules, removeUserRules
}

// convertFromAzureIngressSecurityRuleToCloudRule converts Azure ingress rules from armnetwork.SecurityRule to securitygroup.CloudRule.
func convertFromAzureIngressSecurityRuleToCloudRule(rule armnetwork.SecurityRule, sgID, vnetID string,
	desc *cloudresource.CloudRuleDescription) ([]cloudresource.CloudRule, error) {
	ingressList := make([]cloudresource.CloudRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.Properties.DestinationPortRange)
	srcIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.SourceAddressPrefix, rule.Properties.SourceAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.SourceApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}

	for _, ip := range srcIP {
		ingressRule := cloudresource.CloudRule{
			Rule: &cloudresource.IngressRule{
				FromPort:  port,
				FromSrcIP: []*net.IPNet{ip},
				Protocol:  protoNum,
			},
			AppliedToGrp: sgID,
		}
		if desc != nil {
			ingressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
		}
		ingressRule.Hash = ingressRule.GetHash()
		ingressList = append(ingressList, ingressRule)
	}
	for _, sg := range securityGroups {
		ingressRule := cloudresource.CloudRule{
			Rule: &cloudresource.IngressRule{
				FromPort:           port,
				FromSecurityGroups: []*cloudresource.CloudResourceID{sg},
				Protocol:           protoNum,
			},
			AppliedToGrp: sgID,
		}
		if desc != nil {
			ingressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
		}
		ingressRule.Hash = ingressRule.GetHash()
		ingressList = append(ingressList, ingressRule)
	}

	return ingressList, nil
}

// convertFromAzureEgressSecurityRuleToCloudRule converts Azure egress rules from armnetwork.SecurityRule to securitygroup.CloudRule.
func convertFromAzureEgressSecurityRuleToCloudRule(rule armnetwork.SecurityRule, sgID, vnetID string,
	desc *cloudresource.CloudRuleDescription) ([]cloudresource.CloudRule, error) {
	egressList := make([]cloudresource.CloudRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.Properties.DestinationPortRange)
	dstIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.DestinationAddressPrefix, rule.Properties.DestinationAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.DestinationApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}

	for _, ip := range dstIP {
		egressRule := cloudresource.CloudRule{
			Rule: &cloudresource.EgressRule{
				ToPort:   port,
				ToDstIP:  []*net.IPNet{ip},
				Protocol: protoNum,
			},
			AppliedToGrp: sgID,
		}
		if desc != nil {
			egressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
		}
		egressRule.Hash = egressRule.GetHash()
		egressList = append(egressList, egressRule)
	}
	for _, sg := range securityGroups {
		egressRule := cloudresource.CloudRule{
			Rule: &cloudresource.EgressRule{
				ToPort:           port,
				ToSecurityGroups: []*cloudresource.CloudResourceID{sg},
				Protocol:         protoNum,
			},
			AppliedToGrp: sgID,
		}
		if desc != nil {
			egressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
		}
		egressRule.Hash = egressRule.GetHash()
		egressList = append(egressList, egressRule)
	}

	return egressList, err
}

func convertFromAzureProtocolToNepheControllerProtocol(azureProtoName *armnetwork.SecurityRuleProtocol) (*int, error) {
	if *azureProtoName == armnetwork.SecurityRuleProtocolAsterisk {
		return nil, nil
	}

	// out-of-band modify on Azure rules will cause the protocol name change, not matching Azure own constants, so lowercase is needed.
	// e.g. Tcp will change to TCP after out-of-band modify, even if protocol is not touched at all.
	protocolNum, found := azureProtoNameToNumMap[strings.ToLower(string(*azureProtoName))]
	if !found {
		return nil, fmt.Errorf("unsupported azure protocol %v", *azureProtoName)
	}

	return &protocolNum, nil
}

func convertFromAzureASGsToNepheControllerSecurityGroups(asgs []*armnetwork.ApplicationSecurityGroup,
	vnetID string) []*cloudresource.CloudResourceID {
	var cloudResourceIDs []*cloudresource.CloudResourceID
	if asgs == nil {
		return cloudResourceIDs
	}

	for _, asg := range asgs {
		_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
		if err != nil {
			continue
		}
		sgName, isNepheControllerCreatedAG, _ := utils.IsNepheControllerCreatedSG(asgName)
		if !isNepheControllerCreatedAG {
			continue
		}
		cloudResourceID := &cloudresource.CloudResourceID{
			Name: sgName,
			Vpc:  vnetID,
		}
		cloudResourceIDs = append(cloudResourceIDs, cloudResourceID)
	}
	return cloudResourceIDs
}

func convertFromAzurePrefixesToNepheControllerIPs(ipPrefix *string, ipPrefixes []*string) []*net.IPNet {
	if ipPrefix != nil && *ipPrefix == emptyPort {
		return nil
	}

	var ipNetList []*net.IPNet
	if ipPrefix != nil {
		_, ipNet, err := net.ParseCIDR(*ipPrefix)
		if err == nil {
			ipNetList = append(ipNetList, ipNet)
		}
	}

	for _, prefix := range ipPrefixes {
		_, ipNet, err := net.ParseCIDR(*prefix)
		if err != nil {
			continue
		}
		ipNetList = append(ipNetList, ipNet)
	}

	return ipNetList
}

func convertFromAzurePortToNepheControllerPort(port *string) *int {
	if port == nil || *port == emptyPort {
		return nil
	}
	portNum, err := strconv.ParseInt(*port, 10, 32)
	if err != nil {
		return nil
	}
	return to.IntPtr(int(portNum))
}
