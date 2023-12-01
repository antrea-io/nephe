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
	"strings"
	"sync"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/logging"
)

type CloudAccountInterface interface {
	GetNamespacedName() *types.NamespacedName
	GetServiceConfig(region string) CloudServiceInterface
	GetAllServiceConfigs() []CloudServiceInterface
	FindRegion(vpcID string) string
	GetStatus() *crdv1alpha1.CloudProviderAccountStatus
	GetAccountConfigState() bool
	LockMutex()
	UnlockMutex()
	performInventorySync() error
	resetInventoryCache()
}

type cloudAccountConfig struct {
	mutex                    sync.Mutex
	namespacedName           *types.NamespacedName
	config                   interface{}
	regionToServiceConfigMap map[string]CloudServiceInterface
	logger                   func() logging.Logger
	Status                   *crdv1alpha1.CloudProviderAccountStatus
	// Indicates whether cloud account config can be used to make cloud API calls.
	state bool
}

type CloudConfigValidatorFunc func(client client.Client, config interface{}) (interface{}, error)
type CloudConfigComparatorFunc func(accountName string, existing interface{}, new interface{}) bool
type CloudServiceConfigCreatorFunc func(namespacedName *types.NamespacedName, cloudConvertedConfig interface{},
	helper interface{}) (map[string]CloudServiceInterface, error)
type CloudServiceConfigUpdateFunc func(current, new map[string]CloudServiceInterface) error

func (c *cloudCommon) getFunctionPointers() (CloudConfigValidatorFunc, CloudServiceConfigCreatorFunc,
	CloudConfigComparatorFunc, CloudServiceConfigUpdateFunc, error) {
	configValidatorFunc := c.commonHelper.SetAccountConfigFunc()
	if configValidatorFunc == nil {
		return nil, nil, nil, nil, fmt.Errorf("error creating account config, registered cloud-config validator function cannot be nil")
	}
	cloudServicesCreateFunc := c.commonHelper.GetCloudServicesCreateFunc()
	if cloudServicesCreateFunc == nil {
		return nil, nil, nil, nil, fmt.Errorf("error creating account config, registered cloud-services creator function cannot be nil")
	}

	configComparatorFunc := c.commonHelper.GetCloudConfigComparatorFunc()
	if configComparatorFunc == nil {
		return nil, nil, nil, nil, fmt.Errorf("error creating account config, registered cloud-services comparator function cannot be nil")
	}
	cloudServicesUpdateFunc := c.commonHelper.GetCloudServicesUpdateFunc()
	if cloudServicesUpdateFunc == nil {
		return nil, nil, nil, nil, fmt.Errorf("error updating account config, registered cloud services updater function cannot be nil")
	}

	return configValidatorFunc, cloudServicesCreateFunc, configComparatorFunc, cloudServicesUpdateFunc, nil
}

func (c *cloudCommon) newCloudAccountConfig(client client.Client, namespacedName *types.NamespacedName, credentials interface{},
	loggerFunc func() logging.Logger) (CloudAccountInterface, error) {
	configValidatorFunc, cloudServicesCreateFunc, _, _, err := c.getFunctionPointers()
	if err != nil {
		return nil, err
	}

	accConfig := &cloudAccountConfig{
		logger:                   loggerFunc,
		namespacedName:           namespacedName,
		regionToServiceConfigMap: make(map[string]CloudServiceInterface),
		Status:                   &crdv1alpha1.CloudProviderAccountStatus{},
		state:                    false,
	}

	accConfig.config, err = configValidatorFunc(client, credentials)
	if err != nil {
		accConfig.Status.Error = err.Error()
		return accConfig, err
	}

	accConfig.regionToServiceConfigMap, err = cloudServicesCreateFunc(namespacedName, accConfig.config, c.cloudSpecificHelper)
	if err != nil {
		accConfig.Status.Error = err.Error()
		return accConfig, err
	}
	// Mark account state as valid.
	accConfig.state = true
	return accConfig, nil
}

