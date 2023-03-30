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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/logging"
)

var _ = Describe("Webhook", func() {
	Context("Running Webhook tests for Secret", func() {
		var (
			testSecretNamespacedName1 = types.NamespacedName{Namespace: "namespace01", Name: "secret01"}
			testSecretNamespacedName2 = types.NamespacedName{Namespace: "namespace02", Name: "secret02"}
			testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
			credential                = `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			credentials               = "credentials"
			invalidReqErrorMsg        = "invalid admission webhook request"
			account                   *v1alpha1.CloudProviderAccount
			s1                        *corev1.Secret
			encodedS1                 []byte
			decoder                   *admission.Decoder
			s1Req                     admission.Request
			fakeClient                client.WithWatch
			err                       error
		)

		BeforeEach(func() {
			credential = `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ = json.Marshal(s1)
			s1Req = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}

			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))
			decoder, err = admission.NewDecoder(newScheme)
			Expect(err).Should(BeNil())
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
		})

		It("Validate Secret create", func() {
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), s1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})

		// The below set of tests validate Secret update/delete for AWS config.
		It("Validate Secret delete with dependent AWS CPA", func() {
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			temp := &corev1.Secret{}
			err = fakeClient.Get(context.TODO(), testSecretNamespacedName1, temp)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
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
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())
		})
		It("Validate Secret delete without dependent AWS CPA", func() {
			credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ := json.Marshal(s1)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
			account := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})
		// The below set of tests validate Secret update/delete for Azure config.
		It("Validate Azure Secret delete with dependent Azure CPA", func() {
			credential = `{"subscriptionId": "SubID",
				"clientId": "ClientID",
				"tenantId": "TenantID,
				"clientKey": "ClientKey"
			}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ := json.Marshal(s1)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			temp := &corev1.Secret{}
			err = fakeClient.Get(context.TODO(), testSecretNamespacedName1, temp)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())
		})
		It("Validate Secret delete without dependent AWS CPA", func() {
			credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ := json.Marshal(s1)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
			account := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})

		// The below set of tests validate Secret update/delete for Azure config.
		It("Validate Azure Secret delete with dependent Azure CPA", func() {
			credential = `{"subscriptionId": "SubID",
				"clientId": "ClientID",
				"tenantId": "TenantID,
				"clientKey": "ClientKey"
			}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ := json.Marshal(s1)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			temp := &corev1.Secret{}
			err = fakeClient.Get(context.TODO(), testSecretNamespacedName1, temp)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())
		})

		It("Validate Secret delete without dependent Azure CPA", func() {
			credential = `{"subscriptionId": "SubID",
				"clientId": "ClientID",
				"tenantId": "TenantID,
				"clientKey": "ClientKey"
			}`
			s1 = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
				},
				Data: map[string][]byte{
					credentials: []byte(credential),
				},
			}
			encodedS1, _ := json.Marshal(s1)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
			account := &v1alpha1.CloudProviderAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: "us-east-1",
						SecretRef: &v1alpha1.SecretReference{
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName2.Name,
					Namespace: testSecretNamespacedName2.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})
		// Other tests.
		It("Validate Secret delete with json marshal error", func() {
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating Secret [%s, %s]\n", s1.Name, s1.Namespace)))
			err = fakeClient.Create(context.Background(), s1)
			Expect(err).Should(BeNil())
			temp := &corev1.Secret{}
			err = fakeClient.Get(context.TODO(), testSecretNamespacedName1, temp)
			Expect(err).Should(BeNil())
			var pollIntv uint = 1
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
							Name:      testSecretNamespacedName1.Name,
							Namespace: testSecretNamespacedName1.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Creating CloudProviderAccount [%s, %s] with SecretRef [%s, %s]\n",
				account.Name, account.Namespace, testSecretNamespacedName1.Name,
				testSecretNamespacedName1.Namespace)))
			err = fakeClient.Create(context.Background(), account)
			Expect(err).Should(BeNil())
			// To simulate error during json marshall, do not encode.
			dummyEncodeS1 := []byte("dummy")
			newS1Req := admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: dummyEncodeS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), newS1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
		})

		It("Validate Secret invalid admission request", func() {
			// Set admission.Request type to connect to simulate invalid request.
			s1Req = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "Secret",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "Secrets",
					},
					Name:      testSecretNamespacedName1.Name,
					Namespace: testSecretNamespacedName1.Namespace,
					Operation: v1.Connect,
					Object: runtime.RawExtension{
						Raw: encodedS1,
					},
				},
			}
			SecretValidatorTest1 := &SecretValidator{
				Client: fakeClient,
				Log:    logging.GetLogger("webhook").WithName("Secret")}
			err = SecretValidatorTest1.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
			response := SecretValidatorTest1.Handle(context.Background(), s1Req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response.Result)))
			Expect(response.Allowed).To(BeFalse())
			Expect(response.Result.Code).Should(BeEquivalentTo(400))
			Expect(response.Result.Message).Should(BeEquivalentTo(invalidReqErrorMsg))
		})
	})
})
