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

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
)

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

const (
	// Used to create a rule description.
	Name      = "Name"
	Namespace = "Ns"
)

var (
	ControllerPrefix             string
	ControllerAddressGroupPrefix string
	ControllerAppliedToPrefix    string
)

var ProtocolNameNumMap = map[string]int{
	"icmp":   1,
	"igmp":   2,
	"tcp":    6,
	"udp":    17,
	"icmpv6": 58,
}

// CloudResourceType specifies the type of cloud resource.
type CloudResourceType string

var (
	CloudResourceTypeVM  = CloudResourceType(reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name())
	CloudResourceTypeNIC = CloudResourceType(reflect.TypeOf(runtimev1alpha1.NetworkInterface{}).Name())
)

var (
	// CloudSecurityGroup is global entry point to configure cloud specific security group.
	CloudSecurityGroup CloudSecurityGroupAPI
)

func SetCloudResourcePrefix(CloudResourcePrefix string) {
	ControllerPrefix = CloudResourcePrefix
}

func GetControllerAddressGroupPrefix() string {
	ControllerAddressGroupPrefix = ControllerPrefix + "-ag-"
	return ControllerAddressGroupPrefix
}

func GetControllerAppliedToPrefix() string {
	ControllerAppliedToPrefix = ControllerPrefix + "-at-"
	return ControllerAppliedToPrefix
}

type CloudResourceID struct {
	Name string
	Vpc  string
}

// CloudResource uniquely identify a cloud resource.
type CloudResource struct {
	Type CloudResourceType
	CloudResourceID
	// TODO: Rename AccountID to AccountNameSpacedName.
	AccountID     string
	CloudProvider string
}

func (c *CloudResource) String() string {
	return string(c.Type) + "/" + c.CloudResourceID.String()
}

func (c *CloudResourceID) GetCloudName(membershipOnly bool) string {
	if membershipOnly {
		return fmt.Sprintf("%v%v", GetControllerAddressGroupPrefix(), strings.ToLower(c.Name))
	}
	return fmt.Sprintf("%v%v", GetControllerAppliedToPrefix(), strings.ToLower(c.Name))
}

func (c *CloudResourceID) String() string {
	return c.Name + "/" + c.Vpc
}

type CloudRuleDescription struct {
	Name      string
	Namespace string
}

func (r *CloudRuleDescription) String() string {
	return Name + ":" + r.Name + ", " +
		Namespace + ":" + r.Namespace
}

type Rule interface {
	isRule()
}

// IngressRule specifies one ingress rule of cloud SecurityGroup.
type IngressRule struct {
	FromPort           *int
	FromSrcIP          []*net.IPNet
	FromSecurityGroups []*CloudResourceID
	Protocol           *int
	AppliedToGroup     map[string]struct{}
}

func (i *IngressRule) isRule() {}

// EgressRule specifies one egress rule of cloud SecurityGroup.
type EgressRule struct {
	ToPort           *int
	ToDstIP          []*net.IPNet
	ToSecurityGroups []*CloudResourceID
	Protocol         *int
	AppliedToGroup   map[string]struct{}
}

func (e *EgressRule) isRule() {}

// SetAppliedToGroup set appliedToGroup on ingress or egress rule from rule or policy level.
func SetAppliedToGroup(ruleAppliedTo []string, policyAppliedTo []string, r Rule) {
	var appliedTos []string
	if len(ruleAppliedTo) > 0 {
		appliedTos = ruleAppliedTo
	} else {
		appliedTos = policyAppliedTo
	}

	if iRule, ok := r.(*IngressRule); ok {
		for _, appliedToGroup := range appliedTos {
			iRule.AppliedToGroup[appliedToGroup] = struct{}{}
		}
	} else if eRule, ok := r.(*EgressRule); ok {
		for _, appliedToGroup := range appliedTos {
			eRule.AppliedToGroup[appliedToGroup] = struct{}{}
		}
	}
}

type CloudRule struct {
	Hash             string `json:"-"`
	Rule             Rule
	NpNamespacedName string `json:"-"`
	AppliedToGrp     string
}

func (c *CloudRule) GetHash() string {
	hash := sha1.New()
	bytes, _ := json.Marshal(c)
	hash.Write(bytes)
	hashValue := hex.EncodeToString(hash.Sum(nil))
	return hashValue
}

// SynchronizationContent returns a SecurityGroup content in cloud.
type SynchronizationContent struct {
	Resource                   CloudResource
	MembershipOnly             bool
	Members                    []CloudResource
	MembersWithOtherSGAttached []CloudResource
	IngressRules               []CloudRule
	EgressRules                []CloudRule
}

// CloudSecurityGroupAPI declares interface to program cloud security groups.
type CloudSecurityGroupAPI interface {
	// CreateSecurityGroup request to create SecurityGroup name.
	// membershipOnly is true if the SecurityGroup is used for membership tracking, not
	// applying ingress/egress rules.
	// Caller expects to wait on returned channel for status
	CreateSecurityGroup(name *CloudResource, membershipOnly bool) <-chan error

	// UpdateSecurityGroupRules updates SecurityGroup name's ingress/egress rules in entirety.
	// SecurityGroup name must already been created. SecurityGroups referred to in ingressRules and
	// egressRules must have been already created.
	UpdateSecurityGroupRules(name *CloudResource, addRules, rmRules []*CloudRule) <-chan error

	// UpdateSecurityGroupMembers updates SecurityGroup name with members.
	// SecurityGroup name must already have been created.
	// For appliedSecurityGroup, UpdateSecurityGroupMembers is called only if SG has
	// rules configured.
	UpdateSecurityGroupMembers(name *CloudResource, members []*CloudResource, membershipOnly bool) <-chan error

	// DeleteSecurityGroup deletes SecurityGroup name.
	// SecurityGroup name must already been created, is empty.
	DeleteSecurityGroup(name *CloudResource, membershipOnly bool) <-chan error

	// GetSecurityGroupSyncChan returns a channel that networkPolicy controller waits on to retrieve complete SGs
	// configured by cloud plug-in.
	// Usage patterns:
	// 1. Controller calls it at initialization to obtains the channel.
	// 2. Controller waits on channel returned in 1, and expects that when channel wakes up it return the entire SGs configured.
	// 3. Plug-in shall wake up the channel initially after sync up with the cloud; and then periodically.
	// 4. Controller, upon receive entire SGs set, proceed to reconcile between K8s configuration and cloud configuration.
	// This API ensures cloud plug-in stays stateless.
	// - Correct SGs accidentally changed by customers via cloud API/console directly.
	GetSecurityGroupSyncChan() <-chan SynchronizationContent
}
