// Copyright 2023 Antrea Authors.
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

package accountmanager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloud"
	ctrlsync "antrea.io/nephe/pkg/controllers/sync"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/util"
	"antrea.io/nephe/pkg/util/k8s/crd"
)

const (
	errorMsgAccountPollerNotFound = "account poller not found"
)

const (
	virtualMachineSelectorMatchIndexerByID   = "virtualmachine.selector.id"
	virtualMachineSelectorMatchIndexerByName = "virtualmachine.selector.name"
	virtualMachineSelectorMatchIndexerByVPC  = "virtualmachine.selector.vpc.id"
)

type Interface interface {
	AddAccount(*types.NamespacedName, runtimev1alpha1.CloudProvider, *crdv1alpha1.CloudProviderAccount) (bool, error)
	RemoveAccount(*types.NamespacedName) error
	IsAccountCredentialsValid(namespacedName *types.NamespacedName) bool
	AddResourceFiltersToAccount(*types.NamespacedName, *types.NamespacedName, *crdv1alpha1.CloudEntitySelector, bool) (bool, error)
	RemoveResourceFiltersFromAccount(*types.NamespacedName, *types.NamespacedName) error
}

type AccountManager struct {
	client.Client
	Log              logr.Logger
	mutex            sync.RWMutex
	Inventory        inventory.Interface
	accPollers       map[types.NamespacedName]*accountPoller
	accountConfigMap map[types.NamespacedName]*accountConfig
}

type accountConfig struct {
	providerType runtimev1alpha1.CloudProvider
	// Indicates account initialization state.
	initialized bool
	// Indicates credentials are valid or not.
	credentialsValid bool
	// retry upon failure.
	retry bool
	// map of selector namespaced name to selector.
	selectorMap map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector
}

// ConfigureAccountManager configures and initializes account manager.
func (a *AccountManager) ConfigureAccountManager() {
	// Init maps.
	a.accPollers = make(map[types.NamespacedName]*accountPoller)
	a.accountConfigMap = make(map[types.NamespacedName]*accountConfig)
}

// AddAccount consumes CloudProviderAccount CR and calls cloud plugin to add account. It also creates and starts account
// poller thread for polling inventory.
func (a *AccountManager) AddAccount(namespacedName *types.NamespacedName, accountCloudType runtimev1alpha1.CloudProvider,
	account *crdv1alpha1.CloudProviderAccount) (bool, error) {
	// Create account config.
	config := a.addAccountConfig(namespacedName, accountCloudType)
	// Cloud Provider Type is used to fetch the cloud interface.
	cloudInterface, err := cloud.GetCloudInterface(config.providerType)
	if err != nil {
		return false, err
	}

	// Call plugin to add cloud account.
	if err = cloudInterface.AddProviderAccount(a.Client, account); err != nil {
		return a.handleAddProviderAccountError(namespacedName, config, err), err
	}

	// Create an account poller for polling cloud inventory.
	accPoller, exists := a.addAccountPoller(cloudInterface, namespacedName, account)
	if !exists {
		if !crd.DoesCesCrExistsForAccount(a.Client, namespacedName) {
			a.Log.Info("Starting account poller", "account", namespacedName)
			go wait.Until(accPoller.doAccountPolling, time.Duration(accPoller.pollIntvInSeconds)*time.Second, accPoller.ch)
		} else {
			a.Log.V(1).Info("Ignoring start of account poller", "account", namespacedName)
			if ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCPA) && !config.initialized {
				// Replay CES CR only when account init state is changed from failure to success.
				a.replaySelectorsForAccount(namespacedName, config)
			}
		}
	} else {
		accPoller.restartPoller(namespacedName)
	}

	// Set account init state to true as there were no errors reported from cloud plug-in.
	config.retry = false
	config.credentialsValid = true
	config.initialized = true

	return false, nil
}

