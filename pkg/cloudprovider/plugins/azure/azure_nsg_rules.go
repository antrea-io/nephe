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

	antreacrdv1beta1 "antrea.io/antrea/pkg/apis/crd/v1beta1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
)

const (
	// Nephe rule priority range is 2000 to 4096.
	ruleStartPriority          = 2000
	vnetToVnetDenyRulePriority = 4096
	emptyPort                  = "*"
)

var protoNumAzureNameMap = map[int]armnetwork.SecurityRuleProtocol{
	cloudresource.IcmpProtocol: armnetwork.SecurityRuleProtocolIcmp,
	cloudresource.TcpProtocol:  armnetwork.SecurityRuleProtocolTCP,
	cloudresource.UdpProtocol:  armnetwork.SecurityRuleProtocolUDP,
}

var azureProtoNameToNumMap = map[string]int{
	strings.ToLower(string(armnetwork.SecurityRuleProtocolIcmp)): cloudresource.IcmpProtocol,
	strings.ToLower(string(armnetwork.SecurityRuleProtocolTCP)):  cloudresource.TcpProtocol,
	strings.ToLower(string(armnetwork.SecurityRuleProtocolUDP)):  cloudresource.UdpProtocol,
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

// updateSecurityRuleNameAndPriority updates rule name and priority for new security rules based on existing security rules
// and returns them combined.
func updateSecurityRuleNameAndPriority(existingRules []*armnetwork.SecurityRule,
	newRules []*armnetwork.SecurityRule) []*armnetwork.SecurityRule {
	var rules []*armnetwork.SecurityRule
	rulePriority := int32(ruleStartPriority)
	i := 0
	for _, rule := range existingRules {
		if rule.Properties == nil {
			continue
		}
		desc, ok := utils.ExtractCloudDescription(rule.Properties.Description)
		if !ok {
			// user rule.
			rules = append(rules, rule)
			continue
		}
		currRulePriority := desc.Priority
		for ; i < len(newRules); i++ {
			if newRules[i] == nil {
				continue
			}
			newRuleDesc, _ := utils.ExtractCloudDescription(newRules[i].Properties.Description)
			if newRuleDesc.Priority != nil && currRulePriority != nil && *newRuleDesc.Priority <= *currRulePriority {
				newRules[i].Properties.Priority = to.Int32Ptr(rulePriority)
				rules = append(rules, newRules[i])
				rulePriority++
			} else {
				break
			}
		}

		rule.Properties.Priority = to.Int32Ptr(rulePriority)
		rules = append(rules, rule)
		rulePriority++
	}

	for ; i < len(newRules); i++ {
		if newRules[i] == nil {
			continue
		}
		// update priority for new rules.
		newRules[i].Properties.Priority = to.Int32Ptr(rulePriority)
		rules = append(rules, newRules[i])
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
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName, rule.Priority)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort, dstPort := convertToAzurePortRange(rule.FromPort, rule.IcmpType, rule.IcmpCode)
		action := armnetwork.SecurityRuleAccessAllow
		if rule.Action != nil && *rule.Action == antreacrdv1beta1.RuleActionDrop {
			action = armnetwork.SecurityRuleAccessDeny
		}

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionInbound,
					&srcPort, srcAddrPrefix, srcAddrPrefixes, nil,
					&dstPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description, action)
				securityRules = append(securityRules, &securityRule)
			}
		}

		srcApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}
		if len(srcApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionInbound,
				&srcPort, nil, nil, srcApplicationSecurityGroups,
				&dstPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description, action)
			securityRules = append(securityRules, &securityRule)
		}
	}

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
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName, rule.Priority)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort, dstPort := convertToAzurePortRange(rule.FromPort, rule.IcmpType, rule.IcmpCode)
		action := armnetwork.SecurityRuleAccessAllow
		if rule.Action != nil && *rule.Action == antreacrdv1beta1.RuleActionDrop {
			action = armnetwork.SecurityRuleAccessDeny
		}

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, to.StringPtr(emptyPort), nil, nil, &description, action)
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
					securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionInbound,
						&srcPort, nil, nil, srcApplicationSecurityGroups,
						&dstPort, to.StringPtr(emptyPort), nil, nil, &description, action)
					securityRules = append(securityRules, &securityRule)
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionInbound,
				&srcPort, ruleIP, nil, nil,
				&dstPort, to.StringPtr(emptyPort), nil, nil, &description,
				armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, &securityRule)
		}
	}

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
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName, rule.Priority)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort, dstPort := convertToAzurePortRange(rule.ToPort, rule.IcmpType, rule.IcmpCode)
		action := armnetwork.SecurityRuleAccessAllow
		if rule.Action != nil && *rule.Action == antreacrdv1beta1.RuleActionDrop {
			action = armnetwork.SecurityRuleAccessDeny
		}

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionOutbound,
					&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, action)
				securityRules = append(securityRules, &securityRule)
			}
		}

		dstApplicationSecurityGroups, err := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}
		if len(dstApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionOutbound,
				&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
				&dstPort, nil, nil, dstApplicationSecurityGroups, &description, action)
			securityRules = append(securityRules, &securityRule)
		}
	}

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
		description, err := utils.GenerateCloudDescription(obj.NpNamespacedName, rule.Priority)
		if err != nil {
			return []*armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []*armnetwork.SecurityRule{}, err
		}

		srcPort, dstPort := convertToAzurePortRange(rule.ToPort, rule.IcmpType, rule.IcmpCode)
		action := armnetwork.SecurityRuleAccessAllow
		if rule.Action != nil && *rule.Action == antreacrdv1beta1.RuleActionDrop {
			action = armnetwork.SecurityRuleAccessDeny
		}

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionOutbound,
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
					securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionOutbound,
						&srcPort, to.StringPtr(emptyPort), nil, nil,
						&dstPort, nil, nil, dstApplicationSecurityGroups, &description, action)
					securityRules = append(securityRules, &securityRule)
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildSecurityRule(rule.RuleName, protoName, armnetwork.SecurityRuleDirectionOutbound,
				&srcPort, to.StringPtr(emptyPort), nil, nil,
				&dstPort, ruleIP, nil, nil, &description, action)
			securityRules = append(securityRules, &securityRule)
		}
	}

	return securityRules, nil
}

