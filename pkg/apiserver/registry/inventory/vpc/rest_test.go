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

package vpc

import (
	"context"
	"sort"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	nephelabels "antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/logging"
)

func TestVpc(t *testing.T) {
	logging.SetDebugLog(true)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Vpc Suite")
}

var _ = Describe("VPC", func() {
	l := logging.GetLogger("VPC test")
	accountNamespacedName := types.NamespacedName{Name: "accountname", Namespace: "default"}
	cloudInventory := inventory.InitInventory()

	cacheTest1 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
		},
		Status: runtimev1alpha1.VpcStatus{
			CloudId:   "targetId",
			CloudName: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Cidrs:   []string{"192.168.1.1/24"},
			Managed: true,
			Region:  "region",
		},
	}
	cacheTest2 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId-nondefault",
		},
		Status: runtimev1alpha1.VpcStatus{
			CloudId:   "targetId-Non",
			CloudName: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Cidrs:   []string{"192.168.1.1/24"},
			Managed: false,
		},
	}
	cacheTest3 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId2",
			Labels: map[string]string{
				nephelabels.CloudAccountNamespace: accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:      accountNamespacedName.Name,
			},
		},
		Status: runtimev1alpha1.VpcStatus{
			CloudId:   "targetId2",
			CloudName: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Region:  "region",
			Cidrs:   []string{"192.168.1.1/24"},
			Managed: true,
		},
	}

	cachedVpcs := []*runtimev1alpha1.Vpc{
		cacheTest1,
		cacheTest2,
		cacheTest3,
	}
	expectedVpcs := []*runtimev1alpha1.Vpc{
		cacheTest1,
		nil,
		cacheTest3,
	}

	Describe("Test Get function of Rest", func() {
		It("Should return VPC object by Name", func() {
			cloudInventory = inventory.InitInventory()
			for i, cachedVpc := range cachedVpcs {
				vpcMap := make(map[string]*runtimev1alpha1.Vpc)
				vpcMap[cachedVpc.Status.CloudId] = cachedVpc
				namespacedName := types.NamespacedName{Namespace: cachedVpc.Namespace, Name: accountNamespacedName.Name}
				err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
				Expect(err).Should(BeNil())
				rest := NewREST(cloudInventory, l)
				actualVPC, err := rest.Get(request.NewDefaultContext(), cachedVpc.Name, &metav1.GetOptions{})
				if cachedVpc.Name == "targetId-nondefault" {
					Expect(actualVPC).Should(BeNil())
					Expect(err).To(Equal(errors.NewNotFound(runtimev1alpha1.Resource("vpc"), cachedVpc.Name)))
				} else {
					Expect(err).Should(BeNil())
					Expect(actualVPC).To(Equal(expectedVpcs[i]))
				}
			}
		})
	})

	Describe("Test List function of Rest", func() {
		var (
			expectedPolicyList1 *runtimev1alpha1.VpcList
			expectedPolicyList2 *runtimev1alpha1.VpcList
			expectedPolicyList3 *runtimev1alpha1.VpcList
			expectedPolicyList4 *runtimev1alpha1.VpcList
		)
		BeforeEach(func() {
			cloudInventory = inventory.InitInventory()
			for _, cachedVpc := range cachedVpcs {
				vpcMap := make(map[string]*runtimev1alpha1.Vpc)
				vpcMap[cachedVpc.Status.CloudId] = cachedVpc
				namespacedName := types.NamespacedName{Namespace: cachedVpc.Namespace,
					Name: cachedVpc.Labels[nephelabels.CloudAccountName]}
				err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
				Expect(err).Should(BeNil())
			}
			expectedPolicyList1 = &runtimev1alpha1.VpcList{
				Items: []runtimev1alpha1.Vpc{
					*cacheTest1,
					*cacheTest2,
					*cacheTest3,
				},
			}
			expectedPolicyList2 = &runtimev1alpha1.VpcList{
				Items: []runtimev1alpha1.Vpc{
					*cacheTest3,
				},
			}
			expectedPolicyList3 = &runtimev1alpha1.VpcList{
				Items: []runtimev1alpha1.Vpc{
					*cacheTest2,
				},
			}
			expectedPolicyList4 = &runtimev1alpha1.VpcList{
				Items: []runtimev1alpha1.Vpc{
					*cacheTest1,
					*cacheTest3,
				},
			}
		})

		It("Should return the VPC list by labels", func() {
			expectedPolicyLists := []*runtimev1alpha1.VpcList{
				expectedPolicyList1,
				expectedPolicyList2,
			}

			listLabelSelectorOption1 := &internalversion.ListOptions{}
			// select vpcs based on account name.
			req, _ := labels.NewRequirement(nephelabels.CloudAccountName, selection.Equals,
				[]string{accountNamespacedName.Name})
			labelSelector := labels.NewSelector().Add(*req)
			listLabelSelectorOption2 := &internalversion.ListOptions{LabelSelector: labelSelector}

			vpcLabelSelectorListOptions := []*internalversion.ListOptions{
				listLabelSelectorOption1,
				listLabelSelectorOption2,
			}
			for i, vpcListOption := range vpcLabelSelectorListOptions {
				rest := NewREST(cloudInventory, l)
				var actualObj runtime.Object
				var err error
				if i == 0 {
					// list all objects.
					actualObj, err = rest.List(context.TODO(), vpcListOption)
				} else {
					actualObj, err = rest.List(request.NewDefaultContext(), vpcListOption)
				}
				items := actualObj.(*runtimev1alpha1.VpcList).Items
				sort.SliceStable(items, func(i, j int) bool {
					return items[i].Name < items[j].Name
				})
				Expect(err).Should(BeNil())
				Expect(actualObj).To(Equal(expectedPolicyLists[i]))
			}
		})
		It("Should return error for invalid labels", func() {
			req2, _ := labels.NewRequirement(nephelabels.CloudVpcUID, selection.Equals,
				[]string{"dummy"})
			labelSelector2 := labels.NewSelector().Add(*req2)
			listLabelSelectorOption := &internalversion.ListOptions{LabelSelector: labelSelector2}
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.NewDefaultContext(), listLabelSelectorOption)
			Expect(err).ShouldNot(BeNil())
		})
		It("Should return the VPC list by fields", func() {
			listFieldSelectorOption1 := &internalversion.ListOptions{}
			listFieldSelectorOption1.FieldSelector = fields.OneTermEqualSelector("metadata.name", "targetId2")
			listFieldSelectorOption2 := &internalversion.ListOptions{}
			listFieldSelectorOption2.FieldSelector = fields.OneTermEqualSelector("metadata.namespace", "non-default")
			fieldSelector3 := fields.SelectorFromSet(fields.Set{"status.region": "region"})
			listFieldSelectorOption3 := &internalversion.ListOptions{FieldSelector: fieldSelector3}
			fieldSelector4 := fields.SelectorFromSet(fields.Set{"status.cloudId": "targetId-Non"})
			listFieldSelectorOption4 := &internalversion.ListOptions{FieldSelector: fieldSelector4}

			By("Using field selector metadata name")
			rest := NewREST(cloudInventory, l)
			actualObj, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption1)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPolicyList2))

			By("Using field selector metadata namespace")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "non-default"),
				listFieldSelectorOption2)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPolicyList3))

			By("Using field selector region")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption3)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPolicyList4))

			By("Using field selector cloudId")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "non-default"),
				listFieldSelectorOption4)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPolicyList3))
		})
		It("Should return error for invalid fields", func() {
			listFieldSelectorOption1 := &internalversion.ListOptions{}
			listFieldSelectorOption1.FieldSelector = fields.OneTermEqualSelector("status.cloudName", "dummy")
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption1)
			Expect(err).ShouldNot(BeNil())
		})
	})

	Describe("Test Watch and ConvertTable function of Rest", func() {
		cacheTest4 := &runtimev1alpha1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "non-default1",
				Name:      "targetId4",
			},
			Status: runtimev1alpha1.VpcStatus{
				CloudId:   "targetId4",
				CloudName: "targetName",
				Provider:  runtimev1alpha1.AWSCloudProvider,
				Region:    "us-west-2",
			},
		}
		cacheTest5 := &runtimev1alpha1.Vpc{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "targetId2",
				Labels: map[string]string{
					nephelabels.CloudAccountNamespace: accountNamespacedName.Namespace,
					nephelabels.CloudAccountName:      accountNamespacedName.Name,
					//nephelabels.CloudRegion:           "region",
				},
			},
			Status: runtimev1alpha1.VpcStatus{
				CloudId:   "targetId2",
				CloudName: "targetName",
				Tags: map[string]string{
					"no.delete": "false",
				},
				Cidrs: []string{"192.168.1.1/24"},
			},
		}
		It("Convert VPC objects in table format", func() {
			expectedTables := &metav1.Table{
				ColumnDefinitions: []metav1.TableColumnDefinition{
					{Name: "NAME", Type: "string", Description: "Name"},
					{Name: "CLOUD PROVIDER", Type: "string", Description: "Cloud Provider"},
					{Name: "REGION", Type: "string", Description: "Region"},
					{Name: "MANAGED", Type: "bool", Description: "Managed VPC"},
				},
				Rows: []metav1.TableRow{
					{
						Cells: []interface{}{"targetId4", runtimev1alpha1.AWSCloudProvider, "us-west-2", false},
					},
				},
			}
			vpcMap := make(map[string]*runtimev1alpha1.Vpc)
			vpcMap[cacheTest4.Status.CloudId] = cacheTest4
			namespacedName := types.NamespacedName{Namespace: cacheTest4.Namespace, Name: accountNamespacedName.Name}
			err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
			Expect(err).Should(BeNil())
			rest := NewREST(cloudInventory, l)
			actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
			Expect(err).Should(BeNil())
			Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
			Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
		})
		It("Watch VPC objects", func() {
			expectedEvents := []watch.Event{
				{Type: watch.Bookmark, Object: &runtimev1alpha1.Vpc{}},
				{Type: watch.Added, Object: cacheTest3},
				{Type: watch.Modified, Object: cacheTest5},
				{Type: watch.Deleted, Object: cacheTest5},
			}
			cloudInventory1 := inventory.InitInventory()
			vpcMap := make(map[string]*runtimev1alpha1.Vpc)
			namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: accountNamespacedName.Name}
			rest := NewREST(cloudInventory1, l)
			watcher, err := rest.Watch(request.NewDefaultContext(), &internalversion.ListOptions{})
			Expect(err).Should(BeNil())
			// key = default/accountname-targetId2
			vpcMap[cacheTest3.Status.CloudId] = cacheTest3
			err = cloudInventory1.BuildVpcCache(vpcMap, &namespacedName)
			Expect(err).Should(BeNil())
			// key = default/accountname-targetId2
			vpcMap[cacheTest3.Status.CloudId] = cacheTest5
			err = cloudInventory1.BuildVpcCache(vpcMap, &namespacedName)
			Expect(err).Should(BeNil())
			err = cloudInventory1.DeleteVpcsFromCache(&namespacedName)
			Expect(err).Should(BeNil())
			for _, expectedEvent := range expectedEvents {
				ev := <-watcher.ResultChan()
				Expect(ev.Type).To(Equal(expectedEvent.Type))
				Expect(ev.Object.(*runtimev1alpha1.Vpc)).To(Equal(expectedEvent.Object.(*runtimev1alpha1.Vpc)))
			}
		})
	})
})
