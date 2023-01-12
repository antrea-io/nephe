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

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest/to"

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
)

const (
	ruleStartPriority             = 100
	vnetToVnetDenyRulePriority    = 4096
	vnetToVnetDenyRuleDescription = "nephe-at-" + appliedToSecurityGroupNamePerVnet
	emptyPort                     = "*"
	virtualnetworkAddressPrefix   = "VirtualNetwork"
)

var protoNumAzureNameMap = map[int]network.SecurityRuleProtocol{
	1:  network.SecurityRuleProtocolIcmp,
	6:  network.SecurityRuleProtocolTCP,
	17: network.SecurityRuleProtocolUDP,
}

var azureProtoNameToNumMap = map[network.SecurityRuleProtocol]int{
	network.SecurityRuleProtocolIcmp: 1,
	network.SecurityRuleProtocolTCP:  6,
	network.SecurityRuleProtocolUDP:  17,
}

func updateSecurityRuleNameAndPriority(existingRules []network.SecurityRule, newRules []network.SecurityRule) []network.SecurityRule {
	var rules []network.SecurityRule
	defaultRulesByName := make(map[string]network.SecurityRule)

	rulePriority := int32(ruleStartPriority)
	for _, rule := range existingRules {
		if *rule.Priority == vnetToVnetDenyRulePriority {
			defaultRulesByName[*rule.Name] = rule
			continue
		}
		ruleName := fmt.Sprintf("%v-%v", rulePriority, rule.Direction)
		rule.Name = &ruleName
		rule.Priority = to.Int32Ptr(rulePriority)

		rules = append(rules, rule)
		rulePriority++
	}

	for _, rule := range newRules {
		if *rule.Priority == vnetToVnetDenyRulePriority {
			defaultRulesByName[*rule.Name] = rule
			continue
		}
		ruleName := fmt.Sprintf("%v-%v", rulePriority, rule.Direction)
		rule.Name = &ruleName
		rule.Priority = to.Int32Ptr(rulePriority)

		rules = append(rules, rule)
		rulePriority++
	}

	for _, rule := range defaultRulesByName {
		rules = append(rules, rule)
	}

	return rules
}

// convertIngressToNsgSecurityRules converts ingress rules from securitygroup.CloudRule to azure rules.
func convertIngressToNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup) ([]network.SecurityRule, error) {
	var securityRules []network.SecurityRule

	dstAsgObj, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []network.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}

	rulePriority := int32(ruleStartPriority)
	description := appliedToGroupID.GetCloudName(false)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.IngressRule)
		if rule == nil {
			continue
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []network.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, nil, nil, &[]network.ApplicationSecurityGroup{dstAsgObj}, &description,
					network.SecurityRuleAccessAllow)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}

		srcApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
		if srcApplicationSecurityGroups != nil && len(*srcApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
				&srcPort, nil, nil, &[]network.ApplicationSecurityGroup{dstAsgObj}, &description,
				network.SecurityRuleAccessAllow)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), network.SecurityRuleProtocolAsterisk,
		network.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(vnetToVnetDenyRuleDescription),
		network.SecurityRuleAccessDeny)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertIngressToPeerNsgSecurityRules converts ingress rules that require peering from securitygroup.CloudRule to azure rules.
func convertIngressToPeerNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup,
	ruleIP *string) ([]network.SecurityRule, error) {
	var securityRules []network.SecurityRule

	rulePriority := int32(ruleStartPriority)
	description := appliedToGroupID.GetCloudName(false)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.IngressRule)
		if rule == nil {
			continue
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []network.SecurityRule{}, err
		}

		srcPort := convertToAzurePortRange(rule.FromPort)

		if len(rule.FromSrcIP) != 0 || len(rule.FromSecurityGroups) == 0 {
			srcAddrPrefix, srcAddrPrefixes := convertToAzureAddressPrefix(rule.FromSrcIP)
			if srcAddrPrefix != nil || srcAddrPrefixes != nil {
				securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionInbound,
					to.StringPtr(emptyPort), srcAddrPrefix, srcAddrPrefixes, nil,
					&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
					network.SecurityRuleAccessAllow, appliedToGroupID.Name)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}
		flag := 0
		for _, fromSecurityGroup := range rule.FromSecurityGroups {
			if fromSecurityGroup.Vpc == appliedToGroupID.Vpc {
				srcApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.FromSecurityGroups, agAsgMapByNepheControllerName)
				if srcApplicationSecurityGroups != nil && len(*srcApplicationSecurityGroups) != 0 {
					securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionInbound,
						to.StringPtr(emptyPort), nil, nil, srcApplicationSecurityGroups,
						&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
						network.SecurityRuleAccessAllow, appliedToGroupID.Name)
					securityRules = append(securityRules, securityRule)
					rulePriority++
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionInbound,
				to.StringPtr(emptyPort), ruleIP, nil, nil,
				&srcPort, to.StringPtr(emptyPort), nil, nil, &description,
				network.SecurityRuleAccessAllow, appliedToGroupID.Name)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}
	// add vnet to vnet deny all rule
	securityRule := buildPeerSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), network.SecurityRuleProtocolAsterisk,
		network.SecurityRuleDirectionInbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(vnetToVnetDenyRuleDescription),
		network.SecurityRuleAccessDeny, appliedToGroupID.Name)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertEgressToNsgSecurityRules converts egress rules from securitygroup.CloudRule to azure rules.
func convertEgressToNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup,
	atAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup) ([]network.SecurityRule, error) {
	var securityRules []network.SecurityRule

	srcAsgObj, found := atAsgMapByNepheControllerName[strings.ToLower(appliedToGroupID.Name)]
	if !found {
		return []network.SecurityRule{}, fmt.Errorf("asg not found for applied to SG %v", appliedToGroupID.Name)
	}

	rulePriority := int32(ruleStartPriority)
	description := appliedToGroupID.GetCloudName(false)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.EgressRule)
		if rule == nil {
			continue
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []network.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), nil, nil, &[]network.ApplicationSecurityGroup{srcAsgObj},
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, network.SecurityRuleAccessAllow)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}

		dstApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
		if dstApplicationSecurityGroups != nil && len(*dstApplicationSecurityGroups) != 0 {
			securityRule := buildSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), nil, nil, &[]network.ApplicationSecurityGroup{srcAsgObj},
				&dstPort, nil, nil, dstApplicationSecurityGroups, &description, network.SecurityRuleAccessAllow)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), network.SecurityRuleProtocolAsterisk,
		network.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(vnetToVnetDenyRuleDescription),
		network.SecurityRuleAccessDeny)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

// convertEgressToPeerNsgSecurityRules converts egress rules that require peering from securitygroup.CloudRule to azure rules.
func convertEgressToPeerNsgSecurityRules(appliedToGroupID *securitygroup.CloudResourceID, rules []*securitygroup.CloudRule,
	agAsgMapByNepheControllerName map[string]network.ApplicationSecurityGroup,
	ruleIP *string) ([]network.SecurityRule, error) {
	var securityRules []network.SecurityRule

	rulePriority := int32(ruleStartPriority)
	description := appliedToGroupID.GetCloudName(false)
	for _, obj := range rules {
		rule := obj.Rule.(*securitygroup.EgressRule)
		if rule == nil {
			continue
		}
		protoName, err := convertToAzureProtocolName(rule.Protocol)
		if err != nil {
			return []network.SecurityRule{}, err
		}

		dstPort := convertToAzurePortRange(rule.ToPort)

		if len(rule.ToDstIP) != 0 || len(rule.ToSecurityGroups) == 0 {
			dstAddrPrefix, dstAddrPrefixes := convertToAzureAddressPrefix(rule.ToDstIP)
			if dstAddrPrefix != nil || dstAddrPrefixes != nil {
				securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionOutbound,
					to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
					&dstPort, dstAddrPrefix, dstAddrPrefixes, nil, &description, network.SecurityRuleAccessAllow, appliedToGroupID.Name)
				securityRules = append(securityRules, securityRule)
				rulePriority++
			}
		}
		flag := 0
		for _, toSecurityGroup := range rule.ToSecurityGroups {
			if toSecurityGroup.Vpc == appliedToGroupID.Vpc {
				dstApplicationSecurityGroups := convertToAzureApplicationSecurityGroups(rule.ToSecurityGroups, agAsgMapByNepheControllerName)
				if dstApplicationSecurityGroups != nil && len(*dstApplicationSecurityGroups) != 0 {
					securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionOutbound,
						to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
						&dstPort, nil, nil, dstApplicationSecurityGroups, &description, network.SecurityRuleAccessAllow, appliedToGroupID.Name)
					securityRules = append(securityRules, securityRule)
					rulePriority++
					flag = 1
					break
				}
			}
		}
		if flag == 0 {
			securityRule := buildPeerSecurityRule(to.Int32Ptr(rulePriority), protoName, network.SecurityRuleDirectionOutbound,
				to.StringPtr(emptyPort), to.StringPtr(emptyPort), nil, nil,
				&dstPort, ruleIP, nil, nil, &description, network.SecurityRuleAccessAllow, appliedToGroupID.Name)
			securityRules = append(securityRules, securityRule)
			rulePriority++
		}
	}

	// add vnet to vnet deny all rule
	securityRule := buildPeerSecurityRule(to.Int32Ptr(vnetToVnetDenyRulePriority), network.SecurityRuleProtocolAsterisk,
		network.SecurityRuleDirectionOutbound, to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil,
		to.StringPtr(emptyPort), to.StringPtr(virtualnetworkAddressPrefix), nil, nil, to.StringPtr(vnetToVnetDenyRuleDescription),
		network.SecurityRuleAccessDeny, appliedToGroupID.Name)
	securityRules = append(securityRules, securityRule)

	return securityRules, nil
}

