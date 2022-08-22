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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
	"antrea.io/nephe/pkg/logging"
)

var azurePluginLogger = func() logging.Logger {
	return logging.GetLogger("azure-plugin")
}

const (
	providerType = cloudcommon.ProviderType(v1alpha1.AzureCloudProvider)
)

// azureCloud implements CloudInterface for Azure.
type azureCloud struct {
	cloudCommon internal.CloudCommonInterface
}

// newAzureCloud creates a new instance of azureCloud.
func newAzureCloud(azureSpecificHelper azureServicesHelper) *azureCloud {
	azureCloud := &azureCloud{
		cloudCommon: internal.NewCloudCommon(azurePluginLogger, &azureCloudCommonHelperImpl{}, azureSpecificHelper),
	}
	return azureCloud
}

// Register registers cloud provider type and creates awsCloud object for the provider. Any cloud account added at later
// point with this cloud provider using CloudInterface API will get added to this awsCloud object.
func Register() cloudcommon.CloudInterface {
	return newAzureCloud(&azureServicesHelperImpl{})
}

// ProviderType returns the cloud provider type (aws, azure, gce etc).
func (c *azureCloud) ProviderType() cloudcommon.ProviderType {
	return providerType
}

// /////////////////////////////////////////////
// 	ComputeInterface Implementation
// /////////////////////////////////////////////.
// Instances returns VM status for all virtualMachines across all accounts of a cloud provider.
func (c *azureCloud) Instances() ([]*v1alpha1.VirtualMachine, error) {
	vmCRDs, err := c.cloudCommon.GetAllCloudAccountsComputeResourceCRDs()
	return vmCRDs, err
}

// InstancesGivenProviderAccount returns VM CRD for all virtualMachines of a given cloud provider account.
func (c *azureCloud) InstancesGivenProviderAccount(accountNamespacedName *types.NamespacedName) ([]*v1alpha1.VirtualMachine,
	error) {
	vmCRDs, err := c.cloudCommon.GetCloudAccountComputeResourceCRDs(accountNamespacedName)
	return vmCRDs, err
}

// IsVirtualPrivateCloudPresent returns true if given ID is managed by the cloud, else false.
func (c *azureCloud) IsVirtualPrivateCloudPresent(vpcUniqueIdentifier string) bool {
	if accCfg := c.getVnetAccount(vpcUniqueIdentifier); accCfg == nil {
		return false
	}
	return true
}

// ////////////////////////////////////////////////////////
// 	AccountMgmtInterface Implementation
// ////////////////////////////////////////////////////////
// AddProviderAccount adds and initializes given account of a cloud provider.
func (c *azureCloud) AddProviderAccount(client client.Client, account *v1alpha1.CloudProviderAccount) error {
	return c.cloudCommon.AddCloudAccount(client, account, account.Spec.AzureConfig)
}

// RemoveProviderAccount removes and cleans up any resources of given account of a cloud provider.
func (c *azureCloud) RemoveProviderAccount(namespacedName *types.NamespacedName) {
	c.cloudCommon.RemoveCloudAccount(namespacedName)
}

// AddAccountResourceSelector adds account specific resource selector.
func (c *azureCloud) AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *v1alpha1.CloudEntitySelector) error {
	return c.cloudCommon.AddSelector(accNamespacedName, selector)
}

// RemoveAccountResourcesSelector removes account specific resource selector.
func (c *azureCloud) RemoveAccountResourcesSelector(accNamespacedName *types.NamespacedName, selectorName string) {
	c.cloudCommon.RemoveSelector(accNamespacedName, selectorName)
}

func (c *azureCloud) GetAccountStatus(accNamespacedName *types.NamespacedName) (*cloudv1alpha1.CloudProviderAccountStatus, error) {
	return c.cloudCommon.GetStatus(accNamespacedName)
}
