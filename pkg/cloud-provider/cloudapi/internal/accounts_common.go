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

package internal

import (
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"

	"antrea.io/nephe/pkg/logging"
)

type CloudAccountInterface interface {
	GetNamespacedName() *types.NamespacedName
	GetServiceConfigs() map[CloudServiceName]*CloudServiceCommon
	GetServiceConfigByName(name CloudServiceName) (CloudServiceInterface, error)
	GetStatus() *cloudv1alpha1.CloudProviderAccountStatus

	startPeriodicInventorySync() error
	stopPeriodicInventorySync()
}

type cloudAccountConfig struct {
	mutex                 sync.Mutex
	namespacedName        *types.NamespacedName
	credentials           interface{}
	serviceConfigs        map[CloudServiceName]*CloudServiceCommon
	inventoryPollInterval time.Duration
	inventoryChannel      chan struct{}
	logger                func() logging.Logger
	Status                *cloudv1alpha1.CloudProviderAccountStatus
}

type CloudCredentialValidatorFunc func(client client.Client, credentials interface{}) (interface{}, error)
type CloudCredentialComparatorFunc func(accountName string, existing interface{}, new interface{}) bool
type CloudServiceConfigCreatorFunc func(namespacedName *types.NamespacedName, cloudConvertedCredentials interface{},
	helper interface{}) ([]CloudServiceInterface, error)

func (c *cloudCommon) newCloudAccountConfig(client client.Client, namespacedName *types.NamespacedName, credentials interface{},
	pollInterval time.Duration, loggerFunc func() logging.Logger) (CloudAccountInterface, error) {
	credentialsValidatorFunc := c.commonHelper.SetAccountCredentialsFunc()
	if credentialsValidatorFunc == nil {
		return nil, fmt.Errorf("registered cloud-credentials validator function cannot be nil")
	}
	cloudConvertedCredential, err := credentialsValidatorFunc(client, credentials)
	if err != nil {
		return nil, err
	}
	if cloudConvertedCredential == nil {
		return nil, fmt.Errorf("cloud credentials cannot be nil (accountName: %v)", namespacedName)
	}

	cloudServicesCreateFunc := c.commonHelper.GetCloudServicesCreateFunc()
	if cloudServicesCreateFunc == nil {
		return nil, fmt.Errorf("registered cloud-services creator function cannot be nil")
	}
	serviceConfigs, err := cloudServicesCreateFunc(namespacedName, cloudConvertedCredential, c.cloudSpecificHelper)
	if err != nil {
		return nil, err
	}
	serviceConfigMap := make(map[CloudServiceName]*CloudServiceCommon)
	for _, serviceCfg := range serviceConfigs {
		serviceConfig := &CloudServiceCommon{
			serviceInterface: serviceCfg,
		}
		serviceConfigMap[serviceCfg.GetName()] = serviceConfig
	}

	status := &cloudv1alpha1.CloudProviderAccountStatus{}
	return &cloudAccountConfig{
		logger:                loggerFunc,
		namespacedName:        namespacedName,
		inventoryPollInterval: pollInterval,
		serviceConfigs:        serviceConfigMap,
		credentials:           cloudConvertedCredential,
		Status:                status,
	}, nil
}

func (c *cloudCommon) updateCloudAccountConfig(client client.Client, credentials interface{}, config CloudAccountInterface) error {
	currentConfig := config.(*cloudAccountConfig)
	credentialsValidatorFunc := c.commonHelper.SetAccountCredentialsFunc()
	if credentialsValidatorFunc == nil {
		return fmt.Errorf("registered cloud-credentials validator function cannot be nil")
	}
	cloudConvertedNewCredential, err := credentialsValidatorFunc(client, credentials)
	if err != nil {
		return err
	}
	if cloudConvertedNewCredential == nil {
		return fmt.Errorf("cloud credentials cannot be nil. update failed. (account: %v)", currentConfig.GetNamespacedName())
	}

	credentialsComparatorFunc := c.commonHelper.GetCloudCredentialsComparatorFunc()
	if credentialsComparatorFunc == nil {
		c.logger().Info("cloud credentials comparator func nil. credentials not updated. existing credentials will be used.",
			"account", currentConfig.GetNamespacedName())
		return nil
	}

	cloudServicesCreateFunc := c.commonHelper.GetCloudServicesCreateFunc()
	if cloudServicesCreateFunc == nil {
		return fmt.Errorf("registered cloud-services creator function cannot be nil")
	}
	serviceConfigs, err := cloudServicesCreateFunc(currentConfig.namespacedName, cloudConvertedNewCredential, c.cloudSpecificHelper)
	if err != nil {
		return err
	}
	serviceConfigMap := make(map[CloudServiceName]CloudServiceInterface)
	for _, serviceCfg := range serviceConfigs {
		serviceConfigMap[serviceCfg.GetName()] = serviceCfg
	}

	currentConfig.update(credentialsComparatorFunc, cloudConvertedNewCredential, serviceConfigMap, c.logger())

	return nil
}

