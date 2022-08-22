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
	"time"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	cloud "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/converter/target"
	testing2 "antrea.io/nephe/pkg/testing"
	"antrea.io/nephe/pkg/testing/controllerruntimeclient"
)

var (
	networkInterfaceIPAddresses = []string{"1.1.1.1", "2.2.2.2"}
	testNamespace               = "test-namespace"

	externalEntitySources map[string]target.ExternalEntitySource
)

var _ = Describe("VirtualmachineController", func() {

	BeforeEach(func() {
		mockCtrl = mock.NewController(GinkgoT())
		mockClient = controllerruntimeclient.NewMockClient(mockCtrl)
		externalEntitySources = testing2.SetupExternalEntitySources(networkInterfaceIPAddresses, testNamespace)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	table.DescribeTable("Reconciler",
		func(retError error) {
			vmSource := externalEntitySources["VirtualMachine"].(*source.VirtualMachineSource)
			ch := make(chan cloud.VirtualMachine)
			reconciler := &VirtualMachineReconciler{
				Log:    logf.Log,
				Client: mockClient,
			}
			reconciler.converter = source.VMConverter{
				Log: logf.Log,
				Ch:  ch,
			}

			request := ctrl.Request{}
			request.Namespace = vmSource.Namespace
			request.Name = vmSource.Name
			mockClient.EXPECT().Get(mock.Any(), request.NamespacedName, mock.Any()).Return(retError).
				Do(func(_ context.Context, _ types.NamespacedName, vm *cloud.VirtualMachine) {
					if vmSource != nil {
						vmSource.DeepCopyInto(vm)
					}
				})
			go func() {
				defer GinkgoRecover()
				rlt, err := reconciler.Reconcile(context.Background(), request)
				if retError == nil || errors.IsNotFound(retError) {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(HaveOccurred())
				}
				Expect(rlt).To(Equal(ctrl.Result{}))

			}()
			select {
			case out := <-ch:
				Expect(out).To(Equal(vmSource.VirtualMachine))
			case <-time.After(time.Second):
				Expect(retError).ToNot(BeNil())
			}
		},
		table.Entry("VM source get OK on create/update, forward VM", nil),
		table.Entry("VM source not found on delete, forward empty VM", errors.NewNotFound(ctrl.GroupResource{}, "")),
		table.Entry("VM source get error, error and no forwarding", errors.NewBadRequest("")),
	)
})
