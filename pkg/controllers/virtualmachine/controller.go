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

package virtualmachine

import (
	"context"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/config"
	"antrea.io/nephe/pkg/controllers/sync"
	converter "antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/inventory"
	nephelabels "antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/logging"
)

const (
	ConverterChannelBuffer = 50
)

// VirtualMachineController reconciles a VirtualMachine object.
type VirtualMachineController struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	Inventory inventory.Interface
	converter converter.VMConverter
	vmWatcher watch.Interface
}

// nolint:lll

// ConfigureConverterAndStart configures the converter and starts the VirtualMachine controller.
func (r *VirtualMachineController) ConfigureConverterAndStart() {
	r.converter = converter.VMConverter{
		Client: r.Client,
		Log:    logging.GetLogger("converter").WithName("VMConverter"),
		Ch:     make(chan watch.Event, ConverterChannelBuffer),
		Scheme: r.Scheme,
	}
	go func() {
		if err := r.Start(); err != nil {
			r.Log.Error(err, "failed to start VirtualMachine controller exiting")
			os.Exit(1)
		}
	}()
}

// Start performs the initialization of the controller.
// VM controller is said to be initialized and synced when all the VM and EE
// CRs are reconciled.
func (r *VirtualMachineController) Start() error {
	if err := sync.GetControllerSyncStatusInstance().WaitForControllersToSync(
		[]sync.ControllerType{sync.ControllerTypeEE, sync.ControllerTypeCES, sync.ControllerTypeCPA}, sync.SyncTimeout); err != nil {
		return err
	}
	r.Log.Info("Init done", "controller", sync.ControllerTypeVM.String())

	vmNamespacedNameMap := r.getNamespacedNameVms()
	if err := r.syncExternalEntities(vmNamespacedNameMap); err != nil {
		return err
	}
	if err := r.syncExternalNodes(vmNamespacedNameMap); err != nil {
		return err
	}

	// Start the converter module.
	go r.converter.Start()
	// Set sync status as complete.
	sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeVM)
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

// getNamespacedNameVms returns a map of NamespacedName objects of all VMs.
func (r *VirtualMachineController) getNamespacedNameVms() map[types.NamespacedName]*runtimev1alpha1.VirtualMachine {
	vmObjList := r.Inventory.GetAllVms()
	vmNamespaceNameMap := make(map[types.NamespacedName]*runtimev1alpha1.VirtualMachine)
	for _, vmObj := range vmObjList {
		vm := vmObj.(*runtimev1alpha1.VirtualMachine)
		vmNamespaceNameKey := types.NamespacedName{
			Name:      vm.Name,
			Namespace: vm.Namespace,
		}
		vmNamespaceNameMap[vmNamespaceNameKey] = vm
	}
	return vmNamespaceNameMap
}

// syncExternalEntities validates that each EE has corresponding VM. If it does not exist then
// the EE will be deleted.
func (r *VirtualMachineController) syncExternalEntities(
	vmNamespacedNameMap map[types.NamespacedName]*runtimev1alpha1.VirtualMachine) error {
	eeList := &antreav1alpha2.ExternalEntityList{}
	if err := r.Client.List(context.TODO(), eeList, &client.ListOptions{}); err != nil {
		return err
	}
	for _, ee := range eeList.Items {
		if ee.Spec.ExternalNode != config.ANPNepheController {
			// Ignore EE objects that are not created by nephe.
			continue
		}
		eeLabelKeyName, exists := ee.Labels[nephelabels.ExternalEntityLabelKeyOwnerVm]
		if !exists {
			// Ignore EE objects not created by converter module.
			continue
		}
		eeNamespacedName := types.NamespacedName{
			Name:      eeLabelKeyName,
			Namespace: ee.Namespace,
		}

		cachedVm, ok := vmNamespacedNameMap[eeNamespacedName]
		if !ok {
			r.Log.Info("Could not find matching VM object, deleting ExternalEntity", "namespacedName", eeNamespacedName)
			// Delete the ExternalEntity, since no matching VM found.
			_ = r.Client.Delete(context.TODO(), &ee)
			continue
		}
		r.syncTags(ee.Labels, cachedVm)
	}
	return nil
}

// syncExternalNodes validates that each EN has corresponding VM. If it does not exist then
// the EN will be deleted.
func (r *VirtualMachineController) syncExternalNodes(
	vmNamespacedNameMap map[types.NamespacedName]*runtimev1alpha1.VirtualMachine) error {
	enList := &antreav1alpha1.ExternalNodeList{}
	if err := r.Client.List(context.TODO(), enList, &client.ListOptions{}); err != nil {
		return err
	}
	for _, en := range enList.Items {
		enLabelKeyName, exists := en.Labels[nephelabels.ExternalEntityLabelKeyOwnerVm]
		if !exists {
			// Ignore EN objects not created by converter module.
			continue
		}
		enNamespacedName := types.NamespacedName{
			Name:      enLabelKeyName,
			Namespace: en.Namespace,
		}
		cachedVm, ok := vmNamespacedNameMap[enNamespacedName]
		if !ok {
			r.Log.Info("Could not find matching VM object, deleting ExternalNode", "namespacedName", enNamespacedName)
			// Delete the ExternalNode, since no matching VM found.
			_ = r.Client.Delete(context.TODO(), &en)
			continue
		}
		r.syncTags(en.Labels, cachedVm)
	}
	return nil
}

// syncTags constructs tags from srcLabel and syncs virtual machine object in the inventory.
func (r *VirtualMachineController) syncTags(srcLabels map[string]string, destVm *runtimev1alpha1.VirtualMachine) {
	sourceTags := make(map[string]string)
	// Extract tags key.
	for key, value := range srcLabels {
		result := strings.TrimPrefix(key, nephelabels.LabelPrefixNephe+nephelabels.ExternalEntityLabelKeyTagPrefix)
		if strings.Compare(result, key) != 0 {
			sourceTags[result] = value
		}
	}

	destTags := make(map[string]string)
	for key, value := range sourceTags {
		if _, ok := destVm.Status.Tags[key]; !ok {
			// Tag not found on VM. Treat it as user tag.
			destTags[key] = value
		}
	}
	if len(destTags) > 0 {
		newVM := *destVm
		newVM.Spec.Tags = destTags
		// Update the VM in Cloud Inventory.
		if err := r.Inventory.UpdateVm(&newVM); err != nil {
			r.Log.Error(err, "failed to sync tags on virtual machine", "namespace", newVM.Namespace, "name", newVM.Name)
		}
	}
}

// resetWatcher sets a watcher to watch VM events.
func (r *VirtualMachineController) resetWatcher() (err error) {
	r.vmWatcher, err = r.Inventory.WatchVms(context.TODO(), "", labels.Everything(), fields.Everything())
	if err != nil {
		r.Log.Error(err, "failed to start vmWatcher")
		return err
	}
	return nil
}

// processEvent waits for any VM events and feeds the event to the converter module.
func (r *VirtualMachineController) processEvent() error {
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
