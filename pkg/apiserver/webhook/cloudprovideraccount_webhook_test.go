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
	corev1 "k8s.io/api/core/v1"
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

var _ = Describe("CloudProviderAccountWebhook", func() {
	Context("Validation of CPA webhook workflow", func() {
		var (
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			testSecretNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			credential                = `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			credentials               = "credentials"
			awsAccount                *v1alpha1.CloudProviderAccount
			azureAccount              *v1alpha1.CloudProviderAccount
			account                   *v1alpha1.CloudProviderAccount
			s1                        *corev1.Secret
			validator                 *CPAValidator
			mutator                   *CPAMutator
			fakeClient                k8sclient.WithWatch
			err                       error
			newScheme                 *runtime.Scheme
			pollIntv                  uint = 60
			encodedAccount            []byte
			decoder                   *admission.Decoder
			accountReq                admission.Request
		)
		BeforeEach(func() {
			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
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

			azureAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: []string{"us-east-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}

			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}

			newScheme = runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))
			decoder, err = admission.NewDecoder(newScheme)
			Expect(err).Should(BeNil())
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()

			validator = &CPAValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("CloudProviderAccount"),
			}
			err = validator.InjectDecoder(decoder)
			Expect(err).Should(BeNil())

			// set CPA sync status.
			sync.GetControllerSyncStatusInstance().Configure()
			sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCPA)

			mutator = &CPAMutator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("CloudProviderAccount"),
			}
			err = mutator.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
		})
		It("Validate mutator workflow", func() {
			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
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

			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := mutator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate mutator workflow with account decode error", func() {
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
				},
			}

			response := mutator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
		})
		It("Validate unknown cloud provider", func() {
			account := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{},
			}
			encodedAccount, _ = json.Marshal(account)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.String()).Should(ContainSubstring(util.ErrorMsgUnknownCloudProvider))
		})
		It("Validate when AWS secret not configured", func() {
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgSecretNotConfigured))
		})
		It("Validate AWS poll Interval less than 30 seconds", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			var pollInterval uint = 1
			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
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
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMinPollInterval))
		})
		It("Validate missing field in AWS Secret credential", func() {
			cred := `{"accessKeySecret": "keySecret"}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingCredential))
		})
		It("Validate missing region in AWS", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingRegion))
		})
		It("Validate invalid region in AWS", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-xxx"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgInvalidRegion))
		})
		It("Validate AWS Access and Secret Key with Session Token", func() {
			cred := `{"accessKeyId": "keyId", "accessKeySecret": "keySecret", "sessionToken": "token"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-west-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate AWS RoleARN based credential", func() {
			cred := `{"roleArn": "roleArnId", "externalId": "externalId"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-west-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate AWS RoleARN with Access and Secret Key", func() {
			cred := `{"accessKeyId": "keyId","accessKeySecret": "keySecret", "roleArn": "roleArnId", "externalId": "externalId"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			awsAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-west-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate AWS account add with decode error", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object:    runtime.RawExtension{},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
		})
		It("Validate AWS secret unmarshall error", func() {
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(""),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgJsonUnmarshalFail))
		})
		It("Validate when Azure secret not configured", func() {
			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgSecretNotConfigured))
		})
		It("Validate an Azure Account add", func() {
			cred := `{"subscriptionId": "SubID","clientId": "ClientID","tenantId": "TenantID", "clientKey": "ClientKey"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate Azure secret unmarshall error", func() {
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte("dummy"),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgJsonUnmarshalFail))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
		})
		It("Validate missing Azure subscriptionId", func() {
			cred := `{"clientId": "ClientID","tenantId": "TenantID", "clientKey": "ClientKey"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingSubscritionID))
		})
		It("Validate missing Azure clientID", func() {
			cred := `{"subscriptionId": "SubID","tenantId": "TenantID", "clientKey": "ClientKey"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingClientDetails))
		})
		It("Validate missing Azure TenantID", func() {
			cred := `{"subscriptionId": "SubID", "clientId": "ClientID", "clientKey": "ClientKey"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingTenantID))
		})
		It("Validate webhook update", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(awsAccount)

			oldAccount := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-east-2"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedOldAccount, _ := json.Marshal(oldAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeTrue())
		})
		It("Validate webhook update with decode error", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			oldAccount := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-east-2"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedOldAccount, _ := json.Marshal(oldAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object:    runtime.RawExtension{},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
		})
		It("Validate webhook update with unmarshal error for old account", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			encodedAccount, _ = json.Marshal(azureAccount)

			encodedOldAccount := []byte("dummy")
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
		})
		It("Validate missing Azure region", func() {
			cred := `{"subscriptionId": "SubID","clientId": "ClientID","tenantId": "TenantID", "clientKey": "ClientKey"}`
			s1 := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName.Name,
					Namespace: testSecretNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(cred),
				},
			}
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			azureAccount = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedAccount, _ = json.Marshal(azureAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMissingRegion))
		})
		It("Validate AWS missing secret in webhook update", func() {
			encodedAccount, _ = json.Marshal(awsAccount)

			oldAccount := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{"us-east-2"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedOldAccount, _ := json.Marshal(oldAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgSecretNotConfigured))
		})
		It("Validate invalid poll interval in webhook update", func() {
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())

			var pollInterval uint = 1
			newAccount := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollInterval,
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
			encodedAccount, _ = json.Marshal(newAccount)

			encodedOldAccount, _ := json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}
			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgMinPollInterval))
		})
		It("Validate Azure missing secret in webhook update", func() {
			encodedAccount, _ = json.Marshal(azureAccount)

			oldAccount := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: []string{"us-east-1"},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName.Name,
							Namespace: testSecretNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			encodedOldAccount, _ := json.Marshal(oldAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Update,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
					OldObject: runtime.RawExtension{
						Raw: encodedOldAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgSecretNotConfigured))
		})
		It("Validate invalid admission request", func() {
			encodedAccount, _ = json.Marshal(awsAccount)
			accountReq = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "v1alpha1",
						Kind:    "CloudProviderAccount",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1alpha1",
						Resource: "CloudProviderAccounts",
					},
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
					Operation: v1.Connect,
					Object: runtime.RawExtension{
						Raw: encodedAccount,
					},
				},
			}

			response := validator.Handle(context.Background(), accountReq)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.AdmissionResponse.Allowed).To(BeFalse())
			Expect(response.AdmissionResponse.String()).Should(ContainSubstring(errorMsgInvalidRequest))
		})

		Context("Validate controller is not initialized", func() {
			BeforeEach(func() {
				account = &v1alpha1.CloudProviderAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Spec: v1alpha1.CloudProviderAccountSpec{},
				}
				encodedAccount, _ = json.Marshal(account)
				accountReq = admission.Request{
					AdmissionRequest: v1.AdmissionRequest{
						Kind: metav1.GroupVersionKind{
							Group:   "",
							Version: "v1alpha1",
							Kind:    "CloudProviderAccount",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "",
							Version:  "v1alpha1",
							Resource: "CloudProviderAccounts",
						},
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
						Operation: v1.Create,
						Object: runtime.RawExtension{
							Raw: encodedAccount,
						},
					},
				}
				sync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(sync.ControllerTypeCPA)
			})

			It("Validate create CPA CR failure", func() {
				response := validator.Handle(context.Background(), accountReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(sync.ErrorMsgControllerInitializing))
			})
			It("Validate update CPA CR failure", func() {
				err = fakeClient.Create(context.Background(), s1)
				Expect(err).Should(BeNil())
				oldAccount := &v1alpha1.CloudProviderAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Spec: v1alpha1.CloudProviderAccountSpec{
						PollIntervalInSeconds: &pollIntv,
						AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
							Region: []string{"us-east-2"},
							SecretRef: &v1alpha1.SecretReference{
								Name:      testSecretNamespacedName.Name,
								Namespace: testSecretNamespacedName.Namespace,
								Key:       credentials,
							},
						},
					},
				}
				encodedOldAccount, _ := json.Marshal(oldAccount)
				accountReq.Operation = v1.Update
				accountReq.Object = runtime.RawExtension{
					Raw: encodedAccount,
				}
				accountReq.OldObject = runtime.RawExtension{
					Raw: encodedOldAccount,
				}

				response := validator.Handle(context.Background(), accountReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(sync.ErrorMsgControllerInitializing))
			})
			It("Validate delete CPA CR failure", func() {
				accountReq.Operation = v1.Delete
				response := validator.Handle(context.Background(), accountReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(fmt.Sprintf("%s %s",
					sync.ControllerTypeCPA.String(), sync.ErrorMsgControllerInitializing)))
				// CPA sync done, CES is not synced.
				sync.GetControllerSyncStatusInstance().SetControllerSyncStatus(sync.ControllerTypeCPA)
				sync.GetControllerSyncStatusInstance().ResetControllerSyncStatus(sync.ControllerTypeCES)
				response = validator.Handle(context.Background(), accountReq)
				_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
				Expect(response.AdmissionResponse.Allowed).To(BeFalse())
				Expect(response.String()).Should(ContainSubstring(fmt.Sprintf("%s %s",
					sync.ControllerTypeCES.String(), sync.ErrorMsgControllerInitializing)))
			})
		})
	})
})
