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

package source

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreav1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/converter/target"
)

const (
	retryInterval = 5 * time.Second
	maxRetry      = 5
)

type VMConverter struct {
	client.Client
	Log     logr.Logger
	Ch      chan watch.Event
	retryCh chan VirtualMachineSource
	Scheme  *runtime.Scheme
}

func (v VMConverter) Start() {
	failedUpdates := make(map[string]retryRecord)
	v.retryCh = make(chan VirtualMachineSource)

	for {
		select {
		case recv, ok := <-v.Ch:
			if !ok {
				v.Log.Info("VM converter channel closed")
				return
			}
			vm := &VirtualMachineSource{*recv.Object.(*runtimev1alpha1.VirtualMachine), recv.Type}
			v.processEvent(vm, failedUpdates, false, vm.Status.Agented)
		case recv := <-v.retryCh:
			v.processEvent(&recv, failedUpdates, true, recv.Status.Agented)
		}
	}
}

func (v VMConverter) processEvent(vm *VirtualMachineSource, failedUpdates map[string]retryRecord, isRetry, isExternalNode bool) {
	var err error
	var fetchKey client.ObjectKey
	var resource string

	if isExternalNode {
		resource = "ExternalNode"
		fetchKey = target.GetExternalNodeKeyFromSource(vm)
	} else {
		resource = "ExternalEntity"
		fetchKey = target.GetExternalEntityKeyFromSource(vm)
	}
	v.Log.V(1).Info(fmt.Sprintf("Received %s event", resource), "Key", fetchKey)

	if isRetry {
		retry, ok := failedUpdates[fetchKey.String()]
		// ignore event if newer event succeeds or newer event retrying
		if !ok || v.isNewEvent(retry.item.(*VirtualMachineSource), vm) {
			v.Log.Info(fmt.Sprintf("Ignore retry for %s", resource), "Key", fetchKey,
				"retryCount", retry.retryCount)
			return
		}
	}

	defer func() {
		// Retry logic after processing.
		if err == nil {
			delete(failedUpdates, fetchKey.String())
			return
		}
		record, ok := failedUpdates[fetchKey.String()]
		// new record if new event or if current is newer than record event
		if !ok || v.isNewEvent(vm, record.item.(*VirtualMachineSource)) {
			record = retryRecord{0, vm}
		}
		record.retryCount += 1
		if record.retryCount >= maxRetry {
			v.Log.Info(fmt.Sprintf("Max retry reached, ignoring %s", resource), "Key", fetchKey,
				"maxRetry", maxRetry)
			delete(failedUpdates, fetchKey.String())
			return
		}
		failedUpdates[fetchKey.String()] = record
		time.AfterFunc(retryInterval, func() {
			v.retryCh <- *vm
		})
	}()

	var isDelete bool
	if vm.EventType == watch.Deleted {
		isDelete = true
	}
	ctx := context.Background()
	externNode := &antreav1alpha1.ExternalNode{}
	externEntity := &antreav1alpha2.ExternalEntity{}
	isNotFound := false
	if isExternalNode {
		err = v.Client.Get(ctx, fetchKey, externNode)
	} else {
		err = v.Client.Get(ctx, fetchKey, externEntity)
	}
	if err != nil {
		err = client.IgnoreNotFound(err)
		if err != nil {
			v.Log.Error(err, fmt.Sprintf("unable to fetch %s", resource), "Key", fetchKey)
			return
		}
		isNotFound = true
	}

	// Skip processing if resource is not present and isDelete event is set.
	if isDelete && isNotFound {
		return
	}

	// Delete.
	if isDelete && !isNotFound {
		if isExternalNode {
			err = v.Client.Delete(ctx, externNode)
		} else {
			err = v.Client.Delete(ctx, externEntity)
		}
		err = client.IgnoreNotFound(err)
		if err != nil {
			v.Log.Error(err, fmt.Sprintf("unable to delete resource %s", resource), "Key", fetchKey)
		} else {
			v.Log.Info(fmt.Sprintf("Deleted %s", resource), "Key", fetchKey)
		}
		return
	}

	if !isNotFound {
		if isExternalNode {
			base := client.MergeFrom(externNode.DeepCopy())
			patch, changed := target.PatchExternalNodeFrom(vm, externNode, v.Client)
			if changed {
				err = v.Client.Patch(ctx, patch, base)
			} else {
				return
			}
		} else {
			base := client.MergeFrom(externEntity.DeepCopy())
			patch, changed := target.PatchExternalEntityFrom(vm, externEntity, v.Client)
			if changed {
				err = v.Client.Patch(ctx, patch, base)
			} else {
				return
			}
		}
		if err != nil {
			v.Log.Error(err, fmt.Sprintf("unable to patch %s", resource), "Key", fetchKey)
		} else {
			v.Log.Info(fmt.Sprintf("Patched %s", resource), "Key", fetchKey)
		}
		return
	}

	// TODO: Split Add/Create/Delete is new functions
	if isExternalNode {
		externNode = target.NewExternalNodeFrom(vm, fetchKey.Name, fetchKey.Namespace, v.Client, v.Scheme)
		err = v.Client.Create(ctx, externNode)
	} else {
		externEntity = target.NewExternalEntityFrom(vm, fetchKey.Name, fetchKey.Namespace, v.Client, v.Scheme)
		err = v.Client.Create(ctx, externEntity)
	}
	if err != nil {
		v.Log.Error(err, fmt.Sprintf("unable to create %s", resource), "Key", fetchKey)
	} else {
		v.Log.Info(fmt.Sprintf("Created %s", resource), "Key", fetchKey)
	}
}

func (v VMConverter) isNewEvent(cur, record *VirtualMachineSource) bool {
	acc1, _ := meta.Accessor(cur)
	acc2, _ := meta.Accessor(record)
	return acc1.GetGeneration() != acc2.GetGeneration()
}
