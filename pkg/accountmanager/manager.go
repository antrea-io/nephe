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
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloud"
	ctrlsync "antrea.io/nephe/pkg/controllers/sync"
	"antrea.io/nephe/pkg/inventory"
)

const (
	virtualMachineSelectorMatchIndexerByID   = "virtualmachine.selector.id"
	virtualMachineSelectorMatchIndexerByName = "virtualmachine.selector.name"
	virtualMachineSelectorMatchIndexerByVPC  = "virtualmachine.selector.vpc.id"
)

type Interface interface {
	AddAccount(*types.NamespacedName, runtimev1alpha1.CloudProvider, *crdv1alpha1.CloudProviderAccount) (bool, error)
	RemoveAccount(*types.NamespacedName) error
	AddResourceFiltersToAccount(*types.NamespacedName, *types.NamespacedName, *crdv1alpha1.CloudEntitySelector) (bool, error)
	RemoveResourceFiltersFromAccount(*types.NamespacedName, *types.NamespacedName) error
	SyncAllAccounts()
}

type AccountManager struct {
	client.Client
	Log        logr.Logger
	mutex      sync.RWMutex
	Inventory  inventory.Interface
	accPollers map[types.NamespacedName]*accountPoller
}

// ConfigureAccountManager configures and initializes account manager.
func (a *AccountManager) ConfigureAccountManager() {
	// Init maps.
	a.accPollers = make(map[types.NamespacedName]*accountPoller)
}

// AddAccount consumes CloudProviderAccount CR and calls cloud plugin to add account. It also creates and starts account
// poller thread for polling inventory.
func (a *AccountManager) AddAccount(namespacedName *types.NamespacedName, cloudProviderType runtimev1alpha1.CloudProvider,
	cpa *crdv1alpha1.CloudProviderAccount) (bool, error) {
	// Cloud Provider Type is used to fetch the cloud interface.
	cloudInterface, err := cloud.GetCloudInterface(cloudProviderType)
	if err != nil {
		return false, err
	}

	// Create an account poller for polling cloud inventory.
	accPoller, _ := a.addAccountPoller(cloudInterface, namespacedName, cpa)
	if err = accPoller.cloudInterface.AddProviderAccount(a.Client, cpa); err != nil {
		// If an error occurs while adding the account, stop its poller and cleanup inventory.
		accPoller.stopPoller()
		_ = a.Inventory.DeleteVpcsFromCache(namespacedName)
		_ = a.Inventory.DeleteAllVmsFromCache(namespacedName)
		return false, err
	}

	// If the controller is syncing (restart case), don't start the poller.
	if ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCPA) {
		go accPoller.restartPoller(namespacedName)
	}
	return false, nil
}

// RemoveAccount removes account poller thread and cleans up all internal state in cloud plugin and inventory for
// this account.
func (a *AccountManager) RemoveAccount(namespacedName *types.NamespacedName) error {
	accPoller, exists := a.getAccountPoller(namespacedName)
	if !exists {
		a.Log.Info("Failed to remove account, account poller doesn't exist", "account", namespacedName)
		return nil
	}
	accPoller.cloudInterface.RemoveProviderAccount(namespacedName)
	// Stop and remove the poller.
	go a.removeAccountPoller(accPoller)
	return nil
}

// AddResourceFiltersToAccount add/update cloud plugin for a given account to include the new selector and
// restart poller once done.
func (a *AccountManager) AddResourceFiltersToAccount(accNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName, selector *crdv1alpha1.CloudEntitySelector) (bool, error) {
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		return true, fmt.Errorf(fmt.Sprintf("failed to add or update selector, account %v: "+
			"not configured", accNamespacedName))
	}
	if err := accPoller.cloudInterface.AddAccountResourceSelector(accNamespacedName, selector); err != nil {
		return true, fmt.Errorf(fmt.Sprintf("failed to add or update selector %v, account %v: %v",
			selectorNamespacedName, accNamespacedName, err))
	}
	// Update selectors in account poller.
	accPoller.addOrUpdateSelector(selector)
	if ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCES) {
		go accPoller.restartPoller(accNamespacedName)
	}
	return false, nil
}

