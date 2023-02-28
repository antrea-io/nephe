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

package webhook

import (
	"context"
	"encoding/json"
	"fmt"

	mock "github.com/golang/mock/gomock"
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
	"antrea.io/nephe/pkg/testing/networkpolicy"
)

func getRequestForConfigMapUpdate(name string, namespace string, conf string,
	encodedConfigMap []byte) admission.Request {
	newConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			"nephe-controller.conf": conf,
		},
	}
	encodedNewConfigMap, _ := json.Marshal(newConfigMap)
	req := admission.Request{
		AdmissionRequest: v1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "corev1",
				Kind:    "ConfigMap",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "corev1",
				Resource: "ConfigMaps",
			},
			Name:      name,
			Namespace: namespace,
			Operation: v1.Update,
			Object: runtime.RawExtension{
				Raw: encodedNewConfigMap,
			},
			OldObject: runtime.RawExtension{
				Raw: encodedConfigMap,
			},
		},
	}
	return req
}

var _ = Describe("Webhook", func() {
	Context("Running Webhook tests for ConfigMap", func() {
		var (
			testNamespacedName1 = types.NamespacedName{Namespace: "nephe-system", Name: "nephe-config"}
			testNamespacedName2 = types.NamespacedName{Namespace: "system", Name: "config"}
			controllerConfig    = "nephe-controller.conf"

			mockCtrl                    = mock.NewController(GinkgoT())
			mockNetworkPolicyController = networkpolicy.NewMockNetworkPolicyController(mockCtrl)

			configMap              *corev1.ConfigMap
			ConfigMapValidatorTest *ConfigMapValidator
			decoder                *admission.Decoder

			req admission.Request

			fakeClient       client.WithWatch
			encodedConfigMap []byte
			err              error
		)

		BeforeEach(func() {
			conf1 := `{"CloudResourcePrefix": "anp", "CloudSyncInterval": 70}`
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNamespacedName1.Name,
					Namespace: testNamespacedName1.Namespace,
				},
				Data: map[string]string{
					controllerConfig: conf1,
				},
			}
			encodedConfigMap, _ = json.Marshal(configMap)
			newScheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
			utilruntime.Must(v1alpha1.AddToScheme(newScheme))
			decoder, err = admission.NewDecoder(newScheme)
			Expect(err).Should(BeNil())
			fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
			ConfigMapValidatorTest = &ConfigMapValidator{
				Client:                fakeClient,
				Log:                   logging.GetLogger("webhook").WithName("ConfigMap"),
				NpControllerInterface: mockNetworkPolicyController,
			}
			err = ConfigMapValidatorTest.InjectDecoder(decoder)
			Expect(err).Should(BeNil())
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		It("Validate ConfigMap create", func() {
			req = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "ConfigMap",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "ConfigMaps",
					},
					Name:      testNamespacedName1.Name,
					Namespace: testNamespacedName1.Namespace,
					Operation: v1.Create,
					Object: runtime.RawExtension{
						Raw: encodedConfigMap,
					},
				},
			}
			response := ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})

		It("Validate ConfigMap update", func() {
			conf := `{"CloudResourcePrefix": "addressgroups"}`
			// ConfigMap name and namespace is different.
			req = getRequestForConfigMapUpdate(testNamespacedName2.Name, testNamespacedName2.Namespace, conf, encodedConfigMap)
			response := ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())

			// Invalid Old ConfigMap.
			req = getRequestForConfigMapUpdate(testNamespacedName1.Name, testNamespacedName1.Namespace, conf, nil)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())

			// CloudResourceCreated is false.
			req = getRequestForConfigMapUpdate(testNamespacedName1.Name, testNamespacedName1.Namespace, conf, encodedConfigMap)
			mockNetworkPolicyController.EXPECT().IsCloudResourceCreated().Return(false)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())

			// CloudResourceCreated is true.
			mockNetworkPolicyController.EXPECT().IsCloudResourceCreated().Return(true)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())

			// Invalid CloudResourcePrefix name.
			conf = `{"CloudResourcePrefix": "-addressgroups"}`
			req = getRequestForConfigMapUpdate(testNamespacedName1.Name, testNamespacedName1.Namespace, conf, encodedConfigMap)
			mockNetworkPolicyController.EXPECT().IsCloudResourceCreated().Return(false)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())

			// Invalid ConfigMap data.
			conf = `{"CloudResourcePrefix"::: "addressgroups"}`
			req = getRequestForConfigMapUpdate(testNamespacedName1.Name, testNamespacedName1.Namespace, conf, encodedConfigMap)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())

			// Invalid CloudSyncInterval.
			conf = `{"CloudResourcePrefix": "addressgroups", "CloudSyncInterval": 10}`
			req = getRequestForConfigMapUpdate(testNamespacedName1.Name, testNamespacedName1.Namespace, conf, encodedConfigMap)
			mockNetworkPolicyController.EXPECT().IsCloudResourceCreated().Return(false)
			response = ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeFalse())
		})

		It("Validate ConfigMap Delete", func() {
			req = admission.Request{
				AdmissionRequest: v1.AdmissionRequest{
					Kind: metav1.GroupVersionKind{
						Group:   "",
						Version: "corev1",
						Kind:    "ConfigMap",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "corev1",
						Resource: "ConfigMaps",
					},
					Name:      testNamespacedName1.Name,
					Namespace: testNamespacedName1.Namespace,
					Operation: v1.Delete,
					OldObject: runtime.RawExtension{
						Raw: encodedConfigMap,
					},
				},
			}
			response := ConfigMapValidatorTest.Handle(context.Background(), req)
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("Got admission response %+v\n", response)))
			Expect(response.Allowed).To(BeTrue())
		})
	})
})
