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
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/go-autorest/autorest/to"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/securitygroup"
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
	return securitygroup.ControllerPrefix + "-default-deny"
}

// isAzureRuleAttachedToAtSg check if the given Azure security rule is attached to the specified appliedTo sg.
func isAzureRuleAttachedToAtSg(rule *armnetwork.SecurityRule, asg string) bool {
	atSgs := rule.Properties.DestinationApplicationSecurityGroups
	if *rule.Properties.Direction == armnetwork.SecurityRuleDirectionOutbound {
		atSgs = rule.Properties.SourceApplicationSecurityGroups
	}
	for _, atSg := range atSgs {
		_, _, name, _ := extractFieldsFromAzureResourceID(*atSg.ID)
		_, _, isNepheControllerCreatedRule := securitygroup.IsNepheControllerCreatedSG(name)
		if isNepheControllerCreatedRule && strings.Compare(name, asg) == 0 {
			return true
		}
	}
	return false
}

// updateSecurityRuleNameAndPriority updates rule name and priority from existing
// and new security rules and returns all the security rules.
func updateSecurityRuleNameAndPriority(existingRules []armnetwork.SecurityRule,
	newRules []armnetwork.SecurityRule) []armnetwork.SecurityRule {
	var rules []armnetwork.SecurityRule
	defaultAndUserCreatedRules := make([]armnetwork.SecurityRule, 0)

	rulePriority := int32(ruleStartPriority)
	for _, rule := range existingRules {
		if rule.Properties == nil {
			continue
		}
		// skip priority update for user created custom rules outside Nephe priority range.
		if *rule.Properties.Priority < ruleStartPriority {
			defaultAndUserCreatedRules = append(defaultAndUserCreatedRules, rule)
			continue
		}
		ruleName := fmt.Sprintf("%v-%v", rulePriority, *rule.Properties.Direction)
		rule.Name = &ruleName
		rule.Properties.Priority = to.Int32Ptr(rulePriority)

		rules = append(rules, rule)
		rulePriority++
	}

	for _, rule := range newRules {
		if rule.Properties == nil {
			continue
		}
		// skip priority update for default 4096 deny rule. newRules consists of only Nephe rules.
		if *rule.Properties.Priority == vnetToVnetDenyRulePriority {
			defaultAndUserCreatedRules = append(defaultAndUserCreatedRules, rule)
			continue
		}
		ruleName := fmt.Sprintf("%v-%v", rulePriority, *rule.Properties.Direction)
		rule.Name = &ruleName
		rule.Properties.Priority = to.Int32Ptr(rulePriority)

		rules = append(rules, rule)
		rulePriority++
	}

	rules = append(rules, defaultAndUserCreatedRules...)

	return rules
}

// convertIngressToNsgSecurityRules converts ingress rules from securitygroup.CloudRule to azure rules.
func convertIngressToNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) ([]armnetwork.SecurityRule, error) {
	var securityRules []armnetwork.SecurityRule

	dstAsgObj, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []armnetwork.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}

	rulePriority := int32(ruleStartPriority)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.IngressRule)
		if rule == nil {
			continue
		}
		description, err := securitygroup.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []armnetwork.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description,
					armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}

		srcApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
		if len(srcApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
				&srcPort, nil, nil, []*armnetwork.ApplicationSecurityGroup{&dstAsgObj}, &description,
				armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertIngressToPeerNsgSecurityRules converts ingress rules that require peering from securitygroup.CloudRule to azure rules.
func convertIngressToPeerNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	ruleIP *string) ([]armnetwork.SecurityRule, error) {
	var securityRules []armnetwork.SecurityRule

	rulePriority := int32(ruleStartPriority)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.IngressRule)
		if rule == nil {
			continue
		}
		description, err := securitygroup.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []armnetwork.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
					armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}
		flag := 0
		for _, fromSecurityGroup := range rule.FromSecurityGroups {
			if fromSecurityGroup.Vpc == appliedToGroupID.Vpc {
				srcApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
				if len(srcApplicationSecurityGroups) != 0 {
					securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionInbound,
						to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
						&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
						armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
					securityRules = append(securityRules, securityRule)
					rulePriority++
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), ruleIP, nil, nil,
				&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
				armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildPeerSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny, appliedToGroupID.Name)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertEgressToNsgSecurityRules converts egress rules from securitygroup.CloudRule to azure rules.
func convertEgressToNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) ([]armnetwork.SecurityRule, error) {
	var securityRules []armnetwork.SecurityRule

	srcAsgObj, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []armnetwork.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}

	rulePriority := int32(ruleStartPriority)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.EgressRule)
		if rule == nil {
			continue
		}
		description, err := securitygroup.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []armnetwork.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, armnetwork.SecurityRuleAccessAllow)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}

		dstApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
		if len(dstApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), nil, nil, []*armnetwork.ApplicationSecurityGroup{&srcAsgObj},
				&dstPort, nil, nil, dstApplicationSecurityGroups, &description, armnetwork.SecurityRuleAccessAllow)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertEgressToPeerNsgSecurityRules converts egress rules that require peering from securitygroup.CloudRule to azure rules.
func convertEgressToPeerNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup,
	ruleIP *string) ([]armnetwork.SecurityRule, error) {
	var securityRules []armnetwork.SecurityRule

	rulePriority := int32(ruleStartPriority)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.EgressRule)
		if rule == nil {
			continue
		}
		description, err := securitygroup.GenerateCloudDescription(obj.NpNamespacedName)
		if err != nil {
			return []armnetwork.SecurityRule{}, fmt.Errorf("unable to generate rule description, err: %v", err)
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []armnetwork.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}
		flag := 0
		for _, toSecurityGroup := range rule.ToSecurityGroups {
			if toSecurityGroup.Vpc == appliedToGroupID.Vpc {
				dstApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
				if len(dstApplicationSecurityGroups) != 0 {
					securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionOutbound,
						to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
						&dstPort, nil, nil, dstApplicationSecurityGroups, &description, armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
					securityRules = append(securityRules, securityRule)
					rulePriority++
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, armnetwork.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
				&dstPort, ruleIP, nil, nil, &description, armnetwork.SecurityRuleAccessAllow, appliedToGroupID.Name)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildPeerSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), armnetwork.SecurityRuleProtocolAsterisk,
		armnetwork.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(getDefaultDenyRuleName()),
		armnetwork.SecurityRuleAccessDeny, appliedToGroupID.Name)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

