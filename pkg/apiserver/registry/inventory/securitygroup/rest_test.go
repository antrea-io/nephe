package securitygroup

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
	"k8s.io/apiserver/pkg/endpoints/request"

	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/inventory"
	nephelabels "antrea.io/nephe/pkg/labels"
	"antrea.io/nephe/pkg/logging"
)

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

func TestSecurityGroup(t *testing.T) {
	logging.SetDebugLog(true)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Security Group Suite")
}

var _ = Describe("Security Group", func() {

	l := logging.GetLogger("Security Group rest test")
	cloudInventory := inventory.InitInventory()
	accountNamespacedName := types.NamespacedName{Name: "accountid1", Namespace: "default"}

	cacheTest1 := &runtimev1alpha1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secGroupId",
			Labels: map[string]string{
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "default",
			},
		},
		Status: runtimev1alpha1.SecurityGroupStatus{
			Provider:  runtimev1alpha1.AWSCloudProvider,
			Region:    "region1",
			CloudId:   "cloudId1",
			CloudName: "cloudName1",
			Rules:     nil,
		},
	}
	cacheTest2 := &runtimev1alpha1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "non-default",
			Name:      "secGroupId-nondefault",
			Labels: map[string]string{
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "non-default",
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
			},
		},
		Status: runtimev1alpha1.SecurityGroupStatus{
			Provider:  runtimev1alpha1.AWSCloudProvider,
			Region:    "region2",
			CloudId:   "cloudId2",
			CloudName: "cloudName2",
			Rules:     nil,
		},
	}
	cacheTest3 := &runtimev1alpha1.SecurityGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secGroupId2",
			Labels: map[string]string{
				nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
				nephelabels.CloudAccountName:       accountNamespacedName.Name,
				nephelabels.CloudSelectorName:      "selector01",
				nephelabels.CloudSelectorNamespace: "default",
			},
		},
		Status: runtimev1alpha1.SecurityGroupStatus{
			Provider:  runtimev1alpha1.AWSCloudProvider,
			Region:    "region3",
			CloudId:   "cloudId3",
			CloudName: "cloudName3",
			Rules:     nil,
		},
	}

	cachedSGs := []*runtimev1alpha1.SecurityGroup{
		cacheTest1,
		cacheTest2,
		cacheTest3,
	}
	expectedSGs := []*runtimev1alpha1.SecurityGroup{
		cacheTest1,
		nil,
		cacheTest3,
	}

	Describe("Test Get function of Rest", func() {
		It("Should return SG object by Name", func() {
			for i, cachedSG := range cachedSGs {
				sgMap := make(map[string]*runtimev1alpha1.SecurityGroup)
				sgMap[cachedSG.Name] = cachedSG
				namespacedName := types.NamespacedName{Namespace: cachedSG.Namespace, Name: "selector01"}
				cloudInventory.BuildSgCache(sgMap, &accountNamespacedName, &namespacedName)
				rest := NewREST(cloudInventory, l)
				actualSG, err := rest.Get(request.NewDefaultContext(), cachedSG.Name, &metav1.GetOptions{})
				if cachedSG.Name == "secGroupId-nondefault" {
					Expect(actualSG).Should(BeNil())
					Expect(err).To(Equal(errors.NewNotFound(runtimev1alpha1.Resource("securitygroup"), cachedSG.Name)))
				} else {
					Expect(err).Should(BeNil())
					Expect(actualSG).To(Equal(expectedSGs[i]))
				}
			}
		})
	})

	Describe("Test List function of Rest", func() {
		BeforeEach(func() {
			cloudInventory = inventory.InitInventory()
			sgMap := make(map[string]*runtimev1alpha1.SecurityGroup)
			for _, cachedSG := range cachedSGs {
				sgMap[cachedSG.Name] = cachedSG
			}
			accountNamespacedName := types.NamespacedName{Namespace: cacheTest1.Namespace, Name: "accountID"}
			namespacedName := types.NamespacedName{Namespace: accountNamespacedName.Namespace, Name: "selector01"}
			cloudInventory.BuildSgCache(sgMap, &accountNamespacedName, &namespacedName)
		})

		expectedSGList1 := &runtimev1alpha1.SecurityGroupList{
			Items: []runtimev1alpha1.SecurityGroup{
				*cacheTest1,
				*cacheTest2,
				*cacheTest3,
			},
		}
		expectedSGList2 := &runtimev1alpha1.SecurityGroupList{
			Items: []runtimev1alpha1.SecurityGroup{
				*cacheTest1,
				*cacheTest3,
			},
		}
		expectedSGLists := []*runtimev1alpha1.SecurityGroupList{
			expectedSGList1,
			expectedSGList2,
		}

		listLabelSelectorOption1 := &internalversion.ListOptions{}
		// select sgs based on account name.
		req, _ := labels.NewRequirement(nephelabels.CloudAccountName, selection.Equals,
			[]string{accountNamespacedName.Name})
		labelSelector := labels.NewSelector().Add(*req)
		listLabelSelectorOption2 := &internalversion.ListOptions{LabelSelector: labelSelector}
		sgLabelSelectorListOptions := []*internalversion.ListOptions{
			listLabelSelectorOption1,
			listLabelSelectorOption2,
		}
		It("Should return the SG list by labels", func() {
			for i, sgListOption := range sgLabelSelectorListOptions {
				rest := NewREST(cloudInventory, l)
				var actualObj runtime.Object
				var err error
				if i == 0 {
					// list all objects.
					actualObj, err = rest.List(context.TODO(), sgListOption)
				} else {
					actualObj, err = rest.List(request.NewDefaultContext(), sgListOption)
				}
				Expect(err).Should(BeNil())
				items := actualObj.(*runtimev1alpha1.SecurityGroupList).Items
				sort.SliceStable(items, func(i, j int) bool {
					return items[i].Name < items[j].Name
				})

				Expect(actualObj.(*runtimev1alpha1.SecurityGroupList)).To(Equal(expectedSGLists[i]))
			}
		})
		It("Should return error for invalid labels,", func() {
			req2, _ := labels.NewRequirement(nephelabels.CloudVpcUID, selection.Equals,
				[]string{"dummy"})
			labelSelector2 := labels.NewSelector().Add(*req2)
			listLabelSelectorOption := &internalversion.ListOptions{LabelSelector: labelSelector2}
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.NewDefaultContext(), listLabelSelectorOption)
			Expect(err).ShouldNot(BeNil())
		})
		It("Should return the SG list by fields", func() {
			expectedSGList3 := &runtimev1alpha1.SecurityGroupList{
				Items: []runtimev1alpha1.SecurityGroup{
					*cacheTest1,
				},
			}
			expectedSGList4 := &runtimev1alpha1.SecurityGroupList{
				Items: []runtimev1alpha1.SecurityGroup{
					*cacheTest2,
				},
			}
			expectedSGList5 := &runtimev1alpha1.SecurityGroupList{
				Items: []runtimev1alpha1.SecurityGroup{
					*cacheTest3,
				},
			}
			listFieldSelectorOption1 := &internalversion.ListOptions{}
			listFieldSelectorOption1.FieldSelector = fields.OneTermEqualSelector("metadata.name", "secGroupId")
			listFieldSelectorOption2 := &internalversion.ListOptions{}
			listFieldSelectorOption2.FieldSelector = fields.OneTermEqualSelector("metadata.namespace", "non-default")
			fieldSelector3 := fields.SelectorFromSet(fields.Set{"status.cloudId": "cloudId3"})
			listFieldSelectorOption3 := &internalversion.ListOptions{FieldSelector: fieldSelector3}

			By("Using field selector metadata name")
			rest := NewREST(cloudInventory, l)
			actualObj, err := rest.List(request.NewDefaultContext(), listFieldSelectorOption1)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedSGList3))

			By("Using field selector metadata namespace")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "non-default"),
				listFieldSelectorOption2)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedSGList4))

			By("Using field selector cloudId")
			actualObj, err = rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption3)
			Expect(err).Should(BeNil())
			Expect(actualObj).To(Equal(expectedSGList5))
		})
		It("Should return error for invalid Fields,", func() {
			fieldSelector4 := fields.SelectorFromSet(fields.Set{"status.cloudName": "cloudName3"})
			listFieldSelectorOption4 := &internalversion.ListOptions{FieldSelector: fieldSelector4}
			rest := NewREST(cloudInventory, l)
			_, err := rest.List(request.WithNamespace(request.NewContext(), "default"),
				listFieldSelectorOption4)
			Expect(err).ShouldNot(BeNil())
		})
	})
	Describe("Test ConvertTable function of Rest", func() {
		rule1 := runtimev1alpha1.Rule{
			Action:      "allow",
			Description: "rule description2",
			Destination: []string{"10.0.0.1"},
			Id:          "100",
			Ingress:     true,
			Name:        "rule1",
			Port:        "80",
			Priority:    100,
			Protocol:    "tcp",
			Source:      []string{"20.0.0.1"},
		}
		rule2 := runtimev1alpha1.Rule{
			Action:      "deny",
			Description: "rule description2",
			Destination: []string{"10.0.0.10"},
			Id:          "100",
			Ingress:     false,
			Name:        "rule2",
			Port:        "80",
			Priority:    100,
			Protocol:    "tcp",
			Source:      []string{"20.0.0.10"},
		}
		var rules []runtimev1alpha1.Rule
		rules = append(rules, rule1)
		rules = append(rules, rule2)
		cacheTest4 := &runtimev1alpha1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "non-default1",
				Name:      "secGroupId3",
				Labels: map[string]string{
					nephelabels.CloudAccountNamespace:  accountNamespacedName.Namespace,
					nephelabels.CloudAccountName:       accountNamespacedName.Name,
					nephelabels.VpcName:                "testNetworkID",
					nephelabels.CloudSelectorName:      "selector01",
					nephelabels.CloudSelectorNamespace: "non-default1",
				},
			},
			Status: runtimev1alpha1.SecurityGroupStatus{
				Provider:  runtimev1alpha1.AWSCloudProvider,
				Region:    "test-region",
				CloudId:   "cloudId",
				CloudName: "cloudName",
				Rules:     rules,
			},
		}
		It("Convert SG objects in table format", func() {
			expectedTables := &metav1.Table{
				ColumnDefinitions: []metav1.TableColumnDefinition{
					{Name: "NAME", Type: "string", Description: "Name"},
					{Name: "CLOUD-PROVIDER", Type: "string", Description: "Cloud Provider"},
					{Name: "REGION", Type: "string", Description: "Region"},
					{Name: "VIRTUAL-PRIVATE-CLOUD", Type: "string", Description: "VPC/VNET"},
					{Name: "INGRESS-RULES", Type: "int", Description: "Number of Ingress Rules"},
					{Name: "EGRESS-RULES", Type: "int", Description: "Number of egress Rules"},
				},
				Rows: []metav1.TableRow{
					{
						Cells: []interface{}{"secGroupId3", runtimev1alpha1.AWSCloudProvider, "test-region", "testNetworkID", 1, 1},
					},
				},
			}
			sgMap := make(map[string]*runtimev1alpha1.SecurityGroup)
			sgMap[cacheTest4.Name] = cacheTest4
			namespacedName := types.NamespacedName{Namespace: cacheTest4.Labels[nephelabels.CloudSelectorNamespace],
				Name: cacheTest4.Labels[nephelabels.CloudSelectorName]}
			cloudInventory.BuildSgCache(sgMap, &accountNamespacedName, &namespacedName)

			rest := NewREST(cloudInventory, l)
			actualTable, err := rest.ConvertToTable(request.NewDefaultContext(), cacheTest4, &metav1.TableOptions{})
			Expect(err).Should(BeNil())
			Expect(actualTable.ColumnDefinitions).To(Equal(expectedTables.ColumnDefinitions))
			Expect(actualTable.Rows[0].Cells).To(Equal(expectedTables.Rows[0].Cells))
		})
	})
})
