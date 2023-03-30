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
	"os"
	"sync"
	"time"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	fakewatch "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	cloudprovider "antrea.io/nephe/pkg/cloud-provider"
	"antrea.io/nephe/pkg/cloud-provider/cloudapi/common"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/controllers/utils"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var _ = Describe("CloudProviderAccount Controller", func() {
	Context("CPA workflow", func() {
		var (
			credentials               = "credentials"
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			account                   *v1alpha1.CloudProviderAccount
			reconciler                *CloudProviderAccountReconciler
			secret                    *corev1.Secret
			fakeClient                client.WithWatch
			pollIntv                  uint
		)

		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			mockCtrl = mock.NewController(GinkgoT())
			mockClient = controllerruntimeclient.NewMockClient(mockCtrl)
			reconciler = &CloudProviderAccountReconciler{
				Log:                 logf.Log,
				Client:              fakeClient,
				Scheme:              scheme,
				mutex:               sync.Mutex{},
				accountProviderType: make(map[types.NamespacedName]common.ProviderType),
				Inventory:           inventory.InitInventory(),
				Poller:              InitPollers(),
			}

			pollIntv = 1
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
		})
		It("Account Add and Delete workflow", func() {
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			err := reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err).ShouldNot(HaveOccurred())

			provider, err := reconciler.Poller.getCloudType(&testAccountNamespacedName)
			Expect(provider).To(Not(BeNil()))
			Expect(err).To(BeNil())

			err = reconciler.processDelete(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = reconciler.Poller.getCloudType(&testAccountNamespacedName)
			Expect(err.Error()).Should(ContainSubstring(errorMsgAccountPollerNotFound))
		})
		It("Account add with unknown cloud type", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{},
			}
			_ = fakeClient.Create(context.Background(), secret)

			err := reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err.Error()).Should(ContainSubstring(utils.ErrorMsgUnknownCloudProvider))
		})
		It("Account create error due to no Secret", func() {
			err := reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err).Should(HaveOccurred())
		})
		It("Account delete error due to no account config entry", func() {
			_ = fakeClient.Create(context.Background(), secret)

			err := reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err).ShouldNot(HaveOccurred())

			cloudType := reconciler.getAccountProviderType(&testAccountNamespacedName)
			cloudInterface, err := cloudprovider.GetCloudInterface(cloudType)
			Expect(err).ShouldNot(HaveOccurred())
			cloudInterface.RemoveProviderAccount(&testAccountNamespacedName)

			err = reconciler.processDelete(&testAccountNamespacedName)
			Expect(err).Should(HaveOccurred())
		})
		It("Secret watcher", func() {
			var err error
			reconciler.clientset = fakewatch.NewSimpleClientset()
			credential := `{"accessKeyId": "keyId","accessKeySecret": "Secret"}`
			err = os.Setenv("POD_NAMESPACE", testSecretNamespacedName.Namespace)
			Expect(err).ShouldNot(HaveOccurred())
			// Create a secret using both fakeClient and clientset, since secret watcher uses cleintset
			// while reconciler using client from controller run-time. Both secret watcher and reconciler
			// should be able to retrieve the secret object.
			_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
				Create(context.Background(), secret, v1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			_ = fakeClient.Create(context.Background(), secret)
			// Create CPA.
			_ = fakeClient.Create(context.Background(), account)
			go func() {
				reconciler.setupSecretWatcher()
			}()
			time.Sleep(time.Second * 10)
			// Update the valid secret credentials.
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}
			_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
				Update(context.Background(), secret, v1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			// Update the invalid secret credentials.
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte("credentialg"),
				},
			}
			_ = fakeClient.Update(context.Background(), secret)
			_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
				Update(context.Background(), secret, v1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
