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

	"github.com/Azure/azure-sdk-for-go/services/resourcegraph/mgmt/2021-03-01/resourcegraph"
	"github.com/Azure/go-autorest/autorest"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"antrea.io/nephe/apis/crd/v1alpha1"
)

var _ = Describe("Azure", func() {
	var (
		testAccountNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		testSubID                 = "SubID"
		credentials               = "credentials"
		testClientID              = "ClientID"
		testClientKey             = "ClientKey"
		testTenantID              = "TenantID"
		testRegion                = "eastus"
		testRG                    = "testRG"

		testVnet01   = "testVnet01"
		testVnetID01 = fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v",
			testSubID, testRG, testVnet01)
		testVnet02   = "testVnet02"
		testVnetID02 = fmt.Sprintf("/subscriptions/%v/resourceGroups/%v/providers/Microsoft.Network/virtualNetworks/%v",
			testSubID, testRG, testVnet02)
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

			subIDs    []string
			tenantIDs []string
			locations []string
			vnetIDs   []string
		)
		_ = account

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
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName: testAccountNamespacedName.Name,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testVnet01,
							},
							VMMatch: []v1alpha1.EntityMatch{},
						},
					},
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

			mockAzureServiceHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockazureService, nil).Times(1)
			mockazureService.EXPECT().networkInterfaces(gomock.Any()).Return(mockazureNwIntfWrapper, nil).AnyTimes()
			mockazureService.EXPECT().securityGroups(gomock.Any()).Return(mockazureNsgWrapper, nil).AnyTimes()
			mockazureService.EXPECT().applicationSecurityGroups(gomock.Any()).Return(mockazureAsgWrapper, nil).AnyTimes()
			mockazureService.EXPECT().virtualNetworks(gomock.Any()).Return(mockazureVirtualNetworksWrapper, nil).AnyTimes()
			mockazureService.EXPECT().resourceGraph().Return(mockazureResourceGraph, nil)
			mockazureVirtualNetworksWrapper.EXPECT().listAllComplete(gomock.Any()).AnyTimes()
			mockazureResourceGraph.EXPECT().resources(gomock.Any(), gomock.Any()).Return(getResourceGraphResult(), nil).AnyTimes()

			fakeClient = fake.NewClientBuilder().Build()
			c = newAzureCloud(mockAzureServiceHelper)
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})

		Context("VM Selector scenarios", func() {
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

				err := fakeClient.Create(context.Background(), secret)
				Expect(err).Should(BeNil())
				err = c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				selector.Spec.VMSelector = vmSelector
				err = c.AddAccountResourceSelector(testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				accCfg, _ := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
				serviceConfig, _ := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
				filters := serviceConfig.(*computeServiceConfig).computeFilters[selector.Name]
				Expect(filters).To(Equal(expectedQueryStrs))
			})
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

			err := fakeClient.Create(context.Background(), secret)
			Expect(err).Should(BeNil())
			err = c.AddProviderAccount(fakeClient, account)
			Expect(err).Should(BeNil())
			selector.Spec.VMSelector = vmSelector
			err = c.AddAccountResourceSelector(testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
			serviceConfig, _ := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
			filters := serviceConfig.(*computeServiceConfig).computeFilters[selector.Name]
			Expect(filters).To(Equal(expectedQueryStrs))
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

			err := fakeClient.Create(context.Background(), secret)
			Expect(err).Should(BeNil())
			err = c.AddProviderAccount(fakeClient, account)
			Expect(err).Should(BeNil())
			selector.Spec.VMSelector = vmSelector
			err = c.AddAccountResourceSelector(testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(testAccountNamespacedName)
			serviceConfig, _ := accCfg.GetServiceConfigByName(azureComputeServiceNameCompute)
			filters := serviceConfig.(*computeServiceConfig).computeFilters[selector.Name]
			Expect(filters).To(Equal(expectedQueryStrs))
		})
	})
})

func getResourceGraphResult() resourcegraph.QueryResponse {
	var records int64 = 0
	result := resourcegraph.QueryResponse{
		Response:        autorest.Response{},
		TotalRecords:    &records,
		Count:           nil,
		ResultTruncated: "",
		SkipToken:       nil,
		Data:            nil,
		Facets:          nil,
	}
	return result
}
