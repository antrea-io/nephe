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

package securitygroup

/*
This module maps Antrea internal NetworkPolicy in antrea.io/antrea/pkg/apis/controlplane/v1beta2
to cloud security group.

Network CRD
Each Antrea internal NetworkPolicy contains
-- name and namespace that uniquely identifies an Antrea internal NetworkPolicy.
   name and namespace corresponds to user facing Antrea NetworkPolicy.
-- list of rules (presently all are while list rules), each rule contains
    -- direction
    -- service (port) of this rule.
    -- To/From:  IPBlock and  reference to AddressGroup.
-- list of references to appliedToGroup

AddressGroup is used for To/From field of Rule in a Network policy, each AddressGroup contains
-- auto-generated name (namespace-less) uniquely identifies a AddressGroup.
-- a list  GroupMemberPod
    -- reference to Pod
    -- Pod IP and ports
-- a list of GroupMember, each contains
     -- reference to Pod if applicable.
     -- reference to ExternalEntity if applicable.
     -- a list of Endpoint, each contains IP and ports.

AppliedToGroup is used to the To/From field of Rule in a Network policy, each appliedToGroup contains
-- auto-generated name(namespace-less) uniquely identifies a AppliedToGroup.
-- a list  GroupMemberPod
    -- reference to Pod
    -- Pod IP and ports
-- a list of GroupMember, each contains
     -- reference to Pod if applicable.
     -- reference to ExternalEntity if applicable.
     -- a list of Endpoint, each contains IP and ports.

A SecurityGroup
-- is whitelist.
-- is configured per VPC, and is uniquely identified by its name or ID.
-- contains zero or more NIC/(VM??). A NIC/(VM??) may be associated with zero or more securityGroups.
-- contains Ingress rules, each rule contains
   -- IPBlocks : a list of source IP blocks of permitted incoming traffic.
   -- Ports: a list of source ports of permitted incoming traffic.
   -- SecurityGroups: a list of securityGroups from which incoming traffic is permitted.
-- contains Egress rules, each rule contains
   -- IPBlocks : a list of destination IP blocks of permitted outgoing traffic.
   -- Ports: a list of dest ports of permitted outgoing traffic.
   -- SecurityGroups: a list of securityGroups to which ongoing traffic is permitted.

Antrea internal NetworkPolicy To SecurityGroup Mapping strategy
-- Each Antrea AddressGroup is mapped to zero or more cloud membership only SecurityGroup, and zero or more IP blocks.where
   -- each cloud membership only SecurityGroup corresponding to a VPC, and a list of cloud resources.
   -- each cloud membership only SecurityGroup cannot have ingress/egress rules associated with it.

-- Each Antrea AppliedGroup is mapped to zero or more cloud appliedToSecurityGroup, where
   -- each cloud appliedToSecurityGroup corresponding to a VPC, and a list of cloud resources.
   -- each cloud appliedToSecurityGroup can have ingress/egress rules associated with it.

-- An Antrea internal NetworkPolicy is realized via cloud membership only SecurityGroups and AppliedToSecurityGroups
   -- create cloud membership only SecurityGroups and IPBlocks for each Antrea AddressGroups in To/From fields.
   -- create cloud AppliedToSecurityGroups for each Antrea AppliedToGroup.
   -- for each created cloud appliedToSecurityGroup creates ingress and egress rules based on cloud membership only
      SecurityGroups and IPBlocks associated with this Antrea NetworkPolicy.

-- Cloud resource to Antrea (internal/user facing) NetworkPolicy Mapping
   -- it is desirable to show what Antrea NetworkPolicies are associated with a specific cloud resource, and Antrea
      NetworkPolicies realization status
   -- each cloud resource shall keep track of cloud AppliedToSecurityGroups it is associated with.
   -- the union of Antrea NetworkPolicies associated with these cloud AppliedToSecurityGroups is entirety of the
      NetworkPolicies intended to this network resource.
   -- an Antrea NetworkPolicy is considered to be successfully applied to a network resource, when
      -- its AppliedToSecurityGroups to which the network resource is a member of, are created/updated with no error, and
      -- its membership only SecurityGroups are created/updated with no error.

-- Calls into cloud securityGroup are asynchronous for better performance/scalability.
*/

import (
	"fmt"
	"sync"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloud"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
)

