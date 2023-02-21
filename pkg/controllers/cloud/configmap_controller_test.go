// Copyright 2023 Antrea Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloud

import (
	"context"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
)

var _ = Describe("ConfigMapController", func() {
	var (
		testNamespacedName1 = types.NamespacedName{Namespace: "nephe-system", Name: "nephe-config"}
		testNamespacedName2 = types.NamespacedName{Namespace: "system", Name: "config"}
		controllerConfig    = "nephe-controller.conf"
		conf                = `{"CloudResourcePrefix": "anp"}`

		reconciler *ConfigMapReconciler
		request    ctrl.Request
		fakeClient client.WithWatch
	)

	BeforeEach(func() {
		newScheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(newScheme))
		utilruntime.Must(v1alpha1.AddToScheme(newScheme))
		utilruntime.Must(v1.AddToScheme(newScheme))
		fakeClient = fake.NewClientBuilder().WithScheme(newScheme).Build()
		reconciler = &ConfigMapReconciler{
			Log:              logf.Log,
			Client:           fakeClient,
			Scheme:           scheme,
			ControllerConfig: &config.ControllerConfig{},
		}
		request = ctrl.Request{}
	})

	table.DescribeTable("Reconciler",
		func(name string, namespace string, conf string, create bool, errors bool) {
			request.Namespace = namespace
			request.Name = name
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      config.ConfigMapName,
					Namespace: config.ConfigMapNamespace,
				},
				Data: map[string]string{
					controllerConfig: conf,
				},
			}
			if create {
				err := fakeClient.Create(context.Background(), configMap)
				Expect(err).To(BeNil())
			}
			result, err := reconciler.Reconcile(context.Background(), request)
			if errors {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(result).To(Equal(ctrl.Result{}))

		},

		table.Entry("Set the CloudResourcePrefix",
			testNamespacedName1.Name, testNamespacedName1.Namespace, conf, true, false),
		table.Entry("ConfigMap contains different name and namespace",
			testNamespacedName2.Name, testNamespacedName2.Namespace, conf, true, false),
		table.Entry("Set the default CloudResourcePrefix",
			testNamespacedName1.Name, testNamespacedName1.Namespace, ``, true, false),
		table.Entry("ConfigMap not found",
			testNamespacedName1.Name, testNamespacedName1.Namespace, conf, false, true),
		table.Entry("Invalid ConfigMap data",
			testNamespacedName1.Name, testNamespacedName1.Namespace, `{"CloudResourcePrefix:::"}`, true, true),
	)
})
