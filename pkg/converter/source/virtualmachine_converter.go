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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/converter/target"
)

const (
	retryInterval = 5 * time.Second
	maxRetry      = 5
)

type VMConverter struct {
	client.Client
	Log     logr.Logger
	Ch      chan cloudv1alpha1.VirtualMachine
	retryCh chan cloudv1alpha1.VirtualMachine
	Scheme  *runtime.Scheme
}

func (v VMConverter) Start() {
	failedUpdates := make(map[string]retryRecord)
	v.retryCh = make(chan cloudv1alpha1.VirtualMachine)

	for {
		select {
		case recv, ok := <-v.Ch:
			if !ok {
				v.Log.Info("VMConverter channel closed")
				return
			}
			vm := &VirtualMachineSource{recv}
			v.processEvent(vm, failedUpdates, false)
		case recv := <-v.retryCh:
			vm := &VirtualMachineSource{recv}
			v.processEvent(vm, failedUpdates, true)
		}
	}
}

func (v VMConverter) processEvent(vm *VirtualMachineSource, failedUpdates map[string]retryRecord, isRetry bool) {
	var err error
	log := v.Log.WithName("processVirtualmachineEvent")

	fetchKey := target.GetObjectKeyFromSource(vm)
	log.Info("Received vm event", "FetchKey", fetchKey)
	if isRetry {
		retry, ok := failedUpdates[fetchKey.String()]
		// ignore event if newer event succeeds or newer event retrying
		if !ok || v.isNewEvent(retry.item.(*VirtualMachineSource), vm) {
			log.Info("Ignore retry", "Key", fetchKey, "retryCount", retry.retryCount)
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
			log.Info("Max retry reached, ignoring", "Key", fetchKey, "maxRetry", maxRetry)
			delete(failedUpdates, fetchKey.String())
			return
		}
		failedUpdates[fetchKey.String()] = record
		time.AfterFunc(retryInterval, func() {
			v.retryCh <- vm.VirtualMachine
		})
	}()

	ctx := context.Background()
	ips, err := vm.GetEndPointAddresses()
	if err != nil {
		log.Info("Failed to get IP address for", "Name", fetchKey, "err", err)
		return
	}

	isDelete := len(ips) == 0
	externEntity := &antreatypes.ExternalEntity{}
	isNotFound := false
	err = v.Client.Get(ctx, fetchKey, externEntity)
	if err != nil {
		err = client.IgnoreNotFound(err)
		if err != nil {
			log.Error(err, "Unable to fetch ", "Key", fetchKey)
			return
		}
		isNotFound = true
	}

	// No-op.
	if isDelete && isNotFound {
		log.V(1).Info("Deleting non-existing resource", "Key", fetchKey)
		return
	}

	// Delete.
	if isDelete && !isNotFound {
		err = v.Client.Delete(ctx, externEntity)
		err = client.IgnoreNotFound(err)
		if err != nil {
			log.Error(err, "Unable to delete ", "Key", fetchKey)
		} else {
			log.V(1).Info("Deleted resource", "Key", fetchKey)
		}
		return
	}

	// Update.
	if !isNotFound {
		base := client.MergeFrom(externEntity.DeepCopy())
		patch := target.PatchExternalEntityFrom(vm, externEntity, v.Client)
		if err = v.Client.Patch(ctx, patch, base); err != nil {
			log.Error(err, "Unable to patch ", "Key", fetchKey)
		} else {
			log.V(1).Info("Patched resource", "Key", fetchKey)
		}
		return
	}

	// Create.
	externEntity = target.NewExternalEntityFrom(vm, fetchKey.Name, fetchKey.Namespace, v.Client, v.Scheme)
	if err = v.Client.Create(ctx, externEntity); err != nil {
		log.Error(err, "Unable to create ", "Key", fetchKey)
	} else {
		log.V(1).Info("Created resource", "Key", fetchKey)
	}
}

func (v VMConverter) isNewEvent(cur, record *VirtualMachineSource) bool {
	acc1, _ := meta.Accessor(cur)
	acc2, _ := meta.Accessor(record)
	return acc1.GetGeneration() != acc2.GetGeneration()
}
