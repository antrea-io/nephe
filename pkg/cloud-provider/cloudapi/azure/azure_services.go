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
	"fmt"

	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloud-provider/cloudapi/internal"
)

const (
	azureComputeServiceNameCompute = internal.CloudServiceName("COMPUTE")
)

// azureServiceClientCreateInterface provides interface to create aws service clients.
type azureServiceClientCreateInterface interface {
	resourceGraph() (azureResourceGraphWrapper, error)
	networkInterfaces(subscriptionID string) (azureNwIntfWrapper, error)
	securityGroups(subscriptionID string) (azureNsgWrapper, error)
	applicationSecurityGroups(subscriptionID string) (azureAsgWrapper, error)
	virtualNetworks(subscriptionID string) (azureVirtualNetworksWrapper, error)
	// Add any azure service api client creation methods here
}

// azureServiceSdkConfigProvider provides config required to create azure service clients.
// Implements azureServiceClientCreateInterface interface.
type azureServiceSdkConfigProvider struct {
	authorizer autorest.Authorizer
}

// azureServicesHelper.
type azureServicesHelper interface {
	newServiceSdkConfigProvider(accCfg *azureAccountConfig) (azureServiceClientCreateInterface, error)
}

type azureServicesHelperImpl struct{}

// newServiceSdkConfigProvider returns config to create azure services clients.
func (h *azureServicesHelperImpl) newServiceSdkConfigProvider(accCreds *azureAccountConfig) (
	azureServiceClientCreateInterface, error) {
	var authorizer autorest.Authorizer
	var err error
	clientConfig := auth.NewClientCredentialsConfig(accCreds.ClientID, accCreds.ClientKey, accCreds.TenantID)
	authorizer, err = clientConfig.Authorizer()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize Azure authorizer from credentials: %v", err)
	}
	configProvider := &azureServiceSdkConfigProvider{
		authorizer: authorizer,
	}
	return configProvider, nil
}

func newAzureServiceConfigs(accountNamespacedName *types.NamespacedName, accCredentials interface{}, azureSpecificHelper interface{}) (
	[]internal.CloudServiceInterface, error) {
	azureServicesHelper := azureSpecificHelper.(azureServicesHelper)
	azureAccountCredentials := accCredentials.(*azureAccountConfig)

	var serviceConfigs []internal.CloudServiceInterface

	azureServiceClientCreator, err := azureServicesHelper.newServiceSdkConfigProvider(azureAccountCredentials)
	if err != nil {
		return nil, err
	}

	ec2Service, err := newComputeServiceConfig(accountNamespacedName.String(), azureServiceClientCreator, azureAccountCredentials)
	if err != nil {
		return nil, err
	}
	serviceConfigs = append(serviceConfigs, ec2Service)

	return serviceConfigs, nil
}
