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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/logging"
)

var _ = Describe("Virtual Machine", func() {
	cloudInventory := inventory.InitInventory()

	l := logging.GetLogger("Virtual Machine test")
	cacheTest1 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
			Labels: map[string]string{
				config.LabelCloudNamespacedAccountName: "default/accountid1",
			},
		},
		Status: runtimev1alpha1.VirtualMachineStatus{
			Tags: map[string]string{
				"Name": "test",
			},
			VirtualPrivateCloud: "testNetworkID",
			Provider:            runtimev1alpha1.AWSCloudProvider,
			Agented:             false,
			State:               runtimev1alpha1.Starting,
		},
	}

	cacheTest2 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId-nondefault",
			Labels: map[string]string{
				config.LabelCloudNamespacedAccountName: "default/accountid",
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
				config.LabelCloudNamespacedAccountName: "default/accountid",
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
		for i, cachedVM := range cachedVMs {
			vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
			vmMap[cachedVM.Name] = cachedVM
			namespacedName := types.NamespacedName{Namespace: cachedVM.Namespace, Name: "accountID"}
			cloudInventory.BuildVmCache(vmMap, &namespacedName)
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

	Describe("Test List function of Rest", func() {

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

		cloudInventory := inventory.InitInventory()

		listLabelSelectorOption1 := &internalversion.ListOptions{}

		expectedVMLists := []*runtimev1alpha1.VirtualMachineList{
			expectedVMList1,
			expectedVMList2,
		}

		vmLabelSelectorListOptions := []*internalversion.ListOptions{
			listLabelSelectorOption1,
			listLabelSelectorOption1,
		}
		vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
		for _, cachedVM := range cachedVMs {
			vmMap[cachedVM.Name] = cachedVM
		}
		namespacedName := types.NamespacedName{Namespace: cacheTest1.Namespace, Name: "accountID"}
		cloudInventory.BuildVmCache(vmMap, &namespacedName)
		It("Should return the list result of rest by labels", func() {
			for i, vmListOption := range vmLabelSelectorListOptions {
				rest := NewREST(cloudInventory, l)
				var actualObj runtime.Object
				if i == 0 {
					actualObj, _ = rest.List(context.TODO(), vmListOption)
				} else {
					actualObj, _ = rest.List(request.NewDefaultContext(), vmListOption)
				}

				items := actualObj.(*runtimev1alpha1.VirtualMachineList).Items
				sort.SliceStable(items, func(i, j int) bool {
					return items[i].Name < items[j].Name
				})

				Expect(actualObj.(*runtimev1alpha1.VirtualMachineList)).To(Equal(expectedVMLists[i]))
			}
		})
	})

	Describe("Test Convert table function of Rest", func() {
		cacheTest4 := &runtimev1alpha1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "non-default1",
				Name:      "targetId4",
			},
			Status: runtimev1alpha1.VirtualMachineStatus{
				Tags: map[string]string{
					"Name": "test",
				},
				VirtualPrivateCloud: "testNetworkID",
				Provider:            runtimev1alpha1.AWSCloudProvider,
				Agented:             false,
				State:               runtimev1alpha1.Starting,
				Region:              "test-region",
			},
		}
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
		namespacedName := types.NamespacedName{Namespace: cacheTest4.Namespace, Name: cacheTest4.Labels[config.LabelCloudNamespacedAccountName]}
		cloudInventory.BuildVmCache(vmMap, &namespacedName)

		rest := NewREST(cloudInventory, l)
		actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
		Expect(err).Should(BeNil())
		Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
		Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
	})

	cacheTest5 := &runtimev1alpha1.VirtualMachine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
			Labels: map[string]string{
				config.LabelCloudNamespacedAccountName: "default/accountID",
			},
		},
		Status: runtimev1alpha1.VirtualMachineStatus{
			Tags: map[string]string{
				"Name": "test1",
			},
			VirtualPrivateCloud: "testNetworkID",
			Provider:            runtimev1alpha1.AWSCloudProvider,
			Agented:             false,
			State:               runtimev1alpha1.Starting,
		},
	}
	expectedEvents := []watch.Event{
		{Type: watch.Bookmark, Object: &runtimev1alpha1.VirtualMachine{}},
		{Type: watch.Added, Object: cacheTest1},
		{Type: watch.Modified, Object: cacheTest5},
		{Type: watch.Deleted, Object: cacheTest5},
	}
	Describe("Test Watch function of Rest", func() {
		cloudInventory1 := inventory.InitInventory()
		vmMap := make(map[string]*runtimev1alpha1.VirtualMachine)
		namespacedName := types.NamespacedName{Namespace: cacheTest1.Namespace, Name: "accountID"}
		cloudInventory1.BuildVmCache(vmMap, &namespacedName)
		rest := NewREST(cloudInventory1, l)
		watcher, err := rest.Watch(request.NewDefaultContext(), &internalversion.ListOptions{})
		Expect(err).Should(BeNil())
		vmMap[cacheTest1.Name] = cacheTest1
		cloudInventory1.BuildVmCache(vmMap, &namespacedName)
		vmMap[cacheTest1.Name] = cacheTest5
		cloudInventory1.BuildVmCache(vmMap, &namespacedName)
		err = cloudInventory1.DeleteVmsFromCache(&namespacedName)
		Expect(err).Should(BeNil())
		for _, expectedEvent := range expectedEvents {
			ev := <-watcher.ResultChan()
			Expect(ev.Type).To(Equal(expectedEvent.Type))
			Expect(ev.Object.(*runtimev1alpha1.VirtualMachine)).To(Equal(expectedEvent.Object.(*runtimev1alpha1.VirtualMachine)))
		}
	})
})
