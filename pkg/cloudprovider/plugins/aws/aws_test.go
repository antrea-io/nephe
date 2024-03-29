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

package aws

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"antrea.io/nephe/apis/crd/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/plugins/internal"
)

var (
	testVpcID01 = "vpc-cb82c3b2"
	testSgID    = 1
	testRegion  = "us-east-1"
	testRegion2 = "us-east-2"
)

var _ = Describe("AWS cloud", func() {
	var (
		testAccountNamespacedName  = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		testSelectorNamespacedName = types.NamespacedName{Namespace: "namespace02", Name: "selector01"}
		credentials                = "credentials"
	)

	Context("AddProviderAccount", func() {
		var (
			account            *v1alpha1.CloudProviderAccount
			mockCtrl           *gomock.Controller
			mockawsCloudHelper *MockawsServicesHelper
			secret             *corev1.Secret
			fakeClient         client.WithWatch
		)

		BeforeEach(func() {
			var pollIntv uint = 1
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{testRegion},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testAccountNamespacedName.Name,
							Namespace: testAccountNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret","roleArn" : "roleArn","externalID" : "" }`
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}
			fakeClient = fake.NewClientBuilder().Build()
			mockCtrl = gomock.NewController(GinkgoT())
			mockawsCloudHelper = NewMockawsServicesHelper(mockCtrl)
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})
		Context("New account add success scenarios", func() {
			var (
				selector *v1alpha1.CloudEntitySelector

				mockawsService *MockawsServiceClientCreateInterface
				mockawsEC2     *MockawsEC2Wrapper
			)

			BeforeEach(func() {
				selector = &v1alpha1.CloudEntitySelector{
					ObjectMeta: v1.ObjectMeta{
						Name:      "vpc-match-selector",
						Namespace: testAccountNamespacedName.Namespace,
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName:      testAccountNamespacedName.Name,
						AccountNamespace: testAccountNamespacedName.Namespace,
						VMSelector: []v1alpha1.VirtualMachineSelector{
							{
								VpcMatch: &v1alpha1.EntityMatch{
									MatchID: "xyz",
								},
							},
						},
					},
				}

				mockawsService = NewMockawsServiceClientCreateInterface(mockCtrl)
				mockawsEC2 = NewMockawsEC2Wrapper(mockCtrl)

				mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion).Return(mockawsService, nil).AnyTimes()
				mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()
				mockawsEC2.EXPECT().getRegion().Return(testRegion).AnyTimes()
			})
			It("On account add expect cloud api call for retrieving vpc list", func() {
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
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				err = checkVpcPollResult(c, testAccountNamespacedName, vpcIDs)
				Expect(err).Should(BeNil())
			})
			It("Add multiple regions account should poll vpc from both regions", func() {
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
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				vpcIDs2 := []string{"testVpcID03", "testVpcID04"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).Times(1)
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				// setting mock for second region service config.
				mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
				mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
				mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
				mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs2), nil).Times(1)
				mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				// adding second region.
				account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))
				Expect(len(accCfg.GetAllServiceConfigs())).To(Equal(2))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				err = checkVpcPollResult(c, testAccountNamespacedName, append(vpcIDs, vpcIDs2...))
				Expect(err).Should(BeNil())
			})
			It("Add new region to existing account should poll vpc from both regions", func() {
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
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))
				Expect(len(accCfg.GetAllServiceConfigs())).To(Equal(1))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				err = checkVpcPollResult(c, testAccountNamespacedName, vpcIDs)
				Expect(err).Should(BeNil())

				vpcIDs2 := []string{"testVpcID03", "testVpcID04"}

				// setting mock for second region service config.
				mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
				mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
				mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
				mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs2), nil).Times(1)
				mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()

				// adding second region in account.
				account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)
				err = c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err = c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))
				Expect(len(accCfg.GetAllServiceConfigs())).To(Equal(2))

				errPolAdd = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				err = checkVpcPollResult(c, testAccountNamespacedName, append(vpcIDs, vpcIDs2...))
				Expect(err).Should(BeNil())
			})
			It("Remove region from account should poll vpc from region left", func() {
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
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				vpcIDs2 := []string{"testVpcID03", "testVpcID04"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				// setting mock for second region service config.
				mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
				mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
				mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
				mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs2), nil).Times(1)
				mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)
				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))
				Expect(len(accCfg.GetAllServiceConfigs())).To(Equal(2))
				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())
				err = checkVpcPollResult(c, testAccountNamespacedName, append(vpcIDs, vpcIDs2...))
				Expect(err).Should(BeNil())

				// removing a region from account.
				account.Spec.AWSConfig.Region = account.Spec.AWSConfig.Region[:1]

				err = c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err = c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))
				Expect(len(accCfg.GetAllServiceConfigs())).To(Equal(1))
				errPolAdd = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())
				err = checkVpcPollResult(c, testAccountNamespacedName, vpcIDs)
				Expect(err).Should(BeNil())
			})
			It("Fetch vpc list from snapshot", func() {
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
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				cloudInventory, err := c.GetAccountCloudInventory(&testAccountNamespacedName)
				Expect(err).Should(BeNil())
				Expect(len(cloudInventory.VpcMap)).Should(Equal(len(vpcIDs)))
			})
			It("StopPoller cloud inventory poll on poller delete", func() {
				credential := `{"accessKeyId": "keyId","accessKeySecret": "keySecret", "sessionToken": "token"}`

				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						"credentials": []byte(credential),
					},
				}
				instanceIds := []string{}
				vpcIDs := []string{"testVpcID01", "testVpcID02"}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				errPolDel := c.ResetInventoryCache(&testAccountNamespacedName)
				Expect(errPolDel).Should(BeNil())

				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).Times(0)
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).Times(0)
			})
			It("Should discover instances when selector is in different namespace from account", func() {
				instanceIds := []string{"i-01", "i-02"}
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

				selector = &v1alpha1.CloudEntitySelector{
					ObjectMeta: v1.ObjectMeta{
						Name:      testSelectorNamespacedName.Name,
						Namespace: testSelectorNamespacedName.Namespace,
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName:      testAccountNamespacedName.Name,
						AccountNamespace: testAccountNamespacedName.Namespace,
						VMSelector: []v1alpha1.VirtualMachineSelector{
							{
								VpcMatch: &v1alpha1.EntityMatch{
									MatchID: "xyz",
								},
							},
						},
					},
				}

				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)

				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))

				errSelAdd := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(errSelAdd).Should(BeNil())

				err = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(err).Should(BeNil())

				err = checkAccountAddSuccessCondition(c, testAccountNamespacedName, testSelectorNamespacedName, instanceIds)
				Expect(err).Should(BeNil())
			})
			It("Should discover no instances with get ALL selector", func() {
				instanceIds := []string{}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).AnyTimes()
				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(BeNil())
				Expect(accCfg).To(Not(BeNil()))

				errSelAdd := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(errSelAdd).Should(BeNil())

				err = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(err).Should(BeNil())

				err = checkAccountAddSuccessCondition(c, testAccountNamespacedName, testSelectorNamespacedName, instanceIds)
				Expect(err).Should(BeNil())
			})
		})

		Context("New account add fail scenarios", func() {
			It("Should fail with invalid region", func() {
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
				account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, "invalidRegion")

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).To(Not(BeNil()))
				accCfg, err := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(err).To(Not(BeNil()))
				Expect(accCfg).To(Not(BeNil()))
			})
		})
	})

	Context("AddAccountResourceSelector", func() {
		const (
			testVpcID01 = "vpc-01"
			testVpcID02 = "vpc-02"

			testVpcName01 = "vpcName-01"
			testVpcName02 = "vpcName-02"

			testVMName01 = "vmName-01"
			testVMName02 = "vmName-02"

			testVMID01 = "vmID-01"
			testVMID02 = "vmID-02"
		)
		var (
			account                    *v1alpha1.CloudProviderAccount
			selector                   *v1alpha1.CloudEntitySelector
			mockCtrl                   *gomock.Controller
			mockawsCloudHelper         *MockawsServicesHelper
			fakeClient                 client.Client
			mockawsEC2                 *MockawsEC2Wrapper
			mockawsService             *MockawsServiceClientCreateInterface
			secret                     *corev1.Secret
			testSelectorNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "selector-VpcID"}
		)

		BeforeEach(func() {
			var pollIntv uint = 2
			account = &v1alpha1.CloudProviderAccount{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudProviderAccountSpec{
					PollIntervalInSeconds: &pollIntv,
					AWSConfig: &v1alpha1.CloudProviderAccountAWSConfig{
						Region: []string{testRegion},
						SecretRef: &v1alpha1.SecretReference{
							Name:      testAccountNamespacedName.Name,
							Namespace: testAccountNamespacedName.Namespace,
							Key:       credentials,
						},
					},
				},
			}
			selector = &v1alpha1.CloudEntitySelector{
				ObjectMeta: v1.ObjectMeta{
					Name:      "selector-VpcID",
					Namespace: testAccountNamespacedName.Namespace,
				},
				Spec: v1alpha1.CloudEntitySelectorSpec{
					AccountName:      testAccountNamespacedName.Name,
					AccountNamespace: testAccountNamespacedName.Namespace,
					VMSelector: []v1alpha1.VirtualMachineSelector{
						{
							VpcMatch: &v1alpha1.EntityMatch{
								MatchID: testVpcID01,
							},
							VMMatch: []v1alpha1.EntityMatch{},
						},
					},
				},
			}
			credential := `{"accessKeyId": "","accessKeySecret": "","roleArn" : "roleArn","externalID" : "" }`
			secret = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      testAccountNamespacedName.Name,
					Namespace: testAccountNamespacedName.Namespace,
				},
				Data: map[string][]byte{
					"credentials": []byte(credential),
				},
			}
			mockCtrl = gomock.NewController(GinkgoT())
			mockawsCloudHelper = NewMockawsServicesHelper(mockCtrl)

			mockawsService = NewMockawsServiceClientCreateInterface(mockCtrl)
			mockawsEC2 = NewMockawsEC2Wrapper(mockCtrl)

			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion).Return(mockawsService, nil).Times(1)
			mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()

			instanceIds := []string{testVMID01}
			vpcIds := []string{testVpcID01}
			mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID01), nil).AnyTimes()
			mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
			mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIds), nil).AnyTimes()
			mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).AnyTimes()
			mockawsEC2.EXPECT().getRegion().Return(testRegion).AnyTimes()
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).AnyTimes()
		})

		AfterEach(func() {
			mockCtrl.Finish()
		})
		setAwsAccount := func(mockawsCloudHelper *MockawsServicesHelper) *awsCloud {
			fakeClient = fake.NewClientBuilder().Build()
			_ = fakeClient.Create(context.Background(), secret)
			c1 := newAWSCloud(mockawsCloudHelper)
			_ = c1.AddProviderAccount(fakeClient, account)
			return c1
		}

		Context("VM Selector scenarios", func() {
			It("Should match expected filter - single vpcID only match", func() {
				c := setAwsAccount(mockawsCloudHelper)
				var expectedFilters [][]*ec2.Filter
				var vpcFilters []*ec2.Filter
				vpc01Filter := &ec2.Filter{
					Name:   aws.String(awsFilterKeyVPCID),
					Values: []*string{aws.String(testVpcID01)},
				}
				vpcFilters = append(vpcFilters, vpc01Filter, buildEc2FilterForValidInstanceStates())
				expectedFilters = append(expectedFilters, vpcFilters)

				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
						VMMatch:  []v1alpha1.EntityMatch{},
					},
				}

				selector.Spec.VMSelector = vmSelector
				err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())

				filters := getFilters(c, testSelectorNamespacedName)
				Expect(filters).To(Equal(expectedFilters))
			})
		})

		Context("SecurityGroup snapshot update scenarios", func() {
			It("create snapshots", func() {
				c := newAWSCloud(mockawsCloudHelper)
				//err := fakeClient.Create(context.Background(), secret)
				//Expect(err).Should(BeNil())
				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				vmSelector := []v1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
						VMMatch:  []v1alpha1.EntityMatch{},
					},
				}
				selector.Spec.VMSelector = vmSelector
				err = c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(err).Should(BeNil())
				accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				serviceConfig := accCfg.GetServiceConfig(testRegion)
				selectorNamespacedName := types.NamespacedName{Namespace: selector.Namespace, Name: selector.Name}
				inventory := serviceConfig.(*ec2ServiceConfig).GetCloudInventory()
				Expect(len(inventory.VmMap[selectorNamespacedName])).To(Equal(0))
				Expect(len(inventory.VpcMap)).To(Equal(0))
				Expect(len(inventory.SgMap[selectorNamespacedName])).To(Equal(0))

				secGroup := ec2.SecurityGroup{}
				secGroupId := "secGroupId1"
				secGroupName := "secGroupName1"
				secGroupDescription := "security group example"
				secGroupVpcId := "vpcId01"
				ruleDescription := "rule description"
				srcPort := int64(22)
				dstPort := int64(8080)
				protocol := "tcp"
				fromIPv4 := "10.10.10.10"
				fromIPv6 := "2001:db8::"
				prefixList1 := "prefixList1"
				group1 := "sg-xyz"
				ipv4 := ec2.IpRange{CidrIp: &fromIPv4, Description: &ruleDescription}
				ipv6 := ec2.Ipv6Range{CidrIpv6: &fromIPv6, Description: &ruleDescription}
				prefixList := ec2.PrefixListId{
					PrefixListId: &prefixList1,
					Description:  &ruleDescription,
				}
				userGroup := ec2.UserIdGroupPair{
					GroupId:     &group1,
					GroupName:   &secGroupName,
					Description: &ruleDescription,
				}
				rule1 := ec2.IpPermission{
					FromPort:   &srcPort,
					IpProtocol: &protocol,
					ToPort:     &dstPort,
					IpRanges:   []*ec2.IpRange{&ipv4},
				}
				rule2 := ec2.IpPermission{
					FromPort:   &srcPort,
					IpProtocol: &protocol,
					ToPort:     &dstPort,
					Ipv6Ranges: []*ec2.Ipv6Range{&ipv6},
				}
				rule3 := ec2.IpPermission{
					FromPort:      &srcPort,
					IpProtocol:    &protocol,
					ToPort:        &dstPort,
					PrefixListIds: []*ec2.PrefixListId{&prefixList},
				}
				rule4 := ec2.IpPermission{
					FromPort:         &srcPort,
					IpProtocol:       &protocol,
					ToPort:           &dstPort,
					UserIdGroupPairs: []*ec2.UserIdGroupPair{&userGroup},
				}
				secGroup.GroupId = &secGroupId
				secGroup.GroupName = &secGroupName
				secGroup.VpcId = &secGroupVpcId
				secGroup.Description = &secGroupDescription
				secGroup.IpPermissions = append(secGroup.IpPermissions, &rule1)
				secGroup.IpPermissions = append(secGroup.IpPermissions, &rule2)
				secGroup.IpPermissions = append(secGroup.IpPermissions, &rule3)
				secGroup.IpPermissions = append(secGroup.IpPermissions, &rule4)
				secGroup.IpPermissionsEgress = append(secGroup.IpPermissionsEgress, &rule1)
				secGroup.IpPermissionsEgress = append(secGroup.IpPermissionsEgress, &rule2)
				secGroup.IpPermissionsEgress = append(secGroup.IpPermissionsEgress, &rule3)
				secGroup.IpPermissionsEgress = append(secGroup.IpPermissionsEgress, &rule4)
				sgs := make([]*ec2.SecurityGroup, 0)
				sgs = append(sgs, &secGroup)
				vmSnapshot := make(map[types.NamespacedName][]*ec2.Instance)
				sgSnapshot := make(map[types.NamespacedName][]*ec2.SecurityGroup)
				sgSnapshot[selectorNamespacedName] = sgs

				serviceConfig.(*ec2ServiceConfig).resourcesCache.UpdateSnapshot(&ec2ResourcesCacheSnapshot{
					vmSnapshot, nil, nil, nil, nil, sgSnapshot})
				inventory = serviceConfig.(*ec2ServiceConfig).GetCloudInventory()
				Expect(len(inventory.VmMap[selectorNamespacedName])).To(Equal(0))
				Expect(len(inventory.VpcMap)).To(Equal(0))
				Expect(len(inventory.SgMap[selectorNamespacedName])).To(Equal(1))

			})
		})
		It("Should match expected filter - multiple vpcID only match", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			var vpcFilters []*ec2.Filter
			vpc01Filter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVPCID),
				Values: []*string{aws.String(testVpcID01), aws.String(testVpcID02)},
			}
			vpcFilters = append(vpcFilters, vpc01Filter, buildEc2FilterForValidInstanceStates())
			expectedFilters = append(expectedFilters, vpcFilters)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID02},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should match expected filter - multiple vpcName only match", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			var vpcFilters []*ec2.Filter
			vpc01Filter := &ec2.Filter{
				Name:   aws.String(awsCustomFilterKeyVPCName),
				Values: []*string{aws.String(testVpcName01), aws.String(testVpcName02)},
			}
			vpcFilters = append(vpcFilters, vpc01Filter, buildEc2FilterForValidInstanceStates())
			expectedFilters = append(expectedFilters, vpcFilters)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchName: testVpcName01},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchName: testVpcName02},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())
			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should match expected filter - multiple vpcID & vmName match", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			var vpc01Filter []*ec2.Filter
			vpc01VpcFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVPCID),
				Values: []*string{aws.String(testVpcID01)},
			}
			vpc01VmFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVMName),
				Values: []*string{aws.String(testVMName01)},
			}
			vpc01Filter = append(vpc01Filter, vpc01VpcFilter, vpc01VmFilter, buildEc2FilterForValidInstanceStates())

			var vpc02Filter []*ec2.Filter
			vpc02VpcFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVPCID),
				Values: []*string{aws.String(testVpcID02)},
			}
			vpc02VmFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVMName),
				Values: []*string{aws.String(testVMName02)},
			}
			vpc02Filter = append(vpc02Filter, vpc02VpcFilter, vpc02VmFilter, buildEc2FilterForValidInstanceStates())

			expectedFilters = append(expectedFilters, vpc01Filter, vpc02Filter)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchName: testVMName01,
						},
					},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID02},
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchName: testVMName02,
						},
					},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should match expected filter - multiple with one all", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchName: testVMName01,
						},
					},
				},
				{
					VpcMatch: nil,
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())
			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should match expected filter - multiple vm names only match", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			var vmNameFilters []*ec2.Filter
			vmNameFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVMName),
				Values: []*string{aws.String(testVMName01), aws.String(testVMName02)},
			}
			vmNameFilters = append(vmNameFilters, vmNameFilter, buildEc2FilterForValidInstanceStates())
			expectedFilters = append(expectedFilters, vmNameFilters)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchName: testVMName01,
						},
					},
				},
				{
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchName: testVMName02,
						},
					},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should match expected filter - multiple vm IDs only match", func() {
			c := setAwsAccount(mockawsCloudHelper)
			var expectedFilters [][]*ec2.Filter
			var vmIDFilters []*ec2.Filter
			vmIDFilter := &ec2.Filter{
				Name:   aws.String(awsFilterKeyVMID),
				Values: []*string{aws.String(testVMID01), aws.String(testVMID02)},
			}
			vmIDFilters = append(vmIDFilters, vmIDFilter, buildEc2FilterForValidInstanceStates())
			expectedFilters = append(expectedFilters, vmIDFilters)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchID: testVMID01,
						},
					},
				},
				{
					VMMatch: []v1alpha1.EntityMatch{
						{
							MatchID: testVMID02,
						},
					},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			filters := getFilters(c, testSelectorNamespacedName)
			Expect(filters).To(Equal(expectedFilters))
		})
		It("Should poll vms with multiple regions and multiple vpcID only match", func() {
			fakeClient = fake.NewClientBuilder().Build()
			_ = fakeClient.Create(context.Background(), secret)
			c := newAWSCloud(mockawsCloudHelper)
			account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)
			instanceIds := []string{testVMID02}
			vpcIds := []string{testVpcID02}

			mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
			mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
			mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID02), nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
			mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIds), nil).Times(1)
			mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
				nil).AnyTimes()
			mockaws2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).MaxTimes(1)
			mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()

			_ = c.AddProviderAccount(fakeClient, account)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID02},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
			Expect(errPolAdd).Should(BeNil())

			err = checkVmPollResult(c, testAccountNamespacedName, append(instanceIds, testVMID01))
			Expect(err).Should(BeNil())
		})
		It("Should poll vms with region add and multiple vpcID only match", func() {
			fakeClient = fake.NewClientBuilder().Build()
			_ = fakeClient.Create(context.Background(), secret)
			c := newAWSCloud(mockawsCloudHelper)
			_ = c.AddProviderAccount(fakeClient, account)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID02},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())

			errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
			Expect(errPolAdd).Should(BeNil())

			err = checkVmPollResult(c, testAccountNamespacedName, []string{testVMID01})
			Expect(err).Should(BeNil())

			instanceIds := []string{testVMID02}
			vpcIds := []string{testVpcID02}
			mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
			mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
			mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID02), nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
			mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIds), nil).Times(1)
			mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
				nil).AnyTimes()
			mockaws2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).MaxTimes(1)
			mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()
			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion).Return(mockawsService, nil).Times(1)

			account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)
			_ = c.AddProviderAccount(fakeClient, account)

			errPolAdd = c.DoInventoryPoll(&testAccountNamespacedName)
			Expect(errPolAdd).Should(BeNil())

			err = checkVmPollResult(c, testAccountNamespacedName, append(instanceIds, testVMID01))
			Expect(err).Should(BeNil())
		})
		It("Should poll vms with region remove and multiple vpcID only match", func() {
			fakeClient = fake.NewClientBuilder().Build()
			_ = fakeClient.Create(context.Background(), secret)
			c := newAWSCloud(mockawsCloudHelper)
			account.Spec.AWSConfig.Region = append(account.Spec.AWSConfig.Region, testRegion2)
			instanceIds := []string{testVMID02}
			vpcIds := []string{testVpcID02}

			mockawsService2 := NewMockawsServiceClientCreateInterface(mockCtrl)
			mockaws2 := NewMockawsEC2Wrapper(mockCtrl)
			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion2).Return(mockawsService2, nil)
			mockawsService2.EXPECT().compute().Return(mockaws2, nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds, testVpcID02), nil).AnyTimes()
			mockaws2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
			mockaws2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIds), nil).Times(1)
			mockaws2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
				nil).AnyTimes()
			mockaws2.EXPECT().describeSecurityGroups(gomock.Any()).Return(&ec2.DescribeSecurityGroupsOutput{}, nil).MaxTimes(1)
			mockaws2.EXPECT().getRegion().Return(testRegion2).AnyTimes()

			_ = c.AddProviderAccount(fakeClient, account)

			vmSelector := []v1alpha1.VirtualMachineSelector{
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID01},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
				{
					VpcMatch: &v1alpha1.EntityMatch{MatchID: testVpcID02},
					VMMatch:  []v1alpha1.EntityMatch{},
				},
			}

			selector.Spec.VMSelector = vmSelector
			err := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
			Expect(err).Should(BeNil())
			errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
			Expect(errPolAdd).Should(BeNil())
			err = checkVmPollResult(c, testAccountNamespacedName, append(instanceIds, testVMID01))
			Expect(err).Should(BeNil())

			account.Spec.AWSConfig.Region = account.Spec.AWSConfig.Region[:1]

			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any(), testRegion).Return(mockawsService, nil).Times(1)
			_ = c.AddProviderAccount(fakeClient, account)

			errPolAdd = c.DoInventoryPoll(&testAccountNamespacedName)
			Expect(errPolAdd).Should(BeNil())
			err = checkVmPollResult(c, testAccountNamespacedName, []string{testVMID01})
			Expect(err).Should(BeNil())
		})
	})
})

