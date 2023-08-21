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
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"k8s.io/apimachinery/pkg/types"

	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
)

// azureServiceClientCreateInterface provides interface to create azure service clients.
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
	cred azcore.TokenCredential
}

// azureServicesHelper.
type azureServicesHelper interface {
	newServiceSdkConfigProvider(accCfg *azureAccountConfig) (azureServiceClientCreateInterface, error)
}

type azureServicesHelperImpl struct{}

// newServiceSdkConfigProvider returns config to create azure services clients.
func (h *azureServicesHelperImpl) newServiceSdkConfigProvider(accCreds *azureAccountConfig) (
	azureServiceClientCreateInterface, error) {
	var err error
	var cred azcore.TokenCredential

	// TODO: Expose an option in CPA to specify the cloud type, AzurePublic, AzureGovernment and AzureChina.

	// use role based access if session token is configured.
	if len(strings.TrimSpace(accCreds.SessionToken)) > 0 {
		token := accCreds.SessionToken
		getAssertion := func(_ context.Context) (string, error) { return token, nil }
		cred, err = azidentity.NewClientAssertionCredential(accCreds.TenantID, accCreds.ClientID, getAssertion, nil)
		if err != nil {
			return nil, fmt.Errorf("error initializing Azure authorizer from session token: %v", err)
		}
	} else {
		cred, err = azidentity.NewClientSecretCredential(accCreds.TenantID, accCreds.ClientID, accCreds.ClientKey, nil)
		if err != nil {
			return nil, fmt.Errorf("error initializing Azure authorizer from credentials: %v", err)
		}
	}

	configProvider := &azureServiceSdkConfigProvider{
		cred: cred,
	}
	return configProvider, nil
}

func newAzureServiceConfigs(accountNamespacedName *types.NamespacedName, accCredentials interface{}, azureSpecificHelper interface{}) (
	internal.CloudServiceInterface, error) {
	azureServicesHelper := azureSpecificHelper.(azureServicesHelper)
	azureAccountCredentials := accCredentials.(*azureAccountConfig)

	azureServiceClientCreator, err := azureServicesHelper.newServiceSdkConfigProvider(azureAccountCredentials)
	if err != nil {
		return nil, err
	}

	computeService, err := newComputeServiceConfig(*accountNamespacedName, azureServiceClientCreator, azureAccountCredentials)
	if err != nil {
		return nil, err
	}

	return computeService, nil
}
