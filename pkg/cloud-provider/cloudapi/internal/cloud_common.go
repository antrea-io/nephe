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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/logging"
)

// CloudCommonHelperInterface interface needs to be implemented by each cloud-plugin. It provides a way to inject
// cloud dependent functionality into plugin-common-framework. Cloud dependent functionality can include cloud
// service operations, credentials management etc.
type CloudCommonHelperInterface interface {
	GetCloudServicesCreateFunc() CloudServiceConfigCreatorFunc
	SetAccountCredentialsFunc() CloudCredentialValidatorFunc
	GetCloudCredentialsComparatorFunc() CloudCredentialComparatorFunc
}

// CloudCommonInterface implements functionality common across all supported cloud-plugins. Each cloud plugin uses
// this interface by composition.
type CloudCommonInterface interface {
	GetCloudAccountByName(namespacedName *types.NamespacedName) (CloudAccountInterface, bool)
	GetCloudAccountByAccountId(accountID *string) (CloudAccountInterface, bool)
	GetCloudAccounts() map[types.NamespacedName]CloudAccountInterface

	GetCloudAccountComputeResourceCRDs(namespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.VirtualMachine,
		error)

	AddCloudAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount, credentials interface{}) error
	RemoveCloudAccount(namespacedName *types.NamespacedName)

	AddSelector(namespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error
	RemoveSelector(accNamespacedName *types.NamespacedName, selectorName string)

	GetStatus(accNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error)

	DoInventoryPoll(accountNamespacedName *types.NamespacedName) error

	DeleteInventoryPollCache(accountNamespacedName *types.NamespacedName) error

	GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.Vpc, error)
}

type cloudCommon struct {
	mutex               sync.RWMutex
	commonHelper        CloudCommonHelperInterface
	logger              func() logging.Logger
	accountConfigs      map[types.NamespacedName]CloudAccountInterface
	cloudSpecificHelper interface{}
	Status              string
}

func NewCloudCommon(logger func() logging.Logger, commonHelper CloudCommonHelperInterface,
	cloudSpecificHelper interface{}) CloudCommonInterface {
	return &cloudCommon{
		logger:              logger,
		commonHelper:        commonHelper,
		cloudSpecificHelper: cloudSpecificHelper,
		accountConfigs:      make(map[types.NamespacedName]CloudAccountInterface),
	}
}

func (c *cloudCommon) AddCloudAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount, credentials interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	namespacedName := &types.NamespacedName{
		Namespace: account.GetNamespace(),
		Name:      account.GetName(),
	}

	existingConfig, found := c.accountConfigs[*namespacedName]
	if found {
		return c.updateCloudAccountConfig(client, credentials, existingConfig)
	}

	config, err := c.newCloudAccountConfig(client, namespacedName, credentials, c.logger)
	if err != nil {
		return err
	}

	c.accountConfigs[*config.GetNamespacedName()] = config

	return nil
}

func (c *cloudCommon) RemoveCloudAccount(namespacedName *types.NamespacedName) {
	_, found := c.GetCloudAccountByName(namespacedName)
	if !found {
		c.logger().V(0).Info("unable to find cloud account", "account", *namespacedName)
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.accountConfigs, *namespacedName)
}

// GetCloudAccountByName finds accCfg matching the namespacedName.
func (c *cloudCommon) GetCloudAccountByName(namespacedName *types.NamespacedName) (CloudAccountInterface, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	accCfg, found := c.accountConfigs[*namespacedName]
	return accCfg, found
}

// GetCloudAccountByAccountId converts accountID to namespacedName and finds the matching accCfg.
func (c *cloudCommon) GetCloudAccountByAccountId(accountID *string) (CloudAccountInterface, bool) {
	// accountID is a string representation of namespacedName ie namespace and provider account name joined with a "/".
	tokens := strings.Split(*accountID, "/")
	if len(tokens) == 2 {
		namespacedName := types.NamespacedName{Namespace: tokens[0], Name: tokens[1]}
		accCfg, found := c.GetCloudAccountByName(&namespacedName)
		return accCfg, found
	} else {
		c.logger().V(0).Info("account id is not in the expected format", "AccountID", accountID)
		return nil, false
	}
}

// GetCloudAccounts returns a copy of all account configs.
func (c *cloudCommon) GetCloudAccounts() map[types.NamespacedName]CloudAccountInterface {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	accountConfigs := map[types.NamespacedName]CloudAccountInterface{}
	for namespacedName, accCfg := range c.accountConfigs {
		accountConfigs[namespacedName] = accCfg
	}
	return accountConfigs
}

func (c *cloudCommon) GetCloudAccountComputeResourceCRDs(accountNamespacedName *types.NamespacedName) (
	map[string]*runtimev1alpha1.VirtualMachine, error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("unable to find cloud account: %v", *accountNamespacedName)
	}

	computeCRs := map[string]*runtimev1alpha1.VirtualMachine{}
	serviceConfigs := accCfg.GetServiceConfigs()
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig.getType() == CloudServiceTypeCompute {
			return serviceConfig.getResourceCRDs(accCfg.GetNamespacedName().Namespace, accCfg.GetNamespacedName()), nil
		}
	}

	return computeCRs, nil
}

func (c *cloudCommon) AddSelector(accountNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("unable to find cloud account: %v", *accountNamespacedName)
	}

	for _, serviceCfg := range accCfg.GetServiceConfigs() {
		serviceCfg.setResourceFilters(selector)
	}

	return nil
}

func (c *cloudCommon) RemoveSelector(accNamespacedName *types.NamespacedName, selectorName string) {
	accCfg, found := c.GetCloudAccountByName(accNamespacedName)
	if !found {
		c.logger().Info("Account not found", "account", *accNamespacedName, "name", selectorName)
		return
	}

	for _, serviceCfg := range accCfg.GetServiceConfigs() {
		serviceCfg.removeResourceFilters(selectorName)
	}
}

func (c *cloudCommon) GetStatus(accountNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("unable to find cloud account: %v", *accountNamespacedName)
	}

	return accCfg.GetStatus(), nil
}

// DoInventoryPoll calls cloud API to get vm and vpc resources.
func (c *cloudCommon) DoInventoryPoll(accountNamespacedName *types.NamespacedName) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("unable to find cloud account: %v", *accountNamespacedName)
	}

	err := accCfg.performInventorySync()
	if err != nil {
		return fmt.Errorf("failed to sync inventory, account: %v, err: %v", *accountNamespacedName, err)
	}

	return nil
}

// DeleteInventoryPollCache resets cloud snapshot to nil.
func (c *cloudCommon) DeleteInventoryPollCache(accountNamespacedName *types.NamespacedName) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("unable to find cloud account %v", *accountNamespacedName)
	}

	accCfg.resetInventorySyncCache()
	return nil
}

// GetVpcInventory gets a map of vpcs applicable for the account.
func (c *cloudCommon) GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.Vpc, error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("unable to find cloud account: %v", *accountNamespacedName)
	}

	serviceConfigs := accCfg.GetServiceConfigs()
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig.getType() == CloudServiceTypeCompute {
			return serviceConfig.getVpcInventory(), nil
		}
	}
	return nil, nil
}
