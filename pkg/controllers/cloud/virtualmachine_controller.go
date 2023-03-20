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
	ctrl "sigs.k8s.io/controller-runtime"
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

// VirtualMachineReconciler reconciles a VirtualMachine object.
type VirtualMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	// TODO: pass pointer of Inventory.
	Inventory inventory.Inventory
	converter converter.VMConverter
	vmWatcher watch.Interface
}

// nolint:lll

func (r *VirtualMachineReconciler) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	// TODO: Remove Reconciler for VirtualMachine
	_ = r.Log.WithValues("Virtualmachine", req.NamespacedName)
	return ctrl.Result{}, nil
}

// SetupWithManager registers VirtualMachineReconciler with manager.
// It also sets up converter channels to forward VM events.
func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.converter = converter.VMConverter{
		Client: r.Client,
		Log:    logging.GetLogger("converter").WithName("VMConverter"),
		Ch:     make(chan watch.Event, converterChannelBuffer),
		Scheme: r.Scheme,
	}
	return mgr.Add(r)
}

// Start performs the initialization of the controller.
// VM controller is said to be initialized and synced when all the VM and EE
// CRs are reconciled.
func (r *VirtualMachineReconciler) Start(context.Context) error {
	if err := GetControllerSyncStatusInstance().waitForControllersToSync(
		[]controllerType{ControllerTypeEE, ControllerTypeCES, ControllerTypeCPA}, syncTimeout); err != nil {
		return err
	}
	r.Log.Info("Init done", "controller", ControllerTypeVM.String())

	/* TODO: Handle restart case. Fetch all EE and all VMs (in a map) and walk thru EE and check if it exists
	 * in the map. If not, issues a delete for that EE.
	 */
	if err := r.syncExternalEntities(); err != nil {
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
func (r *VirtualMachineReconciler) getAllVMs() map[types.NamespacedName]struct{} {
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
func (r *VirtualMachineReconciler) syncExternalEntities() error {
	vmMap := r.getAllVMs()
	eeList := &antreav1alpha2.ExternalEntityList{}
	if err := r.Client.List(context.TODO(), eeList, &client.ListOptions{}); err != nil {
		return err
	}
	for _, ee := range eeList.Items {
		eeNn := types.NamespacedName{
			Name:      ee.Labels[config.ExternalEntityLabelKeyName],
			Namespace: ee.Namespace,
		}
		if _, ok := vmMap[eeNn]; !ok {
			r.Log.Info("Could not find matching VM object, deleting ExternalEntity", "namespacedName", eeNn)
			// delete the external entity, since no matching VM found.
			_ = r.Client.Delete(context.TODO(), &ee)
			continue
		}
	}
	return nil
}

// resetWatcher sets a watcher to watch VM events.
func (r *VirtualMachineReconciler) resetWatcher() (err error) {
	r.vmWatcher, err = r.Inventory.WatchVms(context.TODO(), "", labels.Everything(), fields.Everything())
	if err != nil {
		r.Log.Error(err, "failed to start vmWatcher")
		return err
	}
	return nil
}

// processEvent waits for any VM events and feeds the event to the
// converter module.
func (r *VirtualMachineReconciler) processEvent() error {
	resultCh := r.vmWatcher.ResultChan()
	for {
		select {
		case event, ok := <-resultCh:
			if !ok {
				// TODO: we cannot exit here? Retry watcher
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
}
