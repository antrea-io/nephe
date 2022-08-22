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

	"antrea.io/nephe/pkg/cloud-provider/securitygroup"
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

func convertToEc2IpRanges(ips []*net.IPNet, ruleHasGroups bool) []*ec2.IpRange {
	var ipRanges []*ec2.IpRange
	if len(ips) == 0 && !ruleHasGroups {
		ipRange := &ec2.IpRange{
			CidrIp: aws.String("0.0.0.0/0"),
		}
		ipRanges = append(ipRanges, ipRange)
		return ipRanges
	}

	for _, ip := range ips {
		ipRange := &ec2.IpRange{
			CidrIp: aws.String(ip.String()),
		}
		ipRanges = append(ipRanges, ipRange)
	}
	return ipRanges
}

func convertFromIPRange(ipRanges []*ec2.IpRange) []*net.IPNet {
	var srcIPNets []*net.IPNet
	for _, ipRange := range ipRanges {
		_, ipNet, err := net.ParseCIDR(*ipRange.CidrIp)
		if err != nil {
			continue
		}
		srcIPNets = append(srcIPNets, ipNet)
	}
	return srcIPNets
}

func convertFromSecurityGroupPair(cloudGroups []*ec2.UserIdGroupPair, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) []*securitygroup.CloudResourceID {
	var cloudResourceIDs []*securitygroup.CloudResourceID

	for _, cloudGroup := range cloudGroups {
		var sgName string
		var vpcID string

		managedSgObj, foundInManagedSg := managedSGs[*cloudGroup.GroupId]
		if foundInManagedSg {
			sgName, _, _ = securitygroup.IsNepheControllerCreatedSG(*managedSgObj.GroupName)
			vpcID = *managedSgObj.VpcId
		}
		unmanagedSGObj, foundInUnmanagedSg := unmanagedSGs[*cloudGroup.GroupId]
		if foundInUnmanagedSg {
			sgName = *unmanagedSGObj.GroupName
			vpcID = *unmanagedSGObj.VpcId
		}

		cloudResourceID := &securitygroup.CloudResourceID{
			Name: sgName,
			Vpc:  vpcID,
		}

		cloudResourceIDs = append(cloudResourceIDs, cloudResourceID)
	}
	return cloudResourceIDs
}

func convertFromIPPermissionToIngressRule(ipPermissions []*ec2.IpPermission, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) []securitygroup.IngressRule {
	var ingressRules []securitygroup.IngressRule
	for _, ipPermission := range ipPermissions {
		var ingressRule securitygroup.IngressRule

		ingressRule.FromSrcIP = convertFromIPRange(ipPermission.IpRanges)
		ingressRule.FromSecurityGroups = convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		ingressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
		ingressRule.FromPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

		ingressRules = append(ingressRules, ingressRule)
	}
	return ingressRules
}

func convertFromIPPermissionToEgressRule(ipPermissions []*ec2.IpPermission, managedSGs map[string]*ec2.SecurityGroup,
	unmanagedSGs map[string]*ec2.SecurityGroup) []securitygroup.EgressRule {
	var egressRules []securitygroup.EgressRule
	for _, ipPermission := range ipPermissions {
		var egressRule securitygroup.EgressRule

		egressRule.ToDstIP = convertFromIPRange(ipPermission.IpRanges)
		egressRule.ToSecurityGroups = convertFromSecurityGroupPair(ipPermission.UserIdGroupPairs, managedSGs, unmanagedSGs)
		egressRule.Protocol = convertFromIPPermissionProtocol(*ipPermission.IpProtocol)
		egressRule.ToPort = convertFromIPPermissionPort(ipPermission.FromPort, ipPermission.ToPort)

		egressRules = append(egressRules, egressRule)
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
