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

package cloud

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/controllers/inventory"
	converter "antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/logging"
)

const (
	converterChannelBuffer = 50
)

var vmManager *VirtualMachineManager

// VirtualMachineManager reconciles a VirtualMachine object.
type VirtualMachineManager struct {
	client.Client
	log    logr.Logger
	scheme *runtime.Scheme

	// TODO: pass pointer of Inventory.
	Inventory inventory.InventoryInterface
	converter converter.VMConverter
	vmWatcher watch.Interface
}

// nolint:lll

func init() {
	vmManager = &VirtualMachineManager{}
}

// GetVirtualMachineManagerInstance gets the vmManager instance.
func GetVirtualMachineManagerInstance() *VirtualMachineManager {
	return vmManager
}

// Configure configures the client, scheme, log and converter.
func (r *VirtualMachineManager) Configure(client client.Client, scheme *runtime.Scheme,
	inventory inventory.InventoryInterface) *VirtualMachineManager {
	r.Client = client
	r.scheme = scheme
	r.Inventory = inventory
	r.log = logging.GetLogger("controllers").WithName("VirtualMachine")
	r.converter = converter.VMConverter{
		Client: r.Client,
		Log:    logging.GetLogger("converter").WithName("VMConverter"),
		Ch:     make(chan watch.Event, converterChannelBuffer),
		Scheme: r.scheme,
	}
	return r
}

// Start performs the initialization of the controller.
// VM controller is said to be initialized and synced when all the VM and EE
// CRs are reconciled.
func (r *VirtualMachineManager) Start() error {
	if err := GetControllerSyncStatusInstance().waitForControllersToSync(
		[]controllerType{ControllerTypeEE, ControllerTypeCES, ControllerTypeCPA}, syncTimeout); err != nil {
		return err
	}
	r.log.Info("Init done", "controller", ControllerTypeVM.String())

	vmMap := r.getAllVMs()
	if err := r.syncExternalEntities(vmMap); err != nil {
		return err
	}

	// Start the converter module.
	go r.converter.Start()
	// Set sync status as complete.
	GetControllerSyncStatusInstance().SetControllerSyncStatus(ControllerTypeVM)
	// Setup watcher after VM cache is populated and EE CRs are reconciled.
	if err := r.resetWatcher(); err != nil {
		return err
	}
	// Blocking thread to wait for any VM event.
	if err := r.processEvent(); err != nil {
		return err
	}
	return nil
}

// getAllVMs returns a map which contains all the VMs.
func (r *VirtualMachineManager) getAllVMs() map[types.NamespacedName]struct{} {
	vmObjList := r.Inventory.GetAllVms()
	tempVmMap := map[types.NamespacedName]struct{}{}
	for _, vmObj := range vmObjList {
		vm := vmObj.(*runtimev1alpha1.VirtualMachine)
		vmNn := types.NamespacedName{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		}
		tempVmMap[vmNn] = struct{}{}
	}
	return tempVmMap
}

// syncExternalEntities validates that each EE has corresponding VM. If it does not exist then
// the EE will be deleted.
func (r *VirtualMachineManager) syncExternalEntities(vmMap map[types.NamespacedName]struct{}) error {
	eeList := &antreav1alpha2.ExternalEntityList{}
	if err := r.Client.List(context.TODO(), eeList, &client.ListOptions{}); err != nil {
		return err
	}
	for _, ee := range eeList.Items {
		eeLabelKeyName, exists := ee.Labels[config.ExternalEntityLabelKeyName]
		if !exists {
			// Ignore EE objects not created by converter module.
			continue
		}
		eeNn := types.NamespacedName{
			Name:      eeLabelKeyName,
			Namespace: ee.Namespace,
		}
		if _, ok := vmMap[eeNn]; !ok {
			r.log.Info("Could not find matching VM object, deleting ExternalEntity", "namespacedName", eeNn)
			// Delete the external entity, since no matching VM found.
			_ = r.Client.Delete(context.TODO(), &ee)
			continue
		}
	}
	return nil
}

// resetWatcher sets a watcher to watch VM events.
func (r *VirtualMachineManager) resetWatcher() (err error) {
	r.vmWatcher, err = r.Inventory.WatchVms(context.TODO(), "", labels.Everything(), fields.Everything())
	if err != nil {
		r.log.Error(err, "failed to start vmWatcher")
		return err
	}
	return nil
}

// processEvent waits for any VM events and feeds the event to the
// converter module.
func (r *VirtualMachineManager) processEvent() error {
	resultCh := r.vmWatcher.ResultChan()
	for {
		event, ok := <-resultCh
		if !ok {
			r.vmWatcher.Stop()
			if err := r.resetWatcher(); err != nil {
				return err
			}
		}
		// Skip handling Bookmark events.
		if event.Type == watch.Bookmark {
			continue
		}
		r.converter.Ch <- event
	}
}
