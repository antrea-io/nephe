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
	"antrea.io/nephe/pkg/util"
)

// azureServiceClientCreateInterface provides interface to create azure service clients.
type azureServiceClientCreateInterface interface {
	resourceGraph() (azureResourceGraphWrapper, error)
	networkInterfaces(subscriptionID string) (azureNwIntfWrapper, error)
	securityGroups(subscriptionID string) (azureNsgWrapper, error)
	applicationSecurityGroups(subscriptionID string) (azureAsgWrapper, error)
	virtualNetworks(subscriptionID string) (azureVirtualNetworksWrapper, error)
	subscriptions() (azureSubscriptionsWrapper, error)
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
func (h *azureServicesHelperImpl) newServiceSdkConfigProvider(config *azureAccountConfig) (
	azureServiceClientCreateInterface, error) {
	var err error
	var cred azcore.TokenCredential

	// TODO: Expose an option in CPA to specify the cloud type, AzurePublic, AzureGovernment and AzureChina.

	// use role based access if session token is configured.
	if len(strings.TrimSpace(config.SessionToken)) > 0 {
		token := config.SessionToken
		getAssertion := func(_ context.Context) (string, error) { return token, nil }
		cred, err = azidentity.NewClientAssertionCredential(config.TenantID, config.ClientID, getAssertion, nil)
		if err != nil {
			return nil, fmt.Errorf("error initializing Azure authorizer from session token: %v", err)
		}
	} else {
		cred, err = azidentity.NewClientSecretCredential(config.TenantID, config.ClientID, config.ClientKey, nil)
		if err != nil {
			return nil, fmt.Errorf("error initializing Azure authorizer from credentials: %v", err)
		}
	}

	configProvider := &azureServiceSdkConfigProvider{
		cred: cred,
	}
	return configProvider, nil
}

// newAzureServiceConfigs creates a service config for an account and returns a region to service config map or error happened.
func newAzureServiceConfigs(accountNamespacedName *types.NamespacedName, config interface{}, azureSpecificHelper interface{}) (
	map[string]internal.CloudServiceInterface, error) {
	azureServicesHelper := azureSpecificHelper.(azureServicesHelper)
	azureConfig := config.(*azureAccountConfig)

	// create config provider.
	azureServiceClientCreator, err := azureServicesHelper.newServiceSdkConfigProvider(azureConfig)
	if err != nil {
		return map[string]internal.CloudServiceInterface{}, err
	}

	// check locations valid and process locations.
	regions, err := processRegions(accountNamespacedName, azureServiceClientCreator, azureConfig)
	if err != nil {
		return map[string]internal.CloudServiceInterface{}, err
	}
	azureConfig.regions = regions

	// create service config.
	computeService, err := newComputeServiceConfig(*accountNamespacedName, azureServiceClientCreator, azureConfig)
	if err != nil {
		return map[string]internal.CloudServiceInterface{}, err
	}

	return computeService, nil
}

// updateAzureServiceConfigs updates the current region to service config map according to the new map.
// The current single Azure service config is reused and updated.
func updateAzureServiceConfigs(current, new map[string]internal.CloudServiceInterface) error {
	// get the current service config and the new service config.
	var currentServiceConfig internal.CloudServiceInterface
	var newServiceConfig internal.CloudServiceInterface
	for _, s := range current {
		currentServiceConfig = s
		break
	}
	for _, s := range new {
		newServiceConfig = s
		break
	}
	// update the regions in the map.
	for region := range current {
		_, found := new[region]
		if !found {
			delete(current, region)
		} else {
			delete(new, region)
		}
	}
	for region := range new {
		current[region] = currentServiceConfig
	}
	return currentServiceConfig.UpdateServiceConfig(newServiceConfig)
}

// processRegions check if account regions are all valid and deduplicate regions.
func processRegions(acc *types.NamespacedName, services azureServiceClientCreateInterface, config *azureAccountConfig) ([]string, error) {
	subscriptionsClient, err := services.subscriptions()
	if err != nil {
		return nil, fmt.Errorf("error creating subscriptions sdk api client for account: %v, err: %v", *acc, err)
	}
	locations, err := subscriptionsClient.listComplete(context.Background(), config.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("error listing locations for subscription: %v, err: %v", config.SubscriptionID, err)
	}

	locationMap := make(map[string]struct{})
	existingLocations := make(map[string]struct{})
	allLocations := make([]string, 0, len(locations))
	retLocations := make([]string, 0, len(config.regions))
	for _, location := range locations {
		locationMap[strings.ToLower(*location.Name)] = struct{}{}
		allLocations = append(allLocations, *location.Name)
	}
	for _, location := range config.regions {
		if _, found := existingLocations[location]; found {
			continue
		}
		if _, found := locationMap[location]; !found {
			return nil, fmt.Errorf("%s, %s not in supported locations %v", util.ErrorMsgRegion, location, allLocations)
		}
		existingLocations[location] = struct{}{}
		retLocations = append(retLocations, location)
	}
	return retLocations, nil
}