func getEc2InstanceObject(instanceIDs []string, vpcId string) []*ec2.Instance {
	var ec2Instances []*ec2.Instance
	for _, instanceID := range instanceIDs {
		ec2Instance := &ec2.Instance{
			VpcId:      &vpcId,
			InstanceId: aws.String(instanceID),
			State:      &ec2.InstanceState{Name: aws.String("")},
		}
		ec2Instances = append(ec2Instances, ec2Instance)
	}
	return ec2Instances
}

func createVpcObject(vpcIDs []string) *ec2.DescribeVpcsOutput {
	vpcsOutput := new(ec2.DescribeVpcsOutput)

	for i := range vpcIDs {
		key := "Name"
		value := vpcIDs[i]
		tags := make([]*ec2.Tag, 0)
		tag := &ec2.Tag{Key: &key, Value: &value}
		tags = append(tags, tag)
		cidrBlock := new(ec2.VpcCidrBlockAssociation)
		cidr := "192.1.0.0/24"
		cidrBlock.CidrBlock = &cidr
		cidrBlockAssociationSet := make([]*ec2.VpcCidrBlockAssociation, 0)
		cidrBlockAssociationSet = append(cidrBlockAssociationSet, cidrBlock)

		vpc := &ec2.Vpc{
			VpcId:                   &vpcIDs[i],
			CidrBlockAssociationSet: cidrBlockAssociationSet,
			Tags:                    tags,
		}

		vpcsOutput.Vpcs = append(vpcsOutput.Vpcs, vpc)
	}
	return vpcsOutput
}

