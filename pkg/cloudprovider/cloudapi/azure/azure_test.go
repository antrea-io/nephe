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

package azure

import (
	"context"
	"fmt"

	network "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	resourcegraph "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudapi/common"
)

var (
	region = "eastus"
)

var _ = Describe("Azure", func() {
	var (
		testAccountNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		testSubID                 = "SubID"
		credentials               = "credentials"
		testClientID              = "ClientID"
		testClientKey             = "ClientKey"
		testTenantID              = "TenantID"
		testRegion                = region
		testRG                    = "testRG"
		azureProvideType          = "Azure"

		testVnet01   = "testVnet01"
		testVnetID01 = fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v",
			testSubID, testRG, testVnet01)
		testVnet02   = "testVnet02"
		testVnetID02 = fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v",
			testSubID, testRG, testVnet02)

		testVM01   = "testVM01"
		testVMID01 = fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualMachines/%v",
			testSubID, testRG, testVM01)
	)

	Context("AddAccountResourceSelector", func() {
		var (
			c          *azureCloud
			account    *v1alpha1.CloudProviderAccount
			selector   *v1alpha1.CloudEntitySelector
			secret     *corev1.Secret
			fakeClient client.WithWatch

			mockCtrl                        *gomock.Controller
			mockAzureServiceHelper          *MockazureServicesHelper
			mockazureNwIntfWrapper          *MockazureNwIntfWrapper
			mockazureNsgWrapper             *MockazureNsgWrapper
			mockazureAsgWrapper             *MockazureAsgWrapper
			mockazureVirtualNetworksWrapper *MockazureVirtualNetworksWrapper
			mockazureResourceGraph          *MockazureResourceGraphWrapper
			mockazureService                *MockazureServiceClientCreateInterface
			testSelectorNamespacedName      = &types.NamespacedName{Namespace: "namespace01", Name: "selector-VnetID"}

			subIDs    []string
			tenantIDs []string
			locations []string
			vnetIDs   []string
			vmIDs     []string
		)

		BeforeEach(func() {
			subIDs = []string{testSubID}
			tenantIDs = []string{testTenantID}
			locations = []string{testRegion}
			var pollIntv uint = 2
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AzureConfig: &v1alpha1.CloudProviderAccountAzureConfig{
						Region: testRegion,
						SecretRef: &v1alpha1.SecretReference{
							Name:      testAccountNamespacedName.Name,
							Namespace: testAccountNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}

			credential := fmt.Sprintf(`{"subscriptionId": "%s",
				"clientId": "%s",
				"tenantId": "%s",
				"clientKey": "%s"
			}`, testSubID, testClientID, testTenantID, testClientKey)

			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}

			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      "selector-VnetID",
					Namespace: testSelectorNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
				},
			}
			mockCtrl = gomock.NewController(GinkgoT())
			mockAzureServiceHelper = NewMockazureServicesHelper(mockCtrl)

			mockazureService = NewMockazureServiceClientCreateInterface(mockCtrl)
			mockazureNwIntfWrapper = NewMockazureNwIntfWrapper(mockCtrl)
			mockazureNsgWrapper = NewMockazureNsgWrapper(mockCtrl)
			mockazureAsgWrapper = NewMockazureAsgWrapper(mockCtrl)
			mockazureVirtualNetworksWrapper = NewMockazureVirtualNetworksWrapper(mockCtrl)
			mockazureResourceGraph = NewMockazureResourceGraphWrapper(mockCtrl)

			mockAzureServiceHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockazureService, nil).AnyTimes()
			mockazureService.EXPECT().networkInterfaces(gomock.Any()).Return(mockazureNwIntfWrapper, nil).AnyTimes()
			mockazureService.EXPECT().securityGroups(gomock.Any()).Return(mockazureNsgWrapper, nil).AnyTimes()
			mockazureService.EXPECT().applicationSecurityGroups(gomock.Any()).Return(mockazureAsgWrapper, nil).AnyTimes()
			mockazureService.EXPECT().virtualNetworks(gomock.Any()).Return(mockazureVirtualNetworksWrapper, nil).AnyTimes()
			mockazureService.EXPECT().resourceGraph().Return(mockazureResourceGraph, nil).AnyTimes()
			mockazureResourceGraph.EXPECT().resources(gomock.Any(), gomock.Any()).Return(getResourceGraphResult(), nil).AnyTimes()

			fakeClient, c = setupClientAndCloud(mockAzureServiceHelper, account, secret)

		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		Context("Account Add and Delete scenarios", func() {
			It("On account add expect cloud api call for retrieving vpc list", func() {
				vnetIDs := []string{"testVnetID01", "testVnetID02"}
				mockazureVirtualNetworksWrapper.EXPECT().listAllComplete(gomock.Any()).Return(createVnetObject(vnetIDs), nil).AnyTimes()
				credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						"credentials": []byte(credential),
					},
				}

				_ = fakeClient.Create(context.Background(), secret)
				c := newAzureCloud(mockAzureServiceHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				vnetMap, err := c.GetVpcInventory(testAccountNamespacedName)
				Expect(err).Should(BeNil())
				Expect(len(vnetMap)).Should(Equal(len(vnetIDs)))
			})
			It("StopPoller cloud inventory poll on poller delete", func() {
				vnetIDs := []string{"testVnetID01", "testVnetID02"}
				mockazureVirtualNetworksWrapper.EXPECT().listAllComplete(gomock.Any()).Return(createVnetObject(vnetIDs), nil).MinTimes(1)
				credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret"}`
				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						"credentials": []byte(credential),
					},
				}

				_ = fakeClient.Create(context.Background(), secret)
				c := newAzureCloud(mockAzureServiceHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())
				errPolDel := c.DeleteInventoryPollCache(testAccountNamespacedName)
				Expect(errPolDel).Should(BeNil())
				mockazureVirtualNetworksWrapper.EXPECT().listAllComplete(gomock.Any()).Return(createVnetObject(vnetIDs), nil).MinTimes(0)
			})
		})
		Context("VM Selector scenarios", func() {
			BeforeEach(func() {
				mockazureVirtualNetworksWrapper.EXPECT().listAllComplete(gomock.Any()).AnyTimes()
			})

			It("Should match expected filter - single vpcID only match", func() {
				vnetIDs = []string{testVnetID01}
				var expectedQueryStrs []*string
				expectedQueryStr, _ := getVMsByVnetIDsMatchQuery(vnetIDs,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID01},
						VMMatch:  []v1alpha1.EntityMatch{},
					},
				}

				providerType := c.ProviderType()
				Expect(providerType).Should(Equal(common.ProviderType(azureProvideType)))
				selector.Name = "single-vpcIDOnly"
				selector.Spec.VMSelector = vmSelector
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))

				_, err = c.InstancesGivenProviderAccount(testAccountNamespacedName)
				Expect(err).Should(BeNil())

				_ = c.GetEnforcedSecurity()
			})

			It("Should match expected filter with credential - multiple vpcID only match", func() {
				vnetIDs = []string{testVnetID01, testVnetID02}
				var expectedQueryStrs []*string
				expectedQueryStr, _ := getVMsByVnetIDsMatchQuery(vnetIDs,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID01},
						VMMatch:  []v1alpha1.EntityMatch{},
					},
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID02},
						VMMatch:  []v1alpha1.EntityMatch{},
					},
				}

				selector.Spec.VMSelector = vmSelector
				selector.Name = "multiple-vpcIDOnly"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Should match expected filter - multiple with one all", func() {
				var expectedQueryStrs []*string
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID01},
					},
					{
						VpcMatch: nil,
						VMMatch:  []v1alpha1.EntityMatch{},
					},
				}
				selector.Spec.VMSelector = vmSelector
				selector.Name = "multiple-with-one-all"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Should match expected filter - single VMID only match", func() {
				vmIDs = []string{testVMID01}
				var expectedQueryStrs []*string
				expectedQueryStr, _ := getVMsByVMIDsMatchQuery(vmIDs,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VMMatch: []v1alpha1.EntityMatch{{MatchID: testVMID01}},
					},
				}

				selector.Spec.VMSelector = vmSelector
				selector.Name = "VMIDOnly"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Should match expected filter - single VM Name only match", func() {
				vmNames := []string{testVM01}
				var expectedQueryStrs []*string
				expectedQueryStr, _ := getVMsByVMNamesMatchQuery(vmNames,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)

				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VMMatch: []v1alpha1.EntityMatch{{MatchName: testVM01}},
					},
				}

				selector.Spec.VMSelector = vmSelector
				selector.Name = "VMNameOnly"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Should match expected filter - vpcID with VMID match", func() {
				vnetIDs = []string{testVnetID01}
				vmIDs = []string{testVMID01}
				vmNames := []string{}
				var expectedQueryStrs []*string
				expectedQueryStr, _ := getVMsByVnetAndOtherMatchesQuery(vnetIDs, vmNames, vmIDs,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VMMatch:  []v1alpha1.EntityMatch{{MatchID: testVMID01}},
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID01},
					},
				}

				selector.Spec.VMSelector = vmSelector
				selector.Name = "vpcID-VMID"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Should match expected filter - vpcID with VM Name match", func() {
				vnetIDs = []string{testVnetID01}
				vmIDs = []string{}
				vmNames := []string{testVM01}
				var expectedQueryStrs []*string

				expectedQueryStr, _ := getVMsByVnetAndOtherMatchesQuery(vnetIDs, vmNames, vmIDs,
					subIDs, tenantIDs, locations)
				expectedQueryStrs = append(expectedQueryStrs, expectedQueryStr)
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VMMatch:  []v1alpha1.EntityMatch{{MatchName: testVM01}},
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVnetID01},
					},
				}

				selector.Spec.VMSelector = vmSelector
				selector.Name = "vpcID-VMName"
				testSelectorNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: selector.Name}
				err := c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName.String())
				Expect(filters).To(Equal(expectedQueryStrs))

				c.RemoveAccountResourcesSelector(testAccountNamespacedName, testSelectorNamespacedName.String())
				expectedQueryStrs = expectedQueryStrs[:len(expectedQueryStrs)-1]
				filters = getFilters(c, testSelectorNamespacedName.String())
				Expect(len(filters)).To(Equal(len(expectedQueryStrs)))
			})

			It("Update Secret", func() {
				credential2 := fmt.Sprintf(`{"subscriptionId": "%s",
				"clientId": "%s",
				"tenantId": "%s",
				"clientKey": "%s"
			}`, "testSubID01", "testClientID", "testTenantID", "testClientKey")

				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						"credentials": []byte(credential2),
					},
				}
				err := fakeClient.Update(context.Background(), secret)
				Expect(err).Should(BeNil())
				err = c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
			})
		})

		Context("VM Provider scenarios", func() {
			It("Remove Provider Account", func() {
				c.RemoveProviderAccount(testAccountNamespacedName)

				accCfg, ok := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
				Expect(accCfg).Should(BeNil())
				Expect(ok).To(Equal(false))
			})
		})
	})
})