// nolint:whitespace
// suppress whitespace linter to keep the function in a more readable format.
// buildSecurityRule builds Azure security rule with given parameters.
func buildSecurityRule(ruleName string, protoName armnetwork.SecurityRuleProtocol, direction armnetwork.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes []*string, srcASGs []*armnetwork.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes []*string, dstASGs []*armnetwork.ApplicationSecurityGroup,
	description *string, access armnetwork.SecurityRuleAccess) armnetwork.SecurityRule {

	securityRule := armnetwork.SecurityRule{
		Name: &ruleName,
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
			Direction:                            &direction,
			Description:                          description,
		},
	}

	return securityRule
}

// findSecurityRule finds the security rule in the given slice and return the index with boolean indicating found or not.
func findSecurityRule(ruleList []*armnetwork.SecurityRule, rule *armnetwork.SecurityRule) (int, bool) {
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
	normalizedProtocol, ok := protoNumAzureNameMap[normalizedProtocolNum]
	if !ok {
		normalizedProtocol = "*"
	}

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

func convertToAzurePortRange(port *int32, icmpType *int32, icmpCode *int32) (string, string) {
	if port != nil {
		return emptyPort, strconv.Itoa(int(*port))
	}
	if icmpType == nil && icmpCode == nil {
		return emptyPort, emptyPort
	} else if icmpType != nil && icmpCode == nil {
		return strconv.Itoa(int(*icmpType)), emptyPort
	} else if icmpType == nil && icmpCode != nil {
		return emptyPort, strconv.Itoa(int(*icmpCode))
	}
	return strconv.Itoa(int(*icmpType)), strconv.Itoa(int(*icmpCode))
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

// convertToCloudRulesByAppliedToAsgID converts Azure rules to securitygroup.CloudRule and split them by Asg ids.
// It also returns a boolean as the third value indicating whether there are user rules in Nephe priority range or not.
func convertToCloudRulesByAppliedToAsgID(azureSecurityRules []*armnetwork.SecurityRule,
	vnetID string) (map[string][]cloudresource.CloudRule, map[string][]cloudresource.CloudRule, bool) {
	nepheControllerATAsgIDToIngressRules := make(map[string][]cloudresource.CloudRule)
	nepheControllerATAsgIDToEgressRules := make(map[string][]cloudresource.CloudRule)
	removeUserRules := false
	for _, azureSecurityRule := range azureSecurityRules {
		if azureSecurityRule.Properties == nil || *azureSecurityRule.Properties.Priority == vnetToVnetDenyRulePriority {
			continue
		}

		// Nephe inbound rule implies destination is AT sg. Nephe outbound rule implies source is AT sg.
		atAsgs := azureSecurityRule.Properties.DestinationApplicationSecurityGroups
		ruleMap := nepheControllerATAsgIDToIngressRules
		convertFunc := convertFromAzureIngressSecurityRuleToCloudRule
		if *azureSecurityRule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
			atAsgs = azureSecurityRule.Properties.SourceApplicationSecurityGroups
			ruleMap = nepheControllerATAsgIDToEgressRules
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

			rules := ruleMap[*asg.ID]
			rules = append(rules, rule...)
			ruleMap[*asg.ID] = rules
		}
	}

	return nepheControllerATAsgIDToIngressRules, nepheControllerATAsgIDToEgressRules, removeUserRules
}

// convertFromAzureIngressSecurityRuleToCloudRule converts Azure ingress rules from armnetwork.SecurityRule to securitygroup.CloudRule.
func convertFromAzureIngressSecurityRuleToCloudRule(rule armnetwork.SecurityRule, sgID, vnetID string,
	desc *cloudresource.CloudRuleDescription) ([]cloudresource.CloudRule, error) {
	ingressList := make([]cloudresource.CloudRule, 0)

	sPort, dPort := convertFromAzurePortToNepheControllerPort(rule.Properties.SourcePortRange, rule.Properties.DestinationPortRange)
	srcIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.SourceAddressPrefix, rule.Properties.SourceAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.SourceApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}
	action := convertFromAzureActionToNepheControllerAction(*rule.Properties.Access)

	var priority *float64
	var npNamespacedName string
	if desc != nil {
		priority = desc.Priority
		npNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
	}

	for _, ip := range srcIP {
		iRule := &cloudresource.IngressRule{
			FromSrcIP: []*net.IPNet{ip},
			Protocol:  protoNum,
			Priority:  priority,
			Action:    action,
			RuleName:  *rule.Name,
		}
		if iRule.Protocol != nil && *iRule.Protocol == cloudresource.IcmpProtocol {
			iRule.IcmpType = sPort
			iRule.IcmpCode = dPort
		} else {
			iRule.FromPort = dPort
		}

		ingressRule := cloudresource.CloudRule{
			Rule:             iRule,
			AppliedToGrp:     sgID,
			NpNamespacedName: npNamespacedName,
		}
		ingressRule.Hash = ingressRule.GetHash()
		ingressList = append(ingressList, ingressRule)
	}
	for _, sg := range securityGroups {
		iRule := &cloudresource.IngressRule{
			FromSecurityGroups: []*cloudresource.CloudResourceID{sg},
			Protocol:           protoNum,
			Priority:           priority,
			Action:             action,
			RuleName:           *rule.Name,
		}
		if iRule.Protocol != nil && *iRule.Protocol == cloudresource.IcmpProtocol {
			iRule.IcmpType = sPort
			iRule.IcmpCode = dPort
		} else {
			iRule.FromPort = dPort
		}

		ingressRule := cloudresource.CloudRule{
			Rule:             iRule,
			AppliedToGrp:     sgID,
			NpNamespacedName: npNamespacedName,
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

	sPort, dPort := convertFromAzurePortToNepheControllerPort(rule.Properties.SourcePortRange, rule.Properties.DestinationPortRange)
	dstIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.DestinationAddressPrefix, rule.Properties.DestinationAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.DestinationApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}
	action := convertFromAzureActionToNepheControllerAction(*rule.Properties.Access)

	var priority *float64
	var npNamespacedName string
	if desc != nil {
		priority = desc.Priority
		npNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
	}
	for _, ip := range dstIP {
		eRule := &cloudresource.EgressRule{
			ToDstIP:  []*net.IPNet{ip},
			Protocol: protoNum,
			Priority: priority,
			Action:   action,
			RuleName: *rule.Name,
		}
		if eRule.Protocol != nil && *eRule.Protocol == cloudresource.IcmpProtocol {
			eRule.IcmpType = sPort
			eRule.IcmpCode = dPort
		} else {
			eRule.ToPort = dPort
		}

		egressRule := cloudresource.CloudRule{
			Rule:             eRule,
			AppliedToGrp:     sgID,
			NpNamespacedName: npNamespacedName,
		}
		egressRule.Hash = egressRule.GetHash()
		egressList = append(egressList, egressRule)
	}
	for _, sg := range securityGroups {
		eRule := &cloudresource.EgressRule{
			ToSecurityGroups: []*cloudresource.CloudResourceID{sg},
			Protocol:         protoNum,
			Priority:         priority,
			Action:           action,
			RuleName:         *rule.Name,
		}
		if eRule.Protocol != nil && *eRule.Protocol == cloudresource.IcmpProtocol {
			eRule.IcmpType = sPort
			eRule.IcmpCode = dPort
		} else {
			eRule.ToPort = dPort
		}
		egressRule := cloudresource.CloudRule{
			Rule:             eRule,
			AppliedToGrp:     sgID,
			NpNamespacedName: npNamespacedName,
		}
		if desc != nil {
			egressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
		}
		egressRule.Hash = egressRule.GetHash()
		egressList = append(egressList, egressRule)
	}

	return egressList, err
}
func convertFromAzureActionToNepheControllerAction(action armnetwork.SecurityRuleAccess) *antreacrdv1beta1.RuleAction {
	var controllerAction = antreacrdv1beta1.RuleActionAllow
	if action == armnetwork.SecurityRuleAccessDeny {
		controllerAction = antreacrdv1beta1.RuleActionDrop
	}
	return &controllerAction
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

func convertFromAzurePortToNepheControllerPort(sPort, dPort *string) (*int32, *int32) {
	if sPort == nil || dPort == nil {
		return nil, nil
	} else if sPort != nil && dPort == nil {
		if *sPort == emptyPort {
			return nil, nil
		} else {
			portNum, _ := strconv.ParseInt(*sPort, 10, 32)
			return to.Int32Ptr(int32(portNum)), nil
		}
	} else if sPort == nil && dPort != nil {
		if *dPort == emptyPort {
			return nil, nil
		} else {
			portNum, _ := strconv.ParseInt(*dPort, 10, 32)
			return nil, to.Int32Ptr(int32(portNum))
		}
	} else if *sPort == emptyPort && *dPort == emptyPort {
		return nil, nil
	}

	sPortNum, _ := strconv.ParseInt(*sPort, 10, 32)
	dPortNum, _ := strconv.ParseInt(*dPort, 10, 32)
	return to.Int32Ptr(int32(sPortNum)), to.Int32Ptr(int32(dPortNum))
}