func checkAccountAddSuccessCondition(c *awsCloud, namespacedName types.NamespacedName, selectorNamespacedName types.NamespacedName,
	ids []string) error {
	conditionFunc := func() (done bool, e error) {
		accCfg, err := c.cloudCommon.GetCloudAccountByName(&namespacedName)
		if err != nil && strings.Contains(err.Error(), internal.AccountConfigNotFound) {
			return true, err
		}

		instances := accCfg.GetServiceConfig(testRegion).(*ec2ServiceConfig).getCachedInstances(&selectorNamespacedName)
		instanceIds := make([]string, 0, len(instances))
		for _, instance := range instances {
			instanceIds = append(instanceIds, *instance.InstanceId)
		}

		sort.Strings(instanceIds)
		sort.Strings(ids)
		equal := reflect.DeepEqual(instanceIds, ids)
		if equal {
			return true, nil
		}
		return false, nil
	}

	return wait.PollImmediate(1*time.Second, 5*time.Second, conditionFunc)
}

func checkVpcPollResult(c *awsCloud, namespacedName types.NamespacedName, ids []string) error {
	conditionFunc := func() (done bool, e error) {
		accCfg, err := c.cloudCommon.GetCloudAccountByName(&namespacedName)
		if err != nil && strings.Contains(err.Error(), internal.AccountConfigNotFound) {
			return true, err
		}

		var vpcs []*ec2.Vpc
		for _, serviceConfig := range accCfg.GetAllServiceConfigs() {
			vpcs = append(vpcs, serviceConfig.(*ec2ServiceConfig).getCachedVpcs()...)
		}
		vpcIDs := make([]string, 0, len(vpcs))
		for _, vpc := range vpcs {
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("vpc id %s", *vpc.VpcId)))
			vpcIDs = append(vpcIDs, *vpc.VpcId)
		}

		sort.Strings(vpcIDs)
		sort.Strings(ids)
		equal := reflect.DeepEqual(vpcIDs, ids)
		if equal {
			return true, nil
		}
		return false, nil
	}

	return wait.PollImmediate(1*time.Second, 5*time.Second, conditionFunc)
}

