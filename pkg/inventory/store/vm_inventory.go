// Copyright 2023 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package store

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	antreastorage "antrea.io/antrea/pkg/apiserver/storage"
	"antrea.io/antrea/pkg/apiserver/storage/ram"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory/indexer"
	nephelabels "antrea.io/nephe/pkg/labels"
)

// vpcInventoryEvent implements storage.InternalEvent.
type vmInventoryEvent struct {
	// The current version of the stored VPC.
	CurrObject *runtimev1alpha1.VirtualMachine
	// The previous version of the stored VPC.
	PrevObject *runtimev1alpha1.VirtualMachine
	// The key of this VPC.
	Key             string
	ResourceVersion uint64
}

// keyAndSpanSelectFuncVm returns whether the provided selectors matches the key and/or the nodeNames.
func keyAndSpanSelectFuncVm(selectors *antreastorage.Selectors, key string, obj interface{}) bool {
	// If Key is present in selectors, the provided key must match it.
	if selectors.Key != "" && key != selectors.Key {
		return false
	}
	labelSelector := labels.Everything()
	if selectors != nil && selectors.Label != nil {
		labelSelector = selectors.Label
	}
	vpc, _ := obj.(*runtimev1alpha1.VirtualMachine)
	fieldSelector := fields.Everything()
	if selectors != nil && selectors.Field != nil {
		fieldSelector = selectors.Field
	}
	vmFields := map[string]string{
		"metadata.name":      vpc.Name,
		"metadata.namespace": vpc.Namespace,
	}
	return labelSelector.Matches(labels.Set(vpc.Labels)) && fieldSelector.Matches(fields.Set(vmFields))
}

// isSelected determines if the previous and the current version of an object should be selected by the given selectors.
func isSelectedVm(key string, prevObj, currObj interface{}, selectors *antreastorage.Selectors, isInitEvent bool) (bool, bool) {
	// We have filtered out init events that we are not interested in, so the current object must be selected.
	if isInitEvent {
		return false, true
	}
	prevObjSelected := !reflect.ValueOf(prevObj).IsNil() && keyAndSpanSelectFuncVm(selectors, key, prevObj)
	currObjSelected := !reflect.ValueOf(currObj).IsNil() && keyAndSpanSelectFuncVm(selectors, key, currObj)
	return prevObjSelected, currObjSelected
}

// ToWatchEvent converts the vmEvent to *watch.Event based on the provided Selectors. It has the following features:
// 1. Added event will be generated if the Selectors was not interested in the object but is now.
// 2. Modified event will be generated if the Selectors was and is interested in the object.
// 3. Deleted event will be generated if the Selectors was interested in the object but is not now.
func (event *vmInventoryEvent) ToWatchEvent(selectors *antreastorage.Selectors, isInitEvent bool) *watch.Event {
	prevObjSelected, currObjSelected := isSelectedVm(event.Key, event.PrevObject, event.CurrObject, selectors, isInitEvent)
	switch {
	case !currObjSelected && !prevObjSelected:
		return nil
	case currObjSelected && !prevObjSelected:
		// Watcher was not interested in that object but is now, an added event will be generated.
		return &watch.Event{Type: watch.Added, Object: event.CurrObject}
	case currObjSelected && prevObjSelected:
		// Watcher was not interested in that object but is now, an added event will be generated.
		return &watch.Event{Type: watch.Modified, Object: event.CurrObject}
	case !currObjSelected && prevObjSelected:
		// Watcher was interested in that object but is not interested now, a deleted event will be generated.
		return &watch.Event{Type: watch.Deleted, Object: event.PrevObject}
	}
	return nil
}

func (event *vmInventoryEvent) GetResourceVersion() uint64 {
	return event.ResourceVersion
}

var _ antreastorage.GenEventFunc = genVmEvent

// genVPCEvent generates InternalEvent from the given version of a vm.
func genVmEvent(key string, prevObj, currObj interface{}, rv uint64) (antreastorage.InternalEvent, error) {
	if reflect.DeepEqual(prevObj, currObj) {
		return nil, nil
	}
	event := &vmInventoryEvent{Key: key, ResourceVersion: rv}
	if prevObj != nil {
		event.PrevObject = prevObj.(*runtimev1alpha1.VirtualMachine)
	}
	if currObj != nil {
		event.CurrObject = currObj.(*runtimev1alpha1.VirtualMachine)
	}
	return event, nil
}

// vmKeyFunc knows how to get the key of a vm.
func vmKeyFunc(obj interface{}) (string, error) {
	vm, ok := obj.(*runtimev1alpha1.VirtualMachine)
	if !ok {
		return "", fmt.Errorf("object is not of type runtime/v1alpha1/VirtualMachine: %v", obj)
	}
	return fmt.Sprintf("%v/%v", vm.Namespace, vm.Name), nil
}

// NewVmInventoryStore creates a store of Virtual Machine.
func NewVmInventoryStore() antreastorage.Interface {
	indexers := cache.Indexers{
		indexer.ByNamespace: func(obj interface{}) ([]string, error) {
			vm := obj.(*runtimev1alpha1.VirtualMachine)
			return []string{vm.Namespace}, nil
		},
		indexer.ByNamespacedName: func(obj interface{}) ([]string, error) {
			vm := obj.(*runtimev1alpha1.VirtualMachine)
			return []string{vm.Namespace + "/" + vm.Name}, nil
		},
		indexer.VirtualMachineByNamespacedAccountName: func(obj interface{}) ([]string, error) {
			vm := obj.(*runtimev1alpha1.VirtualMachine)
			return []string{vm.Labels[nephelabels.CloudAccountNamespace] + "/" +
				vm.Labels[nephelabels.CloudAccountName]}, nil
		},
		indexer.VirtualMachineByCloudId: func(obj interface{}) ([]string, error) {
			vm := obj.(*runtimev1alpha1.VirtualMachine)
			return []string{vm.Status.CloudId}, nil
		},
	}
	return ram.NewStore(vmKeyFunc, indexers, genVmEvent, keyAndSpanSelectFuncVm,
		func() runtime.Object { return new(runtimev1alpha1.VirtualMachine) })
}

// GetSelectorsVm extracts label selector, field selector, and key selector from the provided options.
func GetSelectorsVm(options *internalversion.ListOptions) (string, labels.Selector, fields.Selector) {
	label := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		label = options.LabelSelector
	}
	field := fields.Everything()
	if options != nil && options.FieldSelector != nil {
		field = options.FieldSelector
	}
	key, _ := field.RequiresExactMatch("metadata.name")
	return key, label, field
}
