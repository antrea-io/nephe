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
	"reflect"
	"strings"
	"sync"
	"time"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/logging"
	nephetypes "antrea.io/nephe/pkg/types"
)

var (
	RuntimeAPIVersion               = "runtime.cloud.antrea.io/v1alpha1"
	VirtualMachineRuntimeObjectKind = reflect.TypeOf(runtimev1alpha1.VirtualMachine{}).Name()

	MaxCloudResourceResponse  int64 = 100
	InventoryInitWaitDuration       = time.Second * 30
	AccountCredentialsDefault       = "default"
)

const (
	AccountConfigNotFound = "unable to find cloud account config"
	AccountConfigInvalid  = "invalid cloud account config"
)

// CloudCommonHelperInterface interface needs to be implemented by each cloud-plugin. It provides a way to inject
// cloud dependent functionality into plugin-cloud-framework. Cloud dependent functionality can include cloud
// service operations, config management etc.
type CloudCommonHelperInterface interface {
	GetCloudServicesCreateFunc() CloudServiceConfigCreatorFunc
	GetCloudServicesUpdateFunc() CloudServiceConfigUpdateFunc
	SetAccountConfigFunc() CloudConfigValidatorFunc
	GetCloudConfigComparatorFunc() CloudConfigComparatorFunc
}

// CloudCommonInterface implements functionality common across all supported cloud-plugins. Each cloud plugin uses
// this interface by composition.
type CloudCommonInterface interface {
	GetCloudAccountByName(namespacedName *types.NamespacedName) (CloudAccountInterface, error)
	GetCloudAccountByAccountId(accountID *string) (CloudAccountInterface, error)
	GetCloudAccounts() map[types.NamespacedName]CloudAccountInterface
	AddCloudAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount, config interface{}) error
	RemoveCloudAccount(namespacedName *types.NamespacedName)

	AddResourceFilters(namespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error
	RemoveResourceFilters(accNamespacedName, selectorNamespacedName *types.NamespacedName)

	GetStatus(accNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error)

	DoInventoryPoll(accountNamespacedName *types.NamespacedName) error

	ResetInventoryCache(accountNamespacedName *types.NamespacedName) error

	GetAccountCloudInventory(accountNamespacedName *types.NamespacedName) (*nephetypes.CloudInventory, error)
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

func (c *cloudCommon) AddCloudAccount(client client.Client, account *crdv1alpha1.CloudProviderAccount, config interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	namespacedName := &types.NamespacedName{
		Namespace: account.GetNamespace(),
		Name:      account.GetName(),
	}

	existingConfig, found := c.accountConfigs[*namespacedName]
	if found {
		err := c.updateCloudAccountConfig(client, config, existingConfig)
		if err != nil {
			c.logger().Error(err, "failed to update cloud account config", "account", namespacedName)
		}
		return err
	}

	newConfig, err := c.newCloudAccountConfig(client, namespacedName, config, c.logger)
	if err != nil {
		c.logger().Info("Error happened creating account config", "account", namespacedName)
	}
	if newConfig != nil {
		c.accountConfigs[*newConfig.GetNamespacedName()] = newConfig
	}
	return err
}

func (c *cloudCommon) RemoveCloudAccount(namespacedName *types.NamespacedName) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.accountConfigs, *namespacedName)
}

// GetCloudAccountByName finds accCfg matching the namespacedName.
func (c *cloudCommon) GetCloudAccountByName(namespacedName *types.NamespacedName) (CloudAccountInterface, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	accCfg, found := c.accountConfigs[*namespacedName]
	if !found {
		return nil, fmt.Errorf("%v: %v", AccountConfigNotFound, namespacedName)
	}
	if !accCfg.GetAccountConfigState() {
		return accCfg, fmt.Errorf("%v: %v", AccountConfigInvalid, namespacedName)
	}
	return accCfg, nil
}