// RemoveAccount removes account poller thread and cleans up all internal state in cloud plugin and inventory for
// this account.
func (a *AccountManager) RemoveAccount(namespacedName *types.NamespacedName) error {
	// Stop and remove the poller.
	_ = a.removeAccountPoller(namespacedName)

	// Cleanup vpc inventory data for this account, vm inventory is deleted in removeAccountPoller.
	_ = a.Inventory.DeleteVpcsFromCache(namespacedName)

	defer func() {
		// Remove account config.
		a.removeAccountConfig(namespacedName)
	}()

	// Clear state in cloud plugin.
	cloudProviderType, ok := a.getAccountProviderType(namespacedName)
	if !ok {
		// Couldn't find a provider type for this account. May not be deleted.
		return nil
	}

	cloudInterface, err := cloud.GetCloudInterface(cloudProviderType)
	if err != nil {
		return err
	}
	_ = cloudInterface.ResetInventoryCache(namespacedName)
	cloudInterface.RemoveProviderAccount(namespacedName)

	return nil
}

// AddResourceFiltersToAccount add/update cloud plugin for a given account to include the new selector and
// restart poller once done.
func (a *AccountManager) AddResourceFiltersToAccount(accNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector, replay bool) (bool, error) {
	cloudProviderType, ok := a.getAccountProviderType(accNamespacedName)
	if !ok {
		return true, fmt.Errorf(fmt.Sprintf("failed to add or update selector %v, account %v: "+
			"provider type not found", selectorNamespacedName, accNamespacedName))
	}
	cloudInterface, _ := cloud.GetCloudInterface(cloudProviderType)
	if !replay {
		// Update the selector config only when the reconciler processes the CR request.
		if err := a.addSelectorToAccountConfig(accNamespacedName, selectorNamespacedName, selector); err != nil {
			return false, fmt.Errorf(fmt.Sprintf("failed to add or update selector %v, %v: %v",
				selectorNamespacedName, accNamespacedName, err))
		}
	}

	if err := cloudInterface.AddAccountResourceSelector(accNamespacedName, selector); err != nil {
		return false, fmt.Errorf(fmt.Sprintf("failed to add or update selector %v, account %v: %v",
			selectorNamespacedName, accNamespacedName, err))
	}

	// Fetch and restart account poller as selector has changed.
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		// Account poller may not exist when account is not successfully initialized.
		return false, fmt.Errorf(fmt.Sprintf("failed to add or update selector %v, account %v: %v",
			selectorNamespacedName, accNamespacedName, errorMsgAccountPollerNotFound))
	}

	// Update account poller with selector config.
	selectorCopy := a.getSelectorFromAccountConfig(accNamespacedName, selectorNamespacedName)
	if selectorCopy != nil {
		accPoller.addOrUpdateSelector(selectorCopy)
	}

	accPoller.restartPoller(accNamespacedName)
	// wait for polling to complete after restart.
	return false, accPoller.waitForPollDone(accNamespacedName)
}

// RemoveResourceFiltersFromAccount removes selector from cloud plugin and restart the poller.
func (a *AccountManager) RemoveResourceFiltersFromAccount(accNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) error {
	cloudProviderType, ok := a.getAccountProviderType(accNamespacedName)
	if !ok {
		// If we cannot find cloud provider type, that means CPA may not be added, or it's already removed.
		return fmt.Errorf(fmt.Sprintf("failed to delete selector %v, account %v: provider type not found",
			selectorNamespacedName, accNamespacedName))
	}
	cloudInterface, _ := cloud.GetCloudInterface(cloudProviderType)
	cloudInterface.RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName)
	// Delete selector config from the account config.
	a.removeSelectorFromAccountConfig(accNamespacedName, selectorNamespacedName)

	// Restart account poller after removing the selector.
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		return fmt.Errorf(fmt.Sprintf("failed to delete selector %v, account %v: %v",
			selectorNamespacedName, accNamespacedName, errorMsgAccountPollerNotFound))
	}
	_ = accPoller.inventory.DeleteVmsFromCache(accNamespacedName, selectorNamespacedName)
	accPoller.restartPoller(accNamespacedName)
	return nil
}