// CloudSecurityGroupInterface declares interface to program cloud security groups.
type CloudSecurityGroupInterface interface {
	// CreateSecurityGroup request to create SecurityGroup name.
	// membershipOnly is true if the SecurityGroup is used for membership tracking, not
	// applying ingress/egress rules.
	// Caller expects to wait on returned channel for status
	CreateSecurityGroup(name *cloudresource.CloudResource, membershipOnly bool) <-chan error

	// UpdateSecurityGroupRules updates SecurityGroup name's ingress/egress rules in entirety.
	// SecurityGroup name must already been created. SecurityGroups referred to in ingressRules and
	// egressRules must have been already created.
	UpdateSecurityGroupRules(name *cloudresource.CloudResource, addRules, rmRules []*cloudresource.CloudRule) <-chan error

	// UpdateSecurityGroupMembers updates SecurityGroup name with members.
	// SecurityGroup name must already have been created.
	// For appliedSecurityGroup, UpdateSecurityGroupMembers is called only if SG has
	// rules configured.
	UpdateSecurityGroupMembers(name *cloudresource.CloudResource, members []*cloudresource.CloudResource, membershipOnly bool) <-chan error

	// DeleteSecurityGroup deletes SecurityGroup name.
	// SecurityGroup name must already been created, is empty.
	DeleteSecurityGroup(name *cloudresource.CloudResource, membershipOnly bool) <-chan error

	// GetSecurityGroupSyncChan returns a channel that networkPolicy controller waits on to retrieve complete SGs
	// configured by cloud plug-in.
	// Usage patterns:
	// 1. Controller calls it at initialization to obtains the channel.
	// 2. Controller waits on channel returned in 1, and expects that when channel wakes up it return the entire SGs configured.
	// 3. Plug-in shall wake up the channel initially after sync up with the cloud; and then periodically.
	// 4. Controller, upon receive entire SGs set, proceed to reconcile between K8s configuration and cloud configuration.
	// This API ensures cloud plug-in stays stateless.
	// - Correct SGs accidentally changed by customers via cloud API/console directly.
	GetSecurityGroupSyncChan() <-chan cloudresource.SynchronizationContent

	// CloudProviderSupportsRulePriority Returns true is Cloud Provider supports priority in the rule.
	CloudProviderSupportsRulePriority(provider string) bool
	// CloudProviderSupportsRuleAction Returns true is Cloud Provider supports action in the rule.
	CloudProviderSupportsRuleAction(provider string) bool
	// CloudProviderSupportsRuleName Returns true is Cloud Provider supports name in the rule.
	CloudProviderSupportsRuleName(provider string) bool
}

type CloudSecurityGroupImpl struct{}

var (
	// CloudSecurityGroup is global entry point to configure cloud specific security group.
	CloudSecurityGroup CloudSecurityGroupInterface
)

func init() {
	CloudSecurityGroup = &CloudSecurityGroupImpl{}
}

// getCloudInterfaceForCloudResource fetches Cloud Interface using Cloud Provider Type present in CloudResource.
func getCloudInterfaceForCloudResource(securityGroupIdentifier *cloudresource.CloudResource) (cloud.CloudInterface, error) {
	providerType := runtimev1alpha1.CloudProvider(securityGroupIdentifier.CloudProvider)
	cloudInterface, err := cloud.GetCloudInterface(providerType)
	if err != nil {
		//cloudprovider.corePluginLogger().Error(err, "get cloud interface failed", "providerType", providerType)
		return nil, fmt.Errorf("virtual private cloud [%v] not managed by supported clouds", securityGroupIdentifier.Vpc)
	}
	return cloudInterface, nil
}

func (sg *CloudSecurityGroupImpl) CreateSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource,
	membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		_, err = cloudInterface.CreateSecurityGroup(securityGroupIdentifier, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *CloudSecurityGroupImpl) UpdateSecurityGroupRules(appliedToGroupIdentifier *cloudresource.CloudResource,
	addRules, rmRules []*cloudresource.CloudRule) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(appliedToGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupRules(appliedToGroupIdentifier, addRules, rmRules)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *CloudSecurityGroupImpl) UpdateSecurityGroupMembers(securityGroupIdentifier *cloudresource.CloudResource,
	members []*cloudresource.CloudResource, membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.UpdateSecurityGroupMembers(securityGroupIdentifier, members, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *CloudSecurityGroupImpl) DeleteSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource,
	membershipOnly bool) <-chan error {
	ch := make(chan error)

	go func() {
		defer close(ch)

		cloudInterface, err := getCloudInterfaceForCloudResource(securityGroupIdentifier)
		if err != nil {
			ch <- err
			return
		}

		err = cloudInterface.DeleteSecurityGroup(securityGroupIdentifier, membershipOnly)
		if err != nil {
			ch <- err
			return
		}

		ch <- nil
	}()

	return ch
}

func (sg *CloudSecurityGroupImpl) GetSecurityGroupSyncChan() <-chan cloudresource.SynchronizationContent {
	retCh := make(chan cloudresource.SynchronizationContent)

	go func() {
		defer close(retCh)

		var wg sync.WaitGroup
		ch := make(chan []cloudresource.SynchronizationContent)
		providerTypes := cloud.GetSupportedCloudProviderTypes()

		wg.Add(len(providerTypes))
		go func() {
			wg.Wait()
			close(ch)
		}()

		for _, providerType := range providerTypes {
			cloudInterface, err := cloud.GetCloudInterface(providerType)
			if err != nil {
				wg.Done()
				continue
			}

			go func() {
				ch <- cloudInterface.GetEnforcedSecurity()
				wg.Done()
			}()
		}

		for val := range ch {
			for _, sg := range val {
				retCh <- sg
			}
		}
	}()

	return retCh
}

func (sg *CloudSecurityGroupImpl) CloudProviderSupportsRulePriority(provider string) bool {
	return provider == string(runtimev1alpha1.AzureCloudProvider)
}

func (sg *CloudSecurityGroupImpl) CloudProviderSupportsRuleAction(provider string) bool {
	return provider == string(runtimev1alpha1.AzureCloudProvider)
}

func (sg *CloudSecurityGroupImpl) CloudProviderSupportsRuleName(provider string) bool {
	return provider == string(runtimev1alpha1.AzureCloudProvider)
}
