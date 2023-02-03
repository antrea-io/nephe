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
	"fmt"
	"sync"
	"time"

	"antrea.io/nephe/pkg/logging"
	"github.com/cenkalti/backoff/v4"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	ctrlSyncStatus *controllerSyncStatus
)

type controllerSyncStatus struct {
	mutex   sync.RWMutex
	syncMap map[controllerType]bool
	// number of values of the type controllerType
	numElements int
	log         func() logging.Logger
}

func init() {
	ctrlSyncStatus = &controllerSyncStatus{
		syncMap:     make(map[controllerType]bool),
		numElements: int(ControllerTypeVM),
		log: func() logging.Logger {
			return logging.GetLogger("controllers-sync-status")
		},
	}

	// initialize the map to default values
	for i := 0; i < ctrlSyncStatus.numElements; i++ {
		ctrlSyncStatus.syncMap[controllerType(i)] = false
	}
}

func GetControllerSyncStatusInstance() *controllerSyncStatus {
	return ctrlSyncStatus
}

// SetControllerSyncStatus sets the sync status of the controller.
func (c *controllerSyncStatus) SetControllerSyncStatus(controller controllerType) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.syncMap[controller] = true
	c.log().Info("Sync done", "controller", controller.String())
}

// ResetControllerSyncStatus resets the sync status of the controller.
func (c *controllerSyncStatus) ResetControllerSyncStatus(controller controllerType) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.syncMap[controller] = false
}

// IsControllerSynced returns true only when the controller is synced.
func (c *controllerSyncStatus) IsControllerSynced(controller controllerType) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.syncMap[controller]
}

// areControllersSynced returns true only when all the controllers are synced.
func (c *controllerSyncStatus) areControllersSynced(controllers []controllerType) (bool, error) {
	for _, name := range controllers {
		if !c.IsControllerSynced(name) {
			return false, fmt.Errorf("failed to sync %v controller", name.String())
		}
	}
	return true, nil
}

// waitForControllersToSync checks status of the controllers with a retry mechanism.
func (c *controllerSyncStatus) waitForControllersToSync(controllers []controllerType, duration time.Duration) error { // nolint:unparam
	operation := func() error {
		_, err := c.areControllersSynced(controllers)
		return err
	}

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = duration
	err := backoff.Retry(operation, b)
	return err
}

// waitTillControllerIsInitialized blocks until controller is initialized or timeout is reached.
func (c *controllerSyncStatus) waitTillControllerIsInitialized(initialized *bool, timeout time.Duration, controller controllerType) error {
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
