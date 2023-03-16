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
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider"
	"antrea.io/nephe/pkg/cloudprovider/cloudapi/common"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/util"
)

const (
	ErrorMsgAccountAddFail        = "failed to add account"
	ErrorMsgAccountPollerNotFound = "account poller not found"
)

const (
	virtualMachineSelectorMatchIndexerByID   = "virtualmachine.selector.id"
	virtualMachineSelectorMatchIndexerByName = "virtualmachine.selector.name"
	virtualMachineSelectorMatchIndexerByVPC  = "virtualmachine.selector.vpc.id"
)

type Interface interface {
	AddAccount(*types.NamespacedName, runtimev1alpha1.CloudProvider, *crdv1alpha1.CloudProviderAccount) error
	RemoveAccount(*types.NamespacedName) error
	AddResourceFiltersToAccount(*types.NamespacedName, *types.NamespacedName, *crdv1alpha1.CloudEntitySelector) (bool, error)
	RemoveResourceFiltersFromAccount(*types.NamespacedName, *types.NamespacedName)
}

type AccountManager struct {
	client.Client
	Log logr.Logger

	mutex               sync.RWMutex
	Inventory           inventory.Interface
	accPollers          map[types.NamespacedName]*accountPoller
	accountProviderType map[types.NamespacedName]common.ProviderType
}

// ConfigureAccountManager configures and initializes account manager.
func (a *AccountManager) ConfigureAccountManager() {
	// Init maps.
	a.accPollers = make(map[types.NamespacedName]*accountPoller)
	a.accountProviderType = make(map[types.NamespacedName]common.ProviderType)
}

// addAccountProviderType create a mapping of account namespaced name with cloud provider type.
func (a *AccountManager) addAccountProviderType(namespacedName *types.NamespacedName,
	provider runtimev1alpha1.CloudProvider) common.ProviderType {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	providerType := common.ProviderType(provider)
	a.accountProviderType[*namespacedName] = providerType
	return providerType
}

func (a *AccountManager) removeAccountProviderType(namespacedName *types.NamespacedName) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	delete(a.accountProviderType, *namespacedName)
}

func (a *AccountManager) getAccountProviderType(namespacedName *types.NamespacedName) (bool, common.ProviderType) {
	a.mutex.RLock()
	defer a.mutex.RUnlock()

	if providerType, ok := a.accountProviderType[*namespacedName]; ok {
		return true, providerType
	}
	return false, ""
}

// AddAccount consumes CloudProviderAccount CR and calls cloud plugin to add account. It also creates and starts account
// poller thread for polling inventory.
func (a *AccountManager) AddAccount(namespacedName *types.NamespacedName, accountCloudType runtimev1alpha1.CloudProvider,
	account *crdv1alpha1.CloudProviderAccount) error {
	// Cloud Provider Type is used to fetch the cloud interface.
	cloudProviderType := a.addAccountProviderType(namespacedName, accountCloudType)
	cloudInterface, err := cloudprovider.GetCloudInterface(cloudProviderType)
	if err != nil {
		return err
	}

	// Call plugin to add cloud account.
	if err = cloudInterface.AddProviderAccount(a.Client, account); err != nil {
		return fmt.Errorf("%s, err: %v", ErrorMsgAccountAddFail, err)
	}
	// Create an account poller for polling cloud inventory.
	accPoller, exists := a.addAccountPoller(cloudInterface, namespacedName, account)
	if !exists {
		if !util.DoesCesCrExistsForAccount(a.Client, namespacedName) {
			a.Log.Info("Starting account poller", "account", namespacedName)
			go wait.Until(accPoller.doAccountPolling, time.Duration(accPoller.PollIntvInSeconds)*time.Second, accPoller.ch)
		} else {
			a.Log.Info("Ignoring start of account poller", "account", namespacedName)
		}
	} else {
		accPoller.restartPoller(namespacedName)
	}
	return nil
}

