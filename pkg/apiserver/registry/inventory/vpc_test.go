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

package inventory

import (
	"sort"

	logger "github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/endpoints/request"

	"antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/controllers/inventory"
)

var _ = Describe("VPC", func() {
	cloudInventory := inventory.InitInventory()

	var l logger.Logger
	cacheTest1 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId",
		},
		Info: runtimev1alpha1.VpcInfo{
			Id:   "targetId",
			Name: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Cidrs: []string{"192.168.1.1/24"},
		},
	}
	cacheTest2 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId-nondefault",
		},
		Info: runtimev1alpha1.VpcInfo{
			Id:   "targetId-Non",
			Name: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Cidrs: []string{"192.168.1.1/24"},
		},
	}
	cacheTest3 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "targetId2",
			Labels: map[string]string{
				inventory.VpcLabelAccountName: "accountname",
				inventory.VpcLabelRegion:      "region",
			},
		},
		Info: runtimev1alpha1.VpcInfo{
			Id:   "targetId2",
			Name: "targetName",
			Tags: map[string]string{
				"no.delete": "true",
			},
			Region: "region",
			Cidrs:  []string{"192.168.1.1/24"},
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
		for i, cachedVpc := range cachedVpcs {
			vpcMap := make(map[string]*runtimev1alpha1.Vpc)
			vpcMap[cachedVpc.Info.Id] = cachedVpc
			namespacedName := types.NamespacedName{Namespace: cachedVpc.Namespace, Name: cachedVpc.Labels[inventory.VpcLabelAccountName]}
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
		expectedPolicyList1 := &runtimev1alpha1.VpcList{
			Items: []runtimev1alpha1.Vpc{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "targetId",
					},
					Info: runtimev1alpha1.VpcInfo{
						Id:   "targetId",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Cidrs: []string{"192.168.1.1/24"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "targetId2",
						Labels: map[string]string{
							inventory.VpcLabelAccountName: "accountname",
							inventory.VpcLabelRegion:      "region",
						},
					},
					Info: runtimev1alpha1.VpcInfo{
						Id:   "targetId2",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Region: "region",
						Cidrs:  []string{"192.168.1.1/24"},
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
							inventory.VpcLabelAccountName: "accountname",
							inventory.VpcLabelRegion:      "region",
						},
					},
					Info: runtimev1alpha1.VpcInfo{
						Id:   "targetId2",
						Name: "targetName",
						Tags: map[string]string{
							"no.delete": "true",
						},
						Region: "region",
						Cidrs:  []string{"192.168.1.1/24"},
					},
				},
			},
		}

		expectedPolicyLists := []*runtimev1alpha1.VpcList{
			expectedPolicyList1,
			expectedPolicyList2,
			expectedPolicyList2,
		}
		req1, _ := labels.NewRequirement("account-name", selection.Equals, []string{"accountname"})
		labelSelector1 := labels.NewSelector()
		labelSelector1 = labelSelector1.Add(*req1)

		req2, _ := labels.NewRequirement("region", selection.Equals, []string{"region"})
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
			vpcMap[cachedVpc.Info.Id] = cachedVpc
			namespacedName := types.NamespacedName{Namespace: cachedVpc.Namespace, Name: cachedVpc.Labels[inventory.VpcLabelAccountName]}
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
		listFieldSelectorOption := &internalversion.ListOptions{}
		listFieldSelectorOption.FieldSelector = fields.OneTermEqualSelector("metadata.name", "targetId2")

		It("Should return the list result of rest by fields", func() {
			rest := NewREST(cloudInventory, l)
			actualObj, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedPolicyList2))
		})
	})

	cacheTest4 := &runtimev1alpha1.Vpc{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "targetId4",
		},
		Info: runtimev1alpha1.VpcInfo{
			Id:            "targetId4",
			Name:          "targetName",
			CloudProvider: v1alpha1.AWSCloudProvider,
			Region:        "us-west-2",
		},
	}
	expectedTables := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "NAME", Type: "string", Description: "Name"},
			{Name: "CLOUD PROVIDER", Type: "string", Description: "Cloud Provider"},
			{Name: "REGION", Type: "string", Description: "Region"},
		},
		Rows: []metav1.TableRow{
			{
				Cells: []interface{}{"targetId4", v1alpha1.AWSCloudProvider, "us-west-2"},
			},
		},
	}
	Describe("Test Convert table function of Rest", func() {
		vpcMap := make(map[string]*runtimev1alpha1.Vpc)
		vpcMap[cacheTest4.Info.Id] = cacheTest4
		namespacedName := types.NamespacedName{Namespace: cacheTest4.Namespace, Name: cacheTest4.Labels[inventory.VpcLabelAccountName]}
		err := cloudInventory.BuildVpcCache(vpcMap, &namespacedName)
		Expect(err).Should(BeNil())
		rest := NewREST(cloudInventory, l)
		actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
		Expect(err).Should(BeNil())
		Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
		Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
	})
})
