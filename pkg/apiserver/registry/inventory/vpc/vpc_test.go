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
	"sort"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/config"
	"antrea.io/nephe/pkg/controllers/inventory"
	"antrea.io/nephe/pkg/logging"
)

var _ = Describe("VPC", func() {
	accountNamespacedName := types.NamespacedName{
		Name:      "accountname",
		Namespace: "default",
	}
	cloudInventory := inventory.InitInventory()

	l := logging.GetLogger("VPC test")
	cacheTest1 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
		},
		Status: runtimev1alpha1.VpcStatus{
			Id:   "targetId",
			Name: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Cidrs:   []string{"192.168.1.1/24"},
			Managed: true,
		},
	}
	cacheTest2 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId-nondefault",
		},
		Status: runtimev1alpha1.VpcStatus{
			Id:   "targetId-Non",
			Name: "targetName",
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
				config.LabelCloudAccountNamespace: accountNamespacedName.Namespace,
				config.LabelCloudAccountName:      accountNamespacedName.Name,
				config.LabelCloudRegion:           "region",
			},
		},
		Status: runtimev1alpha1.VpcStatus{
			Id:   "targetId2",
			Name: "targetName",
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
		By("Test Get function of Rest")
		for i, cachedVpc := range cachedVpcs {
			vpcMap := make(map[string]*runtimev1alpha1.Vpc)
			vpcMap[cachedVpc.Status.Id] = cachedVpc
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

	Describe("Test List function of Rest", func() {
		By("Test List function of Rest")
		expectedPolicyList1 := &runtimev1alpha1.VpcList{
			Items: []runtimev1alpha1.Vpc{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "targetId",
					},
					Status: runtimev1alpha1.VpcStatus{
						Id:   "targetId",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Cidrs:   []string{"192.168.1.1/24"},
						Managed: true,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "targetId2",
						Labels: map[string]string{
							config.LabelCloudAccountNamespace: accountNamespacedName.Namespace,
							config.LabelCloudAccountName:      accountNamespacedName.Name,
							config.LabelCloudRegion:           "region",
						},
					},
					Status: runtimev1alpha1.VpcStatus{
						Id:   "targetId2",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Region:  "region",
						Cidrs:   []string{"192.168.1.1/24"},
						Managed: true,
					},
				},
			},
		}
		expectedPolicyList2 := &runtimev1alpha1.VpcList{
			Items: []runtimev1alpha1.Vpc{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "targetId2",
						Labels: map[string]string{
							config.LabelCloudAccountNamespace: accountNamespacedName.Namespace,
							config.LabelCloudAccountName:      accountNamespacedName.Name,
							config.LabelCloudRegion:           "region",
						},
					},
					Status: runtimev1alpha1.VpcStatus{
						Id:   "targetId2",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Region:  "region",
						Cidrs:   []string{"192.168.1.1/24"},
						Managed: true,
					},
				},
			},
		}

		expectedPolicyLists := []*runtimev1alpha1.VpcList{
			expectedPolicyList1,
			expectedPolicyList2,
			expectedPolicyList2,
		}
		// select vpcs based on account name.
		req1, _ := labels.NewRequirement(config.LabelCloudAccountName, selection.Equals,
			[]string{accountNamespacedName.Name})
		labelSelector1 := labels.NewSelector()
		labelSelector1 = labelSelector1.Add(*req1)

		// select vpcs based on region.
		req2, _ := labels.NewRequirement(config.LabelCloudRegion, selection.Equals, []string{"region"})
		labelSelector2 := labels.NewSelector()
		labelSelector2 = labelSelector2.Add(*req2)

		listLabelSelectorOption1 := &internalversion.ListOptions{}
		listLabelSelectorOption2 := &internalversion.ListOptions{LabelSelector: labelSelector1}
		listLabelSelectorOption3 := &internalversion.ListOptions{LabelSelector: labelSelector2}

		vpcLabelSelectorListOptions := []*internalversion.ListOptions{
			listLabelSelectorOption1,
			listLabelSelectorOption2,
			listLabelSelectorOption3,
		}
		for _, cachedVpc := range cachedVpcs {
			vpcMap := make(map[string]*runtimev1alpha1.Vpc)
			vpcMap[cachedVpc.Status.Id] = cachedVpc
			namespacedName := types.NamespacedName{Namespace: cachedVpc.Namespace,
				Name: cachedVpc.Labels[config.LabelCloudAccountName]}
			err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
			Expect(err).Should(BeNil())
		}
		It("Should return the list result of rest by labels", func() {
			for i, vpcListOption := range vpcLabelSelectorListOptions {
				rest := NewREST(cloudInventory, l)
				actualObj, err := rest.List(request.NewDefaultContext(), vpcListOption)
				items := actualObj.(*runtimev1alpha1.VpcList).Items
				sort.SliceStable(items, func(i, j int) bool {
					return items[i].Name < items[j].Name
				})
				Expect(err).Should(BeNil())
				Expect(actualObj).To(Equal(expectedPolicyLists[i]))
			}
		})

		listFieldSelectorOption1 := &internalversion.ListOptions{}
		listFieldSelectorOption1.FieldSelector = fields.OneTermEqualSelector("metadata.name", "targetId2")
		listFieldSelectorOption2 := &internalversion.ListOptions{}
		listFieldSelectorOption2.FieldSelector = fields.OneTermEqualSelector("metadata.namespace", "non-default")

		expectedPolicyList3 := &runtimev1alpha1.VpcList{
			Items: []runtimev1alpha1.Vpc{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "non-default",
						Name:      "targetId-nondefault",
					},
					Status: runtimev1alpha1.VpcStatus{
						Id:   "targetId-Non",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Cidrs: []string{"192.168.1.1/24"},
					},
				},
			},
		}

		It("Should return the list result of rest by fields", func() {
			rest := NewREST(cloudInventory, l)
			actualObj1, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption1)
			Expect(err).Should(BeNil())
			Expect(actualObj1).To(Equal(expectedPolicyList2))
			actualObj2, err := rest.List(request.WithNamespace(request.NewContext(), "non-default"),
				listFieldSelectorOption2)
			Expect(err).Should(BeNil())
			Expect(actualObj2).To(Equal(expectedPolicyList3))
		})
	})

	cacheTest4 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default1",
			Name:      "targetId4",
		},
		Status: runtimev1alpha1.VpcStatus{
			Id:       "targetId4",
			Name:     "targetName",
			Provider: runtimev1alpha1.AWSCloudProvider,
			Region:   "us-west-2",
		},
	}
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
	Describe("Test Convert table function of Rest", func() {
		By("Test Convert table function of Rest")
		vpcMap := make(map[string]*runtimev1alpha1.Vpc)
		vpcMap[cacheTest4.Status.Id] = cacheTest4
		namespacedName := types.NamespacedName{Namespace: cacheTest4.Namespace, Name: accountNamespacedName.Name}
		err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
		Expect(err).Should(BeNil())
		rest := NewREST(cloudInventory, l)
		actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
		Expect(err).Should(BeNil())
		Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
		Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
	})

	cacheTest5 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId2",
			Labels: map[string]string{
				config.LabelCloudAccountNamespace: accountNamespacedName.Namespace,
				config.LabelCloudAccountName:      accountNamespacedName.Name,
				config.LabelCloudRegion:           "region",
			},
		},
		Status: runtimev1alpha1.VpcStatus{
			Id:   "targetId2",
			Name: "targetName",
			Tags: map[string]string{
				"no.delete": "false",
			},
			Cidrs: []string{"192.168.1.1/24"},
		},
	}
	expectedEvents := []watch.Event{
		{Type: watch.Bookmark, Object: &runtimev1alpha1.Vpc{}},
		{Type: watch.Added, Object: cacheTest3},
		{Type: watch.Modified, Object: cacheTest5},
		{Type: watch.Deleted, Object: cacheTest5},
	}

	Describe("Test Watch function of Rest", func() {
		By("Test Watch function of Rest")
		cloudInventory1 := inventory.InitInventory()
		vpcMap := make(map[string]*runtimev1alpha1.Vpc)
		namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: accountNamespacedName.Name}
		rest := NewREST(cloudInventory1, l)
		watcher, err := rest.Watch(request.NewDefaultContext(), &internalversion.ListOptions{})
		Expect(err).Should(BeNil())
		// key = default/accountname-targetId2
		vpcMap[cacheTest3.Status.Id] = cacheTest3
		err = cloudInventory1.BuildVpcCache(vpcMap, &namespacedName)
		Expect(err).Should(BeNil())
		// key = default/accountname-targetId2
		vpcMap[cacheTest3.Status.Id] = cacheTest5
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
