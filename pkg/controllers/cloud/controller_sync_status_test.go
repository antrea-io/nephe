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
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreav1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
)

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
		err := ctrlStatus.waitForControllersToSync([]controllerType{ControllerTypeCPA}, timeout)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set sync status of CPA controller and verify\n"))
		ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
		err = GetControllerSyncStatusInstance().waitForControllersToSync([]controllerType{ControllerTypeCPA}, timeout)
		Expect(err).Should(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Wait for CPA and CES controller sync and verify\n"))
		err = ctrlStatus.waitForControllersToSync([]controllerType{ControllerTypeCPA, ControllerTypeCES}, timeout)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set sync status of CPA and CES controller and verify\n"))
		ctrlStatus.SetControllerSyncStatus(ControllerTypeCES)
		err = ctrlStatus.waitForControllersToSync([]controllerType{ControllerTypeCPA, ControllerTypeCES}, timeout)
		Expect(err).Should(BeNil())
	})
	It("Wait for controller initialization", func() {
		_, _ = GinkgoWriter.Write([]byte("Wait for CPA controller initialization and verify\n"))
		initialized := false
		err := ctrlStatus.waitTillControllerIsInitialized(&initialized, 5*time.Second, ControllerTypeCPA)
		Expect(err).ShouldNot(BeNil())

		_, _ = GinkgoWriter.Write([]byte("Set CPA controller as initialized and verify\n"))
		initialized = true
		err = ctrlStatus.waitTillControllerIsInitialized(&initialized, 5*time.Second, ControllerTypeCPA)
		Expect(err).Should(BeNil())
	})

	Context("Controller start", func() {
		var (
			fakeClient client.WithWatch
		)

		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(crdv1alpha1.AddToScheme(newScheme))
			utilruntime.Must(antreav1alpha2.AddToScheme(newScheme))
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
		})

		It("CloudEntitySelector Start with no CRs", func() {
			cesReconciler := &CloudEntitySelectorReconciler{
				Log:    logf.Log,
				Client: fakeClient,
				Scheme: scheme,
			}
			ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
			// Start CES reconciler with 0 CRs, and verify sync status, pendingSyncCount and initialization status.
			err := cesReconciler.Start(context.TODO())
			Expect(err).Should(BeNil())
			Expect(cesReconciler.pendingSyncCount).Should(Equal(0))
			Expect(cesReconciler.initialized).Should(BeTrue())
			val := ctrlStatus.IsControllerSynced(ControllerTypeCES)
			Expect(val).Should(BeTrue())
		})
		It("CloudEntitySelector Start with one CR", func() {
			testSelectorNamespacedName := types.NamespacedName{Namespace: "namespace01", Name: "selector01"}
			selector := &crdv1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSelectorNamespacedName.Name,
					Namespace: testSelectorNamespacedName.Namespace,
				},
			}
			cesReconciler := &CloudEntitySelectorReconciler{
				Log:    logf.Log,
				Client: fakeClient,
				Scheme: scheme,
			}
			_ = fakeClient.Create(context.Background(), selector)
			ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
			// Start CES reconciler with one CR and verify sync status, pendingSyncCount and initialization status.
			err := cesReconciler.Start(context.TODO())
			Expect(err).Should(BeNil())
			Expect(cesReconciler.pendingSyncCount).Should(Equal(1))
			Expect(cesReconciler.initialized).Should(BeTrue())
			val := ctrlStatus.IsControllerSynced(ControllerTypeCES)
			Expect(val).Should(BeFalse())
		})
		It("ExternalEntity Start with no CRs", func() {
			npReconciler := &NetworkPolicyReconciler{
				Log:    logf.Log,
				Client: fakeClient,
			}
			ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
			// Start EE reconciler with 0 CRs, and verify sync status, pendingSyncCount and initialization status.
			err := npReconciler.externalEntityStart(context.TODO())
			Expect(err).Should(BeNil())
			Expect(npReconciler.pendingSyncCount).Should(Equal(0))
			Expect(npReconciler.initialized).Should(BeTrue())
			val := ctrlStatus.IsControllerSynced(ControllerTypeEE)
			Expect(val).Should(BeTrue())
		})
		It("ExternalEntity Start with one CR", func() {
			testEENamespacedName := types.NamespacedName{Namespace: "namespace01", Name: "ee01"}
			ee := &antreav1alpha2.ExternalEntity{
				ObjectMeta: v1.ObjectMeta{
					Name:      testEENamespacedName.Name,
					Namespace: testEENamespacedName.Namespace,
				},
			}
			npReconciler := &NetworkPolicyReconciler{
				Log:    logf.Log,
				Client: fakeClient,
			}
			_ = fakeClient.Create(context.Background(), ee)
			ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
			// Start EE reconciler with one CR and verify sync status, pendingSyncCount and initialization status.
			err := npReconciler.externalEntityStart(context.TODO())
			Expect(err).Should(BeNil())
			Expect(npReconciler.pendingSyncCount).Should(Equal(1))
			Expect(npReconciler.initialized).Should(BeTrue())
			val := ctrlStatus.IsControllerSynced(ControllerTypeEE)
			Expect(val).Should(BeFalse())
		})
		// TODO: Enable the tests only VM start is implemented completely.
		/*
			It("VirtualMachine Start with no CRs", func() {
				vmReconciler := &VirtualMachineManager{
					Log:    logf.Log,
					Client: fakeClient,
				}
				ctrlStatus.SetControllerSyncStatus(ControllerTypeEE)
				// Start VM reconciler with 0 CRs, and verify sync status, pendingSyncCount and initialization status.
				err := vmReconciler.Start(context.TODO())
				Expect(err).Should(BeNil())
				Expect(vmReconciler.pendingSyncCount).Should(Equal(0))
				Expect(vmReconciler.Initialized).Should(BeTrue())
				val := ctrlStatus.IsControllerSynced(ControllerTypeVM)
				Expect(val).Should(BeTrue())

			})
			It("VirtualMachine Start with one CR", func() {
				testVMNamespacedName := types.NamespacedName{Namespace: "namespace01", Name: "vm01"}
				vmReconciler := &VirtualMachineManager{
					Log:    logf.Log,
					Client: fakeClient,
				}
				vm := &v1alpha1.VirtualMachine{
					ObjectMeta: v1.ObjectMeta{
						Name:      testVMNamespacedName.Name,
						Namespace: testVMNamespacedName.Namespace,
					},
				}
				_ = fakeClient.Create(context.Background(), vm)
				ctrlStatus.SetControllerSyncStatus(ControllerTypeEE)
				// Start VM reconciler with one CR and verify sync status, pendingSyncCount and initialization status.
				err := vmReconciler.Start(context.TODO())
				Expect(err).Should(BeNil())
				Expect(vmReconciler.pendingSyncCount).Should(Equal(1))
				Expect(vmReconciler.Initialized).Should(BeTrue())
				val := ctrlStatus.IsControllerSynced(ControllerTypeVM)
				Expect(val).Should(BeFalse())

			})
			It("Controllers Start order", func() {
				cesReconciler := &CloudEntitySelectorReconciler{
					Log:    logf.Log,
					Client: fakeClient,
					Scheme: scheme,
				}
				ctrlStatus.SetControllerSyncStatus(ControllerTypeCPA)
				err := cesReconciler.Start(context.TODO())
				Expect(err).Should(BeNil())
				Expect(cesReconciler.pendingSyncCount).Should(Equal(0))
				Expect(cesReconciler.initialized).Should(BeTrue())
				val := ctrlStatus.IsControllerSynced(ControllerTypeCES)
				Expect(val).Should(BeTrue())

				npReconciler := &NetworkPolicyReconciler{
					Log:    logf.Log,
					Client: fakeClient,
				}
				err = npReconciler.externalEntityStart(context.TODO())
				Expect(err).Should(BeNil())
				Expect(cesReconciler.pendingSyncCount).Should(Equal(0))
				Expect(cesReconciler.initialized).Should(BeTrue())
				val = ctrlStatus.IsControllerSynced(ControllerTypeEE)
				Expect(val).Should(BeTrue())

				vmReconciler := &VirtualMachineManager{
					Log:    logf.Log,
					Client: fakeClient,
				}
				err = vmReconciler.Start(context.TODO())
				Expect(err).Should(BeNil())
				Expect(vmReconciler.pendingSyncCount).Should(Equal(0))
				Expect(vmReconciler.Initialized).Should(BeTrue())
				val = ctrlStatus.IsControllerSynced(ControllerTypeVM)
				Expect(val).Should(BeTrue())
			})
		*/
	})
})
