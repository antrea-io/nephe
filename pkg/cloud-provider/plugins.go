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

package cloudprovider

import (
	"fmt"
	"sync"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/aws"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/azure"
	cloudcommon "antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/logging"
)

// All registered cloudv1alpha1 providers.
var (
	providersMutex   sync.Mutex
	providers        = make(map[cloudcommon.ProviderType]cloudcommon.CloudInterface)
	corePluginLogger = func() logging.Logger {
		return logging.GetLogger("core-plugin")
	}
)

// init registers provider object. Each newly added provider should register itself here.
func init() {
	registerCloudProvider(cloudcommon.ProviderType(cloudv1alpha1.AWSCloudProvider), aws.Register())
	registerCloudProvider(cloudcommon.ProviderType(cloudv1alpha1.AzureCloudProvider), azure.Register())
}

// registerCloudProvider registers a cloudv1alpha1 provider factory by type.  This
// is expected to happen during controller startup.
func registerCloudProvider(providerType cloudcommon.ProviderType, cloud cloudcommon.CloudInterface) {
	providersMutex.Lock()
	defer providersMutex.Unlock()
	if _, found := providers[providerType]; found {
		corePluginLogger().V(0).Info("cloudv1alpha1 provider already exists",
			"type", providerType)
		return
	}
	providers[providerType] = cloud

	corePluginLogger().V(1).Info("registered cloudv1alpha1 provider successfully",
		"type", providerType)
}

// GetCloudInterface returns an instance of the cloudv1alpha1 provider type, or nil if
// the type is unknown.  The error return is only used if the named provider
// was known but failed to initialize.
func GetCloudInterface(providerType cloudcommon.ProviderType) (cloudcommon.CloudInterface, error) {
	providersMutex.Lock()
	defer providersMutex.Unlock()
	cloud, found := providers[providerType]
	if !found {
		return nil, fmt.Errorf("unsupported cloudv1alpha1 provider %q", providerType)
	}
	return cloud, nil
}

func GetSupportedCloudProviderTypes() []cloudcommon.ProviderType {
	providersMutex.Lock()
	defer providersMutex.Unlock()

	providerTypes := make([]cloudcommon.ProviderType, 0, len(providers))
	for providerType := range providers {
		providerTypes = append(providerTypes, providerType)
	}
	return providerTypes
}
