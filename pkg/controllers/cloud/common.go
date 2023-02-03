// Copyright 2023 Antrea Authors.
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

package cloud

import (
	"time"
)

const (
	// Error messages
	errorMsgAccountAddFail             = "failed to add account"
	errorMsgSelectorAddFail            = "selector add failed, poller is not created for account"
	errorMsgSelectorAccountMapNotFound = "failed to find account for selector"
	errorMsgAccountPollerNotFound      = "account poller not found"
	ErrorMsgControllerInitializing     = "controller is initializing"

	syncTimeout = 5 * time.Minute
	initTimeout = 30 * time.Second
)

// controllerType is the state of securityGroup.
type controllerType int

// When a new constant is added, also update the numElements
// field of controllerSyncStatus
const (
	ControllerTypeCPA controllerType = iota
	ControllerTypeCES
	ControllerTypeEE
	ControllerTypeVM
)

func (t controllerType) String() string {
	switch t {
	case ControllerTypeCPA:
		return "CloudProviderAccount"
	case ControllerTypeCES:
		return "CloudEntitySelector"
	case ControllerTypeEE:
		return "ExternalEntity"
	case ControllerTypeVM:
		return "VirtualMachine"
	}
	return "unknown"
}
