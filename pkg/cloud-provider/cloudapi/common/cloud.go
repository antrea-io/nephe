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

package common

import (
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/securitygroup"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

var (
	APIVersion                      = "crd.cloud.antrea.io/v1alpha1"
	NetworkInterfaceCRDKind         = reflect.TypeOf(v1alpha1.NetworkInterface{}).Name()
	VirtualMachineCRDKind           = reflect.TypeOf(v1alpha1.VirtualMachine{}).Name()
	AnnotationCloudAssignedIDKey    = "cloud-assigned-id"
	AnnotationCloudAssignedNameKey  = "cloud-assigned-name"
	AnnotationCloudAssignedVPCIDKey = "cloud-assigned-vpc-id"
)

type ProviderType v1alpha1.CloudProvider
type InstanceID string

// CloudInterface is an abstract providing set of methods to be implemented by cloud providers.
type CloudInterface interface {
	// ProviderType returns the cloud provider type (aws, azure, gce etc).
	ProviderType() (providerType ProviderType)

	AccountMgmtInterface

	ComputeInterface

	SecurityInterface
}

// AccountMgmtInterface is an abstract providing set of methods to manage cloud account details to be implemented by cloud providers.
type AccountMgmtInterface interface {
	// AddProviderAccount adds and initializes given account of a cloud provider.
	AddProviderAccount(client client.Client, account *v1alpha1.CloudProviderAccount) error
	// RemoveProviderAccount removes and cleans up any resources of given account of a cloud provider.
	RemoveProviderAccount(namespacedName *types.NamespacedName)
	// AddAccountResourceSelector adds account specific resource selector.
	AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *v1alpha1.CloudEntitySelector) error
	// RemoveAccountResourcesSelector removes account specific resource selector.
	RemoveAccountResourcesSelector(accNamespacedName *types.NamespacedName, selector string)
	// GetAccountStatus gets accounts status.
	GetAccountStatus(accNamespacedName *types.NamespacedName) (*cloudv1alpha1.CloudProviderAccountStatus, error)
}

// ComputeInterface is an abstract providing set of methods to get Instance details to be implemented by cloud providers.
type ComputeInterface interface {
	// Instances returns VirtualMachineStatus across all accounts for a cloud provider.
	Instances() ([]*v1alpha1.VirtualMachine, error)
	// InstancesGivenProviderAccount returns VirtualMachineStatus for a given account of a cloud provider.
	InstancesGivenProviderAccount(namespacedName *types.NamespacedName) ([]*v1alpha1.VirtualMachine, error)
	// IsVirtualPrivateCloudPresent returns true if given virtual private cloud uniqueIdentifier is managed by the cloud, else false.
	IsVirtualPrivateCloudPresent(uniqueIdentifier string) bool
}

type SecurityInterface interface {
	// CreateSecurityGroup creates cloud security group corresponding to provided address group, if it does not already exist.
	// If it exists, returns the existing cloud SG ID
	CreateSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) (*string, error)
	// UpdateSecurityGroupRules updates cloud security group corresponding to provided address group with provided ingress and egress rules
	UpdateSecurityGroupRules(addressGroupIdentifier *securitygroup.CloudResourceID, ingressRules []*securitygroup.IngressRule,
		egressRules []*securitygroup.EgressRule) error
	// UpdateSecurityGroupMembers updates membership of cloud security group corresponding to provided address group. Only
	// provided computeResources will remain attached to cloud security group. UpdateSecurityGroupMembers will also make sure that
	// after membership update, if compute resource is no longer attached to any nephe created cloud security group, then
	// compute resource will get moved to cloud default security group
	UpdateSecurityGroupMembers(addressGroupIdentifier *securitygroup.CloudResourceID, computeResourceIdentifier []*securitygroup.CloudResource,
		membershipOnly bool) error
	// DeleteSecurityGroup will delete the cloud security group corresponding to provided address group. DeleteSecurityGroup expects that
	// UpdateSecurityGroupMembers and UpdateSecurityGroupRules is called prior to calling delete. DeleteSecurityGroup as part of delete,
	// do the best effort to find resources using this address group and detach the cloud security group from those resources.Also if the
	// compute resource is attached to only this security group, it will be moved to cloud default security group.
	DeleteSecurityGroup(addressGroupIdentifier *securitygroup.CloudResourceID, membershipOnly bool) error
	// GetEnforcedSecurity returns the cloud view of enforced security
	GetEnforcedSecurity() []securitygroup.SynchronizationContent
}
