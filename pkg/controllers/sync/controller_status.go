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

package sync

import (
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/wait"

	"antrea.io/nephe/pkg/logging"
)

var (
	ctrlSyncStatus *controllerSyncStatus
)

const (
	SyncTimeout = 5 * time.Minute
	InitTimeout = 30 * time.Second

	ErrorMsgControllerInitializing = "controller is initializing"
)

// ControllerType is the state of securityGroup.
type ControllerType int

// When a new constant is added, also update the numElements
// field of controllerSyncStatus
const (
	ControllerTypeCPA ControllerType = iota
	ControllerTypeCES
	ControllerTypeEE
	ControllerTypeVM
)

func (t ControllerType) String() string {
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

type controllerSyncStatus struct {
	mutex   sync.RWMutex
	syncMap map[ControllerType]bool
	// number of values of the type ControllerType
	numElements int
	log         logr.Logger
}

func init() {
	ctrlSyncStatus = &controllerSyncStatus{
		syncMap:     make(map[ControllerType]bool),
		numElements: int(ControllerTypeVM),
	}

	// initialize the map to default values
	for i := 0; i < ctrlSyncStatus.numElements; i++ {
		ctrlSyncStatus.syncMap[ControllerType(i)] = false
	}
}

// GetControllerSyncStatusInstance returns only instance of controllerSyncStatus.
func GetControllerSyncStatusInstance() *controllerSyncStatus {
	return ctrlSyncStatus
}

// Configure configures controllerSyncStatus.
func (c *controllerSyncStatus) Configure() *controllerSyncStatus {
	c.log = logging.GetLogger("controllers").WithName("VirtualMachine")
	return nil
}

// SetControllerSyncStatus sets the sync status of the controller.
func (c *controllerSyncStatus) SetControllerSyncStatus(controller ControllerType) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.syncMap[controller] = true
	c.log.Info("Sync done", "controller", controller.String())
}

// ResetControllerSyncStatus resets the sync status of the controller.
func (c *controllerSyncStatus) ResetControllerSyncStatus(controller ControllerType) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.syncMap[controller] = false
}

// IsControllerSynced returns true only when the controller is synced.
func (c *controllerSyncStatus) IsControllerSynced(controller ControllerType) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.syncMap[controller]
}

// AreControllersSynced returns true only when all the controllers are synced.
func (c *controllerSyncStatus) AreControllersSynced(controllers []ControllerType) (bool, error) {
	for _, name := range controllers {
		if !c.IsControllerSynced(name) {
			return false, fmt.Errorf("failed to sync %v controller", name.String())
		}
	}
	return true, nil
}

// WaitForControllersToSync checks status of the controllers with a retry mechanism.
func (c *controllerSyncStatus) WaitForControllersToSync(controllers []ControllerType, duration time.Duration) error { // nolint:unparam
	operation := func() error {
		_, err := c.AreControllersSynced(controllers)
		return err
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration
	err := backoff.Retry(operation, b)
	return err
}

// WaitTillControllerIsInitialized blocks until controller is initialized or timeout is reached.
func (c *controllerSyncStatus) WaitTillControllerIsInitialized(initialized *bool, timeout time.Duration, controller ControllerType) error {
	if err := wait.PollImmediate(time.Second*5, timeout, func() (bool, error) {
		if *initialized {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("failed to initialize %v controller, will retry again, err: %v", controller, err)
	}
	return nil
}
