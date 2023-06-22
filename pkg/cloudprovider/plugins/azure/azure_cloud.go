// Copyright 2023 Antrea Authors.
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
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"antrea.io/nephe/pkg/logging"
)

const (
	providerType = runtimev1alpha1.AzureCloudProvider
)

var azurePluginLogger = func() logging.Logger {
	return logging.GetLogger("azure-plugin")
}

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
func Register() *azureCloud {
	return newAzureCloud(&azureServicesHelperImpl{})
}

// ProviderType returns the cloud provider type (aws, azure, gce etc).
func (c *azureCloud) ProviderType() runtimev1alpha1.CloudProvider {
	return providerType
}
