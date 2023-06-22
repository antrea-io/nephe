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
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
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

// convertToEc2IpRanges convert the IPs to EC2 IPv4 and IPv6 ranges.
func convertToEc2IpRanges(ips []*net.IPNet, ruleHasGroups bool, description *string) ([]*ec2.IpRange, []*ec2.Ipv6Range) {
	var ipv4Ranges []*ec2.IpRange
	var ipv6Ranges []*ec2.Ipv6Range
	if len(ips) == 0 && !ruleHasGroups {
		ipRange := &ec2.IpRange{
			CidrIp:      aws.String("0.0.0.0/0"),
			Description: description,
		}
		ipv4Ranges = append(ipv4Ranges, ipRange)
		ipv6Range := &ec2.Ipv6Range{
			CidrIpv6:    aws.String("::/0"),
			Description: description,
		}
		ipv6Ranges = append(ipv6Ranges, ipv6Range)
		return ipv4Ranges, ipv6Ranges
	}

	for _, ip := range ips {
		if ip.IP.To4() != nil {
			ipRange := &ec2.IpRange{
				CidrIp:      aws.String(ip.String()),
				Description: description,
			}
			ipv4Ranges = append(ipv4Ranges, ipRange)
		} else if ip.IP.To16() != nil {
			ipRange := &ec2.Ipv6Range{
				CidrIpv6:    aws.String(ip.String()),
				Description: description,
			}
			ipv6Ranges = append(ipv6Ranges, ipRange)
		}
	}
	return ipv4Ranges, ipv6Ranges
}

// convertFromIPRange convert from EC2 IPv4 and IPv6 ranges to IPs.
func convertFromIPRange(ipRanges []*ec2.IpRange, ipv6Ranges []*ec2.Ipv6Range) ([]*net.IPNet, []*string) {
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
	for _, ipRange := range ipv6Ranges {
		_, ipNet, err := net.ParseCIDR(*ipRange.CidrIpv6)
		if err != nil {
			continue
		}
		desc = append(desc, ipRange.Description)
		srcIPNets = append(srcIPNets, ipNet)
	}
	return srcIPNets, desc
}

func convertFromSecurityGroupPair(cloudGroups []*ec2.UserIdGroupPair, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) ([]*cloudresource.CloudResourceID, []*string) {
	var cloudResourceIDs []*cloudresource.CloudResourceID
	var desc []*string

	for _, cloudGroup := range cloudGroups {
		var sgName string
		var vpcID string

		managedSgObj, foundInManagedSg := managedSGs[*cloudGroup.GroupId]
		if foundInManagedSg {
			sgName, _, _ = utils.IsNepheControllerCreatedSG(*managedSgObj.GroupName)
			vpcID = *managedSgObj.VpcId
			desc = append(desc, cloudGroup.Description)
		}
		unmanagedSGObj, foundInUnmanagedSg := unmanagedSGs[*cloudGroup.GroupId]
		if foundInUnmanagedSg {
			sgName = *unmanagedSGObj.GroupName
			vpcID = *unmanagedSGObj.VpcId
			desc = append(desc, cloudGroup.Description)
		}

		cloudResourceID := &cloudresource.CloudResourceID{
			Name: sgName,
			Vpc:  vpcID,
		}

		cloudResourceIDs = append(cloudResourceIDs, cloudResourceID)
	}
	return cloudResourceIDs, desc
}

// convertFromIngressIpPermissionToCloudRule converts AWS ingress rules from ec2.IpPermission to internal securitygroup.CloudRule.
// Each AT Sg can have one or more ANPs and an ANP can have one or more rules. Each rule can have a description.
func convertFromIngressIpPermissionToCloudRule(sgID string, ipPermissions []*ec2.IpPermission,
	managedSGs, unmanagedSGs map[string]*ec2.SecurityGroup) []cloudresource.CloudRule {
	var ingressRules []cloudresource.CloudRule
	for _, ipPermission := range ipPermissions {
		fromSrcIPs, descriptions := convertFromIPRange(ipPermission.IpRanges, ipPermission.Ipv6Ranges)
		for i, srcIP := range fromSrcIPs {
			// Get cloud rule description.
			desc, ok := utils.ExtractCloudDescription(descriptions[i])
			ingressRule := cloudresource.CloudRule{
				Rule: &cloudresource.IngressRule{
					FromPort:  convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort),
					FromSrcIP: []*net.IPNet{srcIP},
					Protocol:  convertFromIPPermissionProtocol(*ipPermission.IpProtocol),
				},
				AppliedToGrp: sgID,
			}
			if ok {
				ingressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
			}
			ingressRule.Hash = ingressRule.GetHash()
			ingressRules = append(ingressRules, ingressRule)
		}
		fromSecurityGroups, descriptions := convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		for i, SecurityGroup := range fromSecurityGroups {
			// Get cloud rule description.
			desc, ok := utils.ExtractCloudDescription(descriptions[i])
			ingressRule := cloudresource.CloudRule{
				Rule: &cloudresource.IngressRule{
					FromPort:           convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort),
					FromSecurityGroups: []*cloudresource.CloudResourceID{SecurityGroup},
					Protocol:           convertFromIPPermissionProtocol(*ipPermission.IpProtocol),
				},
				AppliedToGrp: sgID,
			}
			if ok {
				ingressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
			}
			ingressRule.Hash = ingressRule.GetHash()
			ingressRules = append(ingressRules, ingressRule)
		}
	}
	return ingressRules
}

// convertFromEgressIpPermissionToCloudRule converts AWS egress rules from ec2.IpPermission to internal securitygroup.CloudRule.
// Each AT Sg can have one or more ANPs and an ANP can have one or more rules. Each rule can have a description.
func convertFromEgressIpPermissionToCloudRule(sgID string, ipPermissions []*ec2.IpPermission,
	managedSGs, unmanagedSGs map[string]*ec2.SecurityGroup) []cloudresource.CloudRule {
	var egressRules []cloudresource.CloudRule
	for _, ipPermission := range ipPermissions {
		toDstIPs, descriptions := convertFromIPRange(ipPermission.IpRanges, ipPermission.Ipv6Ranges)
		for i, dstIP := range toDstIPs {
			// Get cloud rule description.
			desc, ok := utils.ExtractCloudDescription(descriptions[i])
			egressRule := cloudresource.CloudRule{
				Rule: &cloudresource.EgressRule{
					ToPort:   convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort),
					ToDstIP:  []*net.IPNet{dstIP},
					Protocol: convertFromIPPermissionProtocol(*ipPermission.IpProtocol),
				},
				AppliedToGrp: sgID,
			}
			if ok {
				egressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
			}
			egressRule.Hash = egressRule.GetHash()
			egressRules = append(egressRules, egressRule)
		}
		toSecurityGroups, descriptions := convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		for i, SecurityGroup := range toSecurityGroups {
			// Get cloud rule description.
			desc, ok := utils.ExtractCloudDescription(descriptions[i])
			egressRule := cloudresource.CloudRule{
				Rule: &cloudresource.EgressRule{
					ToPort:           convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort),
					ToSecurityGroups: []*cloudresource.CloudResourceID{SecurityGroup},
					Protocol:         convertFromIPPermissionProtocol(*ipPermission.IpProtocol),
				},
				AppliedToGrp: sgID,
			}
			if ok {
				egressRule.NpNamespacedName = types.NamespacedName{Name: desc.Name, Namespace: desc.Namespace}.String()
			}
			egressRule.Hash = egressRule.GetHash()
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
	protoNum := protocolNameNumMap[strings.ToLower(proto)]
	return &protoNum
}
