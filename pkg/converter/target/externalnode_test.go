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

package target_test

import (
	"context"
	"reflect"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	converter "antrea.io/nephe/pkg/converter/target"
	"antrea.io/nephe/pkg/testing"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var _ = Describe("ExternalNode", func() {
	var (
		// Test framework
		mockCtrl   *mock.Controller
		mockclient *controllerruntimeclient.MockClient

		// Test parameters
		namespace = "test-externalnode-sources-namespace"

		// Test tunable
		networkInterfaceIPAddresses = []string{"1.1.1.1", "2.2.2.2"}
		externalNodeSources         map[string]converter.ExternalNodeSource
	)

	BeforeEach(func() {
		mockCtrl = mock.NewController(GinkgoT())
		mockclient = controllerruntimeclient.NewMockClient(mockCtrl)
		externalNodeSources = testing.SetupExternalNodeSources(networkInterfaceIPAddresses, namespace)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	embedTypeTester := func(name string, expType interface{}) {
		externalNodeSource := externalNodeSources[name]
		embed := externalNodeSource.EmbedType()
		Expect(reflect.TypeOf(embed).Elem()).To(Equal(reflect.TypeOf(expType).Elem()))
	}

	getTagsTester := func(name string, hasTag bool) {
		externalNodeSource := externalNodeSources[name]
		tags := externalNodeSource.GetTags()
		if hasTag {
			Expect(tags).ToNot(BeNil())
		} else {
			Expect(tags).To(BeNil())
		}
	}

	getLabelsTester := func(name string, hasLabels bool) {
		mockclient.EXPECT().Get(mock.Any(), mock.Any(), mock.Any()).
			Return(nil).
			Do(func(_ context.Context, key client.ObjectKey, out *runtimev1alpha1.VirtualMachine) {
				vm := externalNodeSources["VirtualMachine"].EmbedType().(*runtimev1alpha1.VirtualMachine)
				Expect(key.Name).To(Equal(vm.Name))
				Expect(key.Namespace).To(Equal(vm.Namespace))
				vm.DeepCopyInto(out)
			}).AnyTimes()

		externalNodeSource := externalNodeSources[name]
		labels := externalNodeSource.GetLabelsFromClient(mockclient)
		if hasLabels {
			Expect(labels).ToNot(BeNil())
		} else {
			Expect(labels).To(BeNil())
		}
	}

	Context("Source has required information", func() {
		table.DescribeTable("GetTags",
			func(name string, hasTags bool) {
				getTagsTester(name, hasTags)
			},
			table.Entry("VirtualMachine", "VirtualMachine", true))

		table.DescribeTable("GetLabels",
			func(name string, hasLabels bool) {
				getLabelsTester(name, hasLabels)
			},
			table.Entry("VirtualMachine", "VirtualMachine", true))

		table.DescribeTable("EmbedType",
			func(name string, expType interface{}) {
				embedTypeTester(name, expType)
			},
			table.Entry("VirtualMachine", "VirtualMachine", &runtimev1alpha1.VirtualMachine{}))
	})
})