// IsAccountCredentialsValid return true for an account, if credentials are valid.
func (a *AccountManager) IsAccountCredentialsValid(namespacedName *types.NamespacedName) bool {
	config := a.getAccountConfig(namespacedName)
	if config != nil {
		return config.credentialsValid
	}
	return false
}

// addAccountPoller creates an account poller for a given account.
func (a *AccountManager) addAccountPoller(cloudInterface cloud.CloudInterface, namespacedName *types.NamespacedName,
	account *crdv1alpha1.CloudProviderAccount) (*accountPoller, bool) {
	// Set poller interval to default if not specified.
	if account.Spec.PollIntervalInSeconds == nil {
		var pollInterval uint = crdv1alpha1.DefaultPollIntervalInSeconds
		account.Spec.PollIntervalInSeconds = &pollInterval
	}

	// Restart account poller after removing the selector.
	accPoller, exists := a.getAccountPoller(namespacedName)
	if exists {
		// Update the polling interval.
		accPoller.pollIntvInSeconds = *account.Spec.PollIntervalInSeconds
		return accPoller, true
	}

	// Add and init the new poller.
	poller := &accountPoller{
		Client:                a.Client,
		log:                   a.Log.WithName("Poller"),
		pollIntvInSeconds:     *account.Spec.PollIntervalInSeconds,
		cloudInterface:        cloudInterface,
		accountNamespacedName: namespacedName,
		ch:                    make(chan struct{}),
		inventory:             a.Inventory,
	}
	poller.initVmSelectorCache()

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.accPollers[*namespacedName] = poller
	return poller, false
}

// removeAccountPoller stops account poller thread and removes account poller entry from accPollers map.
func (a *AccountManager) removeAccountPoller(namespacedName *types.NamespacedName) error {
	a.Log.V(1).Info("Removing account poller", "account", namespacedName)
	accPoller, exists := a.getAccountPoller(namespacedName)
	if !exists {
		return fmt.Errorf(fmt.Sprintf("%v %v", errorMsgAccountPollerNotFound, namespacedName))
	}
	_ = accPoller.inventory.DeleteAllVmsFromCache(namespacedName)
	accPoller.stopPoller()

	a.mutex.Lock()
	defer a.mutex.Unlock()
	delete(a.accPollers, *namespacedName)
	return nil
}

// getAccountPoller returns the account poller matching the nameSpacedName
func (a *AccountManager) getAccountPoller(name *types.NamespacedName) (*accountPoller, bool) {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	accPoller, found := a.accPollers[*name]
	if !found {
		return nil, false
	}
	return accPoller, true
}

// getAccountConfig returns the account configuration.
func (a *AccountManager) getAccountConfig(name *types.NamespacedName) *accountConfig {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	config, ok := a.accountConfigMap[*name]
	if !ok {
		return nil
	}
	return config
}

// addAccountConfig creates the account configuration or returns the existing configuration.
func (a *AccountManager) addAccountConfig(name *types.NamespacedName, provider runtimev1alpha1.CloudProvider) *accountConfig {
	config := a.getAccountConfig(name)
	if config != nil {
		return config
	}
	// Create a new account config.
	config = &accountConfig{
		providerType:     provider,
		initialized:      false,
		credentialsValid: false,
		retry:            false,
		// Init maps.
		selectorMap: make(map[types.NamespacedName]*crdv1alpha1.CloudEntitySelector),
	}
	a.Log.V(1).Info("Adding account config", "account", name)
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.accountConfigMap[*name] = config
	return config
}

// removeAccountConfig removes the account configuration.
func (a *AccountManager) removeAccountConfig(namespacedName *types.NamespacedName) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.Log.V(1).Info("Removing account config", "account", namespacedName)
	delete(a.accountConfigMap, *namespacedName)
}

// getSelectorFromAccountConfig gets the selector configuration of the specified account and selector.
func (a *AccountManager) getSelectorFromAccountConfig(accountNamespacedName,
	selectorNamespacedName *types.NamespacedName) *crdv1alpha1.CloudEntitySelector {
	acctConfig := a.getAccountConfig(accountNamespacedName)
	if acctConfig == nil {
		a.Log.Error(fmt.Errorf("failed to get account config"), "", "account", accountNamespacedName)
		return nil
	}
	return acctConfig.selectorMap[*selectorNamespacedName]
}

