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
			testAbc                   = "abc"
			testDef                   = "def"
			testXyz                   = "xyz"
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
								MatchName: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}

			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgUnsupportedVPCMatchName02))
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
								MatchName: testAbc,
							},
							Agented: true,
						},
					},
				},
			}

			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgUnsupportedAgented))
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
								MatchName: testAbc,
							},
						},
					},
				},
			}
			err = selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgUnsupportedVPCMatchName01))
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
								MatchID: testAbc,
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVPCMatchID))
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
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVMMatchID))
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
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVMAndVPCMatch))
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
									MatchID: testAbc,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: testAbc,
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVMMatchID))
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
									MatchID: testDef,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchID: testXyz,
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
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVMAndVPCMatch))
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
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VpcMatch: &EntityMatch{
								MatchID: testXyz,
							},
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
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
								MatchID: testAbc,
							},
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: testDef,
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
									MatchName: testAbc,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: testAbc,
								},
							},
						},
					},
				},
			}
			err := selector.validateMatchSections()
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring(errorMsgSameVMMatchName))
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
									MatchName: testDef,
								},
							},
						},
						{
							VMMatch: []EntityMatch{
								{
									MatchName: testXyz,
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