func buildSecurityRule(rulePriority *int32, protoName armnetwork.SecurityRuleProtocol, direction armnetwork.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes []*string, srcASGs []*armnetwork.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes []*string, dstASGs []*armnetwork.ApplicationSecurityGroup,
	description *string, access armnetwork.SecurityRuleAccess) armnetwork.SecurityRule {
	ruleName := fmt.Sprintf("%v-%v", *rulePriority, direction)

	securityRule := armnetwork.SecurityRule{
		Name: to.StringPtr(ruleName),
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

	return securityRule
}

func buildPeerSecurityRule(rulePriority *int32, protoName armnetwork.SecurityRuleProtocol, direction armnetwork.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes []*string, srcASGs []*armnetwork.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes []*string, dstASGs []*armnetwork.ApplicationSecurityGroup,
	description *string, access armnetwork.SecurityRuleAccess, name string) armnetwork.SecurityRule {
	ruleName := fmt.Sprintf("%v-%v", *rulePriority, direction)
	azurePluginLogger().Info("Name of rule", "name", name)
	securityRule := armnetwork.SecurityRule{
		Name: to.StringPtr(ruleName),
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

	return securityRule
}

func convertToAzureApplicationSecurityGroups(securityGroups []*securitygroup.CloudResourceID,
	asgByNepheControllerName map[string]armnetwork.ApplicationSecurityGroup) []*armnetwork.ApplicationSecurityGroup {
	var asgsToReturn []*armnetwork.ApplicationSecurityGroup
	for _, securityGroup := range securityGroups {
		asg, found := asgByNepheControllerName[strings.ToLower(securityGroup.Name)]
		if !found {
			continue
		}
		asgsToReturn = append(asgsToReturn, &asg)
	}

	return asgsToReturn
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
func convertToCloudRulesByAppliedToSGName(azureSecurityRules []*armnetwork.SecurityRule,
	vnetID string) (map[string][]securitygroup.CloudRule, map[string][]securitygroup.CloudRule) {
	nepheControllerATSgNameToIngressRules := make(map[string][]securitygroup.CloudRule)
	nepheControllerATSgNameToEgressRules := make(map[string][]securitygroup.CloudRule)
	for _, azureSecurityRule := range azureSecurityRules {
		if azureSecurityRule.Properties == nil {
			continue
		}

		desc, _ := securitygroup.ExtractCloudDescription(azureSecurityRule.Properties.Description)

		// Nephe inbound rule implies destination is AT sg. Nephe outbound rule implies source is AT sg.
		// we don't care about syncing rules that doesn't match above pattern, as they will not conflict with any Nephe rules.
		if *azureSecurityRule.Properties.Direction == armnetwork.SecurityRuleDirectionInbound {
			for _, asg := range azureSecurityRule.Properties.DestinationApplicationSecurityGroups {
				_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
				if err != nil {
					azurePluginLogger().Error(err, "failed to extract asg name from resource id", "id", *asg.ID)
				}
				sgName, _, isATSg := securitygroup.IsNepheControllerCreatedSG(asgName)
				if !isATSg {
					continue
				}
				sgID := securitygroup.CloudResourceID{
					Name: sgName,
					Vpc:  vnetID,
				}
				ingressRule, err := convertFromAzureIngressSecurityRuleToCloudRule(*azureSecurityRule, sgID.String(), vnetID, desc)
				if err != nil {
					azurePluginLogger().Error(err, "failed to convert to ingress cloud rule", "ruleName", azureSecurityRule.Name)
					continue
				}
				rules := nepheControllerATSgNameToIngressRules[sgName]
				rules = append(rules, ingressRule...)
				nepheControllerATSgNameToIngressRules[sgName] = rules
			}
		} else {
			for _, asg := range azureSecurityRule.Properties.SourceApplicationSecurityGroups {
				_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
				if err != nil {
					azurePluginLogger().Error(err, "failed to extract asg name from resource id", "id", *asg.ID)
				}
				sgName, _, isATSg := securitygroup.IsNepheControllerCreatedSG(asgName)
				if !isATSg {
					continue
				}
				sgID := securitygroup.CloudResourceID{
					Name: sgName,
					Vpc:  vnetID,
				}
				egressRule, err := convertFromAzureEgressSecurityRuleToCloudRule(*azureSecurityRule, sgID.String(), vnetID, desc)
				if err != nil {
					azurePluginLogger().Error(err, "failed to convert to egress cloud rule", "ruleName", azureSecurityRule.Name)
					continue
				}
				rules := nepheControllerATSgNameToEgressRules[sgName]
				rules = append(rules, egressRule...)
				nepheControllerATSgNameToEgressRules[sgName] = rules
			}
		}
	}

	return nepheControllerATSgNameToIngressRules, nepheControllerATSgNameToEgressRules
}

// convertFromAzureIngressSecurityRuleToCloudRule converts Azure ingress rules from armnetwork.SecurityRule to securitygroup.CloudRule.
func convertFromAzureIngressSecurityRuleToCloudRule(rule armnetwork.SecurityRule, sgID, vnetID string,
	desc *securitygroup.CloudRuleDescription) ([]securitygroup.CloudRule, error) {
	ingressList := make([]securitygroup.CloudRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.Properties.DestinationPortRange)
	srcIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.SourceAddressPrefix, rule.Properties.SourceAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.SourceApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}

	// special handling for Azure any rule, where both srcIP and securityGroups will be empty.
	if len(srcIP) == 0 && len(securityGroups) == 0 {
		_, ipNet, _ := net.ParseCIDR("0.0.0.0/0")
		srcIP = append(srcIP, ipNet)
	}

	for _, ip := range srcIP {
		ingressRule := securitygroup.CloudRule{
			Rule: &securitygroup.IngressRule{
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
		ingressRule := securitygroup.CloudRule{
			Rule: &securitygroup.IngressRule{
				FromPort:           port,
				FromSecurityGroups: []*securitygroup.CloudResourceID{sg},
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
	desc *securitygroup.CloudRuleDescription) ([]securitygroup.CloudRule, error) {
	egressList := make([]securitygroup.CloudRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.Properties.DestinationPortRange)
	dstIP := convertFromAzurePrefixesToNepheControllerIPs(rule.Properties.DestinationAddressPrefix, rule.Properties.DestinationAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.Properties.DestinationApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Properties.Protocol)
	if err != nil {
		return nil, err
	}

	// special handling for Azure any rule, where both dstIP and securityGroups will be empty.
	if len(dstIP) == 0 && len(securityGroups) == 0 {
		_, ipNet, _ := net.ParseCIDR("0.0.0.0/0")
		dstIP = append(dstIP, ipNet)
	}

	for _, ip := range dstIP {
		egressRule := securitygroup.CloudRule{
			Rule: &securitygroup.EgressRule{
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
		egressRule := securitygroup.CloudRule{
			Rule: &securitygroup.EgressRule{
				ToPort:           port,
				ToSecurityGroups: []*securitygroup.CloudResourceID{sg},
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
	vnetID string) []*securitygroup.CloudResourceID {
	var cloudResourceIDs []*securitygroup.CloudResourceID
	if asgs == nil {
		return cloudResourceIDs
	}

	for _, asg := range asgs {
		_, _, asgName, err := extractFieldsFromAzureResourceID(*asg.ID)
		if err != nil {
			continue
		}
		sgName, isNepheControllerCreatedAG, _ := securitygroup.IsNepheControllerCreatedSG(asgName)
		if !isNepheControllerCreatedAG {
			continue
		}
		cloudResourceID := &securitygroup.CloudResourceID{
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
