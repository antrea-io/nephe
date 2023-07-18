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

package cloud

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/plugins/aws"
	"antrea.io/nephe/pkg/cloudprovider/plugins/azure"
	"antrea.io/nephe/pkg/logging"
	nephetypes "antrea.io/nephe/pkg/types"
)

// CloudInterface is an abstract providing set of methods to be implemented by cloud providers.
type CloudInterface interface {
	// ProviderType returns the cloud provider type (aws, azure, gce etc).
	ProviderType() (providerType runtimev1alpha1.CloudProvider)

	AccountMgmtInterface

	ComputeInterface

	SecurityInterface
}

// AccountMgmtInterface is an abstract providing set of methods to manage cloud account details to be implemented by cloud providers.
type AccountMgmtInterface interface {
	// AddProviderAccount adds and initializes given account of a cloud provider.
	AddProviderAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount) error
	// RemoveProviderAccount removes and cleans up any resources of given account of a cloud provider.
	RemoveProviderAccount(namespacedName *types.NamespacedName)
	// AddAccountResourceSelector adds account specific resource selector.
	AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error
	// RemoveAccountResourcesSelector removes account specific resource selector.
	RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName *types.NamespacedName)
	// GetAccountStatus gets accounts status.
	GetAccountStatus(accNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error)
	// DoInventoryPoll calls cloud API to get cloud resources.
	DoInventoryPoll(accountNamespacedName *types.NamespacedName) error
	// ResetInventoryCache resets cloud snapshot and poll stats to nil.
	ResetInventoryCache(accountNamespacedName *types.NamespacedName) error
}

// ComputeInterface is an abstract providing set of methods to get inventory details to be implemented by cloud providers.
type ComputeInterface interface {
	// GetCloudInventory gets VPC and VM inventory from plugin snapshot for a given cloud provider account.
	GetCloudInventory(accountNamespacedName *types.NamespacedName) (*nephetypes.CloudInventory, error)
}

type SecurityInterface interface {
	// CreateSecurityGroup creates cloud security group corresponding to provided security group, if it does not already exist.
	// If it exists, returns the existing cloud SG ID.
	CreateSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) (*string, error)
	// UpdateSecurityGroupRules updates cloud security group corresponding to provided appliedTo group with provided rules.
	// addRules and rmRules are the changed rules, allRules are rules from all nps of the security group.
	UpdateSecurityGroupRules(appliedToGroupIdentifier *cloudresource.CloudResource, addRules, rmRules []*cloudresource.CloudRule) error
	// UpdateSecurityGroupMembers updates membership of cloud security group corresponding to provided security group. Only
	// provided computeResources will remain attached to cloud security group. UpdateSecurityGroupMembers will also make sure that
	// after membership update, if compute resource is no longer attached to any nephe created cloud security group, then
	// compute resource will get moved to cloud default security group.
	UpdateSecurityGroupMembers(securityGroupIdentifier *cloudresource.CloudResource, computeResourceIdentifier []*cloudresource.CloudResource,
		membershipOnly bool) error
	// DeleteSecurityGroup will delete the cloud security group corresponding to provided security group. DeleteSecurityGroup expects that
	// UpdateSecurityGroupMembers and UpdateSecurityGroupRules is called prior to calling delete. DeleteSecurityGroup as part of delete,
	// do the best effort to find resources using this security group and detach the cloud security group from those resources. Also, if the
	// compute resource is attached to only this security group, it will be moved to cloud default security group.
	DeleteSecurityGroup(securityGroupIdentifier *cloudresource.CloudResource, membershipOnly bool) error
	// GetEnforcedSecurity returns the cloud view of enforced security.
	GetEnforcedSecurity() []cloudresource.SynchronizationContent
}

// All registered crdv1alpha1 providers.
var (
	providersMutex   sync.Mutex
	providers        = make(map[runtimev1alpha1.CloudProvider]CloudInterface)
	corePluginLogger = func() logging.Logger {
		return logging.GetLogger("core-plugin")
	}
)

// Register AWS and Azure cloud.
func init() {
	registerCloudProvider(runtimev1alpha1.AWSCloudProvider, aws.Register())
	registerCloudProvider(runtimev1alpha1.AzureCloudProvider, azure.Register())
}

// registerCloudProvider registers a crdv1alpha1 provider factory by type. This
// is expected to happen during controller startup.
func registerCloudProvider(providerType runtimev1alpha1.CloudProvider, cloud CloudInterface) {
	providersMutex.Lock()
	defer providersMutex.Unlock()
	if _, found := providers[providerType]; found {
		corePluginLogger().V(0).Info("Cloud provider crd.v1alpha1 already exists",
			"type", providerType)
		return
	}
	providers[providerType] = cloud

	corePluginLogger().V(1).Info("Registered crd.v1alpha1 cloud provider successfully",
		"type", providerType)
}

// GetCloudInterface returns an instance of the crd.v1alpha1 cloud Provider type, or nil if
// the type is unknown.  The error return is only used if the named provider
// was known but failed to initialize.
func GetCloudInterface(providerType runtimev1alpha1.CloudProvider) (CloudInterface, error) {
	providersMutex.Lock()
	defer providersMutex.Unlock()
	cloud, found := providers[providerType]
	if !found {
		return nil, fmt.Errorf("unsupported crd.v1alpha1 cloud provider %q", providerType)
	}
	return cloud, nil
}

func GetSupportedCloudProviderTypes() []runtimev1alpha1.CloudProvider {
	providersMutex.Lock()
	defer providersMutex.Unlock()

	providerTypes := make([]runtimev1alpha1.CloudProvider, 0, len(providers))
	for providerType := range providers {
		providerTypes = append(providerTypes, providerType)
	}
	return providerTypes
}
