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
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCloud(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Sync Status")
}

var _ = Describe("Controller sync status", func() {
	var timeout = 1 * time.Second
	var ctrlStatus *controllerSyncStatus

	BeforeEach(func() {
		ctrlStatus = GetControllerSyncStatusInstance()
		ctrlStatus.Configure()
	})

	AfterEach(func() {
		ctrlStatus.ResetControllerSyncStatus(ControllerTypeCPA)
		ctrlStatus.ResetControllerSyncStatus(ControllerTypeCES)
		ctrlStatus.ResetControllerSyncStatus(ControllerTypeEE)
		ctrlStatus.ResetControllerSyncStatus(ControllerTypeVM)
	})

	It("Check sync status of controller", func() {
		_, _ = GinkgoWriter.Write([]byte("Verify default CPA Sync status\n"))
		val := ctrlStatus.IsControllerSynced(ControllerTypeCPA)
		Expect(val).To(BeFalse())

		_, _ = GinkgoWriter.Write([]byte("Set CPA sync status and verify\n"))
		ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
		val = ctrlStatus.IsControllerSynced(ControllerTypeCPA)
		Expect(val).To(BeTrue())

		_, _ = GinkgoWriter.Write([]byte("Verify CES sync status is unchanged\n"))
		val = ctrlStatus.IsControllerSynced(ControllerTypeCES)
		Expect(val).To(BeFalse())

		_, _ = GinkgoWriter.Write([]byte("Reset CPA sync status and verify\n"))
		ctrlStatus.ResetControllerSyncStatus(ControllerTypeCPA)
		val = ctrlStatus.IsControllerSynced(ControllerTypeCPA)
		Expect(val).To(BeFalse())
	})
	It("Wait to check controller is synced", func() {
		_, _ = GinkgoWriter.Write([]byte("Wait for CPA controller sync and verify\n"))
		err := ctrlStatus.WaitForControllersToSync([]ControllerType{ControllerTypeCPA}, timeout)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set sync status of CPA controller and verify\n"))
		ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
		err = GetControllerSyncStatusInstance().WaitForControllersToSync([]ControllerType{ControllerTypeCPA}, timeout)
		Expect(err).Should(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Wait for CPA and CES controller sync and verify\n"))
		err = ctrlStatus.WaitForControllersToSync([]ControllerType{ControllerTypeCPA, ControllerTypeCES}, timeout)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set sync status of CPA and CES controller and verify\n"))
		ctrlStatus.SetControllerSyncStatus(ControllerTypeCES)
		err = ctrlStatus.WaitForControllersToSync([]ControllerType{ControllerTypeCPA, ControllerTypeCES}, timeout)
		Expect(err).Should(BeNil())
	})
	It("Wait for controller initialization", func() {
		_, _ = GinkgoWriter.Write([]byte("Wait for CPA controller initialization and verify\n"))
		initialized := false
		err := ctrlStatus.WaitTillControllerIsInitialized(&initialized, 5*time.Second, ControllerTypeCPA)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set CPA controller as initialized and verify\n"))
		initialized = true
		err = ctrlStatus.WaitTillControllerIsInitialized(&initialized, 5*time.Second, ControllerTypeCPA)
		Expect(err).Should(BeNil())
	})
})