func (accCfg *cloudAccountConfig) update(credentialComparator CloudCredentialComparatorFunc, newCredentials interface{},
	newSvcConfigMap map[CloudServiceName]CloudServiceInterface, logger logging.Logger) {
	accCfg.mutex.Lock()
	defer accCfg.mutex.Unlock()

	credentialsChanged := credentialComparator(accCfg.namespacedName.String(), newCredentials, accCfg.credentials)
	if !credentialsChanged {
		logger.Info("credentials not changed.", "account", accCfg.namespacedName)
		return
	}

	accCfg.credentials = newCredentials
	logger.Info("credentials updated.", "account", accCfg.namespacedName)

	existingSvcConfigMap := accCfg.serviceConfigs
	for name, svcConfig := range existingSvcConfigMap {
		newSvcCfg, found := newSvcConfigMap[name]
		if !found {
			// should not happen
			continue
		}
		svcConfig.updateServiceConfig(newSvcCfg)
		logger.Info("service config updated (api-clients to use new creds)", "account", accCfg.namespacedName,
			"serviceName", name)
	}
}

func (accCfg *cloudAccountConfig) performInventorySync() error {
	accCfg.mutex.Lock()
	defer accCfg.mutex.Unlock()

	serviceConfigs := accCfg.serviceConfigs

	ch := make(chan error)
	var wg sync.WaitGroup
	wg.Add(len(serviceConfigs))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for _, serviceConfig := range serviceConfigs {
		go func(serviceCfg *CloudServiceCommon) {
			defer wg.Done()

			hasFilters, isFilterNil := serviceCfg.hasFiltersConfigured()
			if !hasFilters {
				accCfg.logger().Info("fetching resources from cloud skipped", "service", serviceCfg.getName(),
					"account", accCfg.namespacedName, "resource-filters", "not-configured")
				return
			}
			err := serviceCfg.doResourceInventory()
			if err != nil {
				accCfg.logger().Error(err, "error fetching resources from cloud", "service", serviceCfg.getName(),
					"account", accCfg.namespacedName)
				accCfg.Status.Error = err.Error()
			} else {
				if isFilterNil {
					accCfg.logger().V(1).Info("fetching resources from cloud", "service", serviceCfg.getName(),
						"account", accCfg.namespacedName, "resource-filters", "all(nil)")
				} else {
					accCfg.logger().V(1).Info("fetching resources from cloud", "service", serviceCfg.getName(),
						"account", accCfg.namespacedName, "resource-filters", "configured")
				}
			}
			inventoryStats := serviceCfg.getInventoryStats()
			inventoryStats.UpdateInventoryPollStats(err)
		}(serviceConfig)
	}

	var err error
	for e := range ch {
		if e != nil {
			err = multierr.Append(err, e)
		}
	}

	return err
}

func (accCfg *cloudAccountConfig) GetNamespacedName() *types.NamespacedName {
	return accCfg.namespacedName
}

func (accCfg *cloudAccountConfig) GetServiceConfigs() map[CloudServiceName]*CloudServiceCommon {
	svcNameCfgMap := make(map[CloudServiceName]*CloudServiceCommon)
	for name, serviceCommon := range accCfg.serviceConfigs {
		svcNameCfgMap[name] = serviceCommon
	}
	return svcNameCfgMap
}

func (accCfg *cloudAccountConfig) GetServiceConfigByName(name CloudServiceName) (CloudServiceInterface, error) {
	if serviceCfg, found := accCfg.serviceConfigs[name]; found {
		return serviceCfg.serviceInterface, nil
	}

	return nil, fmt.Errorf("%v service not found for account %v", name, accCfg.namespacedName)
}

func (accCfg *cloudAccountConfig) GetStatus() *cloudv1alpha1.CloudProviderAccountStatus {
	return accCfg.Status
}

func (accCfg *cloudAccountConfig) startPeriodicInventorySync() error {
	err := accCfg.performInventorySync()
	if err != nil {
		return err
	}

	accCfg.mutex.Lock()
	defer accCfg.mutex.Unlock()

	if accCfg.inventoryChannel == nil {
		ch := make(chan struct{})
		condFunc := func() (bool, error) {
			_ = accCfg.performInventorySync()
			return false, nil
		}
		// nolint:errcheck
		go wait.PollUntil(accCfg.inventoryPollInterval, condFunc, ch)
		accCfg.inventoryChannel = ch
	}

	return err
}

func (accCfg *cloudAccountConfig) stopPeriodicInventorySync() {
	if accCfg.inventoryChannel != nil {
		close(accCfg.inventoryChannel)
		accCfg.inventoryChannel = nil
	}

	for _, serviceConfig := range accCfg.serviceConfigs {
		serviceConfig.resetCachedState()
	}
}