// GetCloudAccountByAccountId converts accountID to namespacedName and finds the matching accCfg.
func (c *cloudCommon) GetCloudAccountByAccountId(accountID *string) (CloudAccountInterface, error) {
	// accountID is a string representation of namespacedName ie namespace and provider account name joined with a "/".
	tokens := strings.Split(*accountID, "/")
	if len(tokens) == 2 {
		namespacedName := types.NamespacedName{Namespace: tokens[0], Name: tokens[1]}
		accCfg, err := c.GetCloudAccountByName(&namespacedName)
		return accCfg, err
	} else {
		c.logger().V(0).Info("account id is not in the expected format", "AccountID", accountID)
		return nil, fmt.Errorf("unable to parse account id: %v", accountID)
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

// AddResourceFilters add/updates instances resource filters based on given selector for all services in the account.
func (c *cloudCommon) AddResourceFilters(accountNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) error {
	accCfg, err := c.GetCloudAccountByName(accountNamespacedName)
	if err != nil {
		return err
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()
	var retErr error
	for _, serviceConfig := range accCfg.GetAllServiceConfigs() {
		err := serviceConfig.AddResourceFilters(selector)
		retErr = multierr.Append(retErr, err)
	}
	return retErr
}

// RemoveResourceFilters removes instances resource filters related to the given selector for all services in the account.
func (c *cloudCommon) RemoveResourceFilters(accNamespacedName, selectorNamespacedName *types.NamespacedName) {
	accCfg, err := c.GetCloudAccountByName(accNamespacedName)
	if err != nil && strings.Contains(err.Error(), AccountConfigNotFound) {
		c.logger().Info("Cloud account config not found", "account", *accNamespacedName, "selector", selectorNamespacedName)
		return
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	for _, serviceConfig := range accCfg.GetAllServiceConfigs() {
		serviceConfig.RemoveResourceFilters(selectorNamespacedName)
	}
}

func (c *cloudCommon) GetStatus(accountNamespacedName *types.NamespacedName) (*crdv1alpha1.CloudProviderAccountStatus, error) {
	accCfg, err := c.GetCloudAccountByName(accountNamespacedName)
	if err != nil && strings.Contains(err.Error(), AccountConfigNotFound) {
		return nil, err
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	return accCfg.GetStatus(), nil
}

// DoInventoryPoll calls cloud API to get vm and vpc resources.
func (c *cloudCommon) DoInventoryPoll(accountNamespacedName *types.NamespacedName) error {
	accCfg, err := c.GetCloudAccountByName(accountNamespacedName)
	if err != nil {
		return err
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	return accCfg.performInventorySync()
}

// ResetInventoryCache resets cloud snapshot and poll stats to nil.
func (c *cloudCommon) ResetInventoryCache(accountNamespacedName *types.NamespacedName) error {
	accCfg, err := c.GetCloudAccountByName(accountNamespacedName)
	if err != nil {
		return err
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	accCfg.resetInventoryCache()
	return nil
}

// GetAccountCloudInventory gets VPC and VM inventory from plugin snapshot for a given cloud provider account.
func (c *cloudCommon) GetAccountCloudInventory(accountNamespacedName *types.NamespacedName) (*nephetypes.CloudInventory, error) {
	accCfg, err := c.GetCloudAccountByName(accountNamespacedName)
	if err != nil {
		return nil, err
	}
	accCfg.LockMutex()
	defer accCfg.UnlockMutex()

	cloudInventory := &nephetypes.CloudInventory{
		VmMap:  make(map[types.NamespacedName]map[string]*runtimev1alpha1.VirtualMachine),
		VpcMap: make(map[string]*runtimev1alpha1.Vpc),
	}
	for _, serviceConfig := range accCfg.GetAllServiceConfigs() {
		inventory := serviceConfig.GetCloudInventory()
		for id, vpc := range inventory.VpcMap {
			cloudInventory.VpcMap[id] = vpc
		}
		for selector, vmMap := range inventory.VmMap {
			if _, found := cloudInventory.VmMap[selector]; !found {
				cloudInventory.VmMap[selector] = vmMap
				continue
			}
			for id, vm := range vmMap {
				cloudInventory.VmMap[selector][id] = vm
			}
		}
	}

	return cloudInventory, nil
}
