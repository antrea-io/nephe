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

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("CloudEntitySelectorWebhook", func() {
	Context("Validation of CES configuration", func() {
		var (
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			credentials               = "credentials"
			account                   *CloudProviderAccount
			selector                  *CloudEntitySelector
			fakeClient                k8sclient.WithWatch
			err                       error
			newScheme                 *runtime.Scheme
			pollIntv                  uint = 1
		)

		BeforeEach(func() {
			account = &CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &CloudProviderAccountAWSConfig{
						Region: "us-east-1",
						SecretRef: &SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}

			newScheme = runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(AddToScheme(newScheme))
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			client = fakeClient
		})
		It("Validate vpcMatch matchName with vmMatch in AWS", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchName: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
					},
				},
			}

			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("vpc matchName with either vm matchID" +
				" or vm matchName is not supported"))
		})
		It("Validate vpcMatch matchName with Agented = true in AWS", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchName: "abc",
							},
							Agented: true,
						},
					},
				},
			}

			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("vpc matchName with agented flag set to true" +
				" is not supported"))
		})
		It("Validate vpcMatch matchName in Azure", func() {
			account = &CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchName: "abc",
							},
						},
					},
				},
			}
			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("matchName is not supported in vpcMatch"))
		})

		It("Validate same vpcMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vpcMatch matchID abc configured in two match selectors"))
		})
		It("Validate same vmMatch matchID(with vpcMatch in one) in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vmMatch matchID def configured in" +
				" two match selectors"))
		})
		It("Validate same vpcMatch matchID and vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vpcMatch matchID abc and vmMatch matchID def" +
				" configured in two match selectors"))
		})
		It("Validate same vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VMMatch: []EntityMatch{
								{
									MatchID: "abc",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: "abc",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vmMatch matchID abc configured in two" +
				" match selectors"))
		})
		It("Validate different vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VMMatch: []EntityMatch{
								{
									MatchID: "def",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: "xyz",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Validate same vpcMatch matchID and vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vpcMatch matchID abc and vmMatch matchName def" +
				" configured"))
		})
		It("Validate different vpcMatch matchID and same vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: "xyz",
							},
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Validate same vmMatch matchName(vpcMatch matchID in one) in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VpcMatch: &EntityMatch{
								MatchID: "abc",
							},
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Validate same vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VMMatch: []EntityMatch{
								{
									MatchName: "abc",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: "abc",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("same vmMatch matchName abc configured in two match selectors"))
		})
		It("Validate different vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []VirtualMachineSelector{
						{
							VMMatch: []EntityMatch{
								{
									MatchName: "def",
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: "xyz",
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
