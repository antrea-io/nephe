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

package aws

import (
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
	"antrea.io/nephe/pkg/logging"
)

var awsPluginLogger = func() logging.Logger {
	return logging.GetLogger("aws-plugin")
}

const (
	providerType = runtimev1alpha1.AWSCloudProvider
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
func Register() *awsCloud {
	return newAWSCloud(&awsServicesHelperImpl{})
}

// ProviderType returns the cloud provider type (aws, azure, gce etc).
func (c *awsCloud) ProviderType() runtimev1alpha1.CloudProvider {
	return providerType
}