// RemoveResourceFiltersFromAccount removes selector from cloud plugin and restart the poller.
func (a *AccountManager) RemoveResourceFiltersFromAccount(accNamespacedName *types.NamespacedName,
	selectorNamespacedName *types.NamespacedName) error {
	accPoller, exists := a.getAccountPoller(accNamespacedName)
	if !exists {
		return nil
	}
	accPoller.cloudInterface.RemoveAccountResourcesSelector(accNamespacedName, selectorNamespacedName)
	// TODO: Update accPoller with selector delete.
	go func() {
		accPoller.restartPoller(accNamespacedName)
		_ = accPoller.inventory.DeleteVmsFromCache(accNamespacedName, selectorNamespacedName)
	}()
	return nil
}

// addAccountPoller creates an account poller for a given account.
func (a *AccountManager) addAccountPoller(cloudInterface cloud.CloudInterface, namespacedName *types.NamespacedName,
	cpa *crdv1alpha1.CloudProviderAccount) (*accountPoller, bool) {
	// Set poller interval to default if not specified.
	if cpa.Spec.PollIntervalInSeconds == nil {
		var pollInterval uint = crdv1alpha1.DefaultPollIntervalInSeconds
		cpa.Spec.PollIntervalInSeconds = &pollInterval
	}

	accPoller, exists := a.getAccountPoller(namespacedName)
	if exists {
		// Update the polling interval.
		accPoller.pollIntvInSeconds = *cpa.Spec.PollIntervalInSeconds
		return accPoller, true
	}

	// Add and init the new poller.
	poller := &accountPoller{
		Client:                a.Client,
		log:                   a.Log.WithName("Poller"),
		pollIntvInSeconds:     *cpa.Spec.PollIntervalInSeconds,
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
func (a *AccountManager) removeAccountPoller(accPoller *accountPoller) {
	a.Log.V(1).Info("Removing account poller", "account", *accPoller.accountNamespacedName)
	a.mutex.Lock()
	defer a.mutex.Unlock()
	delete(a.accPollers, *accPoller.accountNamespacedName)

	// Stop the poller and cleanup inventory.
	accPoller.stopPoller()
	_ = accPoller.inventory.DeleteAllVmsFromCache(accPoller.accountNamespacedName)
	_ = accPoller.inventory.DeleteVpcsFromCache(accPoller.accountNamespacedName)
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

// WaitForPollDone restarts account poller and waits till inventory poll is done.
func (a *AccountManager) waitForPollDone(namespacedName *types.NamespacedName, wg *sync.WaitGroup) {
	defer wg.Done()

	accPoller, exists := a.getAccountPoller(namespacedName)
	if exists {
		accPoller.restartPoller(namespacedName)
		// wait for polling to complete after poller restart.
		if err := accPoller.waitForPollDone(namespacedName); err != nil {
			a.Log.Error(err, "failed to poll inventory", "account", namespacedName)
		}
	}
}

// SyncAllAccounts syncs inventory for all accounts (`CloudProviderAccount`).
func (a *AccountManager) SyncAllAccounts() {
	cpaList := &crdv1alpha1.CloudProviderAccountList{}
	if err := a.Client.List(context.TODO(), cpaList, &client.ListOptions{}); err != nil {
		a.Log.Error(err, "failed to retrieve cpa list")
		return
	}

	var wg sync.WaitGroup
	for _, cpa := range cpaList.Items {
		wg.Add(1)
		key := types.NamespacedName{Namespace: cpa.Namespace, Name: cpa.Name}
		a.Log.V(1).Info("Syncing account", "account", key)
		go a.waitForPollDone(&key, &wg)
	}
	wg.Wait()
	if len(cpaList.Items) > 0 {
		a.Log.V(1).Info("Accounts synced")
	}
}
