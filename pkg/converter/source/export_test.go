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
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

var (
	RetryInterval = retryInterval
)

type RetryRecord = retryRecord

func ProcessEvent(v VMConverter, vm *VirtualMachineSource, failedUpdates map[string]retryRecord, isRetry bool, isAgented bool) {
	v.processEvent(vm, failedUpdates, isRetry, isAgented)
}

func (v VMConverter) GetRetryCh() chan cloudv1alpha1.VirtualMachine {
	return v.retryCh
}
