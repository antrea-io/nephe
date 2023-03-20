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

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
	"antrea.io/nephe/pkg/logging"
)

var awsPluginLogger = func() logging.Logger {
	return logging.GetLogger("aws-plugin")
}

const (
	providerType = cloudcommon.ProviderType(runtimev1alpha1.AWSCloudProvider)
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
//
//	ComputeInterface Implementation
//
// /////////////////////////////////////////////.

// InstancesGivenProviderAccount returns VM CRD for all instances of a given cloud provider account.
func (c *awsCloud) InstancesGivenProviderAccount(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.VirtualMachine,
	error) {
	vmCRDs, err := c.cloudCommon.GetCloudAccountComputeResourceCRDs(accountNamespacedName)
	return vmCRDs, err
}

// ////////////////////////////////////////////////////////
//
//	AccountMgmtInterface Implementation
//
// ////////////////////////////////////////////////////////

// AddProviderAccount adds and initializes given account of a cloud provider.
func (c *awsCloud) AddProviderAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount) error {
	return c.cloudCommon.AddCloudAccount(client, account, account.Spec.AWSConfig)
}

// RemoveProviderAccount removes and cleans up any resources of given account of a cloud provider.
func (c *awsCloud) RemoveProviderAccount(namespacedName *types.NamespacedName) {
	c.cloudCommon.RemoveCloudAccount(namespacedName)
}

// AddAccountResourceSelector adds account specific resource selector.
func (c *awsCloud) AddAccountResourceSelector(accNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error {
	return c.cloudCommon.AddSelector(accNamespacedName, selector)
}

// RemoveAccountResourcesSelector removes account specific resource selector.
func (c *awsCloud) RemoveAccountResourcesSelector(accNamespacedName *types.NamespacedName, selectorName string) {
	c.cloudCommon.RemoveSelector(accNamespacedName, selectorName)
}

func (c *awsCloud) GetAccountStatus(accNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error) {
	return c.cloudCommon.GetStatus(accNamespacedName)
}

// DoInventoryPoll calls cloud API to get cloud resources.
func (c *awsCloud) DoInventoryPoll(accountNamespacedName *types.NamespacedName) error {
	return c.cloudCommon.DoInventoryPoll(accountNamespacedName)
}

// DeleteInventoryPollCache resets cloud snapshot to nil.
func (c *awsCloud) DeleteInventoryPollCache(accountNamespacedName *types.NamespacedName) error {
	return c.cloudCommon.DeleteInventoryPollCache(accountNamespacedName)
}

// GetVpcInventory pulls cloud vpc inventory from internal snapshot.
func (c *awsCloud) GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.Vpc, error) {
	return c.cloudCommon.GetVpcInventory(accountNamespacedName)
}