func (c *cloudCommon) updateCloudAccountConfig(client client.Client, config interface{}, cloudAccConfig CloudAccountInterface) error {
	currentConfig := cloudAccConfig.(*cloudAccountConfig)
	cloudAccountConfigState := true

	configValidatorFunc, cloudServicesCreateFunc, configComparatorFunc, cloudServicesUpdateFunc, err := c.getFunctionPointers()
	if err != nil {
		cloudAccountConfigState = false
		return err
	}
	defer func() {
		// Update the cloud account config state for each update.
		currentConfig.state = cloudAccountConfigState
		if err != nil {
			currentConfig.Status.Error = err.Error()
		}
	}()

	if configComparatorFunc == nil {
		c.logger().Info("Cloud config comparator func nil. config not updated. existing credentials will be used.",
			"account", currentConfig.GetNamespacedName())
		return nil
	}

	cloudConvertedNewConfig, err := configValidatorFunc(client, config)
	if !configComparatorFunc(currentConfig.namespacedName.String(), currentConfig.config, cloudConvertedNewConfig) {
		c.logger().Info("Config not changed", "account", currentConfig.namespacedName)
		return err
	}
	currentConfig.config = cloudConvertedNewConfig
	c.logger().Info("Config updated", "account", currentConfig.namespacedName)
	if err != nil {
		cloudAccountConfigState = false
		return err
	}

	newServiceConfigMap, err := cloudServicesCreateFunc(currentConfig.namespacedName, cloudConvertedNewConfig, c.cloudSpecificHelper)
	if err != nil {
		cloudAccountConfigState = false
		return err
	}

	return cloudServicesUpdateFunc(currentConfig.regionToServiceConfigMap, newServiceConfigMap)
}

// performInventorySync gets inventory from cloud for the cloud account.
func (accCfg *cloudAccountConfig) performInventorySync() error {
	var retErr error
	var wg sync.WaitGroup
	ch := make(chan error)
	serviceConfigs := accCfg.GetAllServiceConfigs()
	wg.Add(len(serviceConfigs))
	go func() {
		wg.Wait()
		close(ch)
	}()

	for _, service := range serviceConfigs {
		go func(service CloudServiceInterface, errCh chan error) {
			defer wg.Done()
			err := service.DoResourceInventory()
			// set the error status to be used later in `CloudProviderAccount` CR.
			service.GetInventoryStats().UpdateInventoryPollStats(err)
			errCh <- err
		}(service, ch)
	}
	for err := range ch {
		retErr = multierr.Append(retErr, err)
	}
	if retErr != nil {
		accCfg.Status.Error = retErr.Error()
	} else {
		accCfg.Status.Error = ""
	}
	return retErr
}

func (accCfg *cloudAccountConfig) GetNamespacedName() *types.NamespacedName {
	return accCfg.namespacedName
}

// GetServiceConfig returns the service config associated with the given region.
func (accCfg *cloudAccountConfig) GetServiceConfig(region string) CloudServiceInterface {
	return accCfg.regionToServiceConfigMap[region]
}

// GetAllServiceConfigs returns all unique service configs in the account.
func (accCfg *cloudAccountConfig) GetAllServiceConfigs() []CloudServiceInterface {
	serviceConfigList := make([]CloudServiceInterface, 0, len(accCfg.regionToServiceConfigMap))
	serviceConfigs := make(map[CloudServiceInterface]struct{}, 0)
	for _, serviceConfig := range accCfg.regionToServiceConfigMap {
		if _, found := serviceConfigs[serviceConfig]; !found {
			serviceConfigList = append(serviceConfigList, serviceConfig)
			serviceConfigs[serviceConfig] = struct{}{}
		}
	}
	return serviceConfigList
}

// FindRegion finds the region of a vpc based on its id.
func (accCfg *cloudAccountConfig) FindRegion(vpcId string) string {
	for _, service := range accCfg.GetAllServiceConfigs() {
		if vpc, found := service.GetCloudInventory().VpcMap[strings.ToLower(vpcId)]; found {
			return vpc.Status.Region
		}
	}
	return ""
}

func (accCfg *cloudAccountConfig) GetStatus() *crdv1alpha1.CloudProviderAccountStatus {
	return accCfg.Status
}

// resetInventoryCache resets the account inventory.
func (accCfg *cloudAccountConfig) resetInventoryCache() {
	for _, serviceConfig := range accCfg.GetAllServiceConfigs() {
		serviceConfig.ResetInventoryCache()
	}
}

func (accCfg *cloudAccountConfig) LockMutex() {
	accCfg.mutex.Lock()
}

func (accCfg *cloudAccountConfig) UnlockMutex() {
	accCfg.mutex.Unlock()
}

// GetAccountConfigState returns cloud account config state.
func (accCfg *cloudAccountConfig) GetAccountConfigState() bool {
	return accCfg.state
}
