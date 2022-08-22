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

package source_test

import (
	"context"
	"strings"
	"time"

	mock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	antreatypes "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	cloudv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/converter/source"
	"antrea.io/nephe/pkg/converter/target"
)

var _ = Describe("VirtualmachineConverter", func() {

	var (
		// Test framework.
		converter source.VMConverter

		// Test parameters.
		expectedExternalEntities = make(map[string]*antreatypes.ExternalEntity)

		// Test tunable.
		useInternalMethod      bool
		isEmptyEvent           bool
		generateEvent          bool
		expectExternalEntityOp bool
		externalEntityOpError  error
		expectWaiTime          time.Duration
	)

	BeforeEach(func() {
		commonInitTest()
		converter = source.VMConverter{
			Client: mockClient,
			Log:    logf.Log,
			Ch:     make(chan cloudv1alpha1.VirtualMachine),
			Scheme: scheme,
		}
		useInternalMethod = true
		isEmptyEvent = false
		generateEvent = true
		expectExternalEntityOp = true
		externalEntityOpError = nil
		expectWaiTime = source.RetryInterval

		go converter.Start()

		// Setup expected ExternalEntity from source resources.
		for name, externalEntitySource := range externalEntitySources {
			ee := &antreatypes.ExternalEntity{}
			fetchKey := target.GetObjectKeyFromSource(externalEntitySource)
			ee.Name = fetchKey.Name
			ee.Namespace = fetchKey.Namespace
			eps := make([]antreatypes.Endpoint, 0)
			for _, ip := range networkInterfaceIPAddresses {
				eps = append(eps, antreatypes.Endpoint{IP: ip})
			}
			ee.Spec.Endpoints = eps
			ee.Spec.Ports = externalEntitySource.GetEndPointPort(nil)
			labels := make(map[string]string)
			accessor, _ := meta.Accessor(externalEntitySource)
			labels[config.ExternalEntityLabelKeyKind] = target.GetExternalEntityLabelKind(externalEntitySource.EmbedType())
			labels[config.ExternalEntityLabelKeyName] = strings.ToLower(accessor.GetName())
			labels[config.ExternalEntityLabelKeyNamespace] = strings.ToLower(accessor.GetNamespace())
			for k, v := range externalEntitySource.GetLabelsFromClient(nil) {
				labels[k] = v
			}
			accessor, _ = meta.Accessor(externalEntitySource.EmbedType())
			_ = controllerruntime.SetControllerReference(accessor, ee, scheme)
			for k, v := range externalEntitySource.GetTags() {
				labels[k+config.ExternalEntityLabelKeyTagPostfix] = v
			}
			ee.Labels = labels
			ee.Spec.ExternalNode = config.ANPNepheController
			expectedExternalEntities[name] = ee
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("processEvent", func() {
		var (
			externalEntityGetErr error
			failedUpdates        map[string]source.RetryRecord
			isRetry              bool
		)

		tester := func(name string, op string) {
			finished := make(chan struct{}, 1)
			outstandingExpects := 0
			externalEntitySource := externalEntitySources[name]
			if isEmptyEvent {
				externalEntitySource = emptyExternalEntitySources[name]
			}

			fetchKey := target.GetObjectKeyFromSource(externalEntitySource)
			orderedCalls := make([]*mock.Call, 0)

			// Determine ExternalEntity source exits.
			orderedCalls = append(orderedCalls,
				mockClient.EXPECT().Get(mock.Any(), fetchKey, mock.Any()).
					Return(externalEntityGetErr).
					Do(func(_ context.Context, _ client.ObjectKey, ee *antreatypes.ExternalEntity) {
						expectedExternalEntities[name].DeepCopyInto(ee)
						outstandingExpects--
						if outstandingExpects == 0 {
							finished <- struct{}{}
						}
					}))

			if expectExternalEntityOp {
				if op == "create" {
					orderedCalls = append(orderedCalls,
						mockClient.EXPECT().Create(mock.Any(), mock.Any()).
							Return(externalEntityOpError).
							Do(func(_ context.Context, obj runtime.Object) {
								Expect(obj).To(Equal(expectedExternalEntities[name]))
								outstandingExpects--
								if outstandingExpects == 0 {
									finished <- struct{}{}
								}
							}))
				} else if op == "patch" {
					orderedCalls = append(orderedCalls,
						mockClient.EXPECT().Patch(mock.Any(), mock.Any(), mock.Any()).
							Return(externalEntityOpError).
							Do(func(_ context.Context, patch runtime.Object, _ client.Patch) {
								Expect(patch).To(Equal(expectedExternalEntities[name]))
								outstandingExpects--
								if outstandingExpects == 0 {
									finished <- struct{}{}
								}
							}))

				} else if op == "delete" {
					orderedCalls = append(orderedCalls,
						mockClient.EXPECT().Delete(mock.Any(), mock.Any()).
							Return(externalEntityOpError).
							Do(func(_ context.Context, obj runtime.Object) {
								Expect(obj).To(Equal(expectedExternalEntities[name]))
								outstandingExpects--
								if outstandingExpects == 0 {
									finished <- struct{}{}
								}
							}))

				}
			}
			mock.InOrder(orderedCalls...)
			outstandingExpects = len(orderedCalls)

			if generateEvent {
				if useInternalMethod {
					source.ProcessEvent(converter, externalEntitySource.(*source.VirtualMachineSource), failedUpdates, isRetry)
				} else {
					converter.Ch <- externalEntitySource.(*source.VirtualMachineSource).VirtualMachine
				}
			}

			select {
			case <-finished:
				logf.Log.Info("All expectations are met")
			case <-time.After(expectWaiTime):
				logf.Log.Info("Test timed out")
			case <-converter.GetRetryCh():
				Fail("Received retry event")
			}
		}

		BeforeEach(func() {
			externalEntityGetErr = nil
			failedUpdates = make(map[string]source.RetryRecord)
			isRetry = false
		})

		Context("Should create when ExternalEntity is not found", func() {
			JustBeforeEach(func() {
				externalEntityGetErr = errors.NewNotFound(schema.GroupResource{}, "")
			})
			table.DescribeTable("When source is",
				func(name string) {
					tester(name, "create")
				},
				table.Entry("VirtualMachineSource", "VirtualMachine"),
			)
		})

		Context("Should patch when ExternalEntity is found", func() {
			table.DescribeTable("When source is",
				func(name string) {
					tester(name, "patch")
				},
				table.Entry("VirtualMachineSource", "VirtualMachine"),
			)
		})

		Context("Should delete ExternalEntity when source is empty", func() {
			JustBeforeEach(func() {
				isEmptyEvent = true
			})
			table.DescribeTable("When source is",
				func(name string) {
					tester(name, "delete")
				},
				table.Entry("VirtualMachineSource", "VirtualMachine"),
			)
		})

		Context("Should do nothing if ExternalEntity is not found and source is empty", func() {
			JustBeforeEach(func() {
				externalEntityGetErr = errors.NewNotFound(schema.GroupResource{}, "")
				isEmptyEvent = true
			})
			table.DescribeTable("When source is",
				func(name string) {
					tester(name, "")
				},
				table.Entry("VirtualMachineSource", "VirtualMachine"),
			)
		})

		Context("Handle error with retry", func() {
			JustBeforeEach(func() {
				externalEntityOpError = errors.NewBadRequest("dummy")
				useInternalMethod = false
			})

			Context("Should create when ExternalEntity is not found", func() {
				JustBeforeEach(func() {
					externalEntityGetErr = errors.NewNotFound(schema.GroupResource{}, "")
				})
				table.DescribeTable("When source is",
					func(name string) {
						tester(name, "create")
						tester(name, "create")
						tester(name, "create")

						// a single successful retry cancels all previous failures.
						externalEntityOpError = nil
						generateEvent = false
						expectWaiTime = source.RetryInterval + time.Second*10
						tester(name, "create")
					},
					table.Entry("VirtualMachineSource", "VirtualMachine"),
				)
			})
			Context("Should patch when ExternalEntity is found", func() {
				table.DescribeTable("When source is",
					func(name string) {
						tester(name, "patch")

						externalEntityOpError = nil
						generateEvent = false
						expectWaiTime = source.RetryInterval + time.Second*10
						tester(name, "patch")
					},
					table.Entry("VirtualMachineSource", "VirtualMachine"),
				)
			})

			Context("Should delete ExternalEntity when source is empty", func() {
				JustBeforeEach(func() {
					isEmptyEvent = true
				})
				table.DescribeTable("When source is",
					func(name string) {
						tester(name, "delete")

						externalEntityOpError = nil
						generateEvent = false
						expectWaiTime = source.RetryInterval + time.Second*10
						tester(name, "delete")
					},
					table.Entry("VirtualMachineSource", "VirtualMachine"),
				)
			})
		})
	})
})
