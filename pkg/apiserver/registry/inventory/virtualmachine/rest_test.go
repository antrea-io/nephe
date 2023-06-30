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

package virtualmachine

import (
	"context"
	"sort"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	nephelabels "antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/logging"
	"k8s.io/apimachinery/pkg/fields"
)

func TestVirtualMachine(t *testing.T) {
	logging.SetDebugLog(true)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Virtual Machine Suite")
}

type mockUpdatedObjectInfo struct {
	rest.UpdatedObjectInfo
}

var (
	updateTags = map[string]string{"Tag1": "Value1"}
)

func (m *mockUpdatedObjectInfo) UpdatedObject(_ context.Context, oldobj runtime.Object) (runtime.Object, error) {
	// Implement the UpdatedObjectMeta method as per your test requirements
	vm := oldobj.(*runtimev1alpha1.VirtualMachine)
	vm.Spec.Tags = updateTags
	// Test
	return vm, nil
}

var _ = Describe("Virtual Machine", func() {

	l := logging.GetLogger("Virtual Machine test")
	cloudInventory := inventory.InitInventory()
	accountNamespacedName := types.NamespacedName{Name: "accountid1", Namespace: "default"}

	cacheTest1 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
			Labels: map[string]string{
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "default",
			},
		},
		Status: runtimev1alpha1.VirtualMachineStatus{
			Tags: map[string]string{
				"Name": "test",
			},
			Provider:   runtimev1alpha1.AWSCloudProvider,
			Agented:    false,
			State:      runtimev1alpha1.Starting,
			Region:     "region",
			CloudVpcId: "cloudVpcId",
			CloudId:    "cloudId",
		},
	}
	cacheTest2 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId-nondefault",
			Labels: map[string]string{
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "non-default",
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
			},
		},
		Status: runtimev1alpha1.VirtualMachineStatus{
			Tags: map[string]string{
				"Name": "test",
			},
		},
	}
	cacheTest3 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId2",
			Labels: map[string]string{
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "default",
			},
		},
		Status: runtimev1alpha1.VirtualMachineStatus{
			Tags: map[string]string{
				"Name": "test",
			},
		},
	}

	cachedVMs := []*runtimev1alpha1.VirtualMachine{
		cacheTest1,
		cacheTest2,
		cacheTest3,
	}
	expectedVMs := []*runtimev1alpha1.VirtualMachine{
		cacheTest1,
		nil,
		cacheTest3,
	}

	Describe("Test Get function of Rest", func() {
		It("Should return VM object by Name", func() {
			for i, cachedVM := range cachedVMs {
				vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
				vmMap[cachedVM.Name] = cachedVM
				namespacedName := types.NamespacedName{Namespace: cachedVM.Namespace, Name: "selector01"}
				cloudInventory.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)
				rest := NewREST(cloudInventory, l)
				actualVM, err := rest.Get(request.NewDefaultContext(), cachedVM.Name, &metav1.GetOptions{})
				if cachedVM.Name == "targetId-nondefault" {
					Expect(actualVM).Should(BeNil())
					Expect(err).To(Equal(errors.NewNotFound(runtimev1alpha1.Resource("virtualmachine"), cachedVM.Name)))
				} else {
					Expect(err).Should(BeNil())
					Expect(actualVM).To(Equal(expectedVMs[i]))
				}
			}
		})
	})

	Describe("Test Update function of Rest", func() {
		for _, cachedVM := range cachedVMs {
			vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmMap[cachedVM.Name] = cachedVM
			accountNamespacedName := types.NamespacedName{Namespace: cacheTest1.Namespace, Name: "accountID"}
			namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: "selector01"}
			cloudInventory.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)

			objInfo := &mockUpdatedObjectInfo{}
			cachedVM.Spec.Tags = updateTags
			rest := NewREST(cloudInventory, l)
			_, _, err := rest.Update(request.NewDefaultContext(), cachedVM.Name, objInfo, nil, nil, false, &metav1.UpdateOptions{})
			if cachedVM.Name == "targetId-nondefault" {
				Expect(err).To(Equal(errors.NewNotFound(runtimev1alpha1.Resource("virtualmachine"), cachedVM.Name)))
			} else {
				actualVM, err := rest.Get(request.NewDefaultContext(), cachedVM.Name, &metav1.GetOptions{})
				Expect(err).Should(BeNil())
				Expect(actualVM).To(Equal(cachedVM))
			}
			cachedVM.Spec.Tags = nil
		}
	})

	Describe("Test List function of Rest", func() {
		BeforeEach(func() {
			cloudInventory = inventory.InitInventory()
			vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
			for _, cachedVM := range cachedVMs {
				vmMap[cachedVM.Name] = cachedVM
			}
			accountNamespacedName := types.NamespacedName{Namespace: cacheTest1.Namespace, Name: "accountID"}
			namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: "selector01"}
			cloudInventory.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)
		})

		expectedVMList1 := &runtimev1alpha1.VirtualMachineList{
			Items: []runtimev1alpha1.VirtualMachine{
				*cacheTest1,
				*cacheTest2,
				*cacheTest3,
			},
		}
		expectedVMList2 := &runtimev1alpha1.VirtualMachineList{
			Items: []runtimev1alpha1.VirtualMachine{
				*cacheTest1,
				*cacheTest3,
			},
		}
		expectedVMLists := []*runtimev1alpha1.VirtualMachineList{
			expectedVMList1,
			expectedVMList2,
		}

		listLabelSelectorOption1 := &internalversion.ListOptions{}
		// select vms based on account name.
		req, _ := labels.NewRequirement(nephelabels.CloudAccountName, selection.Equals,
			[]string{accountNamespacedName.Name})
		labelSelector := labels.NewSelector().Add(*req)
		listLabelSelectorOption2 := &internalversion.ListOptions{LabelSelector: labelSelector}

		vmLabelSelectorListOptions := []*internalversion.ListOptions{
			listLabelSelectorOption1,
			listLabelSelectorOption2,
		}
		It("Should return the VM list by labels", func() {
			for i, vmListOption := range vmLabelSelectorListOptions {
				rest := NewREST(cloudInventory, l)
				var actualObj runtime.Object
				var err error
				if i == 0 {
					// list all objects.
					actualObj, err = rest.List(context.TODO(), vmListOption)
				} else {
					actualObj, err = rest.List(request.NewDefaultContext(), vmListOption)
				}
				Expect(err).Should(BeNil())
				items := actualObj.(*runtimev1alpha1.VirtualMachineList).Items
				sort.SliceStable(items, func(i, j int) bool {
					return items[i].Name < items[j].Name
				})

				Expect(actualObj.(*runtimev1alpha1.VirtualMachineList)).To(Equal(expectedVMLists[i]))
			}
		})
		It("Should return error for invalid labels,", func() {
			req2, _ := labels.NewRequirement(nephelabels.CloudVmUID, selection.Equals,
				[]string{"dummy"})
			labelSelector2 := labels.NewSelector().Add(*req2)
			listLabelSelectorOption := &internalversion.ListOptions{LabelSelector: labelSelector2}
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.NewDefaultContext(), listLabelSelectorOption)
			Expect(err).ShouldNot(BeNil())
		})
		It("Should return the VM list by fields", func() {
			expectedVMList3 := &runtimev1alpha1.VirtualMachineList{
				Items: []runtimev1alpha1.VirtualMachine{
					*cacheTest1,
				},
			}
			expectedVMList4 := &runtimev1alpha1.VirtualMachineList{
				Items: []runtimev1alpha1.VirtualMachine{
					*cacheTest2,
				},
			}
			listFieldSelectorOption1 := &internalversion.ListOptions{}
			listFieldSelectorOption1.FieldSelector = fields.OneTermEqualSelector("metadata.name", "targetId")
			listFieldSelectorOption2 := &internalversion.ListOptions{}
			listFieldSelectorOption2.FieldSelector = fields.OneTermEqualSelector("metadata.namespace", "non-default")
			fieldSelector3 := fields.SelectorFromSet(fields.Set{"status.region": "region"})
			listFieldSelectorOption3 := &internalversion.ListOptions{FieldSelector: fieldSelector3}
			fieldSelector4 := fields.SelectorFromSet(fields.Set{"status.cloudId": "cloudId"})
			listFieldSelectorOption4 := &internalversion.ListOptions{FieldSelector: fieldSelector4}
			fieldSelector5 := fields.SelectorFromSet(fields.Set{"status.cloudVpcId": "cloudVpcId"})
			listFieldSelectorOption5 := &internalversion.ListOptions{FieldSelector: fieldSelector5}

			By("Using field selector metadata name")
			rest := NewREST(cloudInventory, l)
			actualObj, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption1)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedVMList3))

			By("Using field selector metadata namespace")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "non-default"),
				listFieldSelectorOption2)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedVMList4))

			By("Using field selector region")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption3)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedVMList3))

			By("Using field selector cloudId")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption4)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedVMList3))

			By("Using field selector cloudVpcId")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption5)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedVMList3))

		})
		It("Should return error for invalid Fields,", func() {
			fieldSelector6 := fields.SelectorFromSet(fields.Set{"status.cloudName": "cloudName"})
			listFieldSelectorOption6 := &internalversion.ListOptions{FieldSelector: fieldSelector6}
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption6)
			Expect(err).ShouldNot(BeNil())
		})
	})

	Describe("Test Watch and ConvertTable function of Rest", func() {
		cacheTest4 := &runtimev1alpha1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "non-default1",
				Name:      "targetId4",
				Labels: map[string]string{
					nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
					nephelabels.CloudAccountName:       accountNamespacedName.Name,
					nephelabels.VpcName:                "testNetworkID",
					nephelabels.CloudSelectorName:      "selector01",
					nephelabels.CloudSelectorNamespace: "non-default1",
				},
			},
			Status: runtimev1alpha1.VirtualMachineStatus{
				Tags: map[string]string{
					"Name": "test",
				},
				Provider: runtimev1alpha1.AWSCloudProvider,
				Agented:  false,
				State:    runtimev1alpha1.Starting,
				Region:   "test-region",
			},
		}
		cacheTest5 := &runtimev1alpha1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "targetId",
				Labels: map[string]string{
					nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
					nephelabels.CloudAccountName:       accountNamespacedName.Name,
					nephelabels.CloudSelectorName:      "selector01",
					nephelabels.CloudSelectorNamespace: "default",
				},
			},
			Status: runtimev1alpha1.VirtualMachineStatus{
				Tags: map[string]string{
					"Name": "test1",
				},
				Provider: runtimev1alpha1.AWSCloudProvider,
				Agented:  false,
				State:    runtimev1alpha1.Starting,
			},
		}
		It("Convert VM objects in table format", func() {
			expectedTables := &metav1.Table{
				ColumnDefinitions: []metav1.TableColumnDefinition{
					{Name: "NAME", Type: "string", Description: "Name"},
					{Name: "CLOUD-PROVIDER", Type: "string", Description: "Cloud Provider"},
					{Name: "REGION", Type: "string", Description: "Region"},
					{Name: "VIRTUAL-PRIVATE-CLOUD", Type: "string", Description: "VPC/VNET"},
					{Name: "STATE", Type: "string", Description: "Running state"},
					{Name: "AGENTED", Type: "bool", Description: "Agent installed"},
				},
				Rows: []metav1.TableRow{
					{
						Cells: []interface{}{"targetId4", runtimev1alpha1.AWSCloudProvider, "test-region", "testNetworkID", runtimev1alpha1.Starting, false},
					},
				},
			}
			vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmMap[cacheTest4.Name] = cacheTest4
			namespacedName := types.NamespacedName{Namespace: cacheTest4.Labels[nephelabels.CloudSelectorNamespace],
				Name: cacheTest4.Labels[nephelabels.CloudSelectorName]}
			cloudInventory.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)

			rest := NewREST(cloudInventory, l)
			actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
			Expect(err).Should(BeNil())
			Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
			Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
		})

		It("Watch VM objects", func() {
			expectedEvents := []watch.Event{
				{Type: watch.Bookmark, Object: &runtimev1alpha1.VirtualMachine{}},
				{Type: watch.Added, Object: cacheTest1},
				{Type: watch.Modified, Object: cacheTest5},
				{Type: watch.Deleted, Object: cacheTest5},
			}
			cloudInventory1 := inventory.InitInventory()
			vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
			namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: "selector01"}
			cloudInventory1.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)
			rest := NewREST(cloudInventory1, l)
			watcher, err := rest.Watch(request.NewDefaultContext(), &internalversion.ListOptions{})
			Expect(err).Should(BeNil())
			vmMap[cacheTest1.Name] = cacheTest1
			cloudInventory1.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)
			vmMap[cacheTest1.Name] = cacheTest5
			cloudInventory1.BuildVmCache(vmMap, &accountNamespacedName, &namespacedName)
			err = cloudInventory1.DeleteVmsFromCache(&accountNamespacedName, &namespacedName)
			Expect(err).Should(BeNil())
			for _, expectedEvent := range expectedEvents {
				ev := <-watcher.ResultChan()
				Expect(ev.Type).To(Equal(expectedEvent.Type))
				Expect(ev.Object.(*runtimev1alpha1.VirtualMachine)).To(Equal(expectedEvent.Object.(*runtimev1alpha1.VirtualMachine)))
			}
		})
	})
})