func buildSecurityRule(rulePriority *int32, protoName network.SecurityRuleProtocol, direction network.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes *[]string, srcASGs *[]network.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes *[]string, dstASGs *[]network.ApplicationSecurityGroup,
	description *string, access network.SecurityRuleAccess) network.SecurityRule {
	ruleName := fmt.Sprintf("%v-%v", *rulePriority, direction)

	securityRule := network.SecurityRule{
		Name: to.StringPtr(ruleName),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Protocol:                             protoName,
			SourcePortRange:                      srcPort,
			SourceAddressPrefix:                  srcAddrPrefix,
			SourceAddressPrefixes:                srcAddrPrefixes,
			SourceApplicationSecurityGroups:      srcASGs,
			DestinationPortRange:                 dstPort,
			DestinationAddressPrefix:             dstAddrPrefix,
			DestinationAddressPrefixes:           dstAddrPrefixes,
			DestinationApplicationSecurityGroups: dstASGs,
			Access:                               access,
			Priority:                             rulePriority,
			Direction:                            direction,
			Description:                          description,
		},
	}

	return securityRule
}

func buildPeerSecurityRule(rulePriority *int32, protoName network.SecurityRuleProtocol, direction network.SecurityRuleDirection,
	srcPort *string, srcAddrPrefix *string, srcAddrPrefixes *[]string, srcASGs *[]network.ApplicationSecurityGroup,
	dstPort *string, dstAddrPrefix *string, dstAddrPrefixes *[]string, dstASGs *[]network.ApplicationSecurityGroup,
	description *string, access network.SecurityRuleAccess, name string) network.SecurityRule {
	ruleName := fmt.Sprintf("%v-%v", *rulePriority, direction)
	azurePluginLogger().Info("Name of rule", "name", name)
	securityRule := network.SecurityRule{
		Name: to.StringPtr(ruleName),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Protocol:                             protoName,
			SourcePortRange:                      srcPort,
			SourceAddressPrefix:                  srcAddrPrefix,
			SourceAddressPrefixes:                srcAddrPrefixes,
			SourceApplicationSecurityGroups:      srcASGs,
			DestinationPortRange:                 dstPort,
			DestinationAddressPrefix:             dstAddrPrefix,
			DestinationAddressPrefixes:           dstAddrPrefixes,
			DestinationApplicationSecurityGroups: dstASGs,
			Access:                               access,
			Priority:                             rulePriority,
			Direction:                            direction,
			Description:                          description,
		},
	}

	return securityRule
}

func convertToAzureApplicationSecurityGroups(securityGroups []*securitygroup.CloudResourceID,
	asgByNepheControllerName map[string]network.ApplicationSecurityGroup) *[]network.ApplicationSecurityGroup {
	var asgsToReturn []network.ApplicationSecurityGroup
	for _, securityGroup := range securityGroups {
		asg, found := asgByNepheControllerName[strings.ToLower(securityGroup.Name)]
		if !found {
			continue
		}
		asgsToReturn = append(asgsToReturn, asg)
	}

	return &asgsToReturn
}

