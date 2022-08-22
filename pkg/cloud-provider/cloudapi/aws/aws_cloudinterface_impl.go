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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
	"antrea.io/nephe/pkg/logging"
)

var awsPluginLogger = func() logging.Logger {
	return logging.GetLogger("aws-plugin")
}

const (
	providerType = cloudcommon.ProviderType(v1alpha1.AWSCloudProvider)
)

// awsCloud implements CloudInterface for AWS.
type awsCloud struct {
	cloudCommon internal.CloudCommonInterface
}

// newAWSCloud creates a new instance of awsCloud.
func newAWSCloud(awsSpecificHelper awsServicesHelper) *awsCloud {
	awsCloud := &awsCloud{
		cloudCommon: internal.NewCloudCommon(awsPluginLogger, &awsCloudCommonHelperImpl{}, awsSpecificHelper),
	}
	return awsCloud
}

// Register registers cloud provider type and creates awsCloud object for the provider. Any cloud account added at later
// point with this cloud provider using CloudInterface API will get added to this awsCloud object.
func Register() cloudcommon.CloudInterface {
	return newAWSCloud(&awsServicesHelperImpl{})
}

// ProviderType returns the cloud provider type (aws, azure, gce etc).
func (c *awsCloud) ProviderType() cloudcommon.ProviderType {
	return providerType
}

// /////////////////////////////////////////////
// 	ComputeInterface Implementation
// /////////////////////////////////////////////.
// Instances returns VM status for all instances across all accounts of a cloud provider.
func (c *awsCloud) Instances() ([]*v1alpha1.VirtualMachine, error) {
	vmCRDs, err := c.cloudCommon.GetAllCloudAccountsComputeResourceCRDs()
	return vmCRDs, err
}

// InstancesGivenProviderAccount returns VM CRD for all instances of a given cloud provider account.
func (c *awsCloud) InstancesGivenProviderAccount(accountNamespacedName *types.NamespacedName) ([]*v1alpha1.VirtualMachine,
	error) {
	vmCRDs, err := c.cloudCommon.GetCloudAccountComputeResourceCRDs(accountNamespacedName)
	return vmCRDs, err
}

// IsVirtualPrivateCloudPresent returns true if given ID is managed by the cloud, else false.
func (c *awsCloud) IsVirtualPrivateCloudPresent(vpcUniqueIdentifier string) bool {
	if accCfg := c.getVpcAccount(vpcUniqueIdentifier); accCfg == nil {
		return false
	}
	return true
}

// ////////////////////////////////////////////////////////
// 	AccountMgmtInterface Implementation
// ////////////////////////////////////////////////////////
// AddProviderAccount adds and initializes given account of a cloud provider.
func (c *awsCloud) AddProviderAccount(client client.Client, account *v1alpha1.CloudProviderAccount) error {
	return c.cloudCommon.AddCloudAccount(client, account, account.Spec.AWSConfig)
}

// RemoveProviderAccount removes and cleans up any resources of given account of a cloud provider.
func (c *awsCloud) RemoveProviderAccount(namespacedName *types.NamespacedName) {
	c.cloudCommon.RemoveCloudAccount(namespacedName)
}

// AddAccountResourceSelector adds account specific resource selector.
func (c *awsCloud) AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *v1alpha1.CloudEntitySelector) error {
	return c.cloudCommon.AddSelector(accNamespacedName, selector)
}

// RemoveAccountResourcesSelector removes account specific resource selector.
func (c *awsCloud) RemoveAccountResourcesSelector(accNamespacedName *types.NamespacedName, selectorName string) {
	c.cloudCommon.RemoveSelector(accNamespacedName, selectorName)
}

func (c *awsCloud) GetAccountStatus(accNamespacedName *types.NamespacedName) (*cloudv1alpha1.CloudProviderAccountStatus, error) {
	return c.cloudCommon.GetStatus(accNamespacedName)
}
