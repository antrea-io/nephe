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
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// awsEC2Wrapper is layer above aws EC2 sdk apis to allow for unit-testing.
type awsEC2Wrapper interface {
	// instances
	pagedDescribeInstancesWrapper(input *ec2.DescribeInstancesInput) ([]*ec2.Instance, error)

	// network interfaces
	pagedDescribeNetworkInterfaces(input *ec2.DescribeNetworkInterfacesInput) ([]*ec2.NetworkInterface, error)
	modifyNetworkInterfaceAttribute(input *ec2.ModifyNetworkInterfaceAttributeInput) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)

	// security groups/rules
	createSecurityGroup(input *ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error)
	describeSecurityGroups(input *ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error)
	deleteSecurityGroup(input *ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error)
	authorizeSecurityGroupEgress(input *ec2.AuthorizeSecurityGroupEgressInput) (*ec2.AuthorizeSecurityGroupEgressOutput, error)
	authorizeSecurityGroupIngress(input *ec2.AuthorizeSecurityGroupIngressInput) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	revokeSecurityGroupEgress(input *ec2.RevokeSecurityGroupEgressInput) (*ec2.RevokeSecurityGroupEgressOutput, error)
	revokeSecurityGroupIngress(input *ec2.RevokeSecurityGroupIngressInput) (*ec2.RevokeSecurityGroupIngressOutput, error)

	// vpcs
	describeVpcsWrapper(input *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)

	// peer connections
	describeVpcPeeringConnectionsWrapper(input *ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error)
}
type awsEC2WrapperImpl struct {
	ec2 *ec2.EC2
}

func (ec2Wrapper *awsEC2WrapperImpl) pagedDescribeInstancesWrapper(input *ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	var instances []*ec2.Instance
	var nextToken *string
	for {
		response, err := ec2Wrapper.ec2.DescribeInstances(input)
		if err != nil {
			return nil, fmt.Errorf("error describing ec2 instances : %q", err)
		}

		reservations := response.Reservations
		for _, reservation := range reservations {
			instances = append(instances, reservation.Instances...)
		}

		nextToken = response.NextToken
		if aws.StringValue(nextToken) == "" {
			break
		}
		input.NextToken = nextToken
	}
	return instances, nil
}

func (ec2Wrapper *awsEC2WrapperImpl) pagedDescribeNetworkInterfaces(input *ec2.DescribeNetworkInterfacesInput) ([]*ec2.NetworkInterface,
	error) {
	var networkInterfaces []*ec2.NetworkInterface
	var nextToken *string
	for {
		response, err := ec2Wrapper.ec2.DescribeNetworkInterfaces(input)
		if err != nil {
			return nil, fmt.Errorf("error describing ec2 network interfaces: %q", err)
		}

		interfaces := response.NetworkInterfaces
		networkInterfaces = append(networkInterfaces, interfaces...)

		nextToken = response.NextToken
		if aws.StringValue(nextToken) == "" {
			break
		}
		input.NextToken = nextToken
	}
	return networkInterfaces, nil
}

func (ec2Wrapper *awsEC2WrapperImpl) modifyNetworkInterfaceAttribute(input *ec2.ModifyNetworkInterfaceAttributeInput) (
	req *ec2.ModifyNetworkInterfaceAttributeOutput, output error) {
	return ec2Wrapper.ec2.ModifyNetworkInterfaceAttribute(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) createSecurityGroup(input *ec2.CreateSecurityGroupInput) (*ec2.CreateSecurityGroupOutput, error) {
	return ec2Wrapper.ec2.CreateSecurityGroup(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) describeSecurityGroups(input *ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput,
	error) {
	return ec2Wrapper.ec2.DescribeSecurityGroups(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) deleteSecurityGroup(input *ec2.DeleteSecurityGroupInput) (*ec2.DeleteSecurityGroupOutput, error) {
	return ec2Wrapper.ec2.DeleteSecurityGroup(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) authorizeSecurityGroupEgress(input *ec2.AuthorizeSecurityGroupEgressInput) (
	*ec2.AuthorizeSecurityGroupEgressOutput, error) {
	return ec2Wrapper.ec2.AuthorizeSecurityGroupEgress(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) authorizeSecurityGroupIngress(input *ec2.AuthorizeSecurityGroupIngressInput) (
	*ec2.AuthorizeSecurityGroupIngressOutput, error) {
	return ec2Wrapper.ec2.AuthorizeSecurityGroupIngress(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) revokeSecurityGroupEgress(input *ec2.RevokeSecurityGroupEgressInput) (
	*ec2.RevokeSecurityGroupEgressOutput, error) {
	return ec2Wrapper.ec2.RevokeSecurityGroupEgress(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) revokeSecurityGroupIngress(input *ec2.RevokeSecurityGroupIngressInput) (
	*ec2.RevokeSecurityGroupIngressOutput, error) {
	return ec2Wrapper.ec2.RevokeSecurityGroupIngress(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) describeVpcsWrapper(input *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
	return ec2Wrapper.ec2.DescribeVpcs(input)
}

func (ec2Wrapper *awsEC2WrapperImpl) describeVpcPeeringConnectionsWrapper(input *ec2.DescribeVpcPeeringConnectionsInput) (
	*ec2.DescribeVpcPeeringConnectionsOutput, error) {
	return ec2Wrapper.ec2.DescribeVpcPeeringConnections(input)
}