func convertToAzureProtocolName(protoNum *int) (network.SecurityRuleProtocol, error) {
	if protoNum == nil {
		return network.SecurityRuleProtocolAsterisk, nil
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

func convertToAzureAddressPrefix(ruleIPs []*net.IPNet) (*string, *[]string) {
	var prefixes []string
	for _, ip := range ruleIPs {
		prefixes = append(prefixes, ip.String())
	}

	var addressPrefix *string
	var addressPrefixes *[]string
	if len(prefixes) == 0 {
		addressPrefix = to.StringPtr(emptyPort)
	} else {
		addressPrefixes = &prefixes
	}
	return addressPrefix, addressPrefixes
}

// convertToInternalRulesByAppliedToSGName converts azure rules to securitygroup.IngressRule and securitygroup.EgressRule and split
// them by security group names.
func convertToInternalRulesByAppliedToSGName(azureSecurityRules *[]network.SecurityRule,
	vnetID string) (map[string][]securitygroup.IngressRule, map[string][]securitygroup.EgressRule) {
	nepheControllerATSgNameToIngressRules := make(map[string][]securitygroup.IngressRule)
	nepheControllerATSgNameToEgressRules := make(map[string][]securitygroup.EgressRule)
	for _, azureSecurityRule := range *azureSecurityRules {
		sgName, _, isATSg := securitygroup.IsNepheControllerCreatedSG(*azureSecurityRule.Description)
		if !isATSg {
			continue
		}
		ruleName := azureSecurityRule.Name
		if azureSecurityRule.Direction == network.SecurityRuleDirectionInbound {
			ingressRule, err := convertFromAzureSecurityRuleToInternalIngressRule(azureSecurityRule, vnetID)
			if err != nil {
				azurePluginLogger().Error(err, "failed to convert to ingress rule", "ruleName", ruleName)
				continue
			}
			rules := nepheControllerATSgNameToIngressRules[sgName]
			rules = append(rules, ingressRule...)
			nepheControllerATSgNameToIngressRules[sgName] = rules
		} else {
			egressRule, err := convertFromAzureSecurityRuleToInternalEgressRule(azureSecurityRule, vnetID)
			if err != nil {
				azurePluginLogger().Error(err, "failed to convert to egress rule", "ruleName", ruleName)
				continue
			}
			rules := nepheControllerATSgNameToEgressRules[sgName]
			rules = append(rules, egressRule...)
			nepheControllerATSgNameToEgressRules[sgName] = rules
		}
	}

	return nepheControllerATSgNameToIngressRules, nepheControllerATSgNameToEgressRules
}

// convertFromAzureSecurityRuleToInternalIngressRule converts azure rules to securitygroup.IngressRule.
func convertFromAzureSecurityRuleToInternalIngressRule(rule network.SecurityRule,
	vnetID string) ([]securitygroup.IngressRule, error) {
	ingressList := make([]securitygroup.IngressRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.DestinationPortRange)
	srcIP := convertFromAzurePrefixesToNepheControllerIPs(rule.SourceAddressPrefix, rule.SourceAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.SourceApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Protocol)
	if err != nil {
		return nil, err
	}
	for _, ip := range srcIP {
		ingressRule := securitygroup.IngressRule{
			FromPort:  port,
			FromSrcIP: []*net.IPNet{ip},
			Protocol:  protoNum,
		}
		ingressList = append(ingressList, ingressRule)
	}
	for _, sg := range securityGroups {
		ingressRule := securitygroup.IngressRule{
			FromPort:           port,
			FromSecurityGroups: []*securitygroup.CloudResourceID{sg},
			Protocol:           protoNum,
		}
		ingressList = append(ingressList, ingressRule)
	}

	return ingressList, nil
}

// convertFromAzureSecurityRuleToInternalEgressRule converts azure rules to securitygroup.EgressRule.
func convertFromAzureSecurityRuleToInternalEgressRule(rule network.SecurityRule,
	vnetID string) ([]securitygroup.EgressRule, error) {
	egressList := make([]securitygroup.EgressRule, 0)

	port := convertFromAzurePortToNepheControllerPort(rule.DestinationPortRange)
	dstIP := convertFromAzurePrefixesToNepheControllerIPs(rule.DestinationAddressPrefix, rule.DestinationAddressPrefixes)
	securityGroups := convertFromAzureASGsToNepheControllerSecurityGroups(rule.DestinationApplicationSecurityGroups, vnetID)
	protoNum, err := convertFromAzureProtocolToNepheControllerProtocol(rule.Protocol)
	if err != nil {
		return nil, err
	}

	for _, ip := range dstIP {
		egressRule := securitygroup.EgressRule{
			ToPort:   port,
			ToDstIP:  []*net.IPNet{ip},
			Protocol: protoNum,
		}
		egressList = append(egressList, egressRule)
	}
	for _, sg := range securityGroups {
		egressRule := securitygroup.EgressRule{
			ToPort:           port,
			ToSecurityGroups: []*securitygroup.CloudResourceID{sg},
			Protocol:         protoNum,
		}
		egressList = append(egressList, egressRule)
	}

	return egressList, err
}

func convertFromAzureProtocolToNepheControllerProtocol(azureProtoName network.SecurityRuleProtocol) (*int, error) {
	if azureProtoName == network.SecurityRuleProtocolAsterisk {
		return nil, nil
	}

	protocolNum, found := azureProtoNameToNumMap[azureProtoName]
	if !found {
		return nil, fmt.Errorf("unsupported azure protocol %v", azureProtoName)
	}

	return &protocolNum, nil
}

func convertFromAzureASGsToNepheControllerSecurityGroups(asgs *[]network.ApplicationSecurityGroup,
	vnetID string) []*securitygroup.CloudResourceID {
	var cloudResourceIDs []*securitygroup.CloudResourceID
	if asgs == nil {
		return cloudResourceIDs
	}

	for _, asg := range *asgs {
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

func convertFromAzurePrefixesToNepheControllerIPs(ipPrefix *string, ipPrefixes *[]string) []*net.IPNet {
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

	for _, prefix := range *ipPrefixes {
		_, ipNet, err := net.ParseCIDR(prefix)
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
