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

package cloud

import (
	"context"
	"sync"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var _ = Describe("CloudEntitySelector Controller", func() {
	Context("CES workflow", func() {
		var (
			credentials                = "credentials"
			testAccountNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSelectorNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "selector01"}
			testSecretNamespacedName   = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			account                    *v1alpha1.CloudProviderAccount
			selector                   *v1alpha1.CloudEntitySelector
			cesReconciler              *CloudEntitySelectorReconciler
			cpaReconciler              *CloudProviderAccountReconciler
			secret                     *corev1.Secret
			fakeClient                 client.WithWatch
		)
		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			mockCtrl = mock.NewController(GinkgoT())
			mockClient = controllerruntimeclient.NewMockClient(mockCtrl)
			Poller := InitPollers()
			cpaReconciler = &CloudProviderAccountReconciler{
				Log:                 logf.Log,
				Client:              fakeClient,
				Scheme:              scheme,
				mutex:               sync.Mutex{},
				accountProviderType: make(map[types.NamespacedName]common.ProviderType),
				Inventory:           inventory.InitInventory(),
				Poller:              Poller,
			}
			cesReconciler = &CloudEntitySelectorReconciler{
				Log:                  logf.Log,
				Client:               fakeClient,
				Scheme:               scheme,
				selectorToAccountMap: make(map[types.NamespacedName]types.NamespacedName),
				Poller:               Poller,
			}

			var pollIntv uint = 1
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}

			credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSelectorNamespacedName.Name,
					Namespace: testSelectorNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       testAccountNamespacedName.Name,
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: "xyzq",
							},
						},
					},
				},
			}
		})
		It("CES Add and Delete workflow", func() {
			_ = fakeClient.Create(context.Background(), secret)

			_ = fakeClient.Create(context.Background(), account)

			err := cpaReconciler.processCreate(&testAccountNamespacedName, account)
			Expect(err).ShouldNot(HaveOccurred())

			err = cesReconciler.processCreateOrUpdate(selector, &testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())

			err = cesReconciler.processDelete(&testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())

			err = cpaReconciler.processDelete(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("CES add before CPA add", func() {
			_ = fakeClient.Create(context.Background(), secret)
			err := cesReconciler.processCreateOrUpdate(selector, &testSelectorNamespacedName)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSelectorAddFail))
		})

		It("CPA delete before CES delete", func() {
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)
			err := cpaReconciler.processCreate(&testAccountNamespacedName, account)
			Expect(err).ShouldNot(HaveOccurred())

			err = cesReconciler.processCreateOrUpdate(selector, &testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
			_ = fakeClient.Create(context.Background(), selector)

			err = cpaReconciler.processDelete(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = cpaReconciler.Poller.getCloudType(&testAccountNamespacedName)
			Expect(err.Error()).Should(ContainSubstring(errorMsgAccountPollerNotFound))

			err = cesReconciler.processDelete(&testSelectorNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())

			err = cesReconciler.processDelete(&testSelectorNamespacedName)
			Expect(err.Error()).Should(ContainSubstring(errorMsgSelectorAccountMapNotFound))
		})
	})
})
