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
	"net"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	crdv1alpha1 "antrea.io/nephe/apis/crd/v1alpha1"
	runtimev1alpha1 "antrea.io/nephe/apis/runtime/v1alpha1"
	"antrea.io/nephe/pkg/cloudprovider/cloudresource"
	"antrea.io/nephe/pkg/cloudprovider/utils"
	"antrea.io/nephe/pkg/config"
)

var _ = Describe("AWS Cloud Security", func() {
	var (
		testVMID01 = "i-02d82ffda0fba57b6"
		testVMID02 = "i-0b194935df0d83eb8"

		testAccountNamespacedName = &types.NamespacedName{Namespace: "namespace01", Name: "account01"}
		testAnpNamespacedName     = &types.NamespacedName{Namespace: "test-anp-ns", Name: "test-anp"}
		testEntitySelectorName    = "testEntitySelector01"
		credentials               = "credentials"

		cloudInterface *awsCloud
		account        *crdv1alpha1.CloudProviderAccount
		selector       *crdv1alpha1.CloudEntitySelector
		secret         *corev1.Secret

		mockCtrl           *gomock.Controller
		mockawsCloudHelper *MockawsServicesHelper
		mockawsEC2         *MockawsEC2Wrapper
		mockawsService     *MockawsServiceClientCreateInterface
	)

	BeforeEach(func() {
		var pollIntv uint = 2
		cloudresource.SetCloudResourcePrefix(config.DefaultCloudResourcePrefix)
		cloudresource.SetCloudSecurityGroupVisibility(true)

		account = &crdv1alpha1.CloudProviderAccount{
			ObjectMeta: v1.ObjectMeta{
				Name:      testAccountNamespacedName.Name,
				Namespace: testAccountNamespacedName.Namespace,
			},
			Spec: crdv1alpha1.CloudProviderAccountSpec{
				PollIntervalInSeconds: &pollIntv,
				AWSConfig: &crdv1alpha1.CloudProviderAccountAWSConfig{
					Region: []string{"us-west-2"},
					SecretRef: &crdv1alpha1.SecretReference{
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
		selector = &crdv1alpha1.CloudEntitySelector{
			ObjectMeta: v1.ObjectMeta{
				Name:      testEntitySelectorName,
				Namespace: testAccountNamespacedName.Namespace,
			},
			Spec: crdv1alpha1.CloudEntitySelectorSpec{
				AccountName:      testAccountNamespacedName.Name,
				AccountNamespace: testAccountNamespacedName.Namespace,
				VMSelector: []crdv1alpha1.VirtualMachineSelector{
					{
						VpcMatch: &crdv1alpha1.EntityMatch{
							MatchID: testVpcID01,
						},
					},
				},
			},
		}

		mockCtrl = gomock.NewController(GinkgoT())
		mockawsCloudHelper = NewMockawsServicesHelper(mockCtrl)

		mockawsService = NewMockawsServiceClientCreateInterface(mockCtrl)
		mockawsEC2 = NewMockawsEC2Wrapper(mockCtrl)

		mockawsCloudHelper.EXPECT().newServiceSdkConfigProvider(gomock.Any()).Return(mockawsService, nil).Times(1)
		mockawsService.EXPECT().compute().Return(mockawsEC2, nil).AnyTimes()

		instanceIds := []string{testVMID01, testVMID02}
		mockawsEC2.EXPECT().pagedDescribeInstancesWrapper(gomock.Any()).Return(getEc2InstanceObject(instanceIds), nil).AnyTimes()
		mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
		mockawsEC2.EXPECT().describeVpcsWrapper(gomock.Any()).Return(&ec2.DescribeVpcsOutput{}, nil).AnyTimes()
		mockawsEC2.EXPECT().describeVpcPeeringConnectionsWrapper(gomock.Any()).Return(&ec2.DescribeVpcPeeringConnectionsOutput{}, nil).AnyTimes()
		managedVpcIds := make(map[string]struct{})
		managedVpcIds[testVpcID01] = struct{}{}
		filters := buildAwsEc2FilterForVpcIDOnlyMatches(managedVpcIds)
		input2 := &ec2.DescribeSecurityGroupsInput{
			Filters: filters,
		}
		output := &ec2.DescribeSecurityGroupsOutput{
			NextToken: nil,
			SecurityGroups: []*ec2.SecurityGroup{{
				GroupId:   aws.String(fmt.Sprintf("%v", testSgID)),
				GroupName: aws.String(awsVpcDefaultSecurityGroupName),
			}},
		}
		mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input2)).Return(output, nil).Times(1)

		fakeClient := fake.NewClientBuilder().Build()
		_ = fakeClient.Create(context.Background(), secret)
		cloudInterface = newAWSCloud(mockawsCloudHelper)
		err := cloudInterface.AddProviderAccount(fakeClient, account)
		Expect(err).Should(BeNil())

		err = cloudInterface.AddAccountResourceSelector(testAccountNamespacedName, selector)
		Expect(err).Should(BeNil())

		err = cloudInterface.DoInventoryPoll(testAccountNamespacedName)
		Expect(err).Should(BeNil())

		// wait for instances to be populated
		time.Sleep(time.Duration(pollIntv+1) * time.Second)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("CreateSecurityGroup", func() {
		It("Should create security group successfully and return ID", func() {
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			input1 := constructEc2DescribeSecurityGroupsInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructEc2DescribeSecurityGroupsOutput(
				nil, true, false), nil).Times(1)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructEc2DescribeSecurityGroupsOutput(
				&webAddressGroupIdentifier.CloudResourceID, true, false), nil).Times(1)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(
				constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, true, false), nil).Times(1)

			createOutput := &ec2.CreateSecurityGroupOutput{GroupId: aws.String(fmt.Sprintf("%v", testSgID))}
			testSgID += 1
			mockawsEC2.EXPECT().createSecurityGroup(gomock.Any()).Return(createOutput, nil).Times(1)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Return(nil, nil).Times(1)

			cloudSgID, err := cloudInterface.CreateSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
			Expect(cloudSgID).Should(Not(BeNil()))
		})
		It("Should return pre-created security group successfully and return ID", func() {
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(
				constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, true, false), nil).Times(1)
			mockawsEC2.EXPECT().createSecurityGroup(gomock.Any()).Times(0)

			cloudSgID, err := cloudInterface.CreateSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
			Expect(cloudSgID).Should(Not(BeNil()))
		})
	})
	Context("DeleteSecurityGroup", func() {
		It("Should delete security groups successfully (SG does not exist in cloud)", func() {
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			input := constructEc2DescribeSecurityGroupsInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input)).Return(constructEc2DescribeSecurityGroupsOutput(
				nil, true, false), nil).Times(1)
			mockawsEC2.EXPECT().deleteSecurityGroup(gomock.Any()).Times(0)
			err := cloudInterface.DeleteSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
		})
		It("Should delete security groups successfully (SG exist in cloud)", func() {
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			input1 := constructEc2DescribeSecurityGroupsInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{webAddressGroupIdentifier.GetCloudName(true): {}})
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input1)).Return(constructEc2DescribeSecurityGroupsOutput(
				&webAddressGroupIdentifier.CloudResourceID, true, false), nil).Times(1)

			input2 := constructEc2DescribeSecurityGroupsInput(webAddressGroupIdentifier.Vpc,
				map[string]struct{}{awsVpcDefaultSecurityGroupName: {}})
			output := &ec2.DescribeSecurityGroupsOutput{
				NextToken: nil,
				SecurityGroups: []*ec2.SecurityGroup{{
					GroupId:   aws.String(fmt.Sprintf("%v", testSgID)),
					GroupName: aws.String(awsVpcDefaultSecurityGroupName),
				}},
			}
			testSgID += 1
			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input2)).Return(output, nil).Times(1)

			mockawsEC2.EXPECT().pagedDescribeNetworkInterfaces(gomock.Any()).Return([]*ec2.NetworkInterface{}, nil).AnyTimes()
			mockawsEC2.EXPECT().deleteSecurityGroup(gomock.Any()).Return(&ec2.DeleteSecurityGroupOutput{}, nil).Times(1)
			err := cloudInterface.DeleteSecurityGroup(webAddressGroupIdentifier, true)
			Expect(err).Should(BeNil())
		})
	})
	Context("UpdateSecurityGroupRules", func() {
		It("Should create ingress rules successfully", func() {
			webSgIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{{
				Rule: &cloudresource.IngressRule{
					FromPort: aws.Int(22),
					FromSrcIP: []*net.IPNet{{
						IP:   net.ParseIP("2600:1f16:c77:a001:fb97:21b2:a8dc:dc60"),
						Mask: net.CIDRMask(128, 128)},
					},
					FromSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier.CloudResourceID},
					Protocol:           aws.Int(6),
				}, NpNamespacedName: testAnpNamespacedName.String()},
			}
			output := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupIngressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, addRule, []*cloudresource.CloudRule{})
			Expect(err).Should(BeNil())
		})
		// Ingress rules without a description field is not allowed.
		It("Should fail to create ingress rules", func() {
			webSgIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{{
				Rule: &cloudresource.IngressRule{
					FromPort:           aws.Int(22),
					FromSrcIP:          []*net.IPNet{},
					FromSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier.CloudResourceID},
					Protocol:           aws.Int(6),
				}},
			}
			output := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, addRule, []*cloudresource.CloudRule{})
			Expect(err).ShouldNot(BeNil())
		})
		It("Should create egress rules successfully", func() {
			webSgIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{{
				Rule: &cloudresource.EgressRule{
					ToPort:           aws.Int(22),
					ToDstIP:          []*net.IPNet{},
					ToSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier.CloudResourceID},
					Protocol:         aws.Int(6),
				}, NpNamespacedName: testAnpNamespacedName.String()}}
			output := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupEgressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
				})

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, addRule, []*cloudresource.CloudRule{})
			Expect(err).Should(BeNil())
		})
		// Egress rules without a description field is not allowed.
		It("Should fail to create egress rules", func() {
			webSgIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{{
				Rule: &cloudresource.EgressRule{
					ToPort:           aws.Int(22),
					ToDstIP:          []*net.IPNet{},
					ToSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier.CloudResourceID},
					Protocol:         aws.Int(6),
				}}}
			output := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier.CloudResourceID, false, false)
			output.SecurityGroups = append(output.SecurityGroups, outputAt.SecurityGroups...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier.Vpc,
				map[string]struct{}{webSgIdentifier.GetCloudName(true): {}, webSgIdentifier.GetCloudName(false): {}})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(output, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier, addRule, []*cloudresource.CloudRule{})
			Expect(err).ShouldNot(BeNil())
		})
		It("Should skip update ingress rules that already exist in cloud", func() {
			webSgIdentifier1 := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			webSgIdentifier2 := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "App",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{
				{
					Rule: &cloudresource.IngressRule{
						FromPort: aws.Int(22),
						FromSrcIP: []*net.IPNet{{
							IP:   net.ParseIP("2600:1f16:c77:a001:fb97:21b2:a8dc:dc60"),
							Mask: net.CIDRMask(128, 128)},
						},
						Protocol: aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.IngressRule{
						FromPort: aws.Int(22),
						FromSrcIP: []*net.IPNet{{
							IP:   net.ParseIP("2600:1f16:c77:a001:fb97:21b2:a8dc:dc61"),
							Mask: net.CIDRMask(128, 128)},
						},
						Protocol: aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.IngressRule{
						FromPort:           aws.Int(22),
						FromSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier1.CloudResourceID},
						Protocol:           aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.IngressRule{
						FromPort:           aws.Int(22),
						FromSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier2.CloudResourceID},
						Protocol:           aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				},
			}
			output1 := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier1.CloudResourceID, true, false)
			output2 := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier2.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier1.CloudResourceID, false, false)
			desc, _ := utils.GenerateCloudDescription(testAnpNamespacedName.String())
			outputAt.SecurityGroups[0].IpPermissions = []*ec2.IpPermission{
				{
					FromPort:   aws.Int64(22),
					IpProtocol: aws.String("tcp"),
					Ipv6Ranges: []*ec2.Ipv6Range{
						{CidrIpv6: aws.String("2600:1f16:c77:a001:fb97:21b2:a8dc:dc61/128"), Description: aws.String(desc)},
						{CidrIpv6: aws.String("2600:1f16:c77:a001:fb97:21b2:a8dc:dc60/128"), Description: aws.String(desc)},
					},
					ToPort: aws.Int64(22),
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						{Description: aws.String(desc), GroupId: output1.SecurityGroups[0].GroupId},
					},
				},
			}
			outputAt.SecurityGroups = append(outputAt.SecurityGroups, append(output1.SecurityGroups, output2.SecurityGroups...)...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier1.Vpc, map[string]struct{}{
				webSgIdentifier1.GetCloudName(true):  {},
				webSgIdentifier2.GetCloudName(true):  {},
				webSgIdentifier1.GetCloudName(false): {},
			})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(outputAt, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupIngressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
					Expect(*req.IpPermissions[0]).To(Equal(ec2.IpPermission{
						FromPort:   aws.Int64(22),
						IpProtocol: aws.String("6"),
						ToPort:     aws.Int64(22),
						UserIdGroupPairs: []*ec2.UserIdGroupPair{
							{Description: aws.String(desc), GroupId: output2.SecurityGroups[0].GroupId},
						},
					}))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(0)

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier1, addRule, []*cloudresource.CloudRule{})
			Expect(err).Should(BeNil())
		})
		It("Should skip update egress rules that already exist in cloud", func() {
			webSgIdentifier1 := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			webSgIdentifier2 := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "App",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}
			addRule := []*cloudresource.CloudRule{
				{
					Rule: &cloudresource.EgressRule{
						ToPort: aws.Int(22),
						ToDstIP: []*net.IPNet{{
							IP:   net.ParseIP("2600:1f16:c77:a001:fb97:21b2:a8dc:dc60"),
							Mask: net.CIDRMask(128, 128)},
						},
						Protocol: aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.EgressRule{
						ToPort: aws.Int(22),
						ToDstIP: []*net.IPNet{{
							IP:   net.ParseIP("2600:1f16:c77:a001:fb97:21b2:a8dc:dc61"),
							Mask: net.CIDRMask(128, 128)},
						},
						Protocol: aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.EgressRule{
						ToPort:           aws.Int(22),
						ToSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier1.CloudResourceID},
						Protocol:         aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				}, {
					Rule: &cloudresource.EgressRule{
						ToPort:           aws.Int(22),
						ToSecurityGroups: []*cloudresource.CloudResourceID{&webSgIdentifier2.CloudResourceID},
						Protocol:         aws.Int(6),
					}, NpNamespacedName: testAnpNamespacedName.String(),
				},
			}
			output1 := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier1.CloudResourceID, true, false)
			output2 := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier2.CloudResourceID, true, false)
			outputAt := constructEc2DescribeSecurityGroupsOutput(&webSgIdentifier1.CloudResourceID, false, false)
			desc, _ := utils.GenerateCloudDescription(testAnpNamespacedName.String())
			outputAt.SecurityGroups[0].IpPermissionsEgress = []*ec2.IpPermission{
				{
					FromPort:   aws.Int64(22),
					IpProtocol: aws.String("tcp"),
					Ipv6Ranges: []*ec2.Ipv6Range{
						{CidrIpv6: aws.String("2600:1f16:c77:a001:fb97:21b2:a8dc:dc61/128"), Description: aws.String(desc)},
						{CidrIpv6: aws.String("2600:1f16:c77:a001:fb97:21b2:a8dc:dc60/128"), Description: aws.String(desc)},
					},
					ToPort: aws.Int64(22),
					UserIdGroupPairs: []*ec2.UserIdGroupPair{
						{Description: aws.String(desc), GroupId: output1.SecurityGroups[0].GroupId},
					},
				},
			}
			outputAt.SecurityGroups = append(outputAt.SecurityGroups, append(output1.SecurityGroups, output2.SecurityGroups...)...)
			input := constructEc2DescribeSecurityGroupsInput(webSgIdentifier1.Vpc, map[string]struct{}{
				webSgIdentifier1.GetCloudName(true):  {},
				webSgIdentifier2.GetCloudName(true):  {},
				webSgIdentifier1.GetCloudName(false): {},
			})

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Any()).Return(outputAt, nil).Times(1).
				Do(func(req *ec2.DescribeSecurityGroupsInput) {
					sortSliceStringPointer(req.Filters[1].Values)
					sortSliceStringPointer(input.Filters[1].Values)
					Expect(req).To(Equal(input))
				})
			mockawsEC2.EXPECT().revokeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupIngress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().revokeSecurityGroupEgress(gomock.Any()).Times(0)
			mockawsEC2.EXPECT().authorizeSecurityGroupEgress(gomock.Any()).Times(1).
				Do(func(req *ec2.AuthorizeSecurityGroupEgressInput) {
					Expect(len(req.IpPermissions)).To(Equal(1))
					Expect(*req.IpPermissions[0]).To(Equal(ec2.IpPermission{
						FromPort:   aws.Int64(22),
						IpProtocol: aws.String("6"),
						ToPort:     aws.Int64(22),
						UserIdGroupPairs: []*ec2.UserIdGroupPair{
							{Description: aws.String(desc), GroupId: output2.SecurityGroups[0].GroupId},
						},
					}))
				})

			err := cloudInterface.UpdateSecurityGroupRules(webSgIdentifier1, addRule, []*cloudresource.CloudRule{})
			Expect(err).Should(BeNil())
		})
	})

	Context("GetEnforcedSecurity", func() {
		It("Should sync cloud security groups and rules with description", func() {
			desc := cloudresource.CloudRuleDescription{
				Name:      testAnpNamespacedName.Name,
				Namespace: testAnpNamespacedName.Namespace,
			}
			descString := desc.String()
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}

			input := &ec2.DescribeSecurityGroupsInput{
				Filters: []*ec2.Filter{{
					Name:   aws.String(awsFilterKeyVPCID),
					Values: []*string{aws.String(testVpcID01)},
				}},
			}
			agOutput := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, true, false)
			irule := &ec2.IpPermission{
				FromPort:   aws.Int64(22),
				IpProtocol: aws.String("tcp"),
				IpRanges: []*ec2.IpRange{{CidrIp: aws.String("1.1.1.1/32"), Description: &descString},
					{CidrIp: aws.String("2.2.2.2/32"), Description: &descString}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(22),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId, Description: &descString}},
			}
			erule := &ec2.IpPermission{
				FromPort:   aws.Int64(80),
				IpProtocol: aws.String("tcp"),
				IpRanges: []*ec2.IpRange{{CidrIp: aws.String("2.2.2.2/32"), Description: &descString},
					{CidrIp: aws.String("1.1.1.1/32"), Description: &descString}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(80),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId, Description: &descString}},
			}
			output := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, false, false)
			for _, sg := range output.SecurityGroups {
				sg.IpPermissions = append(sg.IpPermissions, irule)
				sg.IpPermissionsEgress = append(sg.IpPermissionsEgress, erule)
			}
			output.SecurityGroups = append(output.SecurityGroups, agOutput.SecurityGroups...)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input)).Return(output, nil).Times(1)

			syncContent := cloudInterface.GetEnforcedSecurity()
			Expect(len(syncContent)).To(Equal(2))
			for _, c := range syncContent {
				if !c.MembershipOnly {
					Expect(len(c.IngressRules)).To(Equal(3))
					Expect(len(c.EgressRules)).To(Equal(3))
				}
			}
		})
		It("Should sync cloud security groups and user rules with an invalid description", func() {
			// Description does not contain an ATGroup name.
			desc := cloudresource.CloudRuleDescription{Name: testAnpNamespacedName.Name, Namespace: testAnpNamespacedName.Namespace}
			descString := desc.String()
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}

			input := &ec2.DescribeSecurityGroupsInput{
				Filters: []*ec2.Filter{{
					Name:   aws.String(awsFilterKeyVPCID),
					Values: []*string{aws.String(testVpcID01)},
				}},
			}
			agOutput := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, true, false)
			irule := &ec2.IpPermission{
				FromPort:   aws.Int64(22),
				IpProtocol: aws.String("tcp"),
				IpRanges: []*ec2.IpRange{{CidrIp: aws.String("1.1.1.1/32"), Description: &descString},
					{CidrIp: aws.String("2.2.2.2/32"), Description: &descString}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(22),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId, Description: &descString}},
			}
			expectedIRuleLength := len(irule.IpRanges) + len(irule.UserIdGroupPairs)
			erule := &ec2.IpPermission{
				FromPort:   aws.Int64(80),
				IpProtocol: aws.String("tcp"),
				IpRanges: []*ec2.IpRange{{CidrIp: aws.String("2.2.2.2/32"), Description: &descString},
					{CidrIp: aws.String("1.1.1.1/32"), Description: &descString}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(80),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId, Description: &descString}},
			}
			expectedERuleLength := len(erule.IpRanges) + len(erule.UserIdGroupPairs)
			output := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, false, false)
			for _, sg := range output.SecurityGroups {
				sg.IpPermissions = append(sg.IpPermissions, irule)
				sg.IpPermissionsEgress = append(sg.IpPermissionsEgress, erule)
			}
			output.SecurityGroups = append(output.SecurityGroups, agOutput.SecurityGroups...)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input)).Return(output, nil).Times(1)

			syncContent := cloudInterface.GetEnforcedSecurity()
			Expect(len(syncContent)).To(Equal(2))
			for _, c := range syncContent {
				if !c.MembershipOnly {
					Expect(len(c.IngressRules)).To(Equal(expectedIRuleLength))
					Expect(len(c.EgressRules)).To(Equal(expectedERuleLength))
				}
			}
		})
		It("Should sync cloud security groups and user rules without description", func() {
			webAddressGroupIdentifier := &cloudresource.CloudResource{
				Type: cloudresource.CloudResourceTypeVM,
				CloudResourceID: cloudresource.CloudResourceID{
					Name: "Web",
					Vpc:  testVpcID01,
				},
				AccountID:     testAccountNamespacedName.String(),
				CloudProvider: string(runtimev1alpha1.AWSCloudProvider),
			}

			input := &ec2.DescribeSecurityGroupsInput{
				Filters: []*ec2.Filter{{
					Name:   aws.String(awsFilterKeyVPCID),
					Values: []*string{aws.String(testVpcID01)},
				}},
			}
			agOutput := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, true, false)
			irule := &ec2.IpPermission{
				FromPort:         aws.Int64(22),
				IpProtocol:       aws.String("tcp"),
				IpRanges:         []*ec2.IpRange{{CidrIp: aws.String("1.1.1.1/32")}, {CidrIp: aws.String("2.2.2.2/32")}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(22),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId}},
			}
			expectedIRuleLength := len(irule.IpRanges) + len(irule.UserIdGroupPairs)
			erule := &ec2.IpPermission{
				FromPort:         aws.Int64(80),
				IpProtocol:       aws.String("tcp"),
				IpRanges:         []*ec2.IpRange{{CidrIp: aws.String("2.2.2.2/32")}, {CidrIp: aws.String("1.1.1.1/32")}},
				Ipv6Ranges:       []*ec2.Ipv6Range{},
				PrefixListIds:    []*ec2.PrefixListId{},
				ToPort:           aws.Int64(80),
				UserIdGroupPairs: []*ec2.UserIdGroupPair{{GroupId: agOutput.SecurityGroups[0].GroupId}},
			}
			expectedERuleLength := len(erule.IpRanges) + len(erule.UserIdGroupPairs)
			output := constructEc2DescribeSecurityGroupsOutput(&webAddressGroupIdentifier.CloudResourceID, false, false)
			for _, sg := range output.SecurityGroups {
				sg.IpPermissions = append(sg.IpPermissions, irule)
				sg.IpPermissionsEgress = append(sg.IpPermissionsEgress, erule)
			}
			output.SecurityGroups = append(output.SecurityGroups, agOutput.SecurityGroups...)

			mockawsEC2.EXPECT().describeSecurityGroups(gomock.Eq(input)).Return(output, nil).Times(1)

			syncContent := cloudInterface.GetEnforcedSecurity()
			Expect(len(syncContent)).To(Equal(2))
			for _, c := range syncContent {
				if !c.MembershipOnly {
					Expect(len(c.IngressRules)).To(Equal(expectedIRuleLength))
					Expect(len(c.EgressRules)).To(Equal(expectedERuleLength))
				}
			}
		})
	})
})

