// Copyright 2023 Antrea Authors.
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

package accountmanager

import (
	"context"
	"sync"

	. "github.com/onsi/ginkgo/v2"
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
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloud"
	"antrea.io/nephe/pkg/inventory"
	"antrea.io/nephe/pkg/util"
)

var _ = Describe("Account Manager", func() {
	Context("Account manager workflow", func() {
		var (
			credentials               = "credentials"
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace", Name: "secret01"}
			testCesNamespacedName     = types.NamespacedName{Namespace: "namespace01", Name: "Ces01"}
			fakeClient                client.WithWatch
			secret                    *corev1.Secret
			account                   *v1alpha1.CloudProviderAccount
			ces                       *v1alpha1.CloudEntitySelector
			accountManager            *AccountManager
			accountCloudType          runtimev1alpha1.CloudProvider
			pollIntv                  uint
		)

		BeforeEach(func() {
			var err error
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))

			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			cloudInventory := inventory.InitInventory()
			accountManager = &AccountManager{
				Log:       logf.Log,
				Client:    fakeClient,
				Inventory: cloudInventory,
				mutex:     sync.RWMutex{},
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
			ces = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      testCesNamespacedName.Name,
					Namespace: testCesNamespacedName.Namespace,
					OwnerReferences: []v1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/crdv1alpha1",
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
			accountManager.ConfigureAccountManager()
			accountCloudType, err = util.GetAccountProviderType(account)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Add/Remove Account", func() {
			// Secret is not created.
			_, err := accountManager.AddAccount(&testAccountNamespacedName, accountCloudType, account)
			Expect(err).Should(HaveOccurred())

			// Invalid account cloud type.
			_, err = accountManager.AddAccount(&testAccountNamespacedName, "", account)
			Expect(err).Should(HaveOccurred())

			// Valid add account.
			_ = fakeClient.Create(context.Background(), secret)
			_, err = accountManager.AddAccount(&testAccountNamespacedName, accountCloudType, account)
			Expect(err).ShouldNot(HaveOccurred())

			// Verify whether the poller is restarted.
			_, err = accountManager.AddAccount(&testAccountNamespacedName, accountCloudType, account)
			Expect(err).ShouldNot(HaveOccurred())

			// Delete the account.
			err = accountManager.RemoveAccount(&testAccountNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())

		})
		It("Add/Remove Account Poller", func() {
			// Add account poller.
			cloudInterface, err := cloud.GetCloudInterface(accountCloudType)
			Expect(err).ShouldNot(HaveOccurred())
			_, exists := accountManager.addAccountPoller(cloudInterface, &testAccountNamespacedName, account)
			Expect(exists).To(Equal(false))

			// Already account poller exists.
			accPoller, exists := accountManager.addAccountPoller(cloudInterface, &testAccountNamespacedName, account)
			Expect(exists).To(Equal(true))

			accountManager.removeAccountPoller(accPoller)
		})
		It("Add Resource Filters to Account ", func() {
			// Account is not added so throws error.
			_, err := accountManager.AddResourceFiltersToAccount(&testAccountNamespacedName, &testCesNamespacedName,
				ces)
			Expect(err).Should(HaveOccurred())

			// Invalid account namespaced name.
			_, err = accountManager.AddResourceFiltersToAccount(&testSecretNamespacedName, &testCesNamespacedName,
				ces)
			Expect(err).Should(HaveOccurred())

			// Invalid selector.
			_, err = accountManager.AddResourceFiltersToAccount(&testAccountNamespacedName, &testCesNamespacedName,
				nil)
			Expect(err).Should(HaveOccurred())

			// Add a resource filters to account.
			_ = fakeClient.Create(context.Background(), secret)
			_, err1 := accountManager.AddAccount(&testAccountNamespacedName, accountCloudType, account)
			Expect(err1).ShouldNot(HaveOccurred())
			_, err = accountManager.AddResourceFiltersToAccount(&testAccountNamespacedName, &testCesNamespacedName,
				ces)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Remove Resource Filters from Account", func() {
			// Add a resource filters to account.
			_ = fakeClient.Create(context.Background(), secret)
			_, err := accountManager.AddAccount(&testAccountNamespacedName, accountCloudType, account)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = accountManager.AddResourceFiltersToAccount(&testAccountNamespacedName, &testCesNamespacedName,
				ces)
			Expect(err).ShouldNot(HaveOccurred())

			// Remove resource filters from account.
			err = accountManager.RemoveResourceFiltersFromAccount(&testAccountNamespacedName, &testCesNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())

			// Invalid cloud provider type.
			err = accountManager.RemoveResourceFiltersFromAccount(&testSecretNamespacedName, &testCesNamespacedName)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
