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

package cloudprovideraccount

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	fakewatch "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	antreanetworking "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	"antrea.io/nephe/apis/crd/v1alpha1"
	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	mockaccmanager "antrea.io/nephe/pkg/testing/accountmanager"
	mocknpcontroller "antrea.io/nephe/pkg/testing/networkpolicy"
	"antrea.io/nephe/pkg/util"
)

var (
	mockCtrl         *mock.Controller
	mockAccManager   *mockaccmanager.MockInterface
	mockNpController *mocknpcontroller.MockNetworkPolicyController
	scheme           = runtime.NewScheme()
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
	RunSpecs(t, "CloudProviderAccount controller")
}

var _ = Describe("CloudProviderAccount Controller", func() {
	Context("CPA workflow", func() {
		var (
			credentials               = "credentials"
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace02", Name: "secret01"}
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
			mockAccManager = mockaccmanager.NewMockInterface(mockCtrl)
			mockNpController = mocknpcontroller.NewMockNetworkPolicyController(mockCtrl)
			reconciler = &CloudProviderAccountReconciler{
				Log:          logf.Log,
				Client:       fakeClient,
				Scheme:       scheme,
				mutex:        sync.Mutex{},
				AccManager:   mockAccManager,
				NpController: mockNpController,
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
						Region: []string{"us-east-1"},
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
		It("Account add with unknown cloud type", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{},
			}
			err := reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err.Error()).Should(ContainSubstring(util.ErrorMsgUnknownCloudProvider))
		})
		It("Account add and delete workflow", func() {
			accountCloudType, err := util.GetAccountProviderType(account)
			Expect(err).ShouldNot(HaveOccurred())
			mockAccManager.EXPECT().AddAccount(&testAccountNamespacedName, accountCloudType, account).Return(false, nil).Times(1)
			mockAccManager.EXPECT().RemoveAccount(&testAccountNamespacedName).Return(nil).Times(1)
			deletedCpa := &crdv1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{Name: testAccountNamespacedName.Name, Namespace: testAccountNamespacedName.Namespace},
			}
			mockNpController.EXPECT().LocalEvent(watch.Event{Type: watch.Deleted, Object: deletedCpa}).Times(1)
			err = reconciler.processCreateOrUpdate(&testAccountNamespacedName, account)
			Expect(err).ShouldNot(HaveOccurred())

			selector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      "selector01",
					Namespace: testSecretNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName:      testAccountNamespacedName.Name,
					AccountNamespace: testAccountNamespacedName.Namespace,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: "xyzq",
							},
						},
					},
				},
			}
			_ = fakeClient.Create(context.Background(), selector)

			err = reconciler.processDelete(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("Secret watcher", func() {
			var (
				accountCloudType runtimev1alpha1.CloudProvider
				err              error
			)
			BeforeEach(func() {
				reconciler.clientset = fakewatch.NewSimpleClientset()
				err = os.Setenv("POD_NAMESPACE", testSecretNamespacedName.Namespace)
				Expect(err).ShouldNot(HaveOccurred())
				go func() {
					reconciler.setupSecretWatcher()
				}()
				accountCloudType, err = util.GetAccountProviderType(account)
				Expect(err).ShouldNot(HaveOccurred())
			})
			// Create a Secret using both fakeClient and clientset, since Secret watcher uses clientset
			// while reconciler using client from controller run-time. Both Secret watcher and reconciler
			// should be able to retrieve the Secret object.
			It("Update", func() {
				mockAccManager.EXPECT().AddAccount(&testAccountNamespacedName, accountCloudType, account).Return(false, nil).Times(3)

				By("Add the Secret")
				fmt.Println("Creating secret")
				_ = fakeClient.Create(context.Background(), secret)
				// Create CPA.
				_ = fakeClient.Create(context.Background(), account)
				time.Sleep(1 * time.Second)
				_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Create(context.Background(), secret, v1.CreateOptions{})
				time.Sleep(1 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())

				By("Update the Secret with invalid credentials")
				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testSecretNamespacedName.Name,
						Namespace: testSecretNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						credentials: []byte("credentialg"),
					},
				}
				_ = fakeClient.Update(context.Background(), secret)
				_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Update(context.Background(), secret, v1.UpdateOptions{})
				time.Sleep(1 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())

				By("Update the Secret with valid credentials")
				credential := `{"accessKeyId": "keyId","accessKeySecret": "Secret"}`
				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testSecretNamespacedName.Name,
						Namespace: testSecretNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						credentials: []byte(credential),
					},
				}
				_, err := reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Update(context.Background(), secret, v1.UpdateOptions{})
				time.Sleep(1 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())

			})
			It("Delete", func() {
				mockAccManager.EXPECT().AddAccount(&testAccountNamespacedName, accountCloudType, account).
					Return(false, nil).Times(1)
				By("Add the Secret")
				_ = fakeClient.Create(context.Background(), secret)
				// Create CPA.
				_ = fakeClient.Create(context.Background(), account)
				time.Sleep(1 * time.Second)
				_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Create(context.Background(), secret, v1.CreateOptions{})
				time.Sleep(1 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())

				By("Delete the Secret")
				mockAccManager.EXPECT().AddAccount(&testAccountNamespacedName, accountCloudType, account).
					Return(false, fmt.Errorf(util.ErrorMsgSecretReference)).Times(1)
				time.Sleep(1 * time.Second)
				_ = fakeClient.Delete(context.Background(), secret)

				err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Delete(context.Background(), testSecretNamespacedName.Name, v1.DeleteOptions{})
				time.Sleep(1 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())

				temp := &crdv1alpha1.CloudProviderAccount{}
				err := fakeClient.Get(context.Background(), testAccountNamespacedName, temp)
				Expect(err).ShouldNot(HaveOccurred())
				if strings.Compare(temp.Status.Error, util.ErrorMsgSecretReference) != 0 {
					Fail("CPA is not updated with error message")
				}
			})
			It("Add for AzureAccount", func() {
				credential := `{"subscriptionId": "subId", "clientId": "clientId", "tenantId": "tenantId", "clientKey": "clientKey"}`
				secret.Data = map[string][]byte{credentials: []byte(credential)}
				account.Spec.AWSConfig = nil
				account.Spec.AzureConfig = &v1alpha1.CloudProviderAccountAzureConfig{
					Region: []string{"us-east-1"},
					SecretRef: &v1alpha1.SecretReference{
						Name:      testSecretNamespacedName.Name,
						Namespace: testSecretNamespacedName.Namespace,
						Key:       credentials,
					},
				}
				accountCloudType, err = util.GetAccountProviderType(account)
				mockAccManager.EXPECT().AddAccount(&testAccountNamespacedName, accountCloudType, account).Return(false, nil).Times(1)
				_ = fakeClient.Create(context.Background(), secret)
				_ = fakeClient.Create(context.Background(), account)
				time.Sleep(1 * time.Second)

				_, err = reconciler.clientset.CoreV1().Secrets(testSecretNamespacedName.Namespace).
					Create(context.Background(), secret, v1.CreateOptions{})
				time.Sleep(2 * time.Second)
				Expect(err).ShouldNot(HaveOccurred())
			})
		})
	})
})