func constructEc2DescribeSecurityGroupsInput(vpcID string, sgNamesSet map[string]struct{}) *ec2.DescribeSecurityGroupsInput {
	vpcIDs := []string{vpcID}
	filters := buildAwsEc2FilterForSecurityGroupNameMatches(vpcIDs, sgNamesSet)
	input := &ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	}
	return input
}

// nolint: unparam
func constructEc2DescribeSecurityGroupsOutput(identifier *cloudresource.CloudResourceID, membershipOnly bool,
	isDefaultSg bool) *ec2.DescribeSecurityGroupsOutput {
	var securityGroups []*ec2.SecurityGroup
	if isDefaultSg {
		securityGroup := &ec2.SecurityGroup{
			GroupId:   aws.String(fmt.Sprintf("%v", testSgID)),
			GroupName: aws.String(awsVpcDefaultSecurityGroupName),
		}
		securityGroups = append(securityGroups, securityGroup)
		testSgID += 1
	} else {
		if identifier != nil {
			securityGroup := &ec2.SecurityGroup{
				GroupId:   aws.String(fmt.Sprintf("%v", testSgID)),
				GroupName: aws.String(identifier.GetCloudName(membershipOnly)),
				VpcId:     aws.String(testVpcID01),
			}
			securityGroups = append(securityGroups, securityGroup)
			testSgID += 1
		}
	}

	output := &ec2.DescribeSecurityGroupsOutput{
		NextToken:      nil,
		SecurityGroups: securityGroups,
	}
	return output
}

func sortSliceStringPointer(s []*string) {
	sort.Slice(s, func(i, j int) bool { return *s[i] < *s[j] })
}
