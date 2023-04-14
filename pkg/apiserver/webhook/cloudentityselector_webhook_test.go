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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/controllers/sync"
	"antrea.io/nephe/pkg/logging"
	"antrea.io/nephe/pkg/util"
)

var _ = Describe("CloudEntitySelectorWebhook", func() {
	Context("Validation of CES webhook workflow", func() {
		var (
			testAccountNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testAccountNamespacedName2 = types.NamespacedName{Namespace: "namespace01", Name: "account02"}
			testSecretNamespacedName   = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			credentials                = "credentials"
			testAbc                    = "abc"
			testDef                    = "def"
			testXyz                    = "xyz"
			account                    *v1alpha1.CloudProviderAccount
			selector                   *v1alpha1.CloudEntitySelector
			validator                  *CESValidator
			mutator                    *CESMutator
			fakeClient                 k8sclient.WithWatch
			err                        error
			newScheme                  *runtime.Scheme
			pollIntv                   uint = 1
			encodedSelector            []byte
			decoder                    *admission.Decoder
			selectorReq                admission.Request
		)

		BeforeEach(func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
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

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}

			newScheme = runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))
			decoder, err = admission.NewDecoder(newScheme)
			Expect(err).Should(BeNil())
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()

			validator = &CESValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("CloudEntitySelector"),
			}
			err = validator.InjectDecoder(decoder)
			Expect(err).Should(BeNil())

			// set CES sync status.
			sync.GetControllerSyncStatusInstance().Configure()
			sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCES)

			mutator = &CESMutator{
				Client: fakeClient,
				Sh:     newScheme,
				Log:    logging.GetLogger("webhook").WithName("CloudEntitySelector"),
			}
			err = mutator.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
		})

		It("Validate vpcMatch matchName with vmMatch in AWS", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchName: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgUnsupportedVPCMatchName02))
		})

		It("Validate CPA account name update during CES Update", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			oldSelector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}
			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName2.Name,
					Namespace: testAccountNamespacedName2.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account02",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName2.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testDef,
							},
						},
					},
				},
			}
			encodedOldSelector, _ := json.Marshal(oldSelector)
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgAccountNameUpdate))
		})

		It("Validate vpcMatch matchName with Agented = true in AWS", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchName: testAbc,
							},
							Agented: true,
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgUnsupportedAgented))
		})

		It("Validate vpcMatch matchName in Azure", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchName: testAbc,
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgUnsupportedVPCMatchName01))
		})

		It("Validate same vpcMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVPCMatchID))
		})
		It("Validate same vmMatch matchID(with vpcMatch in one) in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMMatchID))
		})
		It("Validate same vmMatch matchID(with vpcMatch in 2nd) in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMMatchID))
		})
		It("Validate same vpcMatch matchID and vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
					},
				},
			}

			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMAndVPCMatch))
		})
		It("Validate same vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testAbc,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testAbc,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMMatchID))
		})
		It("Validate different vmMatch matchID in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testXyz,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate same vpcMatch matchID and vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMAndVPCMatch))
		})
		It("Validate different vpcMatch matchID and same vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testXyz,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})

		It("Validate same vmMatch matchName(vpcMatch matchID in one) in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate same vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testAbc,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testAbc,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameVMMatchName))
		})
		It("Validate different vmMatch matchName in two vmSelectors", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testDef,
								},
							},
						},
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchName: testXyz,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Mutator Workflow when owner account is not found", func() {
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := mutator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})
		It("Mutator workflow to convert configured vmSelectors to lowercase", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID: testDef,
								},
								{
									MatchName: testXyz,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := mutator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Mutator workflow when cloud type is missing in Account", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
				},
			}
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := mutator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})
		It("Mutator workflow with CES decode error", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object:    runtime.RawExtension{},
				},
			}

			response := mutator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})
		It("Validate CES create with decode error", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object:    runtime.RawExtension{},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))

		})
		It("Validate CES with already existing CES with same owner account", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			err = fakeClient.Create(context.Background(), selector)
			Expect(err).Should(BeNil())
			newSelector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}

			encodedSelector, _ = json.Marshal(newSelector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}
			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgSameAccountUsage))
		})
		It("Validate update during CES Update with vpcMatch matchID matchName in same selector", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			oldSelector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testAbc,
							},
						},
					},
				},
			}
			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID:   testDef,
								MatchName: testXyz,
							},
						},
					},
				},
			}
			encodedOldSelector, _ := json.Marshal(oldSelector)
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgMatchIDNameTogether))
		})
		It("Validate CES update", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			oldSelector := &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testXyz,
							},
						},
					},
				},
			}

			encodedOldSelector, _ := json.Marshal(oldSelector)
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate CES update with unmarshall error for old CES", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			encodedOldSelector := []byte("dummy")
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})
		It("Validate CES update with decode error", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			encodedOldSelector := []byte("dummy")
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object:    runtime.RawExtension{},
					OldObject: runtime.RawExtension{
						Raw: encodedOldSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})
		It("Validate vmMatch matchID matchName in same selector", func() {
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "crd.cloud.antrea.io/v1alpha1",
							Kind:       "CloudProviderAccount",
							Name:       "account01",
						},
					},
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VMMatch: []v1alpha1.EntityMatch{
								{
									MatchID:   testDef,
									MatchName: testXyz,
								},
							},
						},
					},
				},
			}
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}

			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(errorMsgMatchIDNameTogether))
		})
		It("Validate unknown cloud provider", func() {
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{},
			}
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())

			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}
			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(util.ErrorMsgUnknownCloudProvider))
		})
		It("Validate when owner account not found", func() {
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}
			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring("\"account01\" not found"))
		})
		It("Validate invalid admission request", func() {
			encodedSelector, _ = json.Marshal(selector)
			selectorReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudEntitySelector",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudEntitySelectors",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Connect,
					Object: runtime.RawExtension{
						Raw: encodedSelector,
					},
				},
			}
			response := validator.Handle(context.Background(), selectorReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgInvalidRequest))
		})
		Context("Validate controller is not initialized", func() {
			BeforeEach(func() {
				selector = &v1alpha1.CloudEntitySelector{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "crd.cloud.antrea.io/v1alpha1",
								Kind:       "CloudProviderAccount",
								Name:       "account01",
							},
						},
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName: testAccountNamespacedName.Name,
						VMSelector: []v1alpha1.VirtualMachineSelector{
							{
								VpcMatch: &v1alpha1.EntityMatch{
									MatchName: testAbc,
								},
								VMMatch: []v1alpha1.EntityMatch{
									{
										MatchID: testDef,
									},
								},
							},
						},
					},
				}
				encodedSelector, _ = json.Marshal(selector)
				selectorReq = admission.Request{
					AdmissionRequest: v1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1alpha1",
							Kind:    "CloudEntitySelector",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "",
							Version:  "v1alpha1",
							Resource: "CloudEntitySelectors",
						},
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
						Operation: v1.Create,
						Object: runtime.RawExtension{
							Raw: encodedSelector,
						},
					},
				}

			})
			It("Validate create CES CR failure", func() {
				sync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(sync.ControllerTypeCES)
				response := validator.Handle(context.Background(), selectorReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(sync.ErrorMsgControllerInitializing))
			})
			It("Validate update CES CR failure", func() {
				oldSelector := &v1alpha1.CloudEntitySelector{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "crd.cloud.antrea.io/v1alpha1",
								Kind:       "CloudProviderAccount",
								Name:       "account01",
							},
						},
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName: testAccountNamespacedName.Name,
						VMSelector: []v1alpha1.VirtualMachineSelector{
							{
								VpcMatch: &v1alpha1.EntityMatch{
									MatchName: testAbc,
								},
								VMMatch: []v1alpha1.EntityMatch{
									{
										MatchID: testDef,
									},
								},
							},
						},
					},
				}
				oldEncodedSelector, _ := json.Marshal(oldSelector)

				selectorReq.Operation = v1.Update
				selectorReq.Object = runtime.RawExtension{
					Raw: encodedSelector,
				}
				selectorReq.OldObject = runtime.RawExtension{
					Raw: oldEncodedSelector,
				}

				sync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(sync.ControllerTypeCES)
				response := validator.Handle(context.Background(), selectorReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(sync.ErrorMsgControllerInitializing))
			})
			It("Validate delete CES CR failure", func() {
				selectorReq.Operation = v1.Delete

				sync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(sync.ControllerTypeCES)
				response := validator.Handle(context.Background(), selectorReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(sync.ErrorMsgControllerInitializing))
			})
		})
	})
})
