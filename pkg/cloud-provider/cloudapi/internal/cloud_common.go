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
	"time"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
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

	GetCloudAccountComputeResourceCRDs(namespacedName *types.NamespacedName) ([]*cloudv1alpha1.VirtualMachine,
		error)
	GetAllCloudAccountsComputeResourceCRDs() ([]*cloudv1alpha1.VirtualMachine, error)

	AddCloudAccount(client client.Client, account *cloudv1alpha1.CloudProviderAccount, credentials interface{}) error
	RemoveCloudAccount(namespacedName *types.NamespacedName)

	AddSelector(namespacedName *types.NamespacedName, selector *cloudv1alpha1.CloudEntitySelector) error
	RemoveSelector(accNamespacedName *types.NamespacedName, selectorName string)

	GetStatus(accNamespacedName *types.NamespacedName) (*cloudv1alpha1.CloudProviderAccountStatus, error)

	AddInventoryPoller(accountNamespacedName *types.NamespacedName) error

	DeleteInventoryPoller(accountNamespacedName *types.NamespacedName) error

	GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.Vpc, error)
}

type cloudCommon struct {
	mutex               sync.Mutex
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

func (c *cloudCommon) AddCloudAccount(client client.Client, account *cloudv1alpha1.CloudProviderAccount, credentials interface{}) error {
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

	config, err := c.newCloudAccountConfig(client, namespacedName, credentials,
		time.Duration(*account.Spec.PollIntervalInSeconds)*time.Second, c.logger)
	if err != nil {
		return err
	}

	c.accountConfigs[*config.GetNamespacedName()] = config

	return nil
}

func (c *cloudCommon) deleteCloudAccount(namespacedName *types.NamespacedName) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.accountConfigs, *namespacedName)
}

func (c *cloudCommon) RemoveCloudAccount(namespacedName *types.NamespacedName) {
	_, found := c.GetCloudAccountByName(namespacedName)
	if !found {
		c.logger().V(0).Info("unable to find cloud account", "account", *namespacedName)
		return
	}
	c.deleteCloudAccount(namespacedName)
}

// GetCloudAccountByName finds accCfg matching the namespacedName.
func (c *cloudCommon) GetCloudAccountByName(namespacedName *types.NamespacedName) (CloudAccountInterface, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	accCfg, found := c.accountConfigs[*namespacedName]
	return accCfg, found
}

// GetCloudAccountByAccountId converts accountID to namespacedName and finds the matching accCfg.
func (c *cloudCommon) GetCloudAccountByAccountId(accountID *string) (CloudAccountInterface, bool) {
	// accountID is a string representation of namespacedName ie namespace and provider account name joined with a "/".
	tokens := strings.Split(*accountID, "/")
	if len(tokens) == 2 {
		namespacedName := types.NamespacedName{Namespace: tokens[0], Name: tokens[1]}
		c.mutex.Lock()
		defer c.mutex.Unlock()
		accCfg, found := c.accountConfigs[namespacedName]
		return accCfg, found
	} else {
		c.logger().V(0).Info("Account ID is not in the expected format", "AccountID", accountID)
		return nil, false
	}
}

func (c *cloudCommon) GetCloudAccounts() map[types.NamespacedName]CloudAccountInterface {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.accountConfigs
}

func (c *cloudCommon) GetCloudAccountComputeResourceCRDs(accountNamespacedName *types.NamespacedName) ([]*cloudv1alpha1.VirtualMachine,
	error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("unable to find cloud account:%v", *accountNamespacedName)
	}
	namespace := accCfg.GetNamespacedName().Namespace

	var computeCRDs []*cloudv1alpha1.VirtualMachine
	serviceConfigs := accCfg.GetServiceConfigs()
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig.getType() == CloudServiceTypeCompute {
			resourceCRDs := serviceConfig.getResourceCRDs(namespace, accCfg.GetNamespacedName().String())
			computeCRDs = append(computeCRDs, resourceCRDs.virtualMachines...)
		}
	}

	return computeCRDs, nil
}

func (c *cloudCommon) GetAllCloudAccountsComputeResourceCRDs() ([]*cloudv1alpha1.VirtualMachine,
	error) {
	var err error
	var computeCRDs []*cloudv1alpha1.VirtualMachine
	for namespacedName := range c.GetCloudAccounts() {
		accountComputeCRDs, e := c.GetCloudAccountComputeResourceCRDs(&namespacedName)
		if e == nil {
			computeCRDs = append(computeCRDs, accountComputeCRDs...)
		} else {
			err = multierr.Append(err, e)
		}
	}

	return computeCRDs, err
}

func (c *cloudCommon) AddSelector(accountNamespacedName *types.NamespacedName, selector *cloudv1alpha1.CloudEntitySelector) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("account not found %v", *accountNamespacedName)
	}

	for _, serviceCfg := range accCfg.GetServiceConfigs() {
		serviceCfg.setResourceFilters(selector)
	}

	// Invoke this function to force inventory scan for vms soon after CES add.
	err := accCfg.startPeriodicInventorySync()
	if err != nil {
		return fmt.Errorf("inventory sync failed [ account : %v, err: %v ]", *accountNamespacedName, err)
	}

	return nil
}

func (c *cloudCommon) RemoveSelector(accNamespacedName *types.NamespacedName, selectorName string) {
	accCfg, found := c.GetCloudAccountByName(accNamespacedName)
	if !found {
		c.logger().Info("Not found", "account", *accNamespacedName, "name", selectorName)
		return
	}

	for _, serviceCfg := range accCfg.GetServiceConfigs() {
		serviceCfg.removeResourceFilters(selectorName)
	}
}

func (c *cloudCommon) GetStatus(accountNamespacedName *types.NamespacedName) (*cloudv1alpha1.CloudProviderAccountStatus, error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("account not found %v", *accountNamespacedName)
	}

	return accCfg.GetStatus(), nil
}

// AddInventoryPoller adds a periodic poller for fetching cloud inventory.
func (c *cloudCommon) AddInventoryPoller(accountNamespacedName *types.NamespacedName) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("account not found %v", *accountNamespacedName)
	}

	err := accCfg.startPeriodicInventorySync()
	if err != nil {
		return fmt.Errorf("inventory sync failed [ account : %v, err: %v ]", *accountNamespacedName, err)
	}

	return nil
}

// DeleteInventoryPoller deletes an existing poller created for polling cloud inventory along with clearing set filters.
func (c *cloudCommon) DeleteInventoryPoller(accountNamespacedName *types.NamespacedName) error {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return fmt.Errorf("account not found %v", *accountNamespacedName)
	}

	accCfg.stopPeriodicInventorySync()
	return nil
}

// GetVpcInventory gets a map of vpcs applicable for the account.
func (c *cloudCommon) GetVpcInventory(accountNamespacedName *types.NamespacedName) (map[string]*runtimev1alpha1.Vpc, error) {
	accCfg, found := c.GetCloudAccountByName(accountNamespacedName)
	if !found {
		return nil, fmt.Errorf("unable to find cloud account:%v", *accountNamespacedName)
	}

	serviceConfigs := accCfg.GetServiceConfigs()
	for _, serviceConfig := range serviceConfigs {
		if serviceConfig.getType() == CloudServiceTypeCompute {
			return serviceConfig.getVpcInventory(), nil
		}
	}
	return nil, nil
}
