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
	"errors"
	"fmt"
	"reflect"
	"sort"
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
)

var (
	testVpcID01 = "vpc-cb82c3b2"
)

var _ = Describe("AWS cloud", func() {
	var (
		testAccountNamespacedName = types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		credentials               = "credentials"
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
						Region: []string{"us-east-1"},
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
						Name:      "selector-all",
						Namespace: testAccountNamespacedName.Namespace,
					},
					Spec: v1alpha1.CloudEntitySelectorSpec{
						AccountName: testAccountNamespacedName.Name,
						VMSelector:  []v1alpha1.VirtualMachineSelector{},
					},
				}

				mockawsService = NewMockawsServiceClientCreateInterface(mockCtrl)
				mockawsEC2 = NewMockawsEC2Wrapper(mockCtrl)

				mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockawsService, nil)
				mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()
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
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
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
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				vpcMap, err := c.GetVpcInventory(&testAccountNamespacedName)
				Expect(err).Should(BeNil())
				Expect(len(vpcMap)).Should(Equal(len(vpcIDs)))
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
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(createVpcObject(vpcIDs), nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)

				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errPolAdd := c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(errPolAdd).Should(BeNil())

				errPolDel := c.ResetInventoryCache(&testAccountNamespacedName)
				Expect(errPolDel).Should(BeNil())

				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).Times(0)
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).Times(0)
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).Times(0)
			})
			It("Should discover few instances with get ALL selector using credentials", func() {
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

				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)

				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errSelAdd := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(errSelAdd).Should(BeNil())

				err = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(err).Should(BeNil())

				err = checkAccountAddSuccessCondition(c, testAccountNamespacedName, instanceIds)
				Expect(err).Should(BeNil())
			})
			It("Should discover few instances with get ALL selector using roleArn", func() {
				instanceIds := []string{"i-01", "i-02"}
				credential := `{"roleArn" : "roleArn","externalID" : "" }`
				secret = &corev1.Secret{
					ObjectMeta: v1.ObjectMeta{
						Name:      testAccountNamespacedName.Name,
						Namespace: testAccountNamespacedName.Namespace,
					},
					Data: map[string][]byte{
						"credentials": []byte(credential),
					},
				}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()

				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)

				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errSelAdd := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(errSelAdd).Should(BeNil())

				err = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(err).Should(BeNil())

				err = checkAccountAddSuccessCondition(c, testAccountNamespacedName, instanceIds)
				Expect(err).Should(BeNil())
			})
			It("Should discover no instances with get ALL selector", func() {
				instanceIds := []string{}
				mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
				mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
				mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{},
					nil).AnyTimes()
				_ = fakeClient.Create(context.Background(), secret)
				c := newAWSCloud(mockawsCloudHelper)
				err := c.AddProviderAccount(fakeClient, account)
				Expect(err).Should(BeNil())
				accCfg, found := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				Expect(found).To(BeTrue())
				Expect(accCfg).To(Not(BeNil()))

				errSelAdd := c.AddAccountResourceSelector(&testAccountNamespacedName, selector)
				Expect(errSelAdd).Should(BeNil())

				err = c.DoInventoryPoll(&testAccountNamespacedName)
				Expect(err).Should(BeNil())

				err = checkAccountAddSuccessCondition(c, testAccountNamespacedName, instanceIds)
				Expect(err).Should(BeNil())
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
						Region: []string{"us-east-1"},
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
					AccountName: testAccountNamespacedName.Name,
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

			mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockawsService, nil).Times(1)
			mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()

			instanceIds := []string{}
			mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
			mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
			mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
			mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).AnyTimes()
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

				accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
				filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
				Expect(filters).To(Equal(expectedFilters))
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
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

			accCfg, _ := c.cloudCommon.GetCloudAccountByName(&testAccountNamespacedName)
			filters := accCfg.GetServiceConfig().(*ec2ServiceConfig).instanceFilters[testSelectorNamespacedName.String()]
			Expect(filters).To(Equal(expectedFilters))
		})
	})
})

func getEc2InstanceObject(instanceIDs []string) []*ec2.Instance {
	var ec2Instances []*ec2.Instance
	for _, instanceID := range instanceIDs {
		ec2Instance := &ec2.Instance{
			VpcId:      &testVpcID01,
			InstanceId: aws.String(instanceID),
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

func checkAccountAddSuccessCondition(c *awsCloud, namespacedName types.NamespacedName, ids []string) error {
	conditionFunc := func() (done bool, e error) {
		accCfg, found := c.cloudCommon.GetCloudAccountByName(&namespacedName)
		if !found {
			return true, errors.New("failed to find account")
		}

		instances := accCfg.GetServiceConfig().(*ec2ServiceConfig).getCachedInstances()
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
		accCfg, found := c.cloudCommon.GetCloudAccountByName(&namespacedName)
		if !found {
			return true, errors.New("failed to find account")
		}

		vpcs := accCfg.GetServiceConfig().(*ec2ServiceConfig).GetCachedVpcs()
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