func checkVmPollResult(c *awsCloud, namespacedName types.NamespacedName, ids []string) error {
	conditionFunc := func() (done bool, e error) {
		accCfg, err := c.cloudCommon.GetCloudAccountByName(&namespacedName)
		if err != nil {
			return true, fmt.Errorf("failed to find account")
		}

		var vms []*ec2.Instance
		for _, s := range accCfg.GetAllServiceConfigs() {
			serviceConfig := s.(*ec2ServiceConfig)
			for selector := range serviceConfig.selectors {
				vms = append(vms, serviceConfig.getCachedInstances(&selector)...)
			}
		}
		vmIds := make([]string, 0, len(vms))
		for _, vm := range vms {
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("vm id %s", *vm.InstanceId)))
			vmIds = append(vmIds, *vm.InstanceId)
		}

		sort.Strings(vmIds)
		sort.Strings(ids)
		equal := reflect.DeepEqual(vmIds, ids)
		if equal {
			return true, nil
		}
		return false, nil
	}

	return wait.PollImmediate(1*time.Second, 5*time.Second, conditionFunc)
}

func getFilters(c *awsCloud, selectorNamespacedName types.NamespacedName) [][]*ec2.Filter {
	accCfg, err := c.cloudCommon.GetCloudAccountByName(&types.NamespacedName{Namespace: "namespace01",
		Name: "account01"})
	if err != nil && strings.Contains(err.Error(), internal.AccountConfigNotFound) {
		return nil
	}
	if obj, found := accCfg.GetServiceConfig(testRegion).(*ec2ServiceConfig).instanceFilters[selectorNamespacedName]; found {
		return obj
	}
	return nil
}