func getResourceGraphResult() resourcegraph.ClientResourcesResponse {
	var records int64 = 0
	result := resourcegraph.ClientResourcesResponse{
		QueryResponse: resourcegraph.QueryResponse{
			TotalRecords:    &records,
			Count:           nil,
			ResultTruncated: nil,
			SkipToken:       nil,
			Data:            nil,
			Facets:          nil,
		},
	}

	return result
}

func getFilters(c *azureCloud, selectorNamespacedName string) []*string {
	accCfg, _ := c.cloudCommon.GetCloudAccountByName(&types.NamespacedName{Namespace: "namespace01", Name: "account01"})
	serviceConfig, _ := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
	filters := serviceConfig.(*computeServiceConfig).computeFilters[selectorNamespacedName]
	return filters
}
func setupClientAndCloud(mockAzureServiceHelper *MockazureServicesHelper, account *v1alpha1.CloudProviderAccount, secret *corev1.Secret) (
	client.WithWatch, *azureCloud) {
	fakeClient := fake.NewClientBuilder().Build()
	c := newAzureCloud(mockAzureServiceHelper)

	err := fakeClient.Create(context.Background(), secret)
	Expect(err).Should(BeNil())
	err = c.AddProviderAccount(fakeClient, account)
	Expect(err).Should(BeNil())
	return fakeClient, c
}

func createVnetObject(vnetIDs []string) []network.VirtualNetwork {
	var vnets []network.VirtualNetwork
	for i := range vnetIDs {
		name := vnetIDs[i]
		key := "Name"
		value := vnetIDs[i]
		tags := make(map[string]*string)
		tags[key] = &value
		addressPrefix := make([]*string, 0)
		prefix := "192.16.0.0/24"
		addressPrefix = append(addressPrefix, &prefix)
		vnet := &network.VirtualNetwork{
			Location: &region,
			ID:       &vnetIDs[i],
			Name:     &name,
			Tags:     tags,
			Properties: &network.VirtualNetworkPropertiesFormat{
				AddressSpace:           &network.AddressSpace{AddressPrefixes: addressPrefix},
				VirtualNetworkPeerings: []*network.VirtualNetworkPeering{},
			},
		}
		vnets = append(vnets, *vnet)
	}

	return vnets
}
