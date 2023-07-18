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
	"antrea.io/nephe/pkg/apiserver/registry/inventory/selector"
	"antrea.io/nephe/pkg/inventory/indexer"
	nephelabels "antrea.io/nephe/pkg/labels"
	"fmt"
	"k8s.io/client-go/tools/cache"
	"reflect"

	antreastorage "antrea.io/antrea/pkg/apiserver/storage"
	"antrea.io/antrea/pkg/apiserver/storage/ram"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// sgInventoryEvent implements storage.InternalEvent.
type sgInventoryEvent struct {
	// The current version of the stored SG.
	CurrObject *runtimev1alpha1.SecurityGroup
	// The previous version of the stored SG.
	PrevObject *runtimev1alpha1.SecurityGroup
	// The key of this SG.
	Key             string
	ResourceVersion uint64
}

// keyAndSpanSelectFuncSg returns whether the provided selectors matches the key and/or the nodeNames.
func keyAndSpanSelectFuncSg(selectors *antreastorage.Selectors, key string, obj interface{}) bool {
	// If Key is present in selectors, the provided key must match it.
	if selectors.Key != "" && key != selectors.Key {
		return false
	}
	if selectors.Label.Empty() && selectors.Field.Empty() {
		// Match everything.
		return true
	}

	labelSelector := labels.Everything()
	if !selectors.Label.Empty() {
		labelSelector = selectors.Label
	}
	fieldSelector := fields.Everything()
	if !selectors.Field.Empty() {
		fieldSelector = selectors.Field
	}
	sg, _ := obj.(*runtimev1alpha1.SecurityGroup)
	sgFields := map[string]string{
		selector.MetaName:      sg.Name,
		selector.MetaNamespace: sg.Namespace,
	}
	return labelSelector.Matches(labels.Set(sg.Labels)) && fieldSelector.Matches(fields.Set(sgFields))
}

// isSelected determines if the previous and the current version of an object should be selected by the given selectors.
func isSelectedSg(key string, prevObj, currObj interface{}, selectors *antreastorage.Selectors, isInitEvent bool) (bool, bool) {
	// We have filtered out init events that we are not interested in, so the current object must be selected.
	if isInitEvent {
		return false, true
	}
	prevObjSelected := !reflect.ValueOf(prevObj).IsNil() && keyAndSpanSelectFuncSg(selectors, key, prevObj)
	currObjSelected := !reflect.ValueOf(currObj).IsNil() && keyAndSpanSelectFuncSg(selectors, key, currObj)
	return prevObjSelected, currObjSelected
}

// ToWatchEvent converts the vpcEvent to *watch.Event based on the provided Selectors. It has the following features:
// 1. Added event will be generated if the Selectors was not interested in the object but is now.
// 2. Modified event will be generated if the Selectors was and is interested in the object.
// 3. Deleted event will be generated if the Selectors was interested in the object but is not now.
func (event *sgInventoryEvent) ToWatchEvent(selectors *antreastorage.Selectors, isInitEvent bool) *watch.Event {
	prevObjSelected, currObjSelected := isSelectedSg(event.Key, event.PrevObject, event.CurrObject, selectors, isInitEvent)
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

func (event *sgInventoryEvent) GetResourceVersion() uint64 {
	return event.ResourceVersion
}

var _ antreastorage.GenEventFunc = genSgEvent

// genSgEvent generates InternalEvent from the given versions of a SG.
func genSgEvent(key string, prevObj, currObj interface{}, rv uint64) (antreastorage.InternalEvent, error) {
	if reflect.DeepEqual(prevObj, currObj) {
		return nil, nil
	}

	event := &sgInventoryEvent{Key: key, ResourceVersion: rv}
	if prevObj != nil {
		event.PrevObject = prevObj.(*runtimev1alpha1.SecurityGroup)
	}
	if currObj != nil {
		event.CurrObject = currObj.(*runtimev1alpha1.SecurityGroup)
	}
	return event, nil
}

// sgKeyFunc knows how to get the key of an SG.
func sgKeyFunc(obj interface{}) (string, error) {
	sg, ok := obj.(*runtimev1alpha1.SecurityGroup)
	if !ok {
		return "", fmt.Errorf("object is not of type runtime/v1alpha1/SecurityGroup: %v", obj)
	}
	return fmt.Sprintf("%v/%v", sg.Namespace, sg.Name), nil
}

// NewSgInventoryStore creates a store of Security Group.
func NewSgInventoryStore() antreastorage.Interface {
	indexers := cache.Indexers{
		indexer.ByNamespace: func(obj interface{}) ([]string, error) {
			sg := obj.(*runtimev1alpha1.SecurityGroup)
			return []string{sg.Namespace}, nil
		},
		indexer.SecurityGroupByAccountNamespacedName: func(obj interface{}) ([]string, error) {
			sg := obj.(*runtimev1alpha1.SecurityGroup)
			return []string{sg.Labels[nephelabels.CloudAccountNamespace] + "/" +
				sg.Labels[nephelabels.CloudAccountName]}, nil
		},
		indexer.SecurityGroupBySelectorNamespacedName: func(obj interface{}) ([]string, error) {
			sg := obj.(*runtimev1alpha1.SecurityGroup)
			return []string{sg.Labels[nephelabels.CloudSelectorNamespace] + "/" +
				sg.Labels[nephelabels.CloudSelectorName]}, nil
		},
		indexer.SecurityGroupByCloudID: func(obj interface{}) ([]string, error) {
			sg := obj.(*runtimev1alpha1.SecurityGroup)
			return []string{sg.Status.CloudId}, nil
		},
	}
	return ram.NewStore(sgKeyFunc, indexers, genSgEvent, keyAndSpanSelectFuncSg, func() runtime.Object {
		return new(runtimev1alpha1.
			SecurityGroup)
	})
}