// RemoveAccount removes account poller thread and cleans up all internal state in cloud plugin and inventory for this account.
func (a *AccountManager) RemoveAccount(namespacedName *types.NamespacedName) error {
	// Stop and remove the poller.
	a.Log.V(1).Info("Removing account poller", "account", namespacedName)
	if err := a.removeAccountPoller(namespacedName); err != nil {
		return err
	}

	// Cleanup inventory data for this account.
	if err := a.Inventory.DeleteVpcsFromCache(namespacedName); err != nil {
		return err
	}
	if err := a.Inventory.DeleteVmsFromCache(namespacedName); err != nil {
		return err
	}

	// Clear state in cloud plugin.
	ok, cloudProviderType := a.getAccountProviderType(namespacedName)
	if !ok {
		// Couldn't find a provider type for this account. May not be deleted.
		return nil
	}
	defer func() {
		// Cleanup account namespaced name to provider type map object.
		a.removeAccountProviderType(namespacedName)
	}()

	cloudInterface, err := cloudprovider.GetCloudInterface(cloudProviderType)
	if err != nil {
		return err
	}
	_ = cloudInterface.ResetInventoryCache(namespacedName)
	cloudInterface.RemoveProviderAccount(namespacedName)
	return nil
}

// AddResourceFiltersToAccount add/update cloud plugin for a given account to include the new selector and
// restart poller once done.
func (a *AccountManager) AddResourceFiltersToAccount(accNamespacedName *types.NamespacedName, selectorNamespacedName *types.NamespacedName,
	selector *crdv1alpha1.CloudEntitySelector) (bool, error) {
	ok, cloudProviderType := a.getAccountProviderType(accNamespacedName)
	if !ok {
		return true, fmt.Errorf(fmt.Sprintf("failed to add selector, account %v not found", accNamespacedName))
	}
	cloudInterface, _ := cloudprovider.GetCloudInterface(cloudProviderType)
	a.Log.V(1).Info("Updating selectors for account", "name", accNamespacedName)
	if err := cloudInterface.AddAccountResourceSelector(accNamespacedName, selector); err != nil {
		return false, fmt.Errorf(fmt.Sprintf("failed to add selector %v: %v", selectorNamespacedName, err))
	}

	// Fetch and restart account poller as selector has changed.
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		return false, fmt.Errorf(fmt.Sprintf("%v %v", ErrorMsgAccountPollerNotFound, accNamespacedName))
	}
	accPoller.addOrUpdateSelector(selector)
	accPoller.restartPoller(accNamespacedName)

	// wait for polling to complete after restart.
	return false, accPoller.waitForPollDone(accNamespacedName)
}

// RemoveResourceFiltersFromAccount removes selector from cloud plugin and restart the poller.
func (a *AccountManager) RemoveResourceFiltersFromAccount(accNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) {
	ok, cloudProviderType := a.getAccountProviderType(accNamespacedName)
	if !ok {
		// If we cannot find cloud provider type, that means CPA may not be added or it's already removed.
		return
	}
	cloudInterface, _ := cloudprovider.GetCloudInterface(cloudProviderType)
	a.Log.V(1).Info("Removing selectors for account", "name", accNamespacedName)
	cloudInterface.RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName)

	// Restart account poller after removing the selector.
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		return
	}
	accPoller.removeSelector(accNamespacedName)
	accPoller.restartPoller(accNamespacedName)
}

// addAccountPoller creates an account poller for a given account.
func (a *AccountManager) addAccountPoller(cloudInterface common.CloudInterface, namespacedName *types.NamespacedName,
	account *crdv1alpha1.CloudProviderAccount) (*accountPoller, bool) {
	// Restart account poller after removing the selector.
	accPoller, exists := a.getAccountPoller(namespacedName)
	if exists {
		a.Log.Info("Account poller exists", "account", namespacedName)
		return accPoller, true
	}

	// Add and init the new poller.
	poller := &accountPoller{
		Client:            a.Client,
		log:               a.Log.WithName("Poller"),
		PollIntvInSeconds: *account.Spec.PollIntervalInSeconds,
		cloudInterface:    cloudInterface,
		namespacedName:    namespacedName,
		selector:          nil,
		ch:                make(chan struct{}),
		inventory:         a.Inventory,
	}
	poller.initVmSelectorCache()

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.accPollers[*namespacedName] = poller
	return poller, false
}

// removeAccountPoller stops account poller thread and removes account poller entry from accPollers map.
func (a *AccountManager) removeAccountPoller(namespacedName *types.NamespacedName) error {
	accPoller, exists := a.getAccountPoller(namespacedName)
	if !exists {
		return nil
	}
	accPoller.removeSelector(namespacedName)
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
