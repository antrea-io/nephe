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
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/logging"
)

type CloudAccountInterface interface {
	GetNamespacedName() *types.NamespacedName
	GetServiceConfig() CloudServiceInterface
	GetStatus() *crdv1alpha1.CloudProviderAccountStatus
	LockMutex()
	UnlockMutex()
	performInventorySync() error
	resetInventoryCache()
}

type cloudAccountConfig struct {
	mutex          sync.Mutex
	namespacedName *types.NamespacedName
	credentials    interface{}
	serviceConfig  CloudServiceInterface
	logger         func() logging.Logger
	Status         *crdv1alpha1.CloudProviderAccountStatus
}

type CloudCredentialValidatorFunc func(client client.Client, credentials interface{}) (interface{}, error)
type CloudCredentialComparatorFunc func(accountName string, existing interface{}, new interface{}) bool
type CloudServiceConfigCreatorFunc func(namespacedName *types.NamespacedName, cloudConvertedCredentials interface{},
	helper interface{}) (CloudServiceInterface, error)

func (c *cloudCommon) newCloudAccountConfig(client client.Client, namespacedName *types.NamespacedName, credentials interface{},
	loggerFunc func() logging.Logger) (CloudAccountInterface, error) {
	credentialsValidatorFunc := c.commonHelper.SetAccountCredentialsFunc()
	if credentialsValidatorFunc == nil {
		return nil, fmt.Errorf("error creating account config, registered cloud-credentials validator function cannot be nil")
	}
	cloudServicesCreateFunc := c.commonHelper.GetCloudServicesCreateFunc()
	if cloudServicesCreateFunc == nil {
		return nil, fmt.Errorf("error creating account config, registered cloud-services creator function cannot be nil")
	}

	cloudConvertedCredential, err := credentialsValidatorFunc(client, credentials)
	if err != nil {
		return nil, err
	}

	serviceConfig, err := cloudServicesCreateFunc(namespacedName, cloudConvertedCredential, c.cloudSpecificHelper)
	if err != nil {
		return nil, err
	}

	status := &crdv1alpha1.CloudProviderAccountStatus{}
	return &cloudAccountConfig{
		logger:         loggerFunc,
		namespacedName: namespacedName,
		serviceConfig:  serviceConfig,
		credentials:    cloudConvertedCredential,
		Status:         status,
	}, nil
}

func (c *cloudCommon) updateCloudAccountConfig(client client.Client, credentials interface{}, config CloudAccountInterface) error {
	currentConfig := config.(*cloudAccountConfig)
	credentialsValidatorFunc := c.commonHelper.SetAccountCredentialsFunc()
	if credentialsValidatorFunc == nil {
		return fmt.Errorf("error updating account config, registered cloud-credentials validator function cannot be nil")
	}
	credentialsComparatorFunc := c.commonHelper.GetCloudCredentialsComparatorFunc()
	if credentialsComparatorFunc == nil {
		c.logger().Info("Cloud credentials comparator func nil. credentials not updated. existing credentials will be used.",
			"account", currentConfig.GetNamespacedName())
		return nil
	}
	cloudServicesCreateFunc := c.commonHelper.GetCloudServicesCreateFunc()
	if cloudServicesCreateFunc == nil {
		return fmt.Errorf("error updating account config, registered cloud services creator function cannot be nil")
	}

	cloudConvertedNewCredential, err := credentialsValidatorFunc(client, credentials)
	if !credentialsComparatorFunc(currentConfig.namespacedName.String(), cloudConvertedNewCredential, currentConfig.credentials) {
		c.logger().Info("Credentials not changed", "account", currentConfig.namespacedName)
		return err
	}
	currentConfig.credentials = cloudConvertedNewCredential
	c.logger().Info("Credentials updated", "account", currentConfig.namespacedName)
	// When credentialsValidatorFunc() returns error, abort updating service configs.
	if err != nil {
		return err
	}

	serviceConfig, err := cloudServicesCreateFunc(currentConfig.namespacedName, cloudConvertedNewCredential, c.cloudSpecificHelper)
	if err != nil {
		return err
	}
	return currentConfig.serviceConfig.UpdateServiceConfig(serviceConfig)
}

func (accCfg *cloudAccountConfig) performInventorySync() error {
	err := accCfg.serviceConfig.DoResourceInventory()
	if err != nil {
		// set the error status to be used later in `CloudProviderAccount` CR.
		accCfg.Status.Error = err.Error()
	}
	accCfg.serviceConfig.GetInventoryStats().UpdateInventoryPollStats(err)
	return err
}

func (accCfg *cloudAccountConfig) GetNamespacedName() *types.NamespacedName {
	return accCfg.namespacedName
}

func (accCfg *cloudAccountConfig) GetServiceConfig() CloudServiceInterface {
	return accCfg.serviceConfig
}

func (accCfg *cloudAccountConfig) GetStatus() *crdv1alpha1.CloudProviderAccountStatus {
	return accCfg.Status
}

func (accCfg *cloudAccountConfig) resetInventoryCache() {
	accCfg.serviceConfig.ResetInventoryCache()
}

func (accCfg *cloudAccountConfig) LockMutex() {
	accCfg.mutex.Lock()
}

func (accCfg *cloudAccountConfig) UnlockMutex() {
	accCfg.mutex.Unlock()
}