// addSelectorToAccountConfig add the current selector configuration to the specified account.
func (a *AccountManager) addSelectorToAccountConfig(accountNamespacedName, selectorNamespacedName *types.NamespacedName,
	selector *crdv1alpha1.CloudEntitySelector) error {
	acctConfig := a.getAccountConfig(accountNamespacedName)
	if acctConfig == nil {
		return fmt.Errorf("failed to get account config")
	}
	a.Log.V(1).Info("Adding selector config", "account", accountNamespacedName,
		"selector", selectorNamespacedName)
	acctConfig.selectorMap[*selectorNamespacedName] = selector.DeepCopy()
	return nil
}

// removeSelectorFromAccountConfig removes the current selector configuration for the specified account.
func (a *AccountManager) removeSelectorFromAccountConfig(accountNamespacedName, selectorNamespacedName *types.NamespacedName) {
	acctConfig := a.getAccountConfig(accountNamespacedName)
	if acctConfig == nil {
		a.Log.Error(fmt.Errorf("failed to get account config"), "", "account", accountNamespacedName)
		return
	}
	a.Log.V(1).Info("Removing selector config", "account", accountNamespacedName,
		"selector", selectorNamespacedName)
	delete(acctConfig.selectorMap, *selectorNamespacedName)
}

func (a *AccountManager) getAccountProviderType(namespacedName *types.NamespacedName) (runtimev1alpha1.CloudProvider, bool) {
	acctConfig := a.getAccountConfig(namespacedName)
	if acctConfig != nil {
		return acctConfig.providerType, true
	}
	return "", false
}

// handleAddProviderAccountError performs the cleanup on account add/update.
// i.e. Removes poller, inventory and returns if the error can be retried or not.
func (a *AccountManager) handleAddProviderAccountError(namespacedName *types.NamespacedName, config *accountConfig,
	err error) bool {
	// Account poller is removed upon any error in the plug-in.
	_ = a.removeAccountPoller(namespacedName)
	_ = a.Inventory.DeleteVpcsFromCache(namespacedName)
	_ = a.Inventory.DeleteAllVmsFromCache(namespacedName)
	// TODO: require lock to write into account config structure.
	config.initialized = false
	if strings.Contains(err.Error(), util.ErrorMsgSecretReference) {
		config.credentialsValid = false
		config.retry = false
	} else {
		config.credentialsValid = true
		config.retry = true
	}
	return config.retry
}

// replaySelectorsForAccount replays all the selectors that belong to this specified account,
// set/resets the selector status.
func (a *AccountManager) replaySelectorsForAccount(accNamespacedName *types.NamespacedName, config *accountConfig) {
	for namespacedName, selector := range config.selectorMap {
		a.Log.Info("Re-playing selector", "account", accNamespacedName, "selector", namespacedName)
		// TODO: Call the plug-in directly..
		_, err := a.AddResourceFiltersToAccount(accNamespacedName, &namespacedName, selector, true)
		// set status with the latest error message or clear status.
		a.updateSelectorStatus(&namespacedName, err)
	}
}

// updateSelectorStatus updates the status on the CloudEntitySelector CR.
func (a *AccountManager) updateSelectorStatus(namespacedName *types.NamespacedName, err error) {
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	}

	updateStatusFunc := func() error {
		selector := &crdv1alpha1.CloudEntitySelector{}
		if err = a.Get(context.TODO(), *namespacedName, selector); err != nil {
			return nil
		}
		if selector.Status.Error != errorMsg {
			selector.Status.Error = errorMsg
			a.Log.Info("Setting CES status", "selector", namespacedName, "message", errorMsg)
			if err = a.Client.Status().Update(context.TODO(), selector); err != nil {
				a.Log.Error(err, "failed to update CES status, retrying", "selector", namespacedName)
				return err
			}
		}
		return nil
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, updateStatusFunc); err != nil {
		a.Log.Error(err, "failed to update CES status", "selector", namespacedName)
	}
}
