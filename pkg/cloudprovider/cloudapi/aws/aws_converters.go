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
	"net"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	"antrea.io/nephe/pkg/cloudprovider/securitygroup"
)

func convertToIPPermissionProtocol(protocol *int) *string {
	if protocol == nil {
		return aws.String(awsAnyProtocolValue)
	}
	return aws.String(strconv.FormatInt(int64(*protocol), 10))
}

func convertToIPPermissionPort(port *int, protocol *int) (*int64, *int64) {
	if port == nil {
		// For TCP and UDP, aws expects explicit start and end port numbers (for all ports case)
		if protocol != nil && (*protocol == 6 || *protocol == 17) {
			return aws.Int64(int64(tcpUDPPortStart)), aws.Int64(int64(tcpUDPPortEnd))
		}
		return nil, nil
	}
	portVal := aws.Int64(int64(*port))
	return portVal, portVal
}

func convertToEc2IpRanges(ips []*net.IPNet, ruleHasGroups bool, description *string) []*ec2.IpRange {
	var ipRanges []*ec2.IpRange
	if len(ips) == 0 && !ruleHasGroups {
		ipRange := &ec2.IpRange{
			CidrIp:      aws.String("0.0.0.0/0"),
			Description: description,
		}
		ipRanges = append(ipRanges, ipRange)
		return ipRanges
	}

	for _, ip := range ips {
		ipRange := &ec2.IpRange{
			CidrIp:      aws.String(ip.String()),
			Description: description,
		}
		ipRanges = append(ipRanges, ipRange)
	}
	return ipRanges
}

func convertFromIPRange(ipRanges []*ec2.IpRange) ([]*net.IPNet, []*string) {
	var srcIPNets []*net.IPNet
	var desc []*string
	for _, ipRange := range ipRanges {
		_, ipNet, err := net.ParseCIDR(*ipRange.CidrIp)
		if err != nil {
			continue
		}
		desc = append(desc, ipRange.Description)
		srcIPNets = append(srcIPNets, ipNet)
	}

	return srcIPNets, desc
}

func convertFromSecurityGroupPair(cloudGroups []*ec2.UserIdGroupPair, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) ([]*securitygroup.CloudResourceID, []*string) {
	var cloudResourceIDs []*securitygroup.CloudResourceID
	var desc []*string

	for _, cloudGroup := range cloudGroups {
		var sgName string
		var vpcID string

		managedSgObj, foundInManagedSg := managedSGs[*cloudGroup.GroupId]
		if foundInManagedSg {
			sgName, _, _ = securitygroup.IsNepheControllerCreatedSG(*managedSgObj.GroupName)
			vpcID = *managedSgObj.VpcId
			desc = append(desc, cloudGroup.Description)
		}
		unmanagedSGObj, foundInUnmanagedSg := unmanagedSGs[*cloudGroup.GroupId]
		if foundInUnmanagedSg {
			sgName = *unmanagedSGObj.GroupName
			vpcID = *unmanagedSGObj.VpcId
			desc = append(desc, cloudGroup.Description)
		}

		cloudResourceID := &securitygroup.CloudResourceID{
			Name: sgName,
			Vpc:  vpcID,
		}

		cloudResourceIDs = append(cloudResourceIDs, cloudResourceID)
	}
	return cloudResourceIDs, desc
}

// convertFromIPPermissionToIngressRule converts cloud ingress rules from ec2.IpPermission to internal securitygroup.IngressRule.
// Each AT Sg can have one or more ANPs and an ANP can have one or more rules. Each rule can have a description.
func convertFromIPPermissionToIngressRule(ipPermissions []*ec2.IpPermission, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) []securitygroup.IngressRule {
	var ingressRules []securitygroup.IngressRule
	for _, ipPermission := range ipPermissions {
		fromSrcIPs, desc := convertFromIPRange(ipPermission.IpRanges)
		for i, srcIP := range fromSrcIPs {
			// Get cloud rule description.
			_, ok := securitygroup.ExtractCloudDescription(desc[i])
			if !ok {
				// Ignore rules that don't have a valid description field.
				awsPluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc[i])
				continue
			}
			var ingressRule securitygroup.IngressRule

			ingressRule.FromSrcIP = []*net.IPNet{srcIP}
			ingressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
			ingressRule.FromPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

			ingressRules = append(ingressRules, ingressRule)
		}
		fromSecurityGroups, desc := convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		for i, SecurityGroup := range fromSecurityGroups {
			// Get cloud rule description.
			_, ok := securitygroup.ExtractCloudDescription(desc[i])
			if !ok {
				// Ignore rules that don't have a valid description field.
				awsPluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc[i])
				continue
			}
			var ingressRule securitygroup.IngressRule

			ingressRule.FromSecurityGroups = []*securitygroup.CloudResourceID{SecurityGroup}
			ingressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
			ingressRule.FromPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

			ingressRules = append(ingressRules, ingressRule)
		}
	}
	return ingressRules
}

// convertFromIPPermissionToEgressRule converts cloud egress rules from ec2.IpPermission to internal securitygroup.EgressRule.
// Each AT Sg can have one or more ANPs and an ANP can have one or more rules. Each rule can have a description.
func convertFromIPPermissionToEgressRule(ipPermissions []*ec2.IpPermission, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) []securitygroup.EgressRule {
	var egressRules []securitygroup.EgressRule
	for _, ipPermission := range ipPermissions {
		toDstIPs, desc := convertFromIPRange(ipPermission.IpRanges)
		for i, dstIP := range toDstIPs {
			// Get cloud rule description.
			_, ok := securitygroup.ExtractCloudDescription(desc[i])
			if !ok {
				// Ignore rules that don't have a valid description field.
				awsPluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc[i])
				continue
			}
			var egressRule securitygroup.EgressRule

			egressRule.ToDstIP = []*net.IPNet{dstIP}
			egressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
			egressRule.ToPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

			egressRules = append(egressRules, egressRule)
		}
		toSecurityGroups, desc := convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		for i, SecurityGroup := range toSecurityGroups {
			// Get cloud rule description.
			_, ok := securitygroup.ExtractCloudDescription(desc[i])
			if !ok {
				// Ignore rules that don't have a valid description field.
				awsPluginLogger().V(4).Info("Failed to extract cloud rule description", "desc", desc[i])
				continue
			}
			var egressRule securitygroup.EgressRule

			egressRule.ToSecurityGroups = []*securitygroup.CloudResourceID{SecurityGroup}
			egressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
			egressRule.ToPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

			egressRules = append(egressRules, egressRule)
		}
	}
	return egressRules
}

func convertFromIPPermissionPort(startPort *int64, endPort *int64) *int {
	if startPort == nil {
		return nil
	}
	if endPort == nil {
		retVal := int(*startPort)
		return &retVal
	}
	if *startPort == -1 {
		return nil
	}
	if *startPort == *endPort {
		retVal := int(*startPort)
		return &retVal
	}
	// other cases along with all (0 - 65535) tcp/udp ports returns nil
	return nil
}

func convertFromIPPermissionProtocol(proto string) *int {
	if strings.Compare(proto, awsAnyProtocolValue) == 0 {
		return nil
	}
	protoNum := securitygroup.ProtocolNameNumMap[strings.ToLower(proto)]
	return &protoNum
}
