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

package cloudentityselector

import (
	"context"
	"fmt"
	"testing"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	ctrlsync "antrea.io/nephe/pkg/controllers/sync"
	mockaccmanager "antrea.io/nephe/pkg/testing/accountmanager"
)

var (
	mockCtrl       *mock.Controller
	mockAccManager *mockaccmanager.MockInterface
	scheme         = runtime.NewScheme()
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	_ = clientgoscheme.AddToScheme(scheme)
	_ = antreatypes.AddToScheme(scheme)
	_ = crdv1alpha1.AddToScheme(scheme)
	_ = antreanetworking.AddToScheme(scheme)
})

func TestCloud(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CloudEntitySelector controller")
}

var _ = Describe("CloudEntitySelector Controller", func() {
	Context("CES workflow", func() {
		var (
			testAccountNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSelectorNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "selector01"}
			selector                   *crdv1alpha1.CloudEntitySelector
			reconciler                 *CloudEntitySelectorReconciler
			fakeClient                 client.WithWatch
		)
		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(crdv1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			mockCtrl = mock.NewController(GinkgoT())
			mockAccManager = mockaccmanager.NewMockInterface(mockCtrl)
			reconciler = &CloudEntitySelectorReconciler{
				Log:                  logf.Log,
				Client:               fakeClient,
				Scheme:               scheme,
				selectorToAccountMap: make(map[types.NamespacedName]types.NamespacedName),
				AccManager:           mockAccManager,
			}

			selector = &crdv1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSelectorNamespacedName.Name,
					Namespace: testSelectorNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/crdv1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       testAccountNamespacedName.Name,
						},
					},
				},
				Spec: crdv1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []crdv1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &crdv1alpha1.EntityMatch{
								MatchID: "xyzq",
							},
						},
					},
				},
			}
		})

		It("CES Add and Delete workflow", func() {
			mockAccManager.EXPECT().AddResourceFiltersToAccount(&testAccountNamespacedName, &testSelectorNamespacedName,
				selector).Return(true, nil).Times(1)
			mockAccManager.EXPECT().RemoveResourceFiltersFromAccount(&testAccountNamespacedName,
				&testSelectorNamespacedName).Return().Times(1)

			err := reconciler.processCreateOrUpdate(selector, &testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
			err = reconciler.processDelete(&testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("CES Add failure", func() {
			mockAccManager.EXPECT().AddResourceFiltersToAccount(&testAccountNamespacedName, &testSelectorNamespacedName,
				selector).Return(false, fmt.Errorf("dummy")).Times(1)
			mockAccManager.EXPECT().RemoveResourceFiltersFromAccount(&testAccountNamespacedName,
				&testSelectorNamespacedName).Return().Times(1)
			err := reconciler.processCreateOrUpdate(selector, &testSelectorNamespacedName)
			Expect(err).Should(HaveOccurred())
		})
		It("CES Delete failure", func() {
			err := reconciler.processDelete(&testSelectorNamespacedName)
			Expect(err).Should(HaveOccurred())
		})

		It("CloudEntitySelector start with no CRs", func() {
			reconciler := &CloudEntitySelectorReconciler{
				Log:    logf.Log,
				Client: fakeClient,
				Scheme: scheme,
			}
			ctrlsync.GetControllerSyncStatusInstance().Configure()
			ctrlsync.GetControllerSyncStatusInstance().SetControllerSyncStatus(ctrlsync.ControllerTypeCPA)
			// Start CES reconciler with 0 CRs, and verify sync status, pendingSyncCount and initialization status.
			err := reconciler.Start(context.TODO())
			Expect(err).Should(BeNil())
			Expect(reconciler.pendingSyncCount).Should(Equal(0))
			Expect(reconciler.initialized).Should(BeTrue())
			val := ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCES)
			Expect(val).Should(BeTrue())
		})
		It("CloudEntitySelector start with one CR", func() {
			testSelectorNamespacedName := types.NamespacedName{Namespace: "namespace01", Name: "selector01"}
			selector := &crdv1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSelectorNamespacedName.Name,
					Namespace: testSelectorNamespacedName.Namespace,
				},
			}
			reconciler := &CloudEntitySelectorReconciler{
				Log:    logf.Log,
				Client: fakeClient,
				Scheme: scheme,
			}
			_ = fakeClient.Create(context.Background(), selector)
			ctrlsync.GetControllerSyncStatusInstance().Configure()
			ctrlsync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(ctrlsync.ControllerTypeCES)
			ctrlsync.GetControllerSyncStatusInstance().SetControllerSyncStatus(ctrlsync.ControllerTypeCPA)
			// Start CES reconciler with one CR and verify sync status, pendingSyncCount and initialization status.
			err := reconciler.Start(context.TODO())
			Expect(err).Should(BeNil())
			Expect(reconciler.pendingSyncCount).Should(Equal(1))
			Expect(reconciler.initialized).Should(BeTrue())
			val := ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCES)
			Expect(val).Should(BeFalse())
		})
		It("CloudEntitySelector set pending sync count to 1", func() {
			ctrlsync.GetControllerSyncStatusInstance().Configure()
			ctrlsync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(ctrlsync.ControllerTypeCES)
			reconciler.pendingSyncCount = 1
			reconciler.updatePendingSyncCountAndStatus()
			val := ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCES)
			Expect(val).Should(BeTrue())
		})
		It("CloudEntitySelector set pending sync count to 2", func() {
			ctrlsync.GetControllerSyncStatusInstance().Configure()
			ctrlsync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(ctrlsync.ControllerTypeCES)
			reconciler.pendingSyncCount = 2
			reconciler.updatePendingSyncCountAndStatus()
			val := ctrlsync.GetControllerSyncStatusInstance().IsControllerSynced(ctrlsync.ControllerTypeCES)
			Expect(val).Should(BeFalse())
		})
	})
})
