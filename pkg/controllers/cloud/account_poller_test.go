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
	"antrea.io/nephe/pkg/controllers/utils"
)

var _ = Describe("Account poller", func() {
	Context("Account poller workflow", func() {
		var (
			credentials                = "credentials"
			testAccountNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName   = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			testSelectorNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "selector01"}
			account                    *v1alpha1.CloudProviderAccount
			selector                   *v1alpha1.CloudEntitySelector
			reconciler                 *CloudProviderAccountReconciler
			secret                     *corev1.Secret
			fakeClient                 client.WithWatch
		)

		BeforeEach(func() {
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			Poller := InitPollers()
			reconciler = &CloudProviderAccountReconciler{
				Log:                 logf.Log,
				Client:              fakeClient,
				Scheme:              scheme,
				mutex:               sync.Mutex{},
				accountProviderType: make(map[types.NamespacedName]common.ProviderType),
				Inventory:           inventory.InitInventory(),
				Poller:              Poller,
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
		It("Account poller Add, Update and Delete workflow", func() {
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			accountCloudType, err := utils.GetAccountProviderType(account)
			Expect(err).ShouldNot(HaveOccurred())
			accPoller, exists := reconciler.Poller.addAccountPoller(accountCloudType, &testAccountNamespacedName, account, reconciler)
			Expect(accPoller).To(Not(BeNil()))
			Expect(exists).To(BeFalse())

			provider, err := reconciler.Poller.getCloudType(&testAccountNamespacedName)
			Expect(provider).To(Not(BeNil()))
			Expect(err).To(BeNil())

			err = reconciler.Poller.updateAccountPoller(&testAccountNamespacedName, selector)
			Expect(err).To(BeNil())

			err = reconciler.Poller.removeAccountPoller(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = reconciler.Poller.getCloudType(&testAccountNamespacedName)
			Expect(err.Error()).Should(ContainSubstring(errorMsgAccountPollerNotFound))
		})
		It("Account poller re-add", func() {
			_ = fakeClient.Create(context.Background(), secret)
			_ = fakeClient.Create(context.Background(), account)

			accountCloudType, err := utils.GetAccountProviderType(account)
			Expect(err).ShouldNot(HaveOccurred())
			accPoller, exists := reconciler.Poller.addAccountPoller(accountCloudType, &testAccountNamespacedName, account, reconciler)
			Expect(accPoller).To(Not(BeNil()))
			Expect(exists).To(BeFalse())

			accPoller, exists = reconciler.Poller.addAccountPoller(accountCloudType, &testAccountNamespacedName, account, reconciler)
			Expect(accPoller).To(Not(BeNil()))
			Expect(exists).To(BeTrue())
		})
	})
})
